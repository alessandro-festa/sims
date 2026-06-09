package topology

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestLoad_MissingCMReturnsEmpty(t *testing.T) {
	cs := fake.NewSimpleClientset()
	got, err := Load(context.Background(), cs, "gpu-operator")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got == nil || got.Nodes == nil || len(got.Nodes) != 0 {
		t.Errorf("expected empty topology, got %+v", got)
	}
}

func TestSaveLoad_Roundtrip(t *testing.T) {
	cs := fake.NewSimpleClientset()
	ctx := context.Background()
	ns := "gpu-operator"

	want := &Topology{
		Nodes: map[string][]Assignment{
			"node-a": {
				{GPUID: "gpu-0", PodNamespace: "default", PodName: "foo", Container: "payload"},
				{GPUID: "gpu-1", PodNamespace: "default", PodName: "bar", Container: "main"},
			},
			"node-b": {
				{GPUID: "gpu-0", PodNamespace: "tenant", PodName: "baz", Container: "worker"},
			},
		},
	}
	if err := Save(ctx, cs, ns, want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(ctx, cs, ns)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Nodes) != 2 {
		t.Fatalf("nodes = %d, want 2", len(got.Nodes))
	}
	if len(got.Nodes["node-a"]) != 2 {
		t.Errorf("node-a assignments = %d, want 2", len(got.Nodes["node-a"]))
	}
	if got.Nodes["node-a"][0].PodName != "foo" {
		t.Errorf("node-a[0].PodName = %q, want foo", got.Nodes["node-a"][0].PodName)
	}

	// Second Save with identical body is a no-op (avoids needless update
	// churn on every reconcile tick).
	if err := Save(ctx, cs, ns, got); err != nil {
		t.Fatalf("idempotent Save: %v", err)
	}
}

func TestSave_UpdatesExistingCM(t *testing.T) {
	ns := "gpu-operator"
	cs := fake.NewSimpleClientset(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: ConfigMapName, Namespace: ns},
		Data:       map[string]string{DataKey: "nodes: {}\n"},
	})

	t1 := &Topology{Nodes: map[string][]Assignment{
		"node-a": {{GPUID: "gpu-0", PodNamespace: "default", PodName: "x", Container: "c"}},
	}}
	if err := Save(context.Background(), cs, ns, t1); err != nil {
		t.Fatalf("Save: %v", err)
	}

	cm, err := cs.CoreV1().ConfigMaps(ns).Get(context.Background(), ConfigMapName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get cm: %v", err)
	}
	if cm.Data[DataKey] == "nodes: {}\n" {
		t.Error("CM data was not updated")
	}
}

func TestFindAssignment(t *testing.T) {
	top := &Topology{Nodes: map[string][]Assignment{
		"node-a": {
			{GPUID: "gpu-0", PodName: "foo"},
			{GPUID: "gpu-1", PodName: "bar"},
		},
	}}
	if got := top.FindAssignment("node-a", "gpu-1"); got == nil || got.PodName != "bar" {
		t.Errorf("FindAssignment(node-a, gpu-1) = %+v, want PodName=bar", got)
	}
	if got := top.FindAssignment("node-a", "gpu-9"); got != nil {
		t.Errorf("FindAssignment(node-a, gpu-9) = %+v, want nil", got)
	}
	if got := top.FindAssignment("missing-node", "gpu-0"); got != nil {
		t.Errorf("FindAssignment(missing-node, gpu-0) = %+v, want nil", got)
	}
}

// Compile-time check that the apierrors import is used (it is via
// IsNotFound paths the tests exercise indirectly).
var _ = apierrors.IsNotFound
