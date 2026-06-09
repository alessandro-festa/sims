// Package metrics defines the AMD-prefixed Prometheus gauges the
// metrics-exporter exposes. Metric names mirror the real
// ROCm/device-metrics-exporter so dashboards built for the real exporter
// (e.g. Grafana ID 23434) light up against sims out of the box.
//
// Phase 5 swaps the original Observe-once GaugeVec model for a
// Sampler-driven Collector: every scrape re-reads the cluster's topology
// and emits per-pod label values, so gauges follow pod assignments and
// the sims.io/simulated-gpu-utilization annotation live.
package metrics

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
)

// labelNames is the full per-series label set every gauge carries.
// Phase 5 added pod/namespace/container; Phase 7 #48 added
// partition_mode/partition_id for CPX/SPX emulation. Empty values
// stand in for "not applicable" (e.g., idle GPU has empty pod, SPX
// GPU has partition_id="0").
var labelNames = []string{
	"gpu_id", "serial_number", "card_series", "card_model", "hostname",
	"pod", "namespace", "container",
	"partition_mode", "partition_id",
}

// Snapshot is one row of data the exporter emits per GPU on every
// scrape. The exporter builds it by combining simulate.Sample values
// with topology lookup + pod annotation; the Collector just wires it to
// the Prometheus descriptors.
type Snapshot struct {
	// Identity
	GPUID        string
	SerialNumber string
	CardSeries   string
	CardModel    string
	Hostname     string

	// Assignment (empty for idle GPUs)
	Pod       string
	Namespace string
	Container string

	// CPX/SPX partition labels (#48). PartitionMode is "spx" or "cpx";
	// PartitionID is the partition index within the physical card
	// (always "0" under SPX).
	PartitionMode string
	PartitionID   string

	// Values (mirror simulate.Sample)
	JunctionTemp  float64
	PackagePower  float64
	GfxActivity   float64
	UsedVRAM      float64
	TotalVRAM     float64
	ClockGfx      float64
	Voltage       float64
	FanSpeed      float64
	PCIeBandwidth float64
	Health        int
}

// Sampler returns the current set of Snapshots the Collector should
// emit. Called on every Prometheus scrape; implementations should
// answer in well under the scrape timeout (default 10s).
type Sampler interface {
	Sample(ctx context.Context) []Snapshot
}

// SamplerFunc is the closure variant of Sampler.
type SamplerFunc func(ctx context.Context) []Snapshot

// Sample implements Sampler.
func (f SamplerFunc) Sample(ctx context.Context) []Snapshot { return f(ctx) }

// Collector is the prometheus.Collector implementation. Use New to
// construct one and Registry() to wire it into an HTTP handler.
type Collector struct {
	sampler Sampler

	junctionTemp  *prometheus.Desc
	packagePower  *prometheus.Desc
	gfxActivity   *prometheus.Desc
	usedVRAM      *prometheus.Desc
	totalVRAM     *prometheus.Desc
	health        *prometheus.Desc
	clockGfx      *prometheus.Desc
	voltage       *prometheus.Desc
	fanSpeed      *prometheus.Desc
	pcieBandwidth *prometheus.Desc

	registry *prometheus.Registry
}

// New constructs a Collector wrapping sampler and registers it against
// a fresh Registry.
func New(sampler Sampler) *Collector {
	c := &Collector{
		sampler:       sampler,
		junctionTemp:  desc("amd_gpu_junction_temperature", "GPU junction temperature in degrees Celsius."),
		packagePower:  desc("amd_gpu_package_power", "GPU package power draw in Watts."),
		gfxActivity:   desc("amd_gpu_gfx_activity", "GPU graphics engine activity, percent."),
		usedVRAM:      desc("amd_gpu_used_vram", "GPU used video memory in bytes."),
		totalVRAM:     desc("amd_gpu_total_vram", "GPU total video memory in bytes."),
		health:        desc("amd_gpu_health", "GPU health state (1 = healthy, 0 = unhealthy)."),
		clockGfx:      desc("amd_gpu_clock_gfx", "GPU graphics clock frequency in MHz."),
		voltage:       desc("amd_gpu_voltage", "GPU core voltage in millivolts."),
		fanSpeed:      desc("amd_gpu_fan_speed", "GPU fan speed, percent of max."),
		pcieBandwidth: desc("amd_pcie_bandwidth", "GPU PCIe bandwidth in MB/s."),
	}
	c.registry = prometheus.NewRegistry()
	c.registry.MustRegister(c)
	return c
}

// Registry returns the *prometheus.Registry the exporter exposes over
// /metrics. Holds only this Collector — process metrics live elsewhere.
func (c *Collector) Registry() *prometheus.Registry { return c.registry }

// Describe implements prometheus.Collector.
func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range c.allDescs() {
		ch <- d
	}
}

// Collect implements prometheus.Collector. Calls Sampler.Sample with a
// background context — Prometheus's scrape timeout still bounds the
// HTTP request, so we don't need to plumb a ctx in here.
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	for _, s := range c.sampler.Sample(context.Background()) {
		labels := []string{
			s.GPUID, s.SerialNumber, s.CardSeries, s.CardModel, s.Hostname,
			s.Pod, s.Namespace, s.Container,
			s.PartitionMode, s.PartitionID,
		}
		ch <- prometheus.MustNewConstMetric(c.junctionTemp, prometheus.GaugeValue, s.JunctionTemp, labels...)
		ch <- prometheus.MustNewConstMetric(c.packagePower, prometheus.GaugeValue, s.PackagePower, labels...)
		ch <- prometheus.MustNewConstMetric(c.gfxActivity, prometheus.GaugeValue, s.GfxActivity, labels...)
		ch <- prometheus.MustNewConstMetric(c.usedVRAM, prometheus.GaugeValue, s.UsedVRAM, labels...)
		ch <- prometheus.MustNewConstMetric(c.totalVRAM, prometheus.GaugeValue, s.TotalVRAM, labels...)
		ch <- prometheus.MustNewConstMetric(c.health, prometheus.GaugeValue, float64(s.Health), labels...)
		ch <- prometheus.MustNewConstMetric(c.clockGfx, prometheus.GaugeValue, s.ClockGfx, labels...)
		ch <- prometheus.MustNewConstMetric(c.voltage, prometheus.GaugeValue, s.Voltage, labels...)
		ch <- prometheus.MustNewConstMetric(c.fanSpeed, prometheus.GaugeValue, s.FanSpeed, labels...)
		ch <- prometheus.MustNewConstMetric(c.pcieBandwidth, prometheus.GaugeValue, s.PCIeBandwidth, labels...)
	}
}

func (c *Collector) allDescs() []*prometheus.Desc {
	return []*prometheus.Desc{
		c.junctionTemp, c.packagePower, c.gfxActivity,
		c.usedVRAM, c.totalVRAM, c.health,
		c.clockGfx, c.voltage, c.fanSpeed, c.pcieBandwidth,
	}
}

func desc(name, help string) *prometheus.Desc {
	return prometheus.NewDesc(name, help, labelNames, nil)
}
