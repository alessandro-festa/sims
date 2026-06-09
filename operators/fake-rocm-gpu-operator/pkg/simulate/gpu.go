// Package simulate produces idle-baseline and load-driven values for the
// AMD-shaped GPU gauges exposed by the metrics-exporter. Deterministic for a
// given RNG seed so unit tests can pin exact values.
package simulate

import (
	"fmt"
	"math/rand"
	"strconv"
)

// Partition modes for CPX/SPX emulation (#48). Stored as strings so we can
// surface them directly through metric labels.
const (
	PartitionSPX = "spx" // 1 logical device per physical GPU (default)
	PartitionCPX = "cpx" // Count logical devices per physical GPU
)

// GPU is the per-card identity surfaced through metric labels. MemoryTotal is
// constant for the life of the process and feeds both the total_vram gauge and
// the used_vram derivation under load.
//
// Under CPX, each "GPU" here is one partition (XCD slice) of a physical card;
// PartitionMode and PartitionID identify the slice. Under SPX, PartitionMode
// is "spx" and PartitionID is "0".
type GPU struct {
	ID            string
	SerialNumber  string
	CardSeries    string
	CardModel     string
	MemoryTotal   int64
	PartitionMode string // "spx" or "cpx"
	PartitionID   string // partition index within physical GPU (0..count-1); always "0" under SPX
}

// Sample is one snapshot of all the values the metrics-exporter publishes for
// a single GPU. Field names match the gauge they feed, modulo the amd_gpu_
// prefix on the metric side.
type Sample struct {
	Util          float64 // 0-100, the input that drives every load-derived field
	JunctionTemp  float64 // °C
	PackagePower  float64 // W
	GfxActivity   float64 // %
	UsedVRAM      float64 // bytes
	TotalVRAM     float64 // bytes (== GPU.MemoryTotal, copied here for convenience)
	ClockGfx      float64 // MHz
	Voltage       float64 // mV
	FanSpeed      float64 // %
	PCIeBandwidth float64 // MB/s
	Health        int     // 0 unhealthy, 1 healthy
}

// BuildGPUs returns physicalCount * effective(partitions) GPUs with
// deterministic IDs and serials derived from hostname. Each GPU's
// PartitionMode/PartitionID labels reflect its position within the partition
// scheme:
//
//   - SPX (or unrecognised mode): 1 GPU per physical card, PartitionID="0".
//   - CPX with partitionCount=N: N logical GPUs per physical card,
//     PartitionID="0".."N-1". MemoryTotal is split evenly across partitions
//     so the per-device total_vram surface mirrors real hardware.
func BuildGPUs(hostname, productName string, memoryBytes int64, physicalCount int, partitionMode string, partitionCount int) []GPU {
	if partitionMode != PartitionCPX {
		partitionMode = PartitionSPX
		partitionCount = 1
	}
	if partitionCount < 1 {
		partitionCount = 1
	}
	perPartitionMemory := memoryBytes / int64(partitionCount)

	gpus := make([]GPU, 0, physicalCount*partitionCount)
	for phys := range physicalCount {
		for part := range partitionCount {
			global := phys*partitionCount + part
			gpus = append(gpus, GPU{
				ID:            fmt.Sprintf("gpu-%d", global),
				SerialNumber:  fmt.Sprintf("SIM-%s-%02d-p%d", hostname, phys, part),
				CardSeries:    productName,
				CardModel:     productName,
				MemoryTotal:   perPartitionMemory,
				PartitionMode: partitionMode,
				PartitionID:   strconv.Itoa(part),
			})
		}
	}
	return gpus
}

// SampleIdle returns the baseline values an unused GPU reports. No RNG; pure
// function of g.
func (g GPU) SampleIdle() Sample {
	return Sample{
		Util:          0,
		JunctionTemp:  35,
		PackagePower:  30,
		GfxActivity:   0,
		UsedVRAM:      0,
		TotalVRAM:     float64(g.MemoryTotal),
		ClockGfx:      500,
		Voltage:       800,
		FanSpeed:      20,
		PCIeBandwidth: 100,
		Health:        1,
	}
}

// SampleLoaded returns values derived from a non-zero utilization, with small
// jitter on temperature so adjacent scrapes don't show a frozen value. rng
// MUST be non-nil; callers seed it deterministically in tests.
func (g GPU) SampleLoaded(util float64, rng *rand.Rand) Sample {
	if util < 0 {
		util = 0
	}
	if util > 100 {
		util = 100
	}
	jitter := rng.Float64()*2 - 1 // [-1, 1)
	return Sample{
		Util:          util,
		JunctionTemp:  40 + util*0.55 + jitter,
		PackagePower:  50 + util*2.5,
		GfxActivity:   util,
		UsedVRAM:      (util / 100) * float64(g.MemoryTotal) * 0.8,
		TotalVRAM:     float64(g.MemoryTotal),
		ClockGfx:      500 + util*12,
		Voltage:       800 + util*4,
		FanSpeed:      20 + util*0.6,
		PCIeBandwidth: 100 + util*80,
		Health:        1,
	}
}
