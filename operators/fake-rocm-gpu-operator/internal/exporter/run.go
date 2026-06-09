// Package exporter is the metrics-exporter subcommand entrypoint. It builds
// the configured set of fake GPUs once at startup and serves their idle-
// baseline samples over /metrics. Load-driven sampling lands in Phase 5 with
// the status-updater.
package exporter

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/alessandro-festa/sims/operators/fake-rocm-gpu-operator/pkg/metrics"
	"github.com/alessandro-festa/sims/operators/fake-rocm-gpu-operator/pkg/simulate"
)

// Run parses args and runs the metrics-exporter until ctx is cancelled. args
// excludes the subcommand token (caller already stripped os.Args[0:2]).
func Run(ctx context.Context, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("metrics-exporter", flag.ContinueOnError)
	fs.SetOutput(stderr)
	listen := fs.String("listen", ":5000", "Address the metrics HTTP server binds to.")
	gpus := fs.Int("gpus-per-node", 2, "Number of fake GPUs to expose for this node.")
	product := fs.String("product-name", "MI300X", "Card series/model reported in metric labels.")
	memBytes := fs.Int64("memory-bytes", 206158430208, "Per-GPU total VRAM in bytes (default 192 GiB).")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *gpus < 0 {
		return fmt.Errorf("--gpus-per-node must be >= 0, got %d", *gpus)
	}

	hostname := os.Getenv("NODE_NAME")
	if hostname == "" {
		hn, err := os.Hostname()
		if err != nil {
			return fmt.Errorf("resolve hostname: %w", err)
		}
		hostname = hn
	}

	collectors := metrics.New()
	for _, g := range simulate.BuildGPUs(hostname, *product, *memBytes, *gpus) {
		collectors.Observe(hostname, g, g.SampleIdle())
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(collectors.Registry(), promhttp.HandlerOpts{Registry: collectors.Registry()}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok\n")
	})

	srv := &http.Server{
		Addr:              *listen,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		_, _ = fmt.Fprintf(stderr, "metrics-exporter: listening on %s, hostname=%s, gpus=%d, product=%s\n", *listen, hostname, *gpus, *product)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
