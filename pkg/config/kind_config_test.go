package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRender_Golden(t *testing.T) {
	cases := []struct {
		name   string
		opts   Options
		golden string
	}{
		{
			name:   "nvidia_default",
			opts:   Options{Vendor: VendorNVIDIA},
			golden: "nvidia_default.yaml",
		},
		{
			name:   "amd_default",
			opts:   Options{Vendor: VendorAMD},
			golden: "amd_default.yaml",
		},
		{
			name:   "nvidia_taint_4workers",
			opts:   Options{Vendor: VendorNVIDIA, Workers: 4, Taint: true},
			golden: "nvidia_taint_4workers.yaml",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Render(tc.opts)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			goldenPath := filepath.Join("testdata", tc.golden)
			if os.Getenv("UPDATE_GOLDEN") != "" {
				if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
					t.Fatalf("update golden: %v", err)
				}
				t.Logf("updated %s", goldenPath)
				return
			}
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden (re-run with UPDATE_GOLDEN=1 to create): %v", err)
			}
			if string(got) != string(want) {
				t.Errorf("mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}

func TestRender_DefaultName(t *testing.T) {
	out, err := Render(Options{Vendor: VendorNVIDIA})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(string(out), "name: sims-nvidia") {
		t.Errorf("expected default name sims-nvidia, got:\n%s", out)
	}
}

func TestRender_WorkerCount(t *testing.T) {
	out, err := Render(Options{Vendor: VendorAMD, Workers: 5})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := strings.Count(string(out), "role: worker")
	if got != 5 {
		t.Errorf("expected 5 workers, got %d in:\n%s", got, out)
	}
}

func TestRender_NVIDIAEmitsDRA(t *testing.T) {
	out, err := Render(Options{Vendor: VendorNVIDIA})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "DynamicResourceAllocation: true") {
		t.Errorf("NVIDIA config missing DRA feature gate:\n%s", s)
	}
	if !strings.Contains(s, `resource.k8s.io/v1alpha3: "true"`) {
		t.Errorf("NVIDIA config missing DRA runtime config:\n%s", s)
	}
}

func TestRender_AMDOmitsDRA(t *testing.T) {
	out, err := Render(Options{Vendor: VendorAMD})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(string(out), "DynamicResourceAllocation") {
		t.Errorf("AMD config should not include DRA feature gate:\n%s", out)
	}
}

func TestRender_InvalidVendor(t *testing.T) {
	if _, err := Render(Options{Vendor: "intel"}); err == nil {
		t.Fatal("expected error for invalid vendor")
	}
	if _, err := Render(Options{}); err == nil {
		t.Fatal("expected error for empty vendor")
	}
}
