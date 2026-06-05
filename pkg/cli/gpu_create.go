package cli

import (
	"fmt"

	"github.com/spf13/cobra"
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
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateVendor(o.vendor); err != nil {
				return err
			}
			return fmt.Errorf("not yet implemented (Phase 1) — vendor=%s name=%s workers=%d gpus-per-worker=%d k8s=%s monitoring=%v taint=%v",
				o.vendor, o.name, o.workers, o.gpusPerWorker, o.k8sVersion, o.withMonitoring, o.taint)
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

func validateVendor(v string) error {
	switch v {
	case "nvidia", "amd":
		return nil
	default:
		return fmt.Errorf("invalid --vendor %q (must be nvidia or amd)", v)
	}
}
