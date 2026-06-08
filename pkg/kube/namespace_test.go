package kube

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestEnsureNamespace_CreatesWhenMissing(t *testing.T) {
	cs := fake.NewClientset()
	labels := map[string]string{"pod-security.kubernetes.io/enforce": "privileged"}
	if err := ensureNamespace(context.Background(), cs, "gpu-operator", labels); err != nil {
		t.Fatalf("ensureNamespace: %v", err)
	}
	ns, err := cs.CoreV1().Namespaces().Get(context.Background(), "gpu-operator", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got := ns.Labels["pod-security.kubernetes.io/enforce"]; got != "privileged" {
		t.Errorf("PSA enforce label = %q, want privileged", got)
	}
}

func TestEnsureNamespace_PatchesExisting(t *testing.T) {
	cs := fake.NewClientset()
	if _, err := cs.CoreV1().Namespaces().Create(context.Background(), namespaceMissingPSALabels(), metav1.CreateOptions{}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	labels := map[string]string{"pod-security.kubernetes.io/enforce": "privileged"}
	if err := ensureNamespace(context.Background(), cs, "gpu-operator", labels); err != nil {
		t.Fatalf("ensureNamespace: %v", err)
	}
	ns, _ := cs.CoreV1().Namespaces().Get(context.Background(), "gpu-operator", metav1.GetOptions{})
	if got := ns.Labels["pod-security.kubernetes.io/enforce"]; got != "privileged" {
		t.Errorf("PSA enforce label = %q, want privileged", got)
	}
	if got := ns.Labels["existing"]; got != "keepme" {
		t.Errorf("pre-existing label dropped: got %q, want keepme", got)
	}
}

func TestEnsureNamespace_NoOpWhenLabelsMatch(t *testing.T) {
	cs := fake.NewClientset()
	labels := map[string]string{"x": "y"}
	if err := ensureNamespace(context.Background(), cs, "gpu-operator", labels); err != nil {
		t.Fatalf("first ensureNamespace: %v", err)
	}
	if err := ensureNamespace(context.Background(), cs, "gpu-operator", labels); err != nil {
		t.Fatalf("second ensureNamespace: %v", err)
	}
}

func namespaceMissingPSALabels() *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "gpu-operator",
			Labels: map[string]string{"existing": "keepme"},
		},
	}
}
