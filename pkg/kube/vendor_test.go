package kube

import (
	"context"
	"strings"
	"testing"

	"k8s.io/client-go/kubernetes/fake"
)

func TestDetectVendor_NVIDIA(t *testing.T) {
	cs := fake.NewClientset(
		controlPlaneNode("cp1", "v1.31.0"),
		gpuWorkerNode("w1", "nvidia", "v1.31.0", 2),
	)
	got, err := detectVendor(context.Background(), cs)
	if err != nil {
		t.Fatalf("detectVendor: %v", err)
	}
	if got != "nvidia" {
		t.Errorf("vendor = %q, want nvidia", got)
	}
}

func TestDetectVendor_NoLabeledNodes(t *testing.T) {
	cs := fake.NewClientset(controlPlaneNode("cp1", "v1.31.0"))
	_, err := detectVendor(context.Background(), cs)
	if err == nil || !strings.Contains(err.Error(), "sims.io/gpu-vendor") {
		t.Fatalf("expected gpu-vendor error, got: %v", err)
	}
}
