// Package e2e helpers shared by nvidia_test.go and monitoring_test.go.
// These would be _test.go files except multiple test files in the same
// package can already see each other's lowercase identifiers, so a plain
// .go file is fine and keeps the helpers out of test-binary-only state.
//
// Build of these helpers is gated by the fact that they're only referenced
// from _test.go files; production binaries never link them.
package e2e

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/alessandro-festa/sims/pkg/cluster"
)

const (
	clusterName      = "sims-nvidia"
	amdClusterName   = "sims-amd"
	sampleNamespace  = "default"
	samplePodName    = "sims-nvidia-sample"
	amdSamplePodName = "sims-amd-sample"
	createTimeout    = 4 * time.Minute
	createMonTimeout = 8 * time.Minute
	scheduleTimeout  = 1 * time.Minute
	deleteTimeout    = 2 * time.Minute
)

// simsBin is the path to a freshly-built sims binary, set by TestMain in
// the same package.
var simsBin string

// chartDirAbs is the absolute path to the repo's charts/ directory, set by
// TestMain. The CLI's resolver picks this up via SIMS_CHART_DIR.
var chartDirAbs string

// buildSims compiles ./cmd/sims into tmpdir and returns the path. Used by
// TestMain. tmpdir is expected to be removed by the caller on exit.
func buildSims(tmpdir string) error {
	simsBin = filepath.Join(tmpdir, "sims")
	build := exec.Command("go", "build", "-o", simsBin, "../cmd/sims")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		return fmt.Errorf("build sims: %w", err)
	}
	abs, err := filepath.Abs("../charts")
	if err != nil {
		return fmt.Errorf("resolve charts dir: %w", err)
	}
	chartDirAbs = abs
	return nil
}

// runSims executes the built sims binary with the given args, capturing
// stdout + stderr. SIMS_CHART_DIR is set so chart-resolution works from
// the e2e/ cwd.
func runSims(ctx context.Context, args ...string) (stdout, stderr []byte, err error) {
	cmd := exec.CommandContext(ctx, simsBin, args...)
	cmd.Env = append(os.Environ(), "SIMS_CHART_DIR="+chartDirAbs)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return outBuf.Bytes(), errBuf.Bytes(), err
}

// newKubeconfig fetches the named kind cluster's kubeconfig.
func newKubeconfig(ctx context.Context, name string) ([]byte, error) {
	provider := cluster.New(nil)
	return provider.KubeConfig(ctx, name)
}

// mustClientset returns a clientset from the given kubeconfig or fails the
// test.
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

// waitForScheduled polls until the pod gets bound to a node, or timeout.
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

// cleanupCluster runs `sims gpu delete --name <name>` with a fresh context
// and ignores errors. Use from t.Cleanup.
func cleanupCluster(t *testing.T, name string) {
	t.Helper()
	cleanup, cancel := context.WithTimeout(context.Background(), deleteTimeout)
	defer cancel()
	if _, _, err := runSims(cleanup, "gpu", "delete", "--name", name); err != nil {
		t.Logf("cleanup: gpu delete failed (best-effort): %v", err)
	}
}
