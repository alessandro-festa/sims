package metrics

import (
	"context"
	"sort"
	"testing"

	dto "github.com/prometheus/client_model/go"
)

func TestCollector_EmitsAllMetricFamilies(t *testing.T) {
	c := New(SamplerFunc(func(_ context.Context) []Snapshot {
		return []Snapshot{
			{
				GPUID: "gpu-0", SerialNumber: "SIM-h-00", CardSeries: "MI300X", CardModel: "MI300X", Hostname: "h",
				Pod: "", Namespace: "", Container: "",
				JunctionTemp: 35, PackagePower: 30, GfxActivity: 0,
				UsedVRAM: 0, TotalVRAM: 1000, Health: 1,
				ClockGfx: 500, Voltage: 800, FanSpeed: 20, PCIeBandwidth: 100,
			},
		}
	}))

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
	if len(got) != len(want) {
		t.Fatalf("metric count = %d, want %d\ngot:  %v\nwant: %v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("metric[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCollector_LabelSet(t *testing.T) {
	c := New(SamplerFunc(func(_ context.Context) []Snapshot {
		return []Snapshot{
			{
				GPUID: "gpu-1", SerialNumber: "SIM-host-01",
				CardSeries: "MI300X", CardModel: "MI300X", Hostname: "host-a",
				Pod: "foo", Namespace: "team-x", Container: "payload",
				JunctionTemp: 60, TotalVRAM: 2048, Health: 1,
			},
		}
	}))

	wantLabels := map[string]string{
		"gpu_id":        "gpu-1",
		"serial_number": "SIM-host-01",
		"card_series":   "MI300X",
		"card_model":    "MI300X",
		"hostname":      "host-a",
		"pod":           "foo",
		"namespace":     "team-x",
		"container":     "payload",
	}

	mfs, err := c.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	for _, mf := range mfs {
		if len(mf.Metric) != 1 {
			t.Errorf("%s: got %d series, want 1", mf.GetName(), len(mf.Metric))
			continue
		}
		if diff := labelDiff(mf.Metric[0], wantLabels); diff != "" {
			t.Errorf("%s: %s", mf.GetName(), diff)
		}
	}
}

func TestCollector_IdleGPU_EmptyAssignmentLabels(t *testing.T) {
	c := New(SamplerFunc(func(_ context.Context) []Snapshot {
		return []Snapshot{
			{GPUID: "gpu-0", SerialNumber: "SIM-h-00", CardSeries: "MI300X", CardModel: "MI300X", Hostname: "h", TotalVRAM: 1000, Health: 1},
		}
	}))
	mfs, err := c.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	for _, mf := range mfs {
		for _, lp := range mf.Metric[0].Label {
			switch lp.GetName() {
			case "pod", "namespace", "container":
				if lp.GetValue() != "" {
					t.Errorf("%s: idle label %s = %q, want empty", mf.GetName(), lp.GetName(), lp.GetValue())
				}
			}
		}
	}
}

func labelDiff(m *dto.Metric, want map[string]string) string {
	got := map[string]string{}
	for _, lp := range m.Label {
		got[lp.GetName()] = lp.GetValue()
	}
	for k, v := range want {
		if got[k] != v {
			return "label " + k + " = " + got[k] + ", want " + v
		}
	}
	return ""
}
