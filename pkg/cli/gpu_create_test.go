package cli

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestBuildAMDValues(t *testing.T) {
	v := buildAMDValues(3)
	if v["gpusPerNode"] != 3 {
		t.Errorf("top-level gpusPerNode = %v, want 3", v["gpusPerNode"])
	}
	if cp, _ := v["capacityPatching"].(map[string]any); cp == nil || cp["enabled"] != true {
		t.Errorf("capacityPatching.enabled = %v, want true", v["capacityPatching"])
	}
	sub, ok := v["fake-rocm-gpu-operator"].(map[string]any)
	if !ok {
		t.Fatalf("fake-rocm-gpu-operator not a map: %T", v["fake-rocm-gpu-operator"])
	}
	if sub["gpusPerNode"] != 3 {
		t.Errorf("subchart gpusPerNode = %v, want 3", sub["gpusPerNode"])
	}
}

func TestVendorWiring(t *testing.T) {
	t.Run("amd", func(t *testing.T) {
		release, vb, res := vendorWiring("amd")
		if release != chartReleaseAMD {
			t.Errorf("release = %q, want %q", release, chartReleaseAMD)
		}
		if res != gpuResourceAMD {
			t.Errorf("resource = %q, want %q", res, gpuResourceAMD)
		}
		if vb(2)["gpusPerNode"] != 2 {
			t.Errorf("values builder returned wrong gpusPerNode")
		}
	})
	t.Run("nvidia", func(t *testing.T) {
		release, vb, res := vendorWiring("nvidia")
		if release != chartReleaseNVIDIA {
			t.Errorf("release = %q, want %q", release, chartReleaseNVIDIA)
		}
		if res != gpuResourceNVIDIA {
			t.Errorf("resource = %q, want %q", res, gpuResourceNVIDIA)
		}
		if vb(2)["gpusPerNode"] != 2 {
			t.Errorf("values builder returned wrong gpusPerNode")
		}
	})
}
