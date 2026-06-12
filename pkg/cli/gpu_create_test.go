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

func TestBuildNVIDIAValues_NoConfig(t *testing.T) {
	v := buildNVIDIAValues(&createOpts{gpusPerWorker: 4})
	if v["gpusPerNode"] != 4 {
		t.Errorf("top-level gpusPerNode = %v, want 4", v["gpusPerNode"])
	}
	if _, ok := v["gpuProduct"]; ok {
		t.Error("gpuProduct should not be set without config")
	}
	sub := v["fake-gpu-operator"].(map[string]any)
	pool := sub["topology"].(map[string]any)["nodePools"].(map[string]any)["default"].(map[string]any)
	if pool["gpuCount"] != 4 {
		t.Errorf("nodePool gpuCount = %v, want 4", pool["gpuCount"])
	}
}

func TestBuildNVIDIAValues_WithFamily(t *testing.T) {
	v := buildNVIDIAValues(&createOpts{
		gpusPerWorker:  2,
		productName:    "H100",
		gpuMemoryBytes: 80 * (1 << 30),
	})
	if v["gpuProduct"] != "H100" {
		t.Errorf("gpuProduct = %v, want H100", v["gpuProduct"])
	}
	if v["gpuMemory"] != int64(81920) {
		t.Errorf("gpuMemory = %v, want 81920", v["gpuMemory"])
	}
	pool := v["fake-gpu-operator"].(map[string]any)["topology"].(map[string]any)["nodePools"].(map[string]any)["default"].(map[string]any)
	if pool["gpuProduct"] != "H100" {
		t.Errorf("pool gpuProduct = %v, want H100", pool["gpuProduct"])
	}
	dcgm := v["fake-dcgm-extras"].(map[string]any)
	if dcgm["productName"] != "H100" {
		t.Errorf("dcgm productName = %v, want H100", dcgm["productName"])
	}
}

func TestBuildNVIDIAValues_SpaceToDash(t *testing.T) {
	v := buildNVIDIAValues(&createOpts{
		gpusPerWorker:  2,
		productName:    "Tesla T4",
		gpuMemoryBytes: 16 * (1 << 30),
	})
	if v["gpuProduct"] != "Tesla-T4" {
		t.Errorf("gpuProduct = %v, want Tesla-T4", v["gpuProduct"])
	}
	dcgm := v["fake-dcgm-extras"].(map[string]any)
	if dcgm["productName"] != "Tesla T4" {
		t.Errorf("dcgm productName = %v, want 'Tesla T4' (with space)", dcgm["productName"])
	}
}

func TestBuildNVIDIAValues_MIGProfile(t *testing.T) {
	v := buildNVIDIAValues(&createOpts{
		gpusPerWorker:  2,
		productName:    "H100",
		gpuMemoryBytes: 80 * (1 << 30),
		migProfile:     "1g.10gb",
	})
	dcgm := v["fake-dcgm-extras"].(map[string]any)
	if dcgm["migProfile"] != "1g.10gb" {
		t.Errorf("dcgm migProfile = %v, want 1g.10gb", dcgm["migProfile"])
	}
}

func TestBuildAMDValues_NoConfig(t *testing.T) {
	v := buildAMDValues(&createOpts{gpusPerWorker: 3})
	if v["gpusPerNode"] != 3 {
		t.Errorf("top-level gpusPerNode = %v, want 3", v["gpusPerNode"])
	}
	if _, present := v["capacityPatching"]; present {
		t.Errorf("buildAMDValues should not override capacityPatching; got %v", v["capacityPatching"])
	}
	if _, present := v["productName"]; present {
		t.Error("productName should not be set without config")
	}
	sub := v["fake-rocm-gpu-operator"].(map[string]any)
	if sub["gpusPerNode"] != 3 {
		t.Errorf("subchart gpusPerNode = %v, want 3", sub["gpusPerNode"])
	}
}

func TestBuildAMDValues_WithFamilyAndPartition(t *testing.T) {
	v := buildAMDValues(&createOpts{
		gpusPerWorker:  4,
		productName:    "MI300X",
		gpuMemoryBytes: 192 * (1 << 30),
		partitionMode:  "cpx",
		partitionCount: 4,
	})
	if v["productName"] != "MI300X" {
		t.Errorf("productName = %v, want MI300X", v["productName"])
	}
	if v["gpuMemoryBytes"] != int64(192*(1<<30)) {
		t.Errorf("gpuMemoryBytes = %v, want %d", v["gpuMemoryBytes"], 192*(1<<30))
	}
	sub := v["fake-rocm-gpu-operator"].(map[string]any)
	if sub["productName"] != "MI300X" {
		t.Errorf("subchart productName = %v, want MI300X", sub["productName"])
	}
	cp := sub["computePartition"].(map[string]any)
	if cp["mode"] != "cpx" {
		t.Errorf("partition mode = %v, want cpx", cp["mode"])
	}
	if cp["count"] != 4 {
		t.Errorf("partition count = %v, want 4", cp["count"])
	}
}

func TestBuildAMDValues_DefaultUtilization(t *testing.T) {
	v := buildAMDValues(&createOpts{
		gpusPerWorker:      2,
		defaultUtilization: "40-60",
	})
	sub := v["fake-rocm-gpu-operator"].(map[string]any)
	if sub["defaultUtilization"] != "40-60" {
		t.Errorf("defaultUtilization = %v, want 40-60", sub["defaultUtilization"])
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
		v := vb(&createOpts{gpusPerWorker: 2})
		if v["gpusPerNode"] != 2 {
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
		v := vb(&createOpts{gpusPerWorker: 2})
		if v["gpusPerNode"] != 2 {
			t.Errorf("values builder returned wrong gpusPerNode")
		}
	})
}
