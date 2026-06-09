# dashboards

> **Heads up:** the dashboard JSON files moved into the charts that consume them — Helm's `.Files.Get` only reads paths inside the chart root. This directory is kept as a discovery pointer.

## Where the JSON lives

| Vendor | Path | Chart | Phase |
|---|---|---|---|
| NVIDIA | `charts/sims-monitoring/dashboards/nvidia-dcgm.json` | `sims-monitoring` (NVIDIA branch) | Phase 2 |
| AMD | `charts/sims-monitoring/dashboards/amd-gpu.json` | `sims-monitoring` (AMD branch) | Phase 3 |

## Refresh procedure

```bash
./hack/update-dashboards.sh nvidia   # writes charts/sims-monitoring/dashboards/nvidia-dcgm.json
./hack/update-dashboards.sh amd      # writes charts/sims-monitoring/dashboards/amd-gpu.json (Phase 3)
```

Bump the JSON's top-level `version` field after refresh so Grafana's sidecar reloads it from the mounted ConfigMap.

## Provenance

### NVIDIA — `nvidia-dcgm.json`

- **Source:** [NVIDIA DCGM Exporter Dashboard, Grafana ID 12239](https://grafana.com/grafana/dashboards/12239/)
- **Vendored revision:** 2 (uid `Oxed_c6Wz`, schemaVersion 22) — fetched 2026-06-08 via `curl https://grafana.com/api/dashboards/12239/revisions/2/download`.
- **Wired in:** Phase 2 — `charts/sims-monitoring/templates/dashboard-nvidia.yaml` renders a `grafana_dashboard: "1"` ConfigMap when `vendor=nvidia`.
- **Coverage caveat:** `run-ai/fake-gpu-operator` only emits 3 of the ~20 DCGM metrics this dashboard expects (`DCGM_FI_DEV_GPU_UTIL`, `DCGM_FI_DEV_FB_USED`, `DCGM_FI_DEV_FB_FREE`). Util and memory panels populate; temperature, power, clocks, ECC stay empty until Phase 7's sidecar fills the gap.

### AMD — `amd-gpu.json`

- **Source:** [AMD Instinct Single Node Dashboard, Grafana ID 23434](https://grafana.com/grafana/dashboards/23434/) — the official dashboard ROCm publishes for `ROCm/device-metrics-exporter`.
- **Vendored revision:** 2 (uid `adud7tbrozvuoa`, schemaVersion 41, version 6) — fetched 2026-06-09 via `./hack/update-dashboards.sh amd`.
- **Wired in:** Phase 3 — `charts/sims-monitoring/templates/dashboard-amd.yaml` renders a `grafana_dashboard: "1"` ConfigMap when `vendor=amd`.
- **Coverage:** the dashboard queries 6 metrics (`amd_gpu_junction_temperature`, `amd_gpu_package_power`, `amd_gpu_gfx_activity`, `amd_gpu_used_vram`, `amd_gpu_health`, `amd_pcie_bandwidth`); all are emitted by `fake-rocm-gpu-operator`'s `metrics-exporter` from Phase 3 so every panel populates. Surplus gauges (`amd_gpu_total_vram`, `amd_gpu_clock_gfx`, `amd_gpu_voltage`, `amd_gpu_fan_speed`) are emitted but unused — they're available for custom panels.
- **Heads-up:** an earlier draft of the plan referenced Grafana ID **23715** — that ID is a Portuguese Zabbix/Mikrotik dashboard, not AMD. The correct AMD ID is 23434.
