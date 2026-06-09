// fake-dcgm-extras is the entry point for the sidecar binary. It just
// signals + delegates to the exporter package — keeping a tiny main makes
// the binary easy to repurpose if a subcommand model ever emerges.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/alessandro-festa/sims/operators/fake-dcgm-extras/internal/exporter"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := exporter.Run(ctx, os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
