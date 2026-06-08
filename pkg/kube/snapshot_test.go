package kube

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestSnapshot_EmptyCluster(t *testing.T) {
	cs := fake.NewClientset()
	snap, err := snapshot(context.Background(), cs)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.Vendor != "" || snap.K8sVersion != "" || len(snap.Nodes) != 0 || len(snap.GPUPods) != 0 {
		t.Errorf("expected zero snapshot, got %+v", snap)
	}
}

func TestSnapshot_NVIDIACluster(t *testing.T) {
	cs := fake.NewClientset(
		controlPlaneNode("sims-nvidia-control-plane", "v1.31.0"),
		gpuWorkerNode("sims-nvidia-worker", "nvidia", "v1.31.0", 2),
		gpuWorkerNode("sims-nvidia-worker2", "nvidia", "v1.31.0", 2),
		gpuPod("default", "sims-nvidia-sample", "sims-nvidia-worker", "nvidia.com/gpu", 1, corev1.PodRunning),
		gpuPod("default", "other-non-gpu-pod", "sims-nvidia-worker", "", 0, corev1.PodRunning),
	)
	snap, err := snapshot(context.Background(), cs)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.Vendor != "nvidia" {
		t.Errorf("Vendor = %q, want nvidia", snap.Vendor)
	}
	if snap.K8sVersion != "v1.31.0" {
		t.Errorf("K8sVersion = %q, want v1.31.0", snap.K8sVersion)
	}
	if len(snap.Nodes) != 3 {
		t.Fatalf("Nodes len = %d, want 3", len(snap.Nodes))
	}
	for _, n := range snap.Nodes {
		if n.Name == "sims-nvidia-control-plane" {
			if n.Role != "control-plane" || n.GPUCapacity != 0 {
				t.Errorf("control-plane snapshot wrong: %+v", n)
			}
		} else if n.GPUCapacity != 2 || n.Role != "worker" {
			t.Errorf("worker snapshot wrong: %+v", n)
		}
	}
	if len(snap.GPUPods) != 1 {
		t.Fatalf("GPUPods len = %d, want 1 (the non-GPU pod should be filtered out)", len(snap.GPUPods))
	}
	got := snap.GPUPods[0]
	if got.Name != "sims-nvidia-sample" || got.GPUs != 1 || got.Phase != "Running" {
		t.Errorf("pod snapshot wrong: %+v", got)
	}
}

func TestSnapshot_NoVendorMeansNoGPUPodsListed(t *testing.T) {
	// A non-sims cluster: no nodes carry the gpu-vendor label, so we
	// shouldn't list pods at all.
	cs := fake.NewClientset(
		controlPlaneNode("kind-control-plane", "v1.31.0"),
		gpuPod("default", "stray", "kind-control-plane", "nvidia.com/gpu", 1, corev1.PodRunning),
	)
	snap, err := snapshot(context.Background(), cs)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.Vendor != "" {
		t.Errorf("Vendor = %q, want empty", snap.Vendor)
	}
	if len(snap.GPUPods) != 0 {
		t.Errorf("GPUPods should be empty when vendor unknown, got %d", len(snap.GPUPods))
	}
}

func controlPlaneNode(name, kubelet string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{"node-role.kubernetes.io/control-plane": ""},
		},
		Status: corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{KubeletVersion: kubelet}},
	}
}

func gpuWorkerNode(name, vendor, kubelet string, gpus int64) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{gpuVendorLabel: vendor},
		},
		Status: corev1.NodeStatus{
			NodeInfo: corev1.NodeSystemInfo{KubeletVersion: kubelet},
			Capacity: corev1.ResourceList{
				corev1.ResourceName(vendor + ".com/gpu"): *resource.NewQuantity(gpus, resource.DecimalSI),
			},
		},
	}
}

func gpuPod(namespace, name, node, resourceName string, gpus int64, phase corev1.PodPhase) *corev1.Pod {
	limits := corev1.ResourceList{}
	if resourceName != "" {
		limits[corev1.ResourceName(resourceName)] = *resource.NewQuantity(gpus, resource.DecimalSI)
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec: corev1.PodSpec{
			NodeName:   node,
			Containers: []corev1.Container{{Name: "main", Resources: corev1.ResourceRequirements{Limits: limits}}},
		},
		Status: corev1.PodStatus{Phase: phase},
	}
}
