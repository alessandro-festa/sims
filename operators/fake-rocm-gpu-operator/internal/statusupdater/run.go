// Package statusupdater is the status-updater subcommand entrypoint.
// It runs as a single-replica Deployment, watches pods cluster-wide,
// and writes the topology ConfigMap that maps GPUs to the pods that
// hold them. The metrics-exporter on each node reads that CM on every
// scrape to attach pod/namespace/container labels to its gauges.
//
// Phase 5 polls every 5 seconds and rebuilds the whole topology from
// scratch — simple and correct at sims's scale (single-digit nodes).
// Switch to SharedInformer if it ever scales past that.
package statusupdater

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/alessandro-festa/sims/operators/fake-rocm-gpu-operator/pkg/deviceplugin"
	"github.com/alessandro-festa/sims/operators/fake-rocm-gpu-operator/pkg/topology"
)

// amdGPUResource matches what cmd/device-plugin registers + what pods
// request. Imported as a string literal to avoid an explicit dependency
// on the pluginapi package (we already pull deviceplugin for the
// annotation constant).
const amdGPUResource = corev1.ResourceName("amd.com/gpu")

// Run parses args and runs the status-updater until ctx is cancelled.
func Run(ctx context.Context, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("status-updater", flag.ContinueOnError)
	fs.SetOutput(stderr)
	namespace := fs.String("topology-namespace", "gpu-operator", "Namespace holding the topology ConfigMap.")
	interval := fs.Duration("reconcile-interval", 5*time.Second, "How often to rebuild + write the topology CM.")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	log := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cs, err := newInClusterClientset()
	if err != nil {
		return fmt.Errorf("in-cluster client: %w", err)
	}

	log.Info("status-updater starting",
		"namespace", *namespace,
		"interval", *interval)

	reconcile(ctx, log, cs, *namespace) // immediate first pass
	ticker := time.NewTicker(*interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			reconcile(ctx, log, cs, *namespace)
		}
	}
}

// reconcile lists every pod in the cluster, derives the current
// topology, and saves it. topology.Save is a no-op when the body is
// unchanged, so we can run this every 5s cheaply.
func reconcile(ctx context.Context, log *slog.Logger, cs kubernetes.Interface, namespace string) {
	pods, err := cs.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Warn("list pods failed", "err", err)
		return
	}
	top := topology.Empty()
	for i := range pods.Items {
		p := &pods.Items[i]
		if p.Spec.NodeName == "" {
			continue
		}
		assigned := p.Annotations[deviceplugin.AssignedGPUsAnnotation]
		if assigned == "" {
			continue
		}
		container := firstGPUContainer(p)
		for gpuID := range strings.SplitSeq(assigned, ",") {
			gpuID = strings.TrimSpace(gpuID)
			if gpuID == "" {
				continue
			}
			top.Nodes[p.Spec.NodeName] = append(top.Nodes[p.Spec.NodeName], topology.Assignment{
				GPUID:        gpuID,
				PodNamespace: p.Namespace,
				PodName:      p.Name,
				Container:    container,
			})
		}
	}
	if err := topology.Save(ctx, cs, namespace, top); err != nil {
		log.Warn("save topology failed", "err", err)
		return
	}
}

// firstGPUContainer returns the name of the first container in the pod
// that requests amd.com/gpu, or "" if none does. Phase 5 doesn't track
// which specific container holds which GPU (sims has no per-container
// device-plugin granularity) — we attribute the pod's whole allocation
// to its first GPU-requesting container.
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
	if q, ok := r.Limits[amdGPUResource]; ok && !q.IsZero() {
		return true
	}
	if q, ok := r.Requests[amdGPUResource]; ok && !q.IsZero() {
		return true
	}
	return false
}

func newInClusterClientset() (kubernetes.Interface, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(cfg)
}
