# dashboards

> **Heads up:** the dashboard JSON files moved into the charts that consume them — Helm's `.Files.Get` only reads paths inside the chart root. This directory is kept as a discovery pointer.

## Where the JSON lives

| Vendor | Path | Chart | Phase |
|---|---|---|---|
| NVIDIA | `charts/sims-monitoring/dashboards/nvidia-dcgm.json` | `sims-monitoring` (NVIDIA branch) | Phase 2 |
| AMD | `charts/sims-monitoring/dashboards/amd-gpu.json` *(planned)* | `sims-monitoring` (AMD branch) | Phase 3 |

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

- **Source:** [AMD GPU Metrics Dashboard, Grafana ID 23715](https://grafana.com/grafana/dashboards/23715/)
- **Wired in:** Phase 3
- **Coverage:** broader than NVIDIA from day one because `fake-rocm-gpu-operator`'s exporter is in our control — we emit all 10 metrics the dashboard queries.
