package cluster

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestNew_NilLoggerOK(t *testing.T) {
	p := New(nil)
	if p == nil || p.p == nil {
		t.Fatal("New(nil) returned a zero Provider")
	}
}

func TestNew_AdapterImplementsKindLogger(t *testing.T) {
	// Compile-time interface assertion lives in the file. A runtime smoke
	// here just exercises every method to catch nil-pointer regressions in
	// the slog forwarding path.
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	p := New(logger)
	if p == nil {
		t.Fatal("New returned nil")
	}
	a := &slogAdapter{logger: logger}
	a.Warn("warn-msg")
	a.Warnf("warnf-%d", 1)
	a.Error("error-msg")
	a.Errorf("errorf-%d", 2)
	a.V(0).Info("v0-info")
	a.V(0).Infof("v0-infof-%d", 3)
	a.V(2).Info("v2-info")
	if !a.V(0).Enabled() {
		t.Error("V(0).Enabled() should be true at slog.LevelInfo")
	}
	out := buf.String()
	for _, want := range []string{"warn-msg", "warnf-1", "error-msg", "errorf-2", "v0-info", "v0-infof-3", "v2-info"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in adapter output:\n%s", want, out)
		}
	}
}

// TestProvider_Smoke creates a real kind cluster, lists it, fetches its
// kubeconfig, and deletes it. Requires Docker. Gated by KIND_E2E=1.
func TestProvider_Smoke(t *testing.T) {
	if os.Getenv("KIND_E2E") != "1" {
		t.Skip("KIND_E2E=1 required to run this test (creates a real kind cluster)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	p := New(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	name := "sims-cluster-smoke"

	t.Cleanup(func() {
		// Best-effort. A new context because t.Cleanup runs after the test's ctx is cancelled.
		_ = p.Delete(context.Background(), name)
	})

	raw := []byte(`kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
`)
	if err := p.Create(ctx, name, raw); err != nil {
		t.Fatalf("Create: %v", err)
	}

	names, err := p.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !slices.Contains(names, name) {
		t.Errorf("cluster %q not in List(): %v", name, names)
	}

	kc, err := p.KubeConfig(ctx, name)
	if err != nil {
		t.Fatalf("KubeConfig: %v", err)
	}
	if !bytes.Contains(kc, []byte("apiVersion:")) {
		t.Errorf("kubeconfig missing apiVersion:\n%s", kc)
	}
	if !bytes.Contains(kc, []byte("server:")) {
		t.Errorf("kubeconfig missing server:\n%s", kc)
	}
}
