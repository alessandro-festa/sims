package exporter

import (
	"context"
	"log/slog"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/alessandro-festa/sims/operators/fake-rocm-gpu-operator/pkg/annotations"
	"github.com/alessandro-festa/sims/operators/fake-rocm-gpu-operator/pkg/topology"
)

// worldState is the snapshot of cluster data the exporter needs to
// answer a Prometheus scrape: which pods are assigned to which GPUs on
// this node + each assigned pod's utilization range.
type worldState struct {
	topology  *topology.Topology
	podRanges map[string]annotations.Range // key: "namespace/name"
}

// cache periodically reads the topology CM + the annotations of every
// pod referenced by topology entries for this node, then atomically
// publishes the resulting worldState. The exporter's Sampler reads
// state via Snapshot() — fast, no blocking on the apiserver.
type cache struct {
	cs          kubernetes.Interface
	namespace   string
	hostname    string
	log         *slog.Logger
	defaultUtil string

	mu    sync.RWMutex
	state *worldState
}

func newCache(cs kubernetes.Interface, namespace, hostname string, log *slog.Logger, defaultUtil string) *cache {
	return &cache{
		cs:          cs,
		namespace:   namespace,
		hostname:    hostname,
		log:         log,
		defaultUtil: defaultUtil,
		state:       &worldState{topology: topology.Empty(), podRanges: map[string]annotations.Range{}},
	}
}

// Snapshot returns the cache's current worldState. The returned value
// must NOT be mutated — it's shared across scrapes until the next
// refresh.
func (c *cache) Snapshot() *worldState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

// Refresh re-reads topology + each referenced pod's annotation and
// publishes a fresh worldState. Errors are logged and skipped — the
// cache keeps serving the last good state so a transient apiserver hiccup
// doesn't blank /metrics.
func (c *cache) Refresh(ctx context.Context) {
	top, err := topology.Load(ctx, c.cs, c.namespace)
	if err != nil {
		c.log.Warn("topology load failed; keeping previous cache", "err", err)
		return
	}
	podRanges := map[string]annotations.Range{}
	for _, a := range top.Nodes[c.hostname] {
		key := a.PodNamespace + "/" + a.PodName
		if _, seen := podRanges[key]; seen {
			continue
		}
		pod, err := c.cs.CoreV1().Pods(a.PodNamespace).Get(ctx, a.PodName, metav1.GetOptions{})
		if err != nil {
			c.log.Debug("pod fetch failed; will retry next tick", "namespace", a.PodNamespace, "pod", a.PodName, "err", err)
			continue
		}
		r, err := annotations.ParseUtilizationWithDefault(pod.Annotations[annotations.UtilizationAnnotation], c.defaultUtil)
		if err != nil {
			c.log.Debug("invalid utilization annotation; falling back to default", "namespace", a.PodNamespace, "pod", a.PodName, "err", err)
			r, _ = annotations.ParseUtilizationWithDefault("", c.defaultUtil)
		}
		podRanges[key] = r
	}
	c.mu.Lock()
	c.state = &worldState{topology: top, podRanges: podRanges}
	c.mu.Unlock()
}

// Run blocks until ctx is cancelled, refreshing every interval. Does an
// initial refresh immediately so /metrics returns useful values from the
// first scrape rather than waiting one interval.
func (c *cache) Run(ctx context.Context, interval time.Duration) {
	c.Refresh(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.Refresh(ctx)
		}
	}
}
