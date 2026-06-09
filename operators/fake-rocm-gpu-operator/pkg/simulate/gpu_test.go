package simulate

import (
	"math"
	"math/rand"
	"testing"
)

func TestBuildGPUs_DeterministicIDs(t *testing.T) {
	gpus := BuildGPUs("node-a", "MI300X", 1<<37, 3, PartitionSPX, 1)
	if len(gpus) != 3 {
		t.Fatalf("len = %d, want 3", len(gpus))
	}
	want := []string{"gpu-0", "gpu-1", "gpu-2"}
	for i, g := range gpus {
		if g.ID != want[i] {
			t.Errorf("gpus[%d].ID = %q, want %q", i, g.ID, want[i])
		}
		if g.SerialNumber == "" || g.CardSeries != "MI300X" || g.CardModel != "MI300X" {
			t.Errorf("gpus[%d] missing identity fields: %+v", i, g)
		}
		if g.MemoryTotal != 1<<37 {
			t.Errorf("gpus[%d].MemoryTotal = %d, want %d", i, g.MemoryTotal, int64(1<<37))
		}
		if g.PartitionMode != PartitionSPX || g.PartitionID != "0" {
			t.Errorf("gpus[%d] partition = %s/%s, want spx/0", i, g.PartitionMode, g.PartitionID)
		}
	}
}

func TestBuildGPUs_CPXSplitsAndLabels(t *testing.T) {
	// 2 physical × 4 partitions = 8 logical devices, memory split 4-way.
	gpus := BuildGPUs("node-a", "MI300X", 1000, 2, PartitionCPX, 4)
	if len(gpus) != 8 {
		t.Fatalf("len = %d, want 8 (2 physical × 4 partitions)", len(gpus))
	}
	for _, g := range gpus {
		if g.PartitionMode != PartitionCPX {
			t.Errorf("expected cpx, got %q", g.PartitionMode)
		}
		if g.MemoryTotal != 250 {
			t.Errorf("memory not split: got %d, want 250 (1000/4)", g.MemoryTotal)
		}
	}
	// PartitionID cycles 0..3 twice (once per physical card).
	wantPartIDs := []string{"0", "1", "2", "3", "0", "1", "2", "3"}
	for i, g := range gpus {
		if g.PartitionID != wantPartIDs[i] {
			t.Errorf("gpus[%d].PartitionID = %q, want %q", i, g.PartitionID, wantPartIDs[i])
		}
	}
	// Each physical card's first partition's serial reflects its physical
	// index — gpu-0..3 came from physical 0, gpu-4..7 from physical 1.
	if got := gpus[0].SerialNumber; got != "SIM-node-a-00-p0" {
		t.Errorf("gpus[0].SerialNumber = %q, want SIM-node-a-00-p0", got)
	}
	if got := gpus[4].SerialNumber; got != "SIM-node-a-01-p0" {
		t.Errorf("gpus[4].SerialNumber = %q, want SIM-node-a-01-p0", got)
	}
}

func TestBuildGPUs_UnknownModeFallsBackToSPX(t *testing.T) {
	// CRD validation rejects invalid modes, but defense in depth: any
	// non-cpx value behaves like spx.
	gpus := BuildGPUs("node-a", "MI300X", 1<<37, 2, "garbage", 4)
	if len(gpus) != 2 {
		t.Errorf("unknown mode should fall back to spx (1 per physical), got %d", len(gpus))
	}
	for _, g := range gpus {
		if g.PartitionMode != PartitionSPX || g.PartitionID != "0" {
			t.Errorf("expected spx fallback, got mode=%s id=%s", g.PartitionMode, g.PartitionID)
		}
	}
}

func TestSampleIdle_Baseline(t *testing.T) {
	g := GPU{MemoryTotal: 1000}
	s := g.SampleIdle()

	cases := []struct {
		name string
		got  float64
		want float64
	}{
		{"JunctionTemp", s.JunctionTemp, 35},
		{"PackagePower", s.PackagePower, 30},
		{"GfxActivity", s.GfxActivity, 0},
		{"UsedVRAM", s.UsedVRAM, 0},
		{"TotalVRAM", s.TotalVRAM, 1000},
		{"ClockGfx", s.ClockGfx, 500},
		{"Voltage", s.Voltage, 800},
		{"FanSpeed", s.FanSpeed, 20},
		{"PCIeBandwidth", s.PCIeBandwidth, 100},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	if s.Health != 1 {
		t.Errorf("Health = %d, want 1", s.Health)
	}
}

func TestSampleLoaded_DeterministicForSeed(t *testing.T) {
	g := GPU{MemoryTotal: 1000}

	rng1 := rand.New(rand.NewSource(42))
	rng2 := rand.New(rand.NewSource(42))
	s1 := g.SampleLoaded(60, rng1)
	s2 := g.SampleLoaded(60, rng2)

	if s1 != s2 {
		t.Errorf("same seed produced different samples:\n  %+v\n  %+v", s1, s2)
	}
}

func TestSampleLoaded_Formulas(t *testing.T) {
	g := GPU{MemoryTotal: 1000}
	// Custom RNG that always returns 0.5, so jitter == 0.
	rng := rand.New(rand.NewSource(1))
	// burn calls until the next Float64 == 0.5 isn't worth it; instead test
	// the linear parts of the formulas and assert jitter stays within [-1, 1).
	s := g.SampleLoaded(50, rng)

	if math.Abs(s.JunctionTemp-(40+50*0.55)) > 1.0 {
		t.Errorf("JunctionTemp %v outside expected ±1 of %v", s.JunctionTemp, 40+50*0.55)
	}
	if s.PackagePower != 50+50*2.5 {
		t.Errorf("PackagePower = %v, want %v", s.PackagePower, 50+50*2.5)
	}
	if s.GfxActivity != 50 {
		t.Errorf("GfxActivity = %v, want 50", s.GfxActivity)
	}
	if s.UsedVRAM != (50.0/100)*1000*0.8 {
		t.Errorf("UsedVRAM = %v, want %v", s.UsedVRAM, (50.0/100)*1000*0.8)
	}
	if s.ClockGfx != 500+50*12 {
		t.Errorf("ClockGfx = %v, want %v", s.ClockGfx, 500+50*12)
	}
	if s.Voltage != 800+50*4 {
		t.Errorf("Voltage = %v, want %v", s.Voltage, 800+50*4)
	}
	if s.FanSpeed != 20+50*0.6 {
		t.Errorf("FanSpeed = %v, want %v", s.FanSpeed, 20+50*0.6)
	}
	if s.PCIeBandwidth != 100+50*80 {
		t.Errorf("PCIeBandwidth = %v, want %v", s.PCIeBandwidth, 100+50*80)
	}
	if s.Health != 1 {
		t.Errorf("Health = %d, want 1", s.Health)
	}
}

func TestSampleLoaded_ClampsUtil(t *testing.T) {
	g := GPU{MemoryTotal: 1000}
	rng := rand.New(rand.NewSource(1))

	low := g.SampleLoaded(-10, rng)
	if low.Util != 0 {
		t.Errorf("Util for -10 = %v, want 0 (clamped)", low.Util)
	}

	rng2 := rand.New(rand.NewSource(1))
	high := g.SampleLoaded(150, rng2)
	if high.Util != 100 {
		t.Errorf("Util for 150 = %v, want 100 (clamped)", high.Util)
	}
}
