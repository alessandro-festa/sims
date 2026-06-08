// Package helm wraps helm.sh/helm/v3/pkg/action for the install/upgrade/uninstall
// operations the sims CLI uses. It accepts kubeconfig bytes (typically from
// pkg/cluster.KubeConfig), writes them to a temp file for Helm's RESTClientGetter,
// and configures the OCI registry client so OCI chart refs (oci://...) work.
package helm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/registry"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

// Client installs, upgrades, and uninstalls Helm releases in a single
// namespace against a single kubeconfig. Construct with New and call Close
// to remove the temp kubeconfig file when done.
type Client struct {
	kubeconfigPath string
	namespace      string
	settings       *cli.EnvSettings
	registry       *registry.Client
	logger         *slog.Logger
}

// Option configures a Client at construction time.
type Option func(*Client)

// WithLogger forwards Helm's debug messages to the given slog.Logger at Debug level.
// If unset, slog.Default() is used.
func WithLogger(l *slog.Logger) Option {
	return func(c *Client) {
		if l != nil {
			c.logger = l
		}
	}
}

// New returns a Client configured to talk to the cluster described by
// kubeconfig and to operate on releases in the given namespace.
//
// The kubeconfig bytes are written to a temp file because Helm's
// RESTClientGetter (genericclioptions.ConfigFlags) loads from a path. Call
// Close to remove the temp file.
func New(kubeconfig []byte, namespace string, opts ...Option) (*Client, error) {
	if len(kubeconfig) == 0 {
		return nil, errors.New("helm.New: empty kubeconfig")
	}
	if namespace == "" {
		return nil, errors.New("helm.New: empty namespace")
	}

	f, err := os.CreateTemp("", "sims-kubeconfig-*.yaml")
	if err != nil {
		return nil, fmt.Errorf("create temp kubeconfig: %w", err)
	}
	path := f.Name()
	if _, err := f.Write(kubeconfig); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("write temp kubeconfig: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("close temp kubeconfig: %w", err)
	}

	settings := cli.New()
	settings.KubeConfig = path
	settings.SetNamespace(namespace)

	rc, err := registry.NewClient()
	if err != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("helm registry client: %w", err)
	}

	c := &Client{
		kubeconfigPath: path,
		namespace:      namespace,
		settings:       settings,
		registry:       rc,
		logger:         slog.Default(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Close removes the temp kubeconfig file. Safe to call multiple times.
func (c *Client) Close() error {
	if c.kubeconfigPath == "" {
		return nil
	}
	err := os.Remove(c.kubeconfigPath)
	c.kubeconfigPath = ""
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// InstallOption tunes a single Install call.
type InstallOption func(*action.Install)

// WithoutCreateNamespace tells Install to skip Helm's `--create-namespace`
// step. Use this when the chart provides its own Namespace template (rendering
// such a chart with CreateNamespace=true causes an "already exists" conflict).
func WithoutCreateNamespace() InstallOption {
	return func(i *action.Install) { i.CreateNamespace = false }
}

// Install installs a release. chartRef may be a local path to a chart
// directory or .tgz, or an OCI URL (oci://...). The release's namespace is
// auto-created if missing unless WithoutCreateNamespace is passed.
func (c *Client) Install(ctx context.Context, release, chartRef string, values map[string]any, opts ...InstallOption) error {
	cfg, err := c.actionConfig()
	if err != nil {
		return err
	}
	install := action.NewInstall(cfg)
	install.ReleaseName = release
	install.Namespace = c.namespace
	install.CreateNamespace = true
	for _, opt := range opts {
		opt(install)
	}

	chart, err := c.locateAndLoad(chartRef, &install.ChartPathOptions)
	if err != nil {
		return err
	}
	if _, err := install.RunWithContext(ctx, chart, values); err != nil {
		return fmt.Errorf("helm install %q: %w", release, err)
	}
	return nil
}

// Upgrade upgrades an existing release. chartRef rules match Install.
func (c *Client) Upgrade(ctx context.Context, release, chartRef string, values map[string]any) error {
	cfg, err := c.actionConfig()
	if err != nil {
		return err
	}
	upgrade := action.NewUpgrade(cfg)
	upgrade.Namespace = c.namespace

	chart, err := c.locateAndLoad(chartRef, &upgrade.ChartPathOptions)
	if err != nil {
		return err
	}
	if _, err := upgrade.RunWithContext(ctx, release, chart, values); err != nil {
		return fmt.Errorf("helm upgrade %q: %w", release, err)
	}
	return nil
}

// Uninstall removes a release. Returns nil if the release is already gone.
func (c *Client) Uninstall(release string) error {
	cfg, err := c.actionConfig()
	if err != nil {
		return err
	}
	un := action.NewUninstall(cfg)
	if _, err := un.Run(release); err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil
		}
		return fmt.Errorf("helm uninstall %q: %w", release, err)
	}
	return nil
}

func (c *Client) actionConfig() (*action.Configuration, error) {
	flags := genericclioptions.NewConfigFlags(false)
	*flags.KubeConfig = c.kubeconfigPath
	*flags.Namespace = c.namespace
	cfg := new(action.Configuration)
	debugf := func(format string, v ...any) {
		c.logger.Debug(fmt.Sprintf(format, v...))
	}
	driver := os.Getenv("HELM_DRIVER")
	if err := cfg.Init(flags, c.namespace, driver, debugf); err != nil {
		return nil, fmt.Errorf("init helm action config: %w", err)
	}
	cfg.RegistryClient = c.registry
	return cfg, nil
}

// locateAndLoad resolves chartRef (local path or OCI URL) and loads the chart.
// Uses Helm's own ChartPathOptions so OCI fetching is handled identically to
// the helm CLI.
func (c *Client) locateAndLoad(chartRef string, opts *action.ChartPathOptions) (*chart.Chart, error) {
	path, err := opts.LocateChart(chartRef, c.settings)
	if err != nil {
		return nil, fmt.Errorf("locate chart %q: %w", chartRef, err)
	}
	ch, err := loader.Load(path)
	if err != nil {
		return nil, fmt.Errorf("load chart %q (from %s): %w", chartRef, path, err)
	}
	return ch, nil
}
