package cli

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCreate_RejectsAMD(t *testing.T) {
	err := runCreate(context.Background(), io.Discard, &createOpts{vendor: "amd"})
	if err == nil || !strings.Contains(err.Error(), "Phase 3+") {
		t.Fatalf("expected Phase 3+ rejection, got: %v", err)
	}
}

func TestRunCreate_RejectsMonitoring(t *testing.T) {
	err := runCreate(context.Background(), io.Discard, &createOpts{vendor: "nvidia", withMonitoring: true})
	if err == nil || !strings.Contains(err.Error(), "Phase 2") {
		t.Fatalf("expected Phase 2 rejection, got: %v", err)
	}
}

func TestRunCreate_RejectsInvalidVendor(t *testing.T) {
	err := runCreate(context.Background(), io.Discard, &createOpts{vendor: "xyz"})
	if err == nil || !strings.Contains(err.Error(), "invalid --vendor") {
		t.Fatalf("expected invalid vendor error, got: %v", err)
	}
}

func TestResolveChartDir(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		t.Setenv(chartDirEnvVar, "")
		got := resolveChartDir("sims-nvidia")
		want := filepath.Join("charts", "sims-nvidia")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("env override", func(t *testing.T) {
		t.Setenv(chartDirEnvVar, "/opt/sims/charts")
		got := resolveChartDir("sims-nvidia")
		want := filepath.Join("/opt/sims/charts", "sims-nvidia")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestBuildNVIDIAValues(t *testing.T) {
	v := buildNVIDIAValues(4)
	if v["gpusPerNode"] != 4 {
		t.Errorf("top-level gpusPerNode = %v, want 4", v["gpusPerNode"])
	}
	sub, ok := v["fake-gpu-operator"].(map[string]any)
	if !ok {
		t.Fatalf("fake-gpu-operator not a map: %T", v["fake-gpu-operator"])
	}
	pool := sub["topology"].(map[string]any)["nodePools"].(map[string]any)["default"].(map[string]any)
	if pool["gpuCount"] != 4 {
		t.Errorf("nodePool gpuCount = %v, want 4", pool["gpuCount"])
	}
}
