package metrics

import (
	"sort"
	"testing"

	"github.com/alessandro-festa/sims/operators/fake-rocm-gpu-operator/pkg/simulate"
)

func TestNew_RegistersAllGauges(t *testing.T) {
	c := New()

	mfs, err := c.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	got := make([]string, 0, len(mfs))
	for _, m := range mfs {
		got = append(got, m.GetName())
	}
	sort.Strings(got)

	want := []string{
		"amd_gpu_clock_gfx",
		"amd_gpu_fan_speed",
		"amd_gpu_gfx_activity",
		"amd_gpu_health",
		"amd_gpu_junction_temperature",
		"amd_gpu_package_power",
		"amd_gpu_total_vram",
		"amd_gpu_used_vram",
		"amd_gpu_voltage",
		"amd_pcie_bandwidth",
	}
	// New() registers gauges with no series yet, so Gather only sees a
	// metric family if Observe was called for it. Trigger one Observe so we
	// can verify every family registered.
	gpu := simulate.GPU{ID: "gpu-0", SerialNumber: "SIM-x-00", CardSeries: "MI300X", CardModel: "MI300X", MemoryTotal: 1000}
	c.Observe("host", gpu, gpu.SampleIdle())

	mfs, err = c.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather post-Observe: %v", err)
	}
	got = got[:0]
	for _, m := range mfs {
		got = append(got, m.GetName())
	}
	sort.Strings(got)

	if len(got) != len(want) {
		t.Fatalf("metric count = %d, want %d\n got: %v\nwant: %v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("metric[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestObserve_LabelsSet(t *testing.T) {
	c := New()
	gpu := simulate.GPU{ID: "gpu-1", SerialNumber: "SIM-host-01", CardSeries: "MI300X", CardModel: "MI300X", MemoryTotal: 2048}
	c.Observe("host-a", gpu, gpu.SampleIdle())

	mfs, err := c.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}

	wantLabels := map[string]string{
		"gpu_id":        "gpu-1",
		"serial_number": "SIM-host-01",
		"card_series":   "MI300X",
		"card_model":    "MI300X",
		"hostname":      "host-a",
	}

	for _, mf := range mfs {
		if len(mf.Metric) != 1 {
			t.Errorf("%s: got %d series, want 1", mf.GetName(), len(mf.Metric))
			continue
		}
		got := map[string]string{}
		for _, lp := range mf.Metric[0].Label {
			got[lp.GetName()] = lp.GetValue()
		}
		for k, v := range wantLabels {
			if got[k] != v {
				t.Errorf("%s: label %s = %q, want %q", mf.GetName(), k, got[k], v)
			}
		}
	}
}
