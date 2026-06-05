package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newGPULoadImageCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "load-image <image>",
		Short: "Load a local Docker image into the kind cluster's local registry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not yet implemented (Phase 1) — name=%s image=%s", name, args[0])
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Cluster name")
	return cmd
}
