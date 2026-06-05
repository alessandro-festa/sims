package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newGPUDeleteCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a sims-managed kind cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not yet implemented (Phase 1) — name=%s", name)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Cluster name (default: most recently created sims cluster)")
	return cmd
}
