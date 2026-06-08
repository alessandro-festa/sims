# dashboards

Vendor Grafana dashboard JSON, vendored at known versions for reproducible deployment. Refreshed via `hack/update-dashboards.sh`.

## NVIDIA — `nvidia-dcgm.json`

- **Source:** [NVIDIA DCGM Exporter Dashboard, Grafana ID 12239](https://grafana.com/grafana/dashboards/12239/)
- **Vendored revision:** 2 (uid `Oxed_c6Wz`, schemaVersion 22) — fetched 2026-06-08 via `curl https://grafana.com/api/dashboards/12239/revisions/2/download`.
- **Wired in:** Phase 2 (`charts/sims-monitoring` mounts as `grafana_dashboard: "1"` ConfigMap when `vendor=nvidia`)
- **Coverage caveat:** `run-ai/fake-gpu-operator` only emits 3 of the ~20 DCGM metrics this dashboard expects (`DCGM_FI_DEV_GPU_UTIL`, `DCGM_FI_DEV_FB_USED`, `DCGM_FI_DEV_FB_FREE`). Util and memory panels populate; temperature, power, clocks, ECC panels are empty. Phase 7 plans a sidecar exporter to fill the gap.

## AMD — `amd-gpu.json`

- **Source:** [AMD GPU Metrics Dashboard, Grafana ID 23715](https://grafana.com/grafana/dashboards/23715/)
- **Wired in:** Phase 3 (`charts/sims-monitoring` mounts when `vendor=amd`)
- **Coverage:** broader than NVIDIA from day one because `fake-rocm-gpu-operator`'s exporter is in our control — we emit all 10 metrics the dashboard queries.

## Refresh procedure

```bash
./hack/update-dashboards.sh nvidia   # downloads ID 12239 to dashboards/nvidia-dcgm.json
./hack/update-dashboards.sh amd      # downloads ID 23715 to dashboards/amd-gpu.json
```

Bump the `version` field in the JSON when committing changes so Grafana's sidecar reloads it.
