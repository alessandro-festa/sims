package dcgm

import (
	"context"
	"sort"
	"testing"
)

func TestCollector_EmitsAllNineFamilies(t *testing.T) {
	c := New(SamplerFunc(func(_ context.Context) []Snapshot {
		return []Snapshot{{
			GPU: "0", UUID: "GPU-fake-0", Device: "nvidia0", ModelName: "Tesla T4", Hostname: "h",
			GPUTemp: 35, PowerUsage: 30, SMClock: 1500, MemClock: 5500,
			PCIeTXThroughput: 100, PCIeRXThroughput: 100, FanSpeed: 20,
		}}
	}))
	mfs, err := c.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	got := []string{}
	for _, m := range mfs {
		got = append(got, m.GetName())
	}
	sort.Strings(got)
	want := []string{
		"DCGM_FI_DEV_ECC_DBE_VOL_TOTAL",
		"DCGM_FI_DEV_ECC_SBE_VOL_TOTAL",
		"DCGM_FI_DEV_FAN_SPEED",
		"DCGM_FI_DEV_GPU_TEMP",
		"DCGM_FI_DEV_MEM_CLOCK",
		"DCGM_FI_DEV_PCIE_RX_THROUGHPUT",
		"DCGM_FI_DEV_PCIE_TX_THROUGHPUT",
		"DCGM_FI_DEV_POWER_USAGE",
		"DCGM_FI_DEV_SM_CLOCK",
		"DCGM_FI_PROF_PIPE_TENSOR_ACTIVE",
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

func TestCollector_LabelsPresentOnAllMetrics(t *testing.T) {
	c := New(SamplerFunc(func(_ context.Context) []Snapshot {
		return []Snapshot{{
			GPU: "1", UUID: "GPU-x-1", Device: "nvidia1", ModelName: "Tesla T4", Hostname: "host-a",
			Pod: "foo", Namespace: "team-x", Container: "payload",
			GPUTemp: 60, PowerUsage: 100, SMClock: 1600, MemClock: 5500,
		}}
	}))
	mfs, err := c.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	want := map[string]string{
		"gpu": "1", "UUID": "GPU-x-1", "device": "nvidia1", "modelName": "Tesla T4", "Hostname": "host-a",
		"pod": "foo", "namespace": "team-x", "container": "payload",
	}
	for _, mf := range mfs {
		if len(mf.Metric) != 1 {
			t.Errorf("%s: %d series, want 1", mf.GetName(), len(mf.Metric))
			continue
		}
		got := map[string]string{}
		for _, lp := range mf.Metric[0].Label {
			got[lp.GetName()] = lp.GetValue()
		}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("%s: label %s = %q, want %q", mf.GetName(), k, got[k], v)
			}
		}
	}
}
