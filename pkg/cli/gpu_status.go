package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/alessandro-festa/sims/pkg/cluster"
	"github.com/alessandro-festa/sims/pkg/kube"
)

func newGPUStatusCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show cluster state, advertised GPUs, and dashboard URL",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStatus(cmd.Context(), cmd.OutOrStdout(), name)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Cluster name (default: the only sims-* cluster on this host)")
	return cmd
}

func runStatus(ctx context.Context, stdout io.Writer, name string) error {
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
			return errors.New("no sims-managed cluster found on this host")
		case 1:
			name = sims[0]
		default:
			return fmt.Errorf("multiple sims clusters found %v; pass --name", sims)
		}
	}
	if !slices.Contains(all, name) {
		return fmt.Errorf("cluster %q not found", name)
	}

	kc, err := provider.KubeConfig(ctx, name)
	if err != nil {
		return err
	}
	snap, err := kube.Snapshot(ctx, kc)
	if err != nil {
		return err
	}

	return writeStatus(stdout, name, snap)
}

func writeStatus(out io.Writer, clusterName string, snap *kube.ClusterSnapshot) error {
	vendor := snap.Vendor
	if vendor == "" {
		vendor = "<unknown>"
	}
	if _, err := fmt.Fprintf(out, "Cluster:   %s\nVendor:    %s\nK8s:       %s\n",
		clusterName, vendor, snap.K8sVersion); err != nil {
		return err
	}

	if _, err := fmt.Fprintln(out, "\nNODES"); err != nil {
		return err
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "NAME\tROLE\tGPU CAPACITY"); err != nil {
		return err
	}
	for _, n := range snap.Nodes {
		gpus := "-"
		if n.GPUCapacity > 0 {
			gpus = fmt.Sprintf("%d", n.GPUCapacity)
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\n", n.Name, n.Role, gpus); err != nil {
			return err
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	gpuResource := "<unknown>"
	if snap.Vendor != "" {
		gpuResource = snap.Vendor + ".com/gpu"
	}
	if _, err := fmt.Fprintf(out, "\nGPU PODS (%s)\n", gpuResource); err != nil {
		return err
	}
	if len(snap.GPUPods) == 0 {
		_, err := fmt.Fprintln(out, "(none)")
		return err
	}
	tw = tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "NAMESPACE\tPOD\tNODE\tGPUS\tPHASE"); err != nil {
		return err
	}
	for _, p := range snap.GPUPods {
		node := p.Node
		if node == "" {
			node = "<unscheduled>"
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n", p.Namespace, p.Name, node, p.GPUs, p.Phase); err != nil {
			return err
		}
	}
	return tw.Flush()
}
