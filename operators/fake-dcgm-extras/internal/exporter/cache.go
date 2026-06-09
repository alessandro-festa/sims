// Package exporter is the fake-dcgm-extras runtime. It walks pods on
// this node that hold nvidia.com/gpu and translates each one's
// run.ai/simulated-gpu-utilization annotation into per-GPU DCGM gauge
// values; idle GPUs report baselines.
package exporter

import (
	"context"
	"log/slog"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
)

// Annotation key matches what run-ai/fake-gpu-operator uses + what
// sims's NVIDIA sample pod emits (see pkg/cli/gpu_sample.go).
const utilizationAnnotation = "run.ai/simulated-gpu-utilization"

// gpuRequestName is the extended resource real NVIDIA + fake-gpu-
// operator pods both request.
const gpuRequestName = corev1.ResourceName("nvidia.com/gpu")

// nodeAssignment is one pod that has been scheduled onto this node and
// requests at least one GPU.
type nodeAssignment struct {
	PodName   string
	Namespace string
	Container string
	Util      string // raw annotation value; parsed at sample time
}

// cache holds the latest list of GPU-consuming pods on this node.
// Refreshed in a goroutine; sample-time access is lock-fast.
type cache struct {
	cs       kubernetes.Interface
	hostname string
	log      *slog.Logger

	mu          sync.RWMutex
	assignments []nodeAssignment
}

func newCache(cs kubernetes.Interface, hostname string, log *slog.Logger) *cache {
	return &cache{cs: cs, hostname: hostname, log: log}
}

func (c *cache) Snapshot() []nodeAssignment {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]nodeAssignment, len(c.assignments))
	copy(out, c.assignments)
	return out
}

func (c *cache) Refresh(ctx context.Context) {
	pods, err := c.cs.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("spec.nodeName", c.hostname).String(),
	})
	if err != nil {
		c.log.Warn("list pods failed; keeping previous cache", "err", err)
		return
	}
	out := make([]nodeAssignment, 0, len(pods.Items))
	for i := range pods.Items {
		p := &pods.Items[i]
		container := firstGPUContainer(p)
		if container == "" {
			continue
		}
		out = append(out, nodeAssignment{
			PodName:   p.Name,
			Namespace: p.Namespace,
			Container: container,
			Util:      p.Annotations[utilizationAnnotation],
		})
	}
	c.mu.Lock()
	c.assignments = out
	c.mu.Unlock()
}

func (c *cache) Run(ctx context.Context, interval time.Duration) {
	c.Refresh(ctx)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.Refresh(ctx)
		}
	}
}

func firstGPUContainer(p *corev1.Pod) string {
	for i := range p.Spec.Containers {
		c := &p.Spec.Containers[i]
		if hasGPURequest(c.Resources) {
			return c.Name
		}
	}
	return ""
}

func hasGPURequest(r corev1.ResourceRequirements) bool {
	if q, ok := r.Limits[gpuRequestName]; ok && !q.IsZero() {
		return true
	}
	if q, ok := r.Requests[gpuRequestName]; ok && !q.IsZero() {
		return true
	}
	return false
}
