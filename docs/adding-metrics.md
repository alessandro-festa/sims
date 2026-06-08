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

The exporter emits AMD-namespaced gauges per `(gpu_id, pod, namespace, container)`:

| Metric | Idle baseline | Loaded value |
|--------|---------------|--------------|
| `gpu_temperature` (°C) | 35 | `40 + util × 0.55 + jitter` |
| `gpu_power_usage` (W) | 30 | `50 + util × 2.5` |
| `gpu_gfx_busy_instantaneous` (%) | 0 | sampled in pod's `sims.io/simulated-gpu-utilization` range |
| `gpu_gfx_activity` (%) | 0 | cumulative variant of busy |
| `gpu_memory_used` (bytes) | 0 | `(util / 100) × gpu_memory_total × 0.8` |
| `gpu_memory_total` (bytes) | configured | configured |
| `gpu_voltage` (mV) | 800 | `800 + util × 4` |
| `gpu_clock` (MHz) | 500 | `500 + util × 12` |
| `gpu_fan_speed` (%) | 20 | `20 + util × 0.6` |
| `gpu_pcie_bandwidth` (MB/s) | 100 | `100 + util × 80` |

Idle GPUs (no pod assigned) report baseline values. Assigned GPUs read the pod annotation `sims.io/simulated-gpu-utilization` (range `"low-high"`, default `"5-15"`) and derive the rest from the sampled utilization.

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

1. Add the gauge definition to `operators/fake-rocm-gpu-operator/pkg/metrics/`.
2. Add the value generator (idle baseline + loaded formula) to `operators/fake-rocm-gpu-operator/pkg/simulate/`.
3. Update the AMD Grafana dashboard JSON at `charts/sims-monitoring/dashboards/amd-gpu.json` to include a panel querying the new series.
4. Add a unit test in `pkg/simulate` with a seeded RNG asserting deterministic output for a known annotation.

## Adding panels to the NVIDIA dashboard

The NVIDIA dashboard is vendored at `charts/sims-monitoring/dashboards/nvidia-dcgm.json` (Grafana ID 12239). The JSON lives inside the chart so Helm's `.Files.Get` can reach it from the dashboard ConfigMap template. To refresh from upstream:

```bash
./hack/update-dashboards.sh nvidia
```

To add a custom panel, edit the JSON directly and bump the top-level `version` field. The dashboard ConfigMap is auto-reloaded by Grafana's sidecar within ~30 s of `helm upgrade`.
