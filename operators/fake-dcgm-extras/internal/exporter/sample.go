package exporter

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/alessandro-festa/sims/operators/fake-dcgm-extras/pkg/dcgm"
)

// derivative formulas mirror fake-rocm-gpu-operator/pkg/simulate to keep
// AMD + NVIDIA sims visually consistent in mixed dashboards.
const (
	idleTempC          = 30
	tempPerUtil        = 0.55
	tempOffset         = 40
	idlePowerW         = 50
	powerPerUtil       = 2.5
	idleSMClockMHz     = 600
	smClockPerUtil     = 13
	memClockMHz        = 5500 // memory clock stays constant in fake mode
	idlePCIeThroughput = 100
	pciePerUtil        = 80
	idleFanSpeed       = 20
	fanPerUtil         = 0.6
)

// gpuIdentity is one fake GPU slot on the node. Built once at startup.
type gpuIdentity struct {
	Index     int    // 0..N-1
	UUID      string
	Device    string // "nvidia0"
	ModelName string
}

func buildGPUs(hostname, productName string, n int) []gpuIdentity {
	out := make([]gpuIdentity, n)
	for i := range n {
		out[i] = gpuIdentity{
			Index:     i,
			UUID:      fmt.Sprintf("GPU-sim-%s-%02d", hostname, i),
			Device:    fmt.Sprintf("nvidia%d", i),
			ModelName: productName,
		}
	}
	return out
}

// buildSampler returns a dcgm.Sampler that distributes node-level pod
// assignments round-robin across GPU slots: slot i gets assignments[i]
// if it exists, otherwise idle. Pod-to-GPU mapping is best-effort —
// fake-gpu-operator's topology has the same limitation when run as a
// fake (no real device IDs).
func buildSampler(c *cache, gpus []gpuIdentity, hostname string) dcgm.Sampler {
	return dcgm.SamplerFunc(func(_ context.Context) []dcgm.Snapshot {
		assignments := c.Snapshot()
		// Fresh RNG per scrape; values jitter slightly between scrapes
		// without leaking state across them.
		rng := rand.New(rand.NewSource(time.Now().UnixNano())) //nolint:gosec // not security-sensitive
		out := make([]dcgm.Snapshot, 0, len(gpus))
		for i, g := range gpus {
			snap := dcgm.Snapshot{
				GPU:       strconv.Itoa(g.Index),
				UUID:      g.UUID,
				Device:    g.Device,
				ModelName: g.ModelName,
				Hostname:  hostname,
				MemClock:  memClockMHz,
			}
			if i < len(assignments) {
				a := assignments[i]
				util := sampleUtil(a.Util, rng)
				snap.Pod = a.PodName
				snap.Namespace = a.Namespace
				snap.Container = a.Container
				fillLoaded(&snap, util, rng)
			} else {
				fillIdle(&snap)
			}
			out = append(out, snap)
		}
		return out
	})
}

func fillIdle(s *dcgm.Snapshot) {
	s.GPUTemp = idleTempC
	s.PowerUsage = idlePowerW
	s.SMClock = idleSMClockMHz
	s.PCIeTXThroughput = idlePCIeThroughput
	s.PCIeRXThroughput = idlePCIeThroughput
	s.FanSpeed = idleFanSpeed
}

func fillLoaded(s *dcgm.Snapshot, util float64, rng *rand.Rand) {
	jitter := rng.Float64()*2 - 1
	s.GPUTemp = tempOffset + util*tempPerUtil + jitter
	s.PowerUsage = idlePowerW + util*powerPerUtil
	s.SMClock = idleSMClockMHz + util*smClockPerUtil
	s.PCIeTXThroughput = idlePCIeThroughput + util*pciePerUtil
	s.PCIeRXThroughput = idlePCIeThroughput + util*pciePerUtil
	s.FanSpeed = idleFanSpeed + util*fanPerUtil
}

// sampleUtil parses a "low-high" range and samples within. Falls back
// to the default 5-15 range on empty / malformed input.
func sampleUtil(raw string, rng *rand.Rand) float64 {
	low, high := 5.0, 15.0
	if raw != "" {
		parts := strings.SplitN(raw, "-", 2)
		if len(parts) == 2 {
			if l, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64); err == nil {
				if h, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64); err == nil && l >= 0 && h <= 100 && l <= h {
					low, high = l, h
				}
			}
		}
	}
	if low == high {
		return low
	}
	return low + rng.Float64()*(high-low)
}
