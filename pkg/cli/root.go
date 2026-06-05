package cli

import "github.com/spf13/cobra"

const version = "0.0.0-dev"

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "sims",
		Short:         "kind-based GPU cluster simulator for NVIDIA and AMD",
		Long:          "sims spins up kind clusters that simulate NVIDIA or AMD GPUs, with optional Prometheus + Grafana monitoring.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newGPUCmd())
	return root
}
