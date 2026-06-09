// Package dcgm defines the DCGM_FI_DEV_* gauges this sidecar exposes
// to backfill what run-ai/fake-gpu-operator doesn't emit. Mirrors the
// Sampler-based Collector pattern in fake-rocm-gpu-operator's pkg/metrics
// so future refactors can lift shared bits up if it makes sense.
//
// Metric names match NVIDIA's DCGM exporter (k8s-device-plugin's
// dcgm-exporter) so the upstream Grafana dashboard (ID 12239) and any
// dashboard built against real DCGM lights up against sims.
package dcgm

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
)

// labelNames is the per-series label set every gauge carries. Mirrors
// what dcgm-exporter emits in production. Pod-level labels are empty
// for idle GPUs.
var labelNames = []string{
	"gpu", "UUID", "device", "modelName", "Hostname",
	"pod", "namespace", "container",
}

// Snapshot is one row of data the exporter emits per GPU on every
// scrape. The exporter builds it from pod annotations + idle baselines
// (see internal/exporter).
type Snapshot struct {
	// Identity
	GPU       string // "0", "1", ...
	UUID      string // "GPU-<random>" (deterministic for stability)
	Device    string // "nvidia0", "nvidia1", ...
	ModelName string // "Tesla T4"
	Hostname  string

	// Assignment (empty for idle GPUs)
	Pod       string
	Namespace string
	Container string

	// Values
	GPUTemp          float64 // °C
	PowerUsage       float64 // W
	SMClock          float64 // MHz
	MemClock         float64 // MHz
	PCIeTXThroughput float64 // KB/s
	PCIeRXThroughput float64 // KB/s
	FanSpeed         float64 // %
	ECCSBETotal      float64 // counter-as-gauge (idle = 0)
	ECCDBETotal      float64 // counter-as-gauge (idle = 0)
	TensorPipeActive float64 // [0, 1] fraction of cycles tensor cores active
}

// Sampler returns the current Snapshots the Collector should emit.
type Sampler interface {
	Sample(ctx context.Context) []Snapshot
}

// SamplerFunc is the closure variant of Sampler.
type SamplerFunc func(ctx context.Context) []Snapshot

// Sample implements Sampler.
func (f SamplerFunc) Sample(ctx context.Context) []Snapshot { return f(ctx) }

// Collector implements prometheus.Collector. Construct with New and
// expose Registry() over /metrics.
type Collector struct {
	sampler Sampler

	gpuTemp     *prometheus.Desc
	powerUsage  *prometheus.Desc
	smClock     *prometheus.Desc
	memClock    *prometheus.Desc
	pcieTX      *prometheus.Desc
	pcieRX      *prometheus.Desc
	fanSpeed         *prometheus.Desc
	eccSBETotal      *prometheus.Desc
	eccDBETotal      *prometheus.Desc
	tensorPipeActive *prometheus.Desc

	registry *prometheus.Registry
}

// New builds a Collector wrapping sampler and registers it against a
// fresh Registry.
func New(sampler Sampler) *Collector {
	c := &Collector{
		sampler:     sampler,
		gpuTemp:     desc("DCGM_FI_DEV_GPU_TEMP", "GPU temperature in degrees Celsius."),
		powerUsage:  desc("DCGM_FI_DEV_POWER_USAGE", "Power draw in Watts."),
		smClock:     desc("DCGM_FI_DEV_SM_CLOCK", "SM (graphics) clock in MHz."),
		memClock:    desc("DCGM_FI_DEV_MEM_CLOCK", "Memory clock in MHz."),
		pcieTX:      desc("DCGM_FI_DEV_PCIE_TX_THROUGHPUT", "PCIe transmit throughput in KB/s."),
		pcieRX:      desc("DCGM_FI_DEV_PCIE_RX_THROUGHPUT", "PCIe receive throughput in KB/s."),
		fanSpeed:         desc("DCGM_FI_DEV_FAN_SPEED", "Fan speed as percent of max."),
		eccSBETotal:      desc("DCGM_FI_DEV_ECC_SBE_VOL_TOTAL", "ECC single-bit volatile errors total (cumulative)."),
		eccDBETotal:      desc("DCGM_FI_DEV_ECC_DBE_VOL_TOTAL", "ECC double-bit volatile errors total (cumulative)."),
		tensorPipeActive: desc("DCGM_FI_PROF_PIPE_TENSOR_ACTIVE", "Fraction of cycles the tensor (HMMA) pipe was active, [0, 1]."),
	}
	c.registry = prometheus.NewRegistry()
	c.registry.MustRegister(c)
	return c
}

// Registry returns the *prometheus.Registry the exporter exposes.
func (c *Collector) Registry() *prometheus.Registry { return c.registry }

// Describe implements prometheus.Collector.
func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range c.allDescs() {
		ch <- d
	}
}

// Collect implements prometheus.Collector. Calls Sampler.Sample then
// translates each Snapshot into one Metric per gauge.
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	for _, s := range c.sampler.Sample(context.Background()) {
		labels := []string{s.GPU, s.UUID, s.Device, s.ModelName, s.Hostname, s.Pod, s.Namespace, s.Container}
		ch <- prometheus.MustNewConstMetric(c.gpuTemp, prometheus.GaugeValue, s.GPUTemp, labels...)
		ch <- prometheus.MustNewConstMetric(c.powerUsage, prometheus.GaugeValue, s.PowerUsage, labels...)
		ch <- prometheus.MustNewConstMetric(c.smClock, prometheus.GaugeValue, s.SMClock, labels...)
		ch <- prometheus.MustNewConstMetric(c.memClock, prometheus.GaugeValue, s.MemClock, labels...)
		ch <- prometheus.MustNewConstMetric(c.pcieTX, prometheus.GaugeValue, s.PCIeTXThroughput, labels...)
		ch <- prometheus.MustNewConstMetric(c.pcieRX, prometheus.GaugeValue, s.PCIeRXThroughput, labels...)
		ch <- prometheus.MustNewConstMetric(c.fanSpeed, prometheus.GaugeValue, s.FanSpeed, labels...)
		ch <- prometheus.MustNewConstMetric(c.eccSBETotal, prometheus.GaugeValue, s.ECCSBETotal, labels...)
		ch <- prometheus.MustNewConstMetric(c.eccDBETotal, prometheus.GaugeValue, s.ECCDBETotal, labels...)
		ch <- prometheus.MustNewConstMetric(c.tensorPipeActive, prometheus.GaugeValue, s.TensorPipeActive, labels...)
	}
}

func (c *Collector) allDescs() []*prometheus.Desc {
	return []*prometheus.Desc{
		c.gpuTemp, c.powerUsage, c.smClock, c.memClock,
		c.pcieTX, c.pcieRX, c.fanSpeed,
		c.eccSBETotal, c.eccDBETotal, c.tensorPipeActive,
	}
}

func desc(name, help string) *prometheus.Desc {
	return prometheus.NewDesc(name, help, labelNames, nil)
}
