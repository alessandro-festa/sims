package exporter

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/alessandro-festa/sims/operators/fake-dcgm-extras/pkg/dcgm"
)

// Run parses args + serves /metrics until ctx is cancelled.
func Run(ctx context.Context, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("fake-dcgm-extras", flag.ContinueOnError)
	fs.SetOutput(stderr)
	listen := fs.String("listen", ":9401", "Address the metrics HTTP server binds to.")
	gpus := fs.Int("gpus-per-node", 2, "Number of fake GPU slots to expose for this node.")
	product := fs.String("product-name", "Tesla T4", "GPU model name surfaced via modelName label.")
	refresh := fs.Duration("refresh-interval", 5*time.Second, "How often to re-list this node's GPU-consuming pods.")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *gpus < 0 {
		return fmt.Errorf("--gpus-per-node must be >= 0, got %d", *gpus)
	}

	log := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	hostname := os.Getenv("NODE_NAME")
	if hostname == "" {
		hn, err := os.Hostname()
		if err != nil {
			return fmt.Errorf("resolve hostname: %w", err)
		}
		hostname = hn
	}

	gpuList := buildGPUs(hostname, *product, *gpus)
	sampler := buildSamplerOrIdle(ctx, log, gpuList, hostname, *refresh)
	collector := dcgm.New(sampler)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(collector.Registry(), promhttp.HandlerOpts{Registry: collector.Registry()}))
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
		_, _ = fmt.Fprintf(stderr, "fake-dcgm-extras: listening on %s, hostname=%s, gpus=%d, product=%q\n", *listen, hostname, *gpus, *product)
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

// buildSamplerOrIdle returns a topology-aware Sampler when an in-cluster
// client is available, falling back to idle baselines when it isn't.
// Lets the binary smoke-test outside a cluster without crashing.
func buildSamplerOrIdle(ctx context.Context, log *slog.Logger, gpus []gpuIdentity, hostname string, refresh time.Duration) dcgm.Sampler {
	cs, err := newInClusterClientset()
	if err != nil {
		log.Warn("in-cluster Kubernetes client unavailable; emitting idle baselines only", "err", err)
		return idleOnly(gpus, hostname)
	}
	c := newCache(cs, hostname, log)
	go c.Run(ctx, refresh)
	return buildSampler(c, gpus, hostname)
}

func idleOnly(gpus []gpuIdentity, hostname string) dcgm.Sampler {
	return dcgm.SamplerFunc(func(_ context.Context) []dcgm.Snapshot {
		out := make([]dcgm.Snapshot, 0, len(gpus))
		for _, g := range gpus {
			s := dcgm.Snapshot{
				GPU: fmt.Sprint(g.Index), UUID: g.UUID, Device: g.Device, ModelName: g.ModelName, Hostname: hostname,
				MemClock: memClockMHz,
			}
			fillIdle(&s)
			out = append(out, s)
		}
		return out
	})
}

func newInClusterClientset() (kubernetes.Interface, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(cfg)
}
