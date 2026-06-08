package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/alessandro-festa/sims/pkg/cluster"
	"github.com/alessandro-festa/sims/pkg/helm"
)

// simsClusterPrefix matches pkg/cluster.simsPrefix; clusters with this
// prefix are eligible for auto-discovery when --name is omitted.
const simsClusterPrefix = "sims-"

// knownReleases is the set of Helm releases sims may have installed in any
// cluster, paired with their namespaces. Uninstall is best-effort — missing
// releases (e.g. sims-amd in a Phase 1 NVIDIA-only cluster) are ignored.
var knownReleases = []struct{ release, namespace string }{
	{"sims-nvidia", "gpu-operator"},
	{"sims-amd", "gpu-operator"},
	{"sims-monitoring", "monitoring"},
}

func newGPUDeleteCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a sims-managed kind cluster",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDelete(cmd.Context(), cmd.OutOrStdout(), name)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Cluster name (default: the only sims-* cluster on this host; errors if multiple)")
	return cmd
}

func runDelete(ctx context.Context, stdout io.Writer, name string) error {
	log := newStderrLogger()
	provider := cluster.New(log)

	all, err := provider.List(ctx)
	if err != nil {
		return err
	}

	if name == "" {
		sims := filterSimsClusters(all)
		switch len(sims) {
		case 0:
			log.Warn("no sims-managed cluster found; nothing to delete")
			return nil
		case 1:
			name = sims[0]
		default:
			return fmt.Errorf("multiple sims clusters found %v; pass --name", sims)
		}
	}

	if !slices.Contains(all, name) {
		log.Warn("cluster not found; nothing to delete (idempotent)", "name", name)
		return nil
	}

	// Best-effort helm uninstall so chart pre-delete hooks fire before kind
	// rips the API server out from under them. Failures here are non-fatal;
	// the cluster deletion that follows reclaims everything either way.
	if kc, err := provider.KubeConfig(ctx, name); err == nil {
		log.Info("uninstalling known sims releases (best-effort)")
		uninstallKnownReleases(log, kc)
	} else {
		log.Warn("could not fetch kubeconfig; skipping helm uninstall", "err", err)
	}

	log.Info("deleting kind cluster", "name", name)
	if err := provider.Delete(ctx, name); err != nil {
		return err
	}

	log.Info("checking if local registry can be stopped")
	if err := cluster.MaybeStopRegistry(ctx); err != nil {
		log.Warn("registry stop failed (cluster delete still succeeded)", "err", err)
	}

	_, _ = fmt.Fprintf(stdout, "cluster %q deleted; kubeconfig context kind-%s removed\n", name, name)
	return nil
}

func uninstallKnownReleases(log *slog.Logger, kubeconfig []byte) {
	for _, r := range knownReleases {
		hc, err := helm.New(kubeconfig, r.namespace, helm.WithLogger(log))
		if err != nil {
			log.Warn("helm client init failed; skipping", "release", r.release, "namespace", r.namespace, "err", err)
			continue
		}
		if err := hc.Uninstall(r.release); err != nil {
			log.Warn("helm uninstall failed (continuing)", "release", r.release, "namespace", r.namespace, "err", err)
		}
		_ = hc.Close()
	}
}

func filterSimsClusters(names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		if strings.HasPrefix(n, simsClusterPrefix) {
			out = append(out, n)
		}
	}
	return out
}
