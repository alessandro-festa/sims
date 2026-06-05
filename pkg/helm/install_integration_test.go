//go:build integration

// Integration test for pkg/helm. Spins up a real kind cluster (via the kind
// SDK directly, to keep this package's deps minimal), installs a tiny chart
// from testdata/, asserts the resulting ConfigMap, upgrades the values,
// uninstalls.
//
// Build tag means this file is NOT compiled into regular `go test ./...`.
// Run with: HELM_E2E=1 go test -tags integration ./pkg/helm/...

package helm

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	kindcluster "sigs.k8s.io/kind/pkg/cluster"
)

const integrationKindConfig = `kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
`

func TestHelm_Integration(t *testing.T) {
	if os.Getenv("HELM_E2E") != "1" {
		t.Skip("HELM_E2E=1 required (also requires Docker + kind)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	provider := kindcluster.NewProvider()
	name := "sims-helm-itest"
	t.Cleanup(func() { _ = provider.Delete(name, "") })

	if err := provider.Create(name, kindcluster.CreateWithRawConfig([]byte(integrationKindConfig))); err != nil {
		t.Fatalf("kind create: %v", err)
	}
	kcStr, err := provider.KubeConfig(name, false)
	if err != nil {
		t.Fatalf("kind kubeconfig: %v", err)
	}
	kc := []byte(kcStr)

	c, err := New(kc, "sims-helm-test")
	if err != nil {
		t.Fatalf("helm.New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	chartPath, _ := filepath.Abs("testdata/test-chart")

	if err := c.Install(ctx, "itest", chartPath, map[string]any{"key": "installed"}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if got := getConfigMap(t, kc, "sims-helm-test", "itest-cm").Data["key"]; got != "installed" {
		t.Errorf("after Install, key=%q want %q", got, "installed")
	}

	if err := c.Upgrade(ctx, "itest", chartPath, map[string]any{"key": "upgraded"}); err != nil {
		t.Fatalf("Upgrade: %v", err)
	}
	if got := getConfigMap(t, kc, "sims-helm-test", "itest-cm").Data["key"]; got != "upgraded" {
		t.Errorf("after Upgrade, key=%q want %q", got, "upgraded")
	}

	if err := c.Uninstall("itest"); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	// Idempotent: second uninstall is a no-op.
	if err := c.Uninstall("itest"); err != nil {
		t.Errorf("second Uninstall returned %v, want nil", err)
	}
}

func getConfigMap(t *testing.T, kubeconfig []byte, namespace, name string) *corev1.ConfigMap {
	t.Helper()
	cfg, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		t.Fatalf("RESTConfigFromKubeConfig: %v", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("kubernetes.NewForConfig: %v", err)
	}
	cm, err := cs.CoreV1().ConfigMaps(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get ConfigMap %s/%s: %v", namespace, name, err)
	}
	return cm
}
