package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newGPUMonitoringCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "monitoring",
		Short: "Enable or disable monitoring on an existing cluster",
	}
	cmd.AddCommand(
		newMonitoringActionCmd("enable", "Install kube-prometheus-stack + vendor dashboard"),
		newMonitoringActionCmd("disable", "Remove the monitoring release"),
	)
	return cmd
}

func newMonitoringActionCmd(action, short string) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   action,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not yet implemented (Phase 2) — action=%s name=%s", action, name)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Cluster name")
	return cmd
}
