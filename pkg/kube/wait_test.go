package kube

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestWaitForResourceCapacity_AlreadyReady(t *testing.T) {
	cs := fake.NewClientset(
		worker("w1", 2),
		worker("w2", 2),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := waitForCapacity(ctx, cs, "nvidia.com/gpu", 2, 2); err != nil {
		t.Fatalf("waitForCapacity: %v", err)
	}
}

func TestWaitForResourceCapacity_BecomesReady(t *testing.T) {
	prev := pollInterval
	pollInterval = 10 * time.Millisecond
	t.Cleanup(func() { pollInterval = prev })

	cs := fake.NewClientset(
		worker("w1", 0),
		worker("w2", 0),
	)

	var wg sync.WaitGroup
	wg.Go(func() {
		time.Sleep(50 * time.Millisecond)
		setCapacity(t, cs, "w1", 2)
		setCapacity(t, cs, "w2", 2)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := waitForCapacity(ctx, cs, "nvidia.com/gpu", 2, 2); err != nil {
		t.Fatalf("waitForCapacity: %v", err)
	}
	wg.Wait()
}

func TestWaitForResourceCapacity_Timeout(t *testing.T) {
	prev := pollInterval
	pollInterval = 10 * time.Millisecond
	t.Cleanup(func() { pollInterval = prev })

	cs := fake.NewClientset(worker("w1", 0))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := waitForCapacity(ctx, cs, "nvidia.com/gpu", 2, 1)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want DeadlineExceeded, got %v", err)
	}
}

func TestWaitForResourceCapacity_IgnoresControlPlane(t *testing.T) {
	// A control-plane node without the gpu-vendor label should not count even
	// if it carries the resource (defensive — this shouldn't happen in real
	// clusters but proves the label selector is doing its job).
	cs := fake.NewClientset(
		controlPlane("cp1", 99),
		worker("w1", 2),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := waitForCapacity(ctx, cs, "nvidia.com/gpu", 2, 1); err != nil {
		t.Fatalf("waitForCapacity: %v", err)
	}
}

func worker(name string, gpus int64) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{gpuVendorLabel: "nvidia"},
		},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				"nvidia.com/gpu": *resource.NewQuantity(gpus, resource.DecimalSI),
			},
		},
	}
}

func controlPlane(name string, gpus int64) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				"nvidia.com/gpu": *resource.NewQuantity(gpus, resource.DecimalSI),
			},
		},
	}
}

func setCapacity(t *testing.T, cs *fake.Clientset, name string, gpus int64) {
	t.Helper()
	n, err := cs.CoreV1().Nodes().Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get %s: %v", name, err)
	}
	n.Status.Capacity["nvidia.com/gpu"] = *resource.NewQuantity(gpus, resource.DecimalSI)
	if _, err := cs.CoreV1().Nodes().UpdateStatus(context.Background(), n, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update %s: %v", name, err)
	}
}
