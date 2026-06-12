// Package exporter is the metrics-exporter subcommand entrypoint.
//
// Phase 5: the exporter is topology-driven. Every Prometheus scrape
// asks the cache for the latest worldState (refreshed every few seconds
// by a background goroutine) and emits one Snapshot per fake GPU. Pods
// holding amd.com/gpu allocations (per the topology CM written by
// status-updater) get their sims.io/simulated-gpu-utilization annotation
// translated to a per-scrape util sample; idle GPUs stay at baseline.
//
// If the in-cluster Kubernetes client isn't available (dev/smoke
// scenarios), the exporter falls back to a static "all idle" Sampler so
// /metrics keeps responding.
package exporter

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/alessandro-festa/sims/operators/fake-rocm-gpu-operator/pkg/annotations"
	"github.com/alessandro-festa/sims/operators/fake-rocm-gpu-operator/pkg/metrics"
	"github.com/alessandro-festa/sims/operators/fake-rocm-gpu-operator/pkg/simulate"
)

// Run parses args and runs the metrics-exporter until ctx is cancelled.
// args excludes the subcommand token (caller already stripped
// os.Args[0:2]).
func Run(ctx context.Context, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("metrics-exporter", flag.ContinueOnError)
	fs.SetOutput(stderr)
	listen := fs.String("listen", ":5000", "Address the metrics HTTP server binds to.")
	gpus := fs.Int("gpus-per-node", 2, "Number of PHYSICAL fake GPUs on this node. CPX partitioning (--partition-mode=cpx --partition-count=N) multiplies the advertised device count.")
	product := fs.String("product-name", "MI300X", "Card series/model reported in metric labels.")
	memBytes := fs.Int64("memory-bytes", 206158430208, "Per-physical-GPU total VRAM in bytes (default 192 GiB); split evenly across partitions under CPX.")
	namespace := fs.String("topology-namespace", "gpu-operator", "Namespace holding the topology ConfigMap written by status-updater.")
	refresh := fs.Duration("refresh-interval", 5*time.Second, "How often to re-read topology + pod annotations.")
	partitionMode := fs.String("partition-mode", simulate.PartitionSPX, "CPX/SPX emulation mode: spx (1 logical per physical, default) or cpx (--partition-count logical per physical).")
	partitionCount := fs.Int("partition-count", 1, "Number of CPX partitions per physical GPU when --partition-mode=cpx.")
	defaultUtil := fs.String("default-utilization", annotations.DefaultUtilizationRange, "Default utilization range (e.g. 5-15) for pods without a sims.io/simulated-gpu-utilization annotation.")
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

	gpuList := simulate.BuildGPUs(hostname, *product, *memBytes, *gpus, *partitionMode, *partitionCount)

	sampler := buildSampler(ctx, log, gpuList, hostname, *namespace, *refresh, *defaultUtil)
	collector := metrics.New(sampler)

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
		_, _ = fmt.Fprintf(stderr, "metrics-exporter: listening on %s, hostname=%s, gpus=%d, product=%s, partition=%s/%d (advertising %d logical)\n", *listen, hostname, *gpus, *product, *partitionMode, *partitionCount, len(gpuList))
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

// buildSampler returns a metrics.Sampler that either reads cluster state
// (in-cluster client succeeded) or always reports idle (no client). The
// fallback keeps the exporter useful for local smoke tests outside a
// kind cluster.
func buildSampler(ctx context.Context, log *slog.Logger, gpus []simulate.GPU, hostname, namespace string, refresh time.Duration, defaultUtil string) metrics.Sampler {
	cs, err := newInClusterClientset()
	if err != nil {
		log.Warn("in-cluster Kubernetes client unavailable; serving idle baseline for all GPUs", "err", err)
		return idleSampler(gpus, hostname)
	}
	c := newCache(cs, namespace, hostname, log, defaultUtil)
	go c.Run(ctx, refresh)
	return topologySampler(c, gpus, hostname)
}

// idleSampler emits one idle Snapshot per GPU, never changing. Used
// when the exporter can't reach a Kubernetes API.
func idleSampler(gpus []simulate.GPU, hostname string) metrics.Sampler {
	return metrics.SamplerFunc(func(_ context.Context) []metrics.Snapshot {
		out := make([]metrics.Snapshot, 0, len(gpus))
		for _, g := range gpus {
			out = append(out, snapshotFromIdle(g, hostname))
		}
		return out
	})
}

// topologySampler emits one Snapshot per GPU using the cache's latest
// worldState. Assigned GPUs derive their values from the pod's
// sims.io/simulated-gpu-utilization range; idle GPUs use the baseline.
func topologySampler(c *cache, gpus []simulate.GPU, hostname string) metrics.Sampler {
	return metrics.SamplerFunc(func(_ context.Context) []metrics.Snapshot {
		state := c.Snapshot()
		// A fresh RNG per scrape: values vary slightly between scrapes
		// (visible in Grafana) without leaking state across scrapes.
		rng := rand.New(rand.NewSource(time.Now().UnixNano())) //nolint:gosec // not security-sensitive
		out := make([]metrics.Snapshot, 0, len(gpus))
		for _, g := range gpus {
			assignment := state.topology.FindAssignment(hostname, g.ID)
			if assignment == nil {
				out = append(out, snapshotFromIdle(g, hostname))
				continue
			}
			r := state.podRanges[assignment.PodNamespace+"/"+assignment.PodName]
			util := r.Sample(rng)
			sample := g.SampleLoaded(util, rng)
			out = append(out, snapshotFromLoaded(g, hostname, assignment.PodNamespace, assignment.PodName, assignment.Container, sample))
		}
		return out
	})
}

func snapshotFromIdle(g simulate.GPU, hostname string) metrics.Snapshot {
	s := g.SampleIdle()
	return metrics.Snapshot{
		GPUID:         g.ID,
		SerialNumber:  g.SerialNumber,
		CardSeries:    g.CardSeries,
		CardModel:     g.CardModel,
		Hostname:      hostname,
		PartitionMode: g.PartitionMode,
		PartitionID:   g.PartitionID,
		JunctionTemp:  s.JunctionTemp,
		PackagePower:  s.PackagePower,
		GfxActivity:   s.GfxActivity,
		UsedVRAM:      s.UsedVRAM,
		TotalVRAM:     s.TotalVRAM,
		Health:        s.Health,
		ClockGfx:      s.ClockGfx,
		Voltage:       s.Voltage,
		FanSpeed:      s.FanSpeed,
		PCIeBandwidth: s.PCIeBandwidth,
	}
}

func snapshotFromLoaded(g simulate.GPU, hostname, ns, pod, container string, s simulate.Sample) metrics.Snapshot {
	return metrics.Snapshot{
		GPUID:         g.ID,
		SerialNumber:  g.SerialNumber,
		CardSeries:    g.CardSeries,
		CardModel:     g.CardModel,
		Hostname:      hostname,
		Pod:           pod,
		Namespace:     ns,
		Container:     container,
		PartitionMode: g.PartitionMode,
		PartitionID:   g.PartitionID,
		JunctionTemp:  s.JunctionTemp,
		PackagePower:  s.PackagePower,
		GfxActivity:   s.GfxActivity,
		UsedVRAM:      s.UsedVRAM,
		TotalVRAM:     s.TotalVRAM,
		Health:        s.Health,
		ClockGfx:      s.ClockGfx,
		Voltage:       s.Voltage,
		FanSpeed:      s.FanSpeed,
		PCIeBandwidth: s.PCIeBandwidth,
	}
}

func newInClusterClientset() (kubernetes.Interface, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(cfg)
}
