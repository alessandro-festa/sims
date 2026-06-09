# fake-dcgm-extras

Sidecar that fills the DCGM-metric gap `run-ai/fake-gpu-operator` leaves: emits the `DCGM_FI_DEV_*` gauges (temperature, power, clocks, PCIe throughput, ECC, fan) the upstream NVIDIA Grafana dashboard expects but the fake operator doesn't produce.

## Why

`run-ai/fake-gpu-operator` emits 3 DCGM metrics (`DCGM_FI_DEV_GPU_UTIL`, `DCGM_FI_DEV_FB_USED`, `DCGM_FI_DEV_FB_FREE`). The dashboard sims vendors (Grafana ID 12239) queries 5 (those 3 + `DCGM_FI_DEV_GPU_TEMP`, `DCGM_FI_DEV_POWER_USAGE`, `DCGM_FI_DEV_SM_CLOCK`). The temperature, power, and clock panels stayed empty until this sidecar landed.

This binary emits 9 commonly-used `DCGM_FI_DEV_*` gauges so the vendored panels populate immediately and any user-added panels in that family are also covered.

## What it does

For each fake GPU on the node, every Prometheus scrape:

1. Walk pods on this node that request `nvidia.com/gpu` and read `run.ai/simulated-gpu-utilization`.
2. Derive temperature, power, clocks, PCIe throughput from utilization using the same formulas as the AMD path (see `docs/adding-metrics.md`).
3. Emit gauges with DCGM-standard labels: `gpu`, `UUID`, `device`, `modelName`, `Hostname`, plus `pod`, `namespace`, `container` when assigned.

When no in-cluster Kubernetes client is available (smoke tests, local dev) the exporter falls back to per-GPU idle baselines so `/metrics` keeps responding.

## Build & test

```bash
make build                            # bin/fake-dcgm-extras
make test
make image                            # docker buildx build --load
make kind-load                        # kind load into sims-nvidia
```

## Run standalone

```bash
fake-dcgm-extras --listen :9401 --gpus-per-node 2 --product-name "Tesla T4"
curl localhost:9401/metrics | grep DCGM_FI_DEV_GPU_TEMP
```

## In a sims cluster

```bash
sims gpu create --vendor nvidia --monitoring         # chart deploys the sidecar DS
sims gpu load-image fake-dcgm-extras:dev             # push to local registry
# Grafana DCGM dashboard now has temperature, power, SM clock panels populated.
```

The chart at `chart/` mounts as a `file://` dep under `charts/sims-nvidia` when `extraMetrics.enabled=true` (default).
