package helm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureDependencies_FileScheme(t *testing.T) {
	dir := t.TempDir()
	copyTree(t, "testdata/parent-chart", filepath.Join(dir, "parent-chart"))
	copyTree(t, "testdata/child-chart", filepath.Join(dir, "child-chart"))

	// Point helm at the tempdir for repo/cache state so the test is hermetic
	// and doesn't scan the developer's ~/.config/helm.
	t.Setenv("HELM_REPOSITORY_CONFIG", filepath.Join(dir, "repositories.yaml"))
	t.Setenv("HELM_REPOSITORY_CACHE", filepath.Join(dir, "cache"))

	c, err := New([]byte(stubKubeconfig), "default")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	if err := c.EnsureDependencies(context.Background(), filepath.Join(dir, "parent-chart")); err != nil {
		t.Fatalf("EnsureDependencies: %v", err)
	}

	matches, err := filepath.Glob(filepath.Join(dir, "parent-chart", "charts", "child-chart-*.tgz"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("expected child-chart .tgz in parent-chart/charts/; matches=%v err=%v", matches, err)
	}
}

func TestEnsureDependencies_ContextCancelled(t *testing.T) {
	c, err := New([]byte(stubKubeconfig), "default")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = c.EnsureDependencies(ctx, "testdata/parent-chart")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dst, err)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		if e.IsDir() {
			copyTree(t, s, d)
			continue
		}
		data, err := os.ReadFile(s)
		if err != nil {
			t.Fatalf("read %s: %v", s, err)
		}
		if err := os.WriteFile(d, data, 0o644); err != nil {
			t.Fatalf("write %s: %v", d, err)
		}
	}
}
