// Single binary, kubectl-style subcommand dispatch. Only the metrics-exporter
// subcommand is implemented in Phase 3; the others ship in Phases 4-6 and
// return a friendly "not implemented" message.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/alessandro-festa/sims/operators/fake-rocm-gpu-operator/internal/exporter"
)

const usage = `fake-rocm-gpu-operator — sims AMD GPU simulator

Usage:
  fake-rocm-gpu-operator <subcommand> [flags]

Subcommands:
  metrics-exporter   Serve AMD-namespaced Prometheus metrics for fake GPUs.
  device-plugin      (Phase 4) Kubelet device-plugin advertising amd.com/gpu.
  status-updater     (Phase 5) Watch pods, write per-node topology ConfigMap.
  node-labeller      (Phase 5) Patch node labels at startup.
  controller         (Phase 6) Reconcile DeviceConfig CRDs.

Run 'fake-rocm-gpu-operator <subcommand> --help' for subcommand flags.
`

var phaseStubs = map[string]string{
	"device-plugin":  "device-plugin lands in Phase 4 of sims; see operators/fake-rocm-gpu-operator/README.md",
	"status-updater": "status-updater lands in Phase 5 of sims; see operators/fake-rocm-gpu-operator/README.md",
	"node-labeller":  "node-labeller lands in Phase 5 of sims; see operators/fake-rocm-gpu-operator/README.md",
	"controller":     "controller lands in Phase 6 of sims; see operators/fake-rocm-gpu-operator/README.md",
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, errUsage) {
			fmt.Fprint(os.Stderr, usage)
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var errUsage = errors.New("usage")

func run(args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	sub, subArgs := args[0], args[1:]

	switch sub {
	case "-h", "--help", "help":
		fmt.Print(usage)
		return nil
	case "metrics-exporter":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return exporter.Run(ctx, subArgs, os.Stderr)
	}

	if msg, ok := phaseStubs[sub]; ok {
		// --help on a stubbed subcommand still exits 0 so docker/CI smoke
		// tests can probe the binary without failing.
		for _, a := range subArgs {
			if a == "-h" || a == "--help" {
				fmt.Printf("%s — %s\n", sub, msg)
				return nil
			}
		}
		return fmt.Errorf("%s: %s", sub, msg)
	}

	return fmt.Errorf("unknown subcommand %q\n\n%s", sub, usage)
}
