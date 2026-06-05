package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

type dashboardOpts struct {
	name string
	open bool
	stop bool
}

func newGPUDashboardCmd() *cobra.Command {
	var o dashboardOpts
	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Start (or stop) a background port-forward to Grafana on :3000",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not yet implemented (Phase 2) — name=%s open=%v stop=%v", o.name, o.open, o.stop)
		},
	}
	cmd.Flags().StringVar(&o.name, "name", "", "Cluster name")
	cmd.Flags().BoolVar(&o.open, "open", false, "Open Grafana in the default browser")
	cmd.Flags().BoolVar(&o.stop, "stop", false, "Stop the running port-forward instead of starting one")
	return cmd
}
