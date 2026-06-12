package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/alessandro-festa/sims/pkg/cluster"
)

func newGPUListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List sims-managed kind clusters",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runList(cmd.Context(), cmd.OutOrStdout())
		},
	}
}

func runList(ctx context.Context, stdout io.Writer) error {
	provider := cluster.New(nil)
	all, err := provider.List(ctx)
	if err != nil {
		return err
	}
	sims := filterSimsClusters(all)
	if len(sims) == 0 {
		_, err := fmt.Fprintln(stdout, "(none)")
		return err
	}
	for _, name := range sims {
		if _, err := fmt.Fprintln(stdout, name); err != nil {
			return err
		}
	}
	return nil
}
