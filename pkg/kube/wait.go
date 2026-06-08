package kube

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// gpuVendorLabel is set on worker nodes by pkg/config.Render so callers can
// distinguish simulated-GPU workers from the control-plane.
const gpuVendorLabel = "sims.io/gpu-vendor"

// pollInterval is the gap between node-list polls. Exposed only for tests.
var pollInterval = 2 * time.Second

// WaitForResourceCapacity blocks until at least `nodes` worker nodes
// (selected via the sims.io/gpu-vendor label) report `status.capacity[resource]`
// >= `perNode`, or ctx is done.
//
// The kubeconfig bytes are typically obtained from pkg/cluster.Provider.KubeConfig.
func WaitForResourceCapacity(ctx context.Context, kubeconfig []byte, resourceName string, perNode, nodes int) error {
	clientset, err := newClientset(kubeconfig)
	if err != nil {
		return err
	}
	return waitForCapacity(ctx, clientset, resourceName, perNode, nodes)
}

func newClientset(kubeconfig []byte) (kubernetes.Interface, error) {
	cfg, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("parse kubeconfig: %w", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build clientset: %w", err)
	}
	return cs, nil
}

func waitForCapacity(ctx context.Context, cs kubernetes.Interface, resourceName string, perNode, nodes int) error {
	want, err := resource.ParseQuantity(fmt.Sprintf("%d", perNode))
	if err != nil {
		return fmt.Errorf("parse perNode: %w", err)
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		ready, err := countReadyWorkers(ctx, cs, resourceName, want)
		if err != nil {
			return err
		}
		if ready >= nodes {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for %d worker(s) to report %s >= %d: %w",
				nodes, resourceName, perNode, ctx.Err())
		case <-ticker.C:
		}
	}
}

func countReadyWorkers(ctx context.Context, cs kubernetes.Interface, resourceName string, want resource.Quantity) (int, error) {
	list, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: gpuVendorLabel})
	if err != nil {
		return 0, fmt.Errorf("list nodes: %w", err)
	}
	ready := 0
	for i := range list.Items {
		if hasCapacity(&list.Items[i], corev1.ResourceName(resourceName), want) {
			ready++
		}
	}
	return ready, nil
}

func hasCapacity(n *corev1.Node, name corev1.ResourceName, want resource.Quantity) bool {
	got, ok := n.Status.Capacity[name]
	if !ok {
		return false
	}
	return got.Cmp(want) >= 0
}
