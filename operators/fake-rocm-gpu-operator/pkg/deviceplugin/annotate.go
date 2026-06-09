package deviceplugin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	podresourcesv1 "k8s.io/kubelet/pkg/apis/podresources/v1"
)

// AssignedGPUsAnnotation is the annotation key the Annotator writes to
// pods consuming amd.com/gpu. The value is a sorted, comma-joined list of
// the device IDs the kubelet allocated to the pod (across all containers).
const AssignedGPUsAnnotation = "sims.io/assigned-gpus"

// DefaultPodResourcesSocket is the canonical kubelet PodResources socket
// path mounted into the plugin pod via hostPath.
const DefaultPodResourcesSocket = "/var/lib/kubelet/pod-resources/kubelet.sock"

// Annotator runs alongside the device-plugin server and reconciles pod
// annotations with kubelet's authoritative pod ↔ device mapping. The
// server itself never calls into the Annotator; the Allocate path returns
// empty responses and the Annotator picks up the new assignment on its
// next reconcile tick.
type Annotator interface {
	Run(ctx context.Context) error
}

// PodResourcesAnnotator polls the kubelet PodResources API (every
// ReconcileInterval) and writes AssignedGPUsAnnotation to any pod whose
// containers were allocated devices for ResourceName but doesn't carry
// the annotation yet (or carries a stale value).
type PodResourcesAnnotator struct {
	Client             kubernetes.Interface
	ResourceName       string
	SocketPath         string
	ReconcileInterval  time.Duration
	Logger             *slog.Logger
}

// Run blocks until ctx is cancelled. Returns nil on clean shutdown.
func (a *PodResourcesAnnotator) Run(ctx context.Context) error {
	if a.ReconcileInterval <= 0 {
		a.ReconcileInterval = 5 * time.Second
	}
	if a.ResourceName == "" {
		a.ResourceName = DefaultResourceName
	}
	if a.SocketPath == "" {
		a.SocketPath = DefaultPodResourcesSocket
	}
	log := a.Logger
	if log == nil {
		log = slog.Default()
	}

	conn, err := grpc.NewClient(
		"unix://"+a.SocketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("dial pod-resources socket %s: %w", a.SocketPath, err)
	}
	defer func() { _ = conn.Close() }()
	client := podresourcesv1.NewPodResourcesListerClient(conn)

	ticker := time.NewTicker(a.ReconcileInterval)
	defer ticker.Stop()

	// Reconcile once immediately so freshly scheduled pods don't wait a
	// full tick.
	a.reconcile(ctx, client, log)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			a.reconcile(ctx, client, log)
		}
	}
}

func (a *PodResourcesAnnotator) reconcile(ctx context.Context, client podresourcesv1.PodResourcesListerClient, log *slog.Logger) {
	resp, err := client.List(ctx, &podresourcesv1.ListPodResourcesRequest{})
	if err != nil {
		log.Warn("pod-resources List failed", "err", err)
		return
	}
	for _, pod := range resp.GetPodResources() {
		devs := collectDevicesForResource(pod, a.ResourceName)
		if len(devs) == 0 {
			continue
		}
		want := strings.Join(devs, ",")
		if err := a.annotatePod(ctx, pod.GetNamespace(), pod.GetName(), want); err != nil {
			log.Warn("annotate pod failed", "namespace", pod.GetNamespace(), "pod", pod.GetName(), "err", err)
		}
	}
}

// collectDevicesForResource walks a PodResources entry and returns the
// sorted, deduplicated list of device IDs allocated for the given
// resource across all containers. Returns nil if the pod has no
// allocation for the resource.
func collectDevicesForResource(pod *podresourcesv1.PodResources, resource string) []string {
	seen := make(map[string]struct{})
	for _, c := range pod.GetContainers() {
		for _, d := range c.GetDevices() {
			if d.GetResourceName() != resource {
				continue
			}
			for _, id := range d.GetDeviceIds() {
				seen[id] = struct{}{}
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// annotatePod patches the pod's metadata.annotations with the given
// AssignedGPUsAnnotation value. Read-modify-write via JSON merge patch
// (idempotent: the patch is empty when the pod already carries the
// expected annotation).
func (a *PodResourcesAnnotator) annotatePod(ctx context.Context, namespace, name, value string) error {
	pod, err := a.Client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get pod: %w", err)
	}
	if pod.Annotations[AssignedGPUsAnnotation] == value {
		return nil
	}
	patch, err := json.Marshal(map[string]any{
		"metadata": map[string]any{
			"annotations": map[string]string{
				AssignedGPUsAnnotation: value,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("marshal patch: %w", err)
	}
	_, err = a.Client.CoreV1().Pods(namespace).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("patch pod: %w", err)
	}
	return nil
}
