package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/alessandro-festa/sims/pkg/cluster"
	"github.com/alessandro-festa/sims/pkg/config"
	"github.com/alessandro-festa/sims/pkg/helm"
	"github.com/alessandro-festa/sims/pkg/kube"
)

const (
	chartRelease       = "sims-nvidia"
	chartNamespace     = "gpu-operator"
	defaultChartDir    = "charts"
	chartDirEnvVar     = "SIMS_CHART_DIR"
	gpuResourceNVIDIA  = "nvidia.com/gpu"
	capacityWaitWindow = 2 * time.Minute
)

type createOpts struct {
	vendor         string
	name           string
	workers        int
	gpusPerWorker  int
	k8sVersion     string
	withMonitoring bool
	taint          bool
}

func newGPUCreateCmd() *cobra.Command {
	var o createOpts
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a kind cluster simulating GPUs of the chosen vendor",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCreate(cmd.Context(), cmd.OutOrStdout(), &o)
		},
	}
	cmd.Flags().StringVar(&o.vendor, "vendor", "", "GPU vendor to simulate (nvidia|amd)")
	cmd.Flags().StringVar(&o.name, "name", "", "Cluster name (default: sims-<vendor>)")
	cmd.Flags().IntVar(&o.workers, "workers", 2, "Number of worker nodes")
	cmd.Flags().IntVar(&o.gpusPerWorker, "gpus-per-worker", 2, "Fake GPUs advertised per worker")
	cmd.Flags().StringVar(&o.k8sVersion, "k8s-version", "v1.31.0", "Kubernetes version for kind nodes")
	cmd.Flags().BoolVar(&o.withMonitoring, "monitoring", false, "Install kube-prometheus-stack + vendor dashboard")
	cmd.Flags().BoolVar(&o.taint, "taint", false, "Add <vendor>.com/gpu=present:NoSchedule taint to worker nodes")
	_ = cmd.MarkFlagRequired("vendor")
	return cmd
}

func runCreate(ctx context.Context, stdout io.Writer, o *createOpts) error {
	if err := validateVendor(o.vendor); err != nil {
		return err
	}
	if o.vendor == config.VendorAMD {
		return errors.New("--vendor amd not yet supported (Phase 3+); only nvidia is wired in Phase 1")
	}
	if o.withMonitoring {
		return errors.New("--monitoring not yet supported (Phase 2)")
	}

	log := newStderrLogger()
	name := o.name
	if name == "" {
		name = "sims-" + o.vendor
	}

	log.Info("ensuring local image registry",
		"name", cluster.DefaultRegistryName, "port", cluster.DefaultRegistryPort)
	if err := cluster.EnsureRegistry(ctx); err != nil {
		return err
	}

	raw, err := config.Render(config.Options{
		Vendor:     o.vendor,
		Name:       name,
		Workers:    o.workers,
		K8sVersion: o.k8sVersion,
		Taint:      o.taint,
	})
	if err != nil {
		return err
	}

	log.Info("creating kind cluster", "name", name, "workers", o.workers, "k8s", o.k8sVersion)
	provider := cluster.New(log)
	if err := provider.Create(ctx, name, raw); err != nil {
		return err
	}

	log.Info("attaching registry to kind network")
	if err := cluster.ConnectRegistryToKindNetwork(ctx); err != nil {
		return err
	}

	kc, err := provider.KubeConfig(ctx, name)
	if err != nil {
		return err
	}

	log.Info("ensuring chart namespace with PSA labels", "namespace", chartNamespace)
	if err := kube.EnsureNamespace(ctx, kc, chartNamespace, psaPrivilegedLabels()); err != nil {
		return err
	}

	hc, err := helm.New(kc, chartNamespace, helm.WithLogger(log))
	if err != nil {
		return err
	}
	defer func() { _ = hc.Close() }()

	chartDir := resolveChartDir(chartRelease)
	log.Info("installing chart", "chart", chartDir, "release", chartRelease, "namespace", chartNamespace)
	if err := hc.EnsureDependencies(ctx, chartDir); err != nil {
		return err
	}
	if err := hc.Install(ctx, chartRelease, chartDir, buildNVIDIAValues(o.gpusPerWorker), helm.WithoutCreateNamespace()); err != nil {
		return err
	}

	log.Info("waiting for GPU capacity on workers",
		"resource", gpuResourceNVIDIA, "per-worker", o.gpusPerWorker, "workers", o.workers)
	wait, cancel := context.WithTimeout(ctx, capacityWaitWindow)
	defer cancel()
	if err := kube.WaitForResourceCapacity(wait, kc, gpuResourceNVIDIA, o.gpusPerWorker, o.workers); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stdout,
		"cluster %q ready — %d workers × %d %s\nkubeconfig context: kind-%s\n",
		name, o.workers, o.gpusPerWorker, gpuResourceNVIDIA, name)
	return nil
}

func validateVendor(v string) error {
	switch v {
	case config.VendorNVIDIA, config.VendorAMD:
		return nil
	default:
		return fmt.Errorf("invalid --vendor %q (must be %q or %q)", v, config.VendorNVIDIA, config.VendorAMD)
	}
}

func newStderrLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

func resolveChartDir(name string) string {
	if v := os.Getenv(chartDirEnvVar); v != "" {
		return filepath.Join(v, name)
	}
	return filepath.Join(defaultChartDir, name)
}

// psaPrivilegedLabels returns the Pod Security Admission labels needed for
// fake-gpu-operator's device-plugin DaemonSet, which mounts the kubelet
// device-plugin socket via hostPath (blocked by the default baseline profile).
// Also applies the standard sims/managed-by labels for namespace ownership.
func psaPrivilegedLabels() map[string]string {
	return map[string]string{
		"pod-security.kubernetes.io/enforce": "privileged",
		"pod-security.kubernetes.io/audit":   "privileged",
		"pod-security.kubernetes.io/warn":    "privileged",
		"app.kubernetes.io/managed-by":       "sims",
		"app.kubernetes.io/part-of":          "sims",
	}
}

func buildNVIDIAValues(gpusPerWorker int) map[string]any {
	return map[string]any{
		"gpusPerNode": gpusPerWorker,
		"fake-gpu-operator": map[string]any{
			"topology": map[string]any{
				"nodePools": map[string]any{
					"default": map[string]any{
						"gpuCount": gpusPerWorker,
					},
				},
			},
		},
	}
}
