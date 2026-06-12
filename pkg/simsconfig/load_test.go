package simsconfig

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_NvidiaH100(t *testing.T) {
	cfg, err := Load(filepath.Join("testdata", "nvidia-h100.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Vendor != "nvidia" {
		t.Errorf("vendor = %q, want nvidia", cfg.Vendor)
	}
	if cfg.GPU.Family != "H100" {
		t.Errorf("family = %q, want H100", cfg.GPU.Family)
	}
	if cfg.GPU.PerWorker != 4 {
		t.Errorf("perWorker = %d, want 4", cfg.GPU.PerWorker)
	}
	if cfg.GPU.MemoryBytes != 80*GiB {
		t.Errorf("memoryBytes = %d, want %d", cfg.GPU.MemoryBytes, 80*GiB)
	}
	if cfg.Workers != 2 {
		t.Errorf("workers = %d, want 2 (default)", cfg.Workers)
	}
	if !cfg.Monitoring {
		t.Error("monitoring should be true")
	}
}

func TestLoad_NvidiaT4MIG(t *testing.T) {
	cfg, err := Load(filepath.Join("testdata", "nvidia-t4-mig.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Workers != 3 {
		t.Errorf("workers = %d, want 3", cfg.Workers)
	}
	if cfg.GPU.Family != "T4" {
		t.Errorf("family = %q, want T4", cfg.GPU.Family)
	}
	if cfg.GPU.Features.MIG != "1g.10gb" {
		t.Errorf("mig = %q, want 1g.10gb", cfg.GPU.Features.MIG)
	}
	if cfg.GPU.MemoryBytes != 16*GiB {
		t.Errorf("memoryBytes = %d, want %d", cfg.GPU.MemoryBytes, 16*GiB)
	}
}

func TestLoad_AMDMI300X(t *testing.T) {
	cfg, err := Load(filepath.Join("testdata", "amd-mi300x.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Vendor != "amd" {
		t.Errorf("vendor = %q, want amd", cfg.Vendor)
	}
	if cfg.GPU.MemoryBytes != 192*GiB {
		t.Errorf("memoryBytes = %d, want %d", cfg.GPU.MemoryBytes, 192*GiB)
	}
	if cfg.GPU.PerWorker != 2 {
		t.Errorf("perWorker = %d, want 2 (default)", cfg.GPU.PerWorker)
	}
}

func TestLoad_AMDMI300X_CPX(t *testing.T) {
	cfg, err := Load(filepath.Join("testdata", "amd-mi300x-cpx.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Workers != 4 {
		t.Errorf("workers = %d, want 4", cfg.Workers)
	}
	if cfg.GPU.Features.Partition.Mode != "cpx" {
		t.Errorf("partition.mode = %q, want cpx", cfg.GPU.Features.Partition.Mode)
	}
	if cfg.GPU.Features.Partition.Count != 8 {
		t.Errorf("partition.count = %d, want 8", cfg.GPU.Features.Partition.Count)
	}
	if cfg.Workload.DefaultUtilization != "40-60" {
		t.Errorf("defaultUtilization = %q, want 40-60", cfg.Workload.DefaultUtilization)
	}
	if !cfg.Monitoring {
		t.Error("monitoring should be true")
	}
}

func TestLoad_BadAPIVersion(t *testing.T) {
	_, err := Load(filepath.Join("testdata", "bad-apiversion.yaml"))
	if err == nil {
		t.Fatal("expected error for bad apiVersion")
	}
	if !strings.Contains(err.Error(), "apiVersion") {
		t.Errorf("error should mention apiVersion: %v", err)
	}
}

func TestLoad_BadVendor(t *testing.T) {
	_, err := Load(filepath.Join("testdata", "bad-vendor.yaml"))
	if err == nil {
		t.Fatal("expected error for bad vendor")
	}
	if !strings.Contains(err.Error(), "vendor") {
		t.Errorf("error should mention vendor: %v", err)
	}
}

func TestLoad_BadFamilyMismatch(t *testing.T) {
	_, err := Load(filepath.Join("testdata", "bad-family-mismatch.yaml"))
	if err == nil {
		t.Fatal("expected error for family/vendor mismatch")
	}
	if !strings.Contains(err.Error(), "MI300X") {
		t.Errorf("error should mention MI300X: %v", err)
	}
}

func TestParse_MIGOnAMD(t *testing.T) {
	data := []byte(`apiVersion: sims.io/v1
kind: SimsConfig
vendor: amd
gpu:
  family: MI300X
  features:
    mig: 1g.10gb
`)
	_, err := Parse(data)
	if err == nil {
		t.Fatal("expected error for MIG on AMD")
	}
	if !strings.Contains(err.Error(), "NVIDIA-only") {
		t.Errorf("error should mention NVIDIA-only: %v", err)
	}
}

func TestParse_PartitionOnNVIDIA(t *testing.T) {
	data := []byte(`apiVersion: sims.io/v1
kind: SimsConfig
vendor: nvidia
gpu:
  family: H100
  features:
    partition:
      mode: cpx
      count: 4
`)
	_, err := Parse(data)
	if err == nil {
		t.Fatal("expected error for partition on NVIDIA")
	}
	if !strings.Contains(err.Error(), "AMD-only") {
		t.Errorf("error should mention AMD-only: %v", err)
	}
}

func TestParse_BadUtilRange(t *testing.T) {
	data := []byte(`apiVersion: sims.io/v1
kind: SimsConfig
vendor: nvidia
gpu:
  family: H100
workload:
  defaultUtilization: "80-50"
`)
	_, err := Parse(data)
	if err == nil {
		t.Fatal("expected error for reversed util range")
	}
	if !strings.Contains(err.Error(), "low bound") {
		t.Errorf("error should mention low bound: %v", err)
	}
}

func TestParse_BadPartitionCount(t *testing.T) {
	data := []byte(`apiVersion: sims.io/v1
kind: SimsConfig
vendor: amd
gpu:
  family: MI300X
  features:
    partition:
      mode: cpx
      count: 16
`)
	_, err := Parse(data)
	if err == nil {
		t.Fatal("expected error for partition count > 8")
	}
	if !strings.Contains(err.Error(), "1-8") {
		t.Errorf("error should mention 1-8: %v", err)
	}
}

func TestApplyFamilyDefaults_UserOverride(t *testing.T) {
	cfg := &SimsConfig{
		Vendor:  "nvidia",
		Workers: 8,
		GPU: GPU{
			Family:      "H100",
			PerWorker:   4,
			MemoryBytes: 42,
		},
	}
	ApplyFamilyDefaults(cfg)
	if cfg.Workers != 8 {
		t.Errorf("workers should stay 8, got %d", cfg.Workers)
	}
	if cfg.GPU.PerWorker != 4 {
		t.Errorf("perWorker should stay 4, got %d", cfg.GPU.PerWorker)
	}
	if cfg.GPU.MemoryBytes != 42 {
		t.Errorf("memoryBytes should stay 42 (user override), got %d", cfg.GPU.MemoryBytes)
	}
}
