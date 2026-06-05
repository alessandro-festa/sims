package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newGPUStatusCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show cluster state, advertised GPUs, and dashboard URL",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not yet implemented (Phase 1) — name=%s", name)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Cluster name")
	return cmd
}
