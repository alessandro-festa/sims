package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/spf13/cobra"

	"github.com/alessandro-festa/sims/pkg/cluster"
	"github.com/alessandro-festa/sims/pkg/helm"
	"github.com/alessandro-festa/sims/pkg/kube"
)

func newGPUMonitoringCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "monitoring",
		Short: "Enable or disable monitoring on an existing cluster",
	}
	cmd.AddCommand(
		newMonitoringEnableCmd(),
		newMonitoringDisableCmd(),
	)
	return cmd
}

func newMonitoringEnableCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "enable",
		Short: "Install kube-prometheus-stack + vendor dashboard",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMonitoringEnable(cmd.Context(), cmd.OutOrStdout(), name)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Cluster name (default: the only sims-* cluster on this host)")
	return cmd
}

func newMonitoringDisableCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "disable",
		Short: "Remove the monitoring release and namespace",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMonitoringDisable(cmd.Context(), cmd.OutOrStdout(), name)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Cluster name (default: the only sims-* cluster on this host)")
	return cmd
}

func runMonitoringEnable(ctx context.Context, stdout io.Writer, name string) error {
	log := newStderrLogger()
	provider := cluster.New(log)

	resolved, kc, err := resolveClusterAndKubeconfig(ctx, provider, name)
	if err != nil {
		return err
	}

	vendor, err := kube.DetectVendor(ctx, kc)
	if err != nil {
		return err
	}

	log.Info("enabling monitoring", "cluster", resolved, "vendor", vendor)
	if err := installMonitoring(ctx, log, kc, vendor); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stdout,
		"monitoring enabled on cluster %q\nkubectl -n %s port-forward svc/%s-grafana 3000:80\n",
		resolved, monitoringNamespace, monitoringRelease)
	return nil
}

func runMonitoringDisable(ctx context.Context, stdout io.Writer, name string) error {
	log := newStderrLogger()
	provider := cluster.New(log)

	resolved, kc, err := resolveClusterAndKubeconfig(ctx, provider, name)
	if err != nil {
		return err
	}

	log.Info("uninstalling monitoring release", "release", monitoringRelease, "namespace", monitoringNamespace)
	hc, err := helm.New(kc, monitoringNamespace, helm.WithLogger(log))
	if err != nil {
		return err
	}
	defer func() { _ = hc.Close() }()
	if err := hc.Uninstall(monitoringRelease); err != nil {
		log.Warn("helm uninstall failed (continuing to namespace delete)", "err", err)
	}

	log.Info("deleting monitoring namespace", "namespace", monitoringNamespace)
	if err := kube.DeleteNamespace(ctx, kc, monitoringNamespace); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stdout, "monitoring disabled on cluster %q (namespace %s scheduled for deletion)\n",
		resolved, monitoringNamespace)
	return nil
}

// resolveClusterAndKubeconfig returns the cluster name (resolved from --name
// or the single sims-* cluster on the host) and its kubeconfig bytes. Errors
// if no cluster matches or multiple do without --name.
func resolveClusterAndKubeconfig(ctx context.Context, provider *cluster.Provider, name string) (string, []byte, error) {
	all, err := provider.List(ctx)
	if err != nil {
		return "", nil, err
	}
	if name == "" {
		sims := filterSimsClusters(all)
		switch len(sims) {
		case 0:
			return "", nil, errors.New("no sims-managed cluster found on this host")
		case 1:
			name = sims[0]
		default:
			return "", nil, fmt.Errorf("multiple sims clusters found %v; pass --name", sims)
		}
	}
	if !slices.Contains(all, name) {
		return "", nil, fmt.Errorf("cluster %q not found", name)
	}
	kc, err := provider.KubeConfig(ctx, name)
	if err != nil {
		return "", nil, err
	}
	return name, kc, nil
}
