// Package e2e holds end-to-end tests that drive the real sims CLI against
// a real kind cluster. Gated by E2E=1 — `go test ./...` without it just
// skips. Run from CI via .github/workflows/e2e.yml.
package e2e

import (
	"context"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"

	"github.com/alessandro-festa/sims/pkg/cluster"
)

func TestMain(m *testing.M) {
	if os.Getenv("E2E") != "1" {
		os.Exit(m.Run()) // tests will Skip themselves
	}
	tmpdir, err := os.MkdirTemp("", "sims-e2e-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "tempdir:", err)
		os.Exit(2)
	}
	defer func() { _ = os.RemoveAll(tmpdir) }()
	if err := buildSims(tmpdir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(m.Run())
}

func TestNVIDIA_EndToEnd(t *testing.T) {
	if os.Getenv("E2E") != "1" {
		t.Skip("E2E=1 required (runs a real kind cluster — takes several minutes)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	t.Cleanup(func() { cleanupCluster(t) })

	// 1. Create the cluster.
	t.Log("creating sims-nvidia cluster (this takes ~1-2 minutes)")
	createCtx, cancelCreate := context.WithTimeout(ctx, createTimeout)
	defer cancelCreate()
	if _, stderr, err := runSims(createCtx, "gpu", "create",
		"--vendor", "nvidia", "--workers", "2", "--gpus-per-worker", "2"); err != nil {
		t.Fatalf("sims gpu create failed: %v\nstderr:\n%s", err, stderr)
	}

	// 2. Verify capacity is advertised.
	kc, err := newKubeconfig(ctx)
	if err != nil {
		t.Fatalf("fetch kubeconfig: %v", err)
	}
	cs := mustClientset(t, kc)
	assertWorkerGPUCapacity(t, ctx, cs, 2)

	// 3. Render + apply the sample pod manifest.
	sampleYAML, sampleStderr, err := runSims(ctx, "gpu", "sample", "--vendor", "nvidia")
	if err != nil {
		t.Fatalf("sims gpu sample failed: %v\nstderr:\n%s", err, sampleStderr)
	}
	pod := &corev1.Pod{}
	if err := yaml.Unmarshal(sampleYAML, pod); err != nil {
		t.Fatalf("parse sample YAML: %v\nyaml:\n%s", err, sampleYAML)
	}
	if _, err := cs.CoreV1().Pods(sampleNamespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create sample pod: %v", err)
	}

	// 4. Wait for the scheduler to bind the pod to a node.
	t.Log("waiting for sample pod to be Scheduled")
	if err := waitForScheduled(ctx, cs, sampleNamespace, samplePodName, scheduleTimeout); err != nil {
		t.Fatalf("sample pod never Scheduled: %v", err)
	}

	// 5. Delete the cluster cleanly.
	if _, stderr, err := runSims(ctx, "gpu", "delete", "--name", clusterName); err != nil {
		t.Fatalf("sims gpu delete failed: %v\nstderr:\n%s", err, stderr)
	}

	// 6. Verify cluster is gone.
	provider := cluster.New(nil)
	names, err := provider.List(ctx)
	if err != nil {
		t.Fatalf("list clusters after delete: %v", err)
	}
	if slices.Contains(names, clusterName) {
		t.Errorf("cluster %q still listed after delete: %v", clusterName, names)
	}
}

func assertWorkerGPUCapacity(t *testing.T, ctx context.Context, cs kubernetes.Interface, want int64) {
	t.Helper()
	nodes, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: "sims.io/gpu-vendor=nvidia"})
	if err != nil {
		t.Fatalf("list workers: %v", err)
	}
	if len(nodes.Items) == 0 {
		t.Fatal("no worker nodes carry the sims.io/gpu-vendor=nvidia label")
	}
	for _, n := range nodes.Items {
		q, ok := n.Status.Capacity["nvidia.com/gpu"]
		if !ok {
			t.Errorf("node %q does not advertise nvidia.com/gpu", n.Name)
			continue
		}
		got, _ := q.AsInt64()
		if got < want {
			t.Errorf("node %q: nvidia.com/gpu = %d, want >= %d", n.Name, got, want)
		}
	}
}
