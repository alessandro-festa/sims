// Package annotations parses sims-specific pod annotations. The only
// annotation Phase 5 cares about is sims.io/simulated-gpu-utilization,
// which lets a pod declare a utilization range the metrics-exporter
// samples within.
package annotations

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
)

// UtilizationAnnotation is the pod annotation key sims reads to drive
// the load-derived gauge values (gpu_gfx_busy_instantaneous, temperature,
// power, etc.). Mirrors run-ai/fake-gpu-operator's run.ai/simulated-gpu-
// utilization key one-for-one for cross-vendor parity.
const UtilizationAnnotation = "sims.io/simulated-gpu-utilization"

// DefaultUtilizationRange is what the exporter samples within when a pod
// requests amd.com/gpu but doesn't carry UtilizationAnnotation.
const DefaultUtilizationRange = "5-15"

// Range is an inclusive [Low, High] utilization band in percent (0-100).
type Range struct {
	Low  float64
	High float64
}

// ParseUtilization decodes a "low-high" annotation value into a Range.
// Falls back to DefaultUtilizationRange on empty input; returns an error
// for malformed input (callers can choose to default or surface).
func ParseUtilization(s string) (Range, error) {
	if s == "" {
		s = DefaultUtilizationRange
	}
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return Range{}, fmt.Errorf("expected 'low-high', got %q", s)
	}
	low, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return Range{}, fmt.Errorf("parse low: %w", err)
	}
	high, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		return Range{}, fmt.Errorf("parse high: %w", err)
	}
	if low < 0 || high > 100 || low > high {
		return Range{}, fmt.Errorf("range out of bounds or inverted: low=%v high=%v", low, high)
	}
	return Range{Low: low, High: high}, nil
}

// Sample returns a value in [r.Low, r.High] using the provided RNG.
// When Low == High, returns Low exactly.
func (r Range) Sample(rng *rand.Rand) float64 {
	if r.High == r.Low {
		return r.Low
	}
	return r.Low + rng.Float64()*(r.High-r.Low)
}
