// Package metrics defines the AMD-prefixed Prometheus gauges the
// metrics-exporter exposes. Metric names mirror the real
// ROCm/device-metrics-exporter so dashboards built for the real exporter
// (e.g. Grafana ID 23434) light up against sims out of the box.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/alessandro-festa/sims/operators/fake-rocm-gpu-operator/pkg/simulate"
)

// labelNames is the per-series label set every gauge carries in Phase 3.
// Pod-level labels (pod, namespace, container) land in Phase 5 alongside the
// status-updater.
var labelNames = []string{"gpu_id", "serial_number", "card_series", "card_model", "hostname"}

// Collectors bundles the ten AMD-shaped gauges so the exporter and tests can
// share one registry.
type Collectors struct {
	JunctionTemperature *prometheus.GaugeVec
	PackagePower        *prometheus.GaugeVec
	GfxActivity         *prometheus.GaugeVec
	UsedVRAM            *prometheus.GaugeVec
	TotalVRAM           *prometheus.GaugeVec
	Health              *prometheus.GaugeVec
	ClockGfx            *prometheus.GaugeVec
	Voltage             *prometheus.GaugeVec
	FanSpeed            *prometheus.GaugeVec
	PCIeBandwidth       *prometheus.GaugeVec

	registry *prometheus.Registry
}

// New constructs a Collectors with all ten gauges registered against a fresh
// Registry. Callers expose the registry over /metrics; tests inspect it
// directly via Gather().
func New() *Collectors {
	c := &Collectors{
		JunctionTemperature: gauge("amd_gpu_junction_temperature", "GPU junction temperature in degrees Celsius."),
		PackagePower:        gauge("amd_gpu_package_power", "GPU package power draw in Watts."),
		GfxActivity:         gauge("amd_gpu_gfx_activity", "GPU graphics engine activity, percent."),
		UsedVRAM:            gauge("amd_gpu_used_vram", "GPU used video memory in bytes."),
		TotalVRAM:           gauge("amd_gpu_total_vram", "GPU total video memory in bytes."),
		Health:              gauge("amd_gpu_health", "GPU health state (1 = healthy, 0 = unhealthy)."),
		ClockGfx:            gauge("amd_gpu_clock_gfx", "GPU graphics clock frequency in MHz."),
		Voltage:             gauge("amd_gpu_voltage", "GPU core voltage in millivolts."),
		FanSpeed:            gauge("amd_gpu_fan_speed", "GPU fan speed, percent of max."),
		PCIeBandwidth:       gauge("amd_pcie_bandwidth", "GPU PCIe bandwidth in MB/s."),
	}

	c.registry = prometheus.NewRegistry()
	for _, g := range c.all() {
		c.registry.MustRegister(g)
	}
	return c
}

// Registry returns the *prometheus.Registry holding every gauge in this set.
func (c *Collectors) Registry() *prometheus.Registry { return c.registry }

// Observe writes one sample to every gauge for the given GPU and hostname.
// Callers invoke it for every GPU on every scrape (Phase 3 has no caching
// layer).
func (c *Collectors) Observe(hostname string, g simulate.GPU, s simulate.Sample) {
	lbls := prometheus.Labels{
		"gpu_id":        g.ID,
		"serial_number": g.SerialNumber,
		"card_series":   g.CardSeries,
		"card_model":    g.CardModel,
		"hostname":      hostname,
	}
	c.JunctionTemperature.With(lbls).Set(s.JunctionTemp)
	c.PackagePower.With(lbls).Set(s.PackagePower)
	c.GfxActivity.With(lbls).Set(s.GfxActivity)
	c.UsedVRAM.With(lbls).Set(s.UsedVRAM)
	c.TotalVRAM.With(lbls).Set(s.TotalVRAM)
	c.Health.With(lbls).Set(float64(s.Health))
	c.ClockGfx.With(lbls).Set(s.ClockGfx)
	c.Voltage.With(lbls).Set(s.Voltage)
	c.FanSpeed.With(lbls).Set(s.FanSpeed)
	c.PCIeBandwidth.With(lbls).Set(s.PCIeBandwidth)
}

func (c *Collectors) all() []*prometheus.GaugeVec {
	return []*prometheus.GaugeVec{
		c.JunctionTemperature, c.PackagePower, c.GfxActivity,
		c.UsedVRAM, c.TotalVRAM, c.Health,
		c.ClockGfx, c.Voltage, c.FanSpeed, c.PCIeBandwidth,
	}
}

func gauge(name, help string) *prometheus.GaugeVec {
	return prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: name, Help: help}, labelNames)
}
