// Package e2e holds end-to-end tests that drive the real sims CLI against
// a real kind cluster. Gated by E2E=1 — `go test ./...` without it just
// skips. Run from CI via .github/workflows/e2e.yml.
package e2e

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/yaml"

	"github.com/alessandro-festa/sims/pkg/cluster"
)

const (
	clusterName     = "sims-nvidia"
	sampleNamespace = "default"
	samplePodName   = "sims-nvidia-sample"
	createTimeout   = 4 * time.Minute
	scheduleTimeout = 1 * time.Minute
	deleteTimeout   = 2 * time.Minute
)

// simsBin is the path to a freshly-built sims binary, set by TestMain.
var simsBin string

// chartDirAbs is the absolute path to the repo's charts/ directory, set by
// TestMain. The CLI's resolver picks this up via SIMS_CHART_DIR so it works
// even though the test binary's cwd is e2e/, not the repo root.
var chartDirAbs string

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
	simsBin = filepath.Join(tmpdir, "sims")
	build := exec.Command("go", "build", "-o", simsBin, "../cmd/sims")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "build sims:", err)
		os.Exit(2)
	}
	abs, err := filepath.Abs("../charts")
	if err != nil {
		fmt.Fprintln(os.Stderr, "resolve charts dir:", err)
		os.Exit(2)
	}
	chartDirAbs = abs
	os.Exit(m.Run())
}

func TestNVIDIA_EndToEnd(t *testing.T) {
	if os.Getenv("E2E") != "1" {
		t.Skip("E2E=1 required (runs a real kind cluster — takes several minutes)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Best-effort cleanup regardless of where we fail.
	t.Cleanup(func() {
		// Use a fresh context — the test's may be cancelled.
		cleanup, cancelCleanup := context.WithTimeout(context.Background(), deleteTimeout)
		defer cancelCleanup()
		if _, _, err := runSims(cleanup, "gpu", "delete", "--name", clusterName); err != nil {
			t.Logf("cleanup: gpu delete failed (best-effort): %v", err)
		}
	})

	// 1. Create the cluster.
	t.Log("creating sims-nvidia cluster (this takes ~1-2 minutes)")
	createCtx, cancelCreate := context.WithTimeout(ctx, createTimeout)
	defer cancelCreate()
	if _, stderr, err := runSims(createCtx, "gpu", "create",
		"--vendor", "nvidia", "--workers", "2", "--gpus-per-worker", "2"); err != nil {
		t.Fatalf("sims gpu create failed: %v\nstderr:\n%s", err, stderr)
	}

	// 2. Verify capacity is advertised — gpu create's own wait already proved
	// this, but assert it directly for symmetry with the issue acceptance.
	kc, err := newKubeconfig(ctx)
	if err != nil {
		t.Fatalf("fetch kubeconfig: %v", err)
	}
	cs := mustClientset(t, kc)
	assertWorkerGPUCapacity(t, ctx, cs, 2)

	// 3. Render the sample pod manifest.
	sampleYAML, sampleStderr, err := runSims(ctx, "gpu", "sample", "--vendor", "nvidia")
	if err != nil {
		t.Fatalf("sims gpu sample failed: %v\nstderr:\n%s", err, sampleStderr)
	}
	pod := &corev1.Pod{}
	if err := yaml.Unmarshal(sampleYAML, pod); err != nil {
		t.Fatalf("parse sample YAML: %v\nyaml:\n%s", err, sampleYAML)
	}

	// 4. Apply it.
	if _, err := cs.CoreV1().Pods(sampleNamespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create sample pod: %v", err)
	}

	// 5. Wait for the scheduler to bind the pod to a node.
	t.Log("waiting for sample pod to be Scheduled")
	if err := waitForScheduled(ctx, cs, sampleNamespace, samplePodName, scheduleTimeout); err != nil {
		t.Fatalf("sample pod never Scheduled: %v", err)
	}

	// 6. Delete the cluster cleanly.
	if _, stderr, err := runSims(ctx, "gpu", "delete", "--name", clusterName); err != nil {
		t.Fatalf("sims gpu delete failed: %v\nstderr:\n%s", err, stderr)
	}

	// 7. Verify cluster is gone.
	provider := cluster.New(nil)
	names, err := provider.List(ctx)
	if err != nil {
		t.Fatalf("list clusters after delete: %v", err)
	}
	if slices.Contains(names, clusterName) {
		t.Errorf("cluster %q still listed after delete: %v", clusterName, names)
	}
}

func runSims(ctx context.Context, args ...string) (stdout, stderr []byte, err error) {
	cmd := exec.CommandContext(ctx, simsBin, args...)
	cmd.Env = append(os.Environ(), "SIMS_CHART_DIR="+chartDirAbs)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return outBuf.Bytes(), errBuf.Bytes(), err
}

func newKubeconfig(ctx context.Context) ([]byte, error) {
	provider := cluster.New(nil)
	return provider.KubeConfig(ctx, clusterName)
}

func mustClientset(t *testing.T, kubeconfig []byte) kubernetes.Interface {
	t.Helper()
	cfg, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		t.Fatalf("RESTConfigFromKubeConfig: %v", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("NewForConfig: %v", err)
	}
	return cs
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

func waitForScheduled(ctx context.Context, cs kubernetes.Interface, namespace, name string, timeout time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		pod, err := cs.CoreV1().Pods(namespace).Get(waitCtx, name, metav1.GetOptions{})
		if err != nil && !strings.Contains(err.Error(), "not found") {
			return fmt.Errorf("get pod: %w", err)
		}
		if err == nil && pod.Spec.NodeName != "" {
			return nil
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("pod %s/%s not Scheduled within %s: %w", namespace, name, timeout, waitCtx.Err())
		case <-ticker.C:
		}
	}
}
