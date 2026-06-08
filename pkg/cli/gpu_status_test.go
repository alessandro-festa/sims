package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/alessandro-festa/sims/pkg/kube"
)

func TestWriteStatus_HappyPath(t *testing.T) {
	snap := &kube.ClusterSnapshot{
		Vendor:     "nvidia",
		K8sVersion: "v1.31.0",
		Nodes: []kube.NodeSnapshot{
			{Name: "sims-nvidia-control-plane", Role: "control-plane", GPUCapacity: 0},
			{Name: "sims-nvidia-worker", Role: "worker", GPUCapacity: 2},
		},
		GPUPods: []kube.PodSnapshot{
			{Namespace: "default", Name: "sims-nvidia-sample", Node: "sims-nvidia-worker", GPUs: 1, Phase: "Running"},
		},
	}
	var buf bytes.Buffer
	if err := writeStatus(&buf, "sims-nvidia", snap); err != nil {
		t.Fatalf("writeStatus: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"Cluster:   sims-nvidia",
		"Vendor:    nvidia",
		"K8s:       v1.31.0",
		"sims-nvidia-control-plane",
		"sims-nvidia-worker",
		"nvidia.com/gpu",
		"sims-nvidia-sample",
		"Running",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestWriteStatus_EmptyGPUPods(t *testing.T) {
	snap := &kube.ClusterSnapshot{
		Vendor:     "nvidia",
		K8sVersion: "v1.31.0",
		Nodes:      []kube.NodeSnapshot{{Name: "n", Role: "worker", GPUCapacity: 2}},
	}
	var buf bytes.Buffer
	if err := writeStatus(&buf, "sims-nvidia", snap); err != nil {
		t.Fatalf("writeStatus: %v", err)
	}
	if !strings.Contains(buf.String(), "(none)") {
		t.Errorf("expected '(none)' when no GPU pods, got:\n%s", buf.String())
	}
}

func TestWriteStatus_UnknownVendor(t *testing.T) {
	snap := &kube.ClusterSnapshot{K8sVersion: "v1.31.0"}
	var buf bytes.Buffer
	if err := writeStatus(&buf, "kind", snap); err != nil {
		t.Fatalf("writeStatus: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Vendor:    <unknown>") {
		t.Errorf("expected '<unknown>' vendor, got:\n%s", out)
	}
}
