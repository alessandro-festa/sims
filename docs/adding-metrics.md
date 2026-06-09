# Adding & customizing metrics

## How simulated metrics work

Both the NVIDIA and AMD paths follow the same model: GPU metrics are *derived* from which pods are currently scheduled on which GPUs, plus an optional per-pod annotation that sets the simulated utilization range.

### NVIDIA — handled by `run-ai/fake-gpu-operator`

The fake-gpu-operator's `status-exporter` emits three DCGM metrics:

| Metric | Type | Source |
|--------|------|--------|
| `DCGM_FI_DEV_GPU_UTIL` | gauge | Per pod assignment; jittered in the range from `run.ai/simulated-gpu-utilization` annotation |
| `DCGM_FI_DEV_FB_USED` | gauge | Derived from utilization × node `gpuMemory` |
| `DCGM_FI_DEV_FB_FREE` | gauge | `gpuMemory − FB_USED` |

Labels: `gpu`, `UUID`, `device`, `modelName`, `Hostname`, `container`, `namespace`, `pod`. The full DCGM-namespaced metric set most NVIDIA Grafana dashboards expect (temperature, power, clocks, ECC) is **not** emitted — Phase 7 of `sims` plans a sidecar to fill the gap.

### AMD — handled by `fake-rocm-gpu-operator` (from Phase 3 onward)

Metric names mirror the real [ROCm/device-metrics-exporter](https://github.com/ROCm/device-metrics-exporter) so dashboards built for that exporter (e.g. Grafana ID **23434** — _AMD Instinct Single Node Dashboard_) light up against sims out of the box.

| Metric | Idle baseline | Loaded value |
|--------|---------------|--------------|
| `amd_gpu_junction_temperature` (°C) | 35 | `40 + util × 0.55 + jitter` |
| `amd_gpu_package_power` (W) | 30 | `50 + util × 2.5` |
| `amd_gpu_gfx_activity` (%) | 0 | `util` |
| `amd_gpu_used_vram` (bytes) | 0 | `(util / 100) × total_vram × 0.8` |
| `amd_gpu_total_vram` (bytes) | configured | configured |
| `amd_gpu_health` (0/1) | 1 | 1 |
| `amd_gpu_clock_gfx` (MHz) | 500 | `500 + util × 12` |
| `amd_gpu_voltage` (mV) | 800 | `800 + util × 4` |
| `amd_gpu_fan_speed` (%) | 20 | `20 + util × 0.6` |
| `amd_pcie_bandwidth` (MB/s) | 100 | `100 + util × 80` |

Labels: `gpu_id`, `serial_number`, `card_series`, `card_model`, `hostname`, plus `pod`, `namespace`, `container` (empty for idle GPUs). Phase 5 added the per-pod attribution: `status-updater` watches the device-plugin's `sims.io/assigned-gpus` annotation and writes a `topology` ConfigMap; the metrics-exporter reads it on every scrape so gauges follow pod assignments. When a pod carries the `sims.io/simulated-gpu-utilization: "low-high"` annotation, the exporter samples util within that range each scrape and derives the loaded values from the table above.

## Annotating pods to control utilization

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: gpu-load-test
  annotations:
    sims.io/simulated-gpu-utilization: "70-90"  # AMD (sims-native)
    run.ai/simulated-gpu-utilization: "70-90"   # NVIDIA (fake-gpu-operator)
spec:
  containers:
    - name: payload
      image: busybox
      command: ["sh", "-c", "sleep 3600"]
      resources:
        limits:
          amd.com/gpu: 1     # or nvidia.com/gpu: 1
```

The exporters re-read annotations on every Prometheus scrape (default 15 s), so changes take effect within one scrape interval.

## Adding a new AMD metric

1. Add the gauge to `operators/fake-rocm-gpu-operator/pkg/metrics/registry.go` (mirror an existing entry — name with `amd_gpu_` / `amd_pcie_` prefix, register in `New()`, write in `Observe()`).
2. Add the value to `operators/fake-rocm-gpu-operator/pkg/simulate/gpu.go` (extend `Sample`, set in `SampleIdle` and `SampleLoaded`).
3. Update the AMD Grafana dashboard JSON at `charts/sims-monitoring/dashboards/amd-gpu.json` to include a panel querying the new series.
4. Add a unit test in `pkg/simulate` with a seeded RNG asserting deterministic output for a known util value.

## Adding panels to the NVIDIA dashboard

The NVIDIA dashboard is vendored at `charts/sims-monitoring/dashboards/nvidia-dcgm.json` (Grafana ID 12239). The JSON lives inside the chart so Helm's `.Files.Get` can reach it from the dashboard ConfigMap template. To refresh from upstream:

```bash
./hack/update-dashboards.sh nvidia
```

To add a custom panel, edit the JSON directly and bump the top-level `version` field. The dashboard ConfigMap is auto-reloaded by Grafana's sidecar within ~30 s of `helm upgrade`.
