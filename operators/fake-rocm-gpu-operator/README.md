# fake-rocm-gpu-operator

A sister project to [`run-ai/fake-gpu-operator`](https://github.com/run-ai/fake-gpu-operator), targeting AMD ROCm instead of NVIDIA. Built incrementally across Phases 3-6 of [sims](../../README.md).

## What it provides

| Component | Phase | Description |
|-----------|:----:|-------------|
| `metrics-exporter` | 3 | Prometheus exporter emitting AMD-namespaced GPU metrics. Idle baseline today; load-driven values in Phase 5. |
| `device-plugin` | 4 | Kubelet device-plugin API server advertising `amd.com/gpu`. |
| `status-updater` | 5 | Watches pods, writes `topology` ConfigMap (`node → [{gpu, pod}]`). |
| `node-labeller` | 5 | Patches node labels (`feature.node.kubernetes.io/amd-gpu=true`, `amd.com/gpu.product-name=...`). |
| `controller` | 6 | Reconciles `DeviceConfig` CRD (`amd.com/v1alpha1`), mirroring `ROCm/gpu-operator`'s surface. |

All five run from **one binary** dispatched by subcommand (kubectl-style). Phase 3 ships only the `metrics-exporter` subcommand; the others print a `Phase N` notice and exit non-zero.

## Layout

```
operators/fake-rocm-gpu-operator/
  cmd/
    fake-rocm-gpu-operator/main.go     # subcommand dispatcher (single main pkg)
  internal/
    exporter/                          # metrics-exporter logic
    deviceplugin/                      # Phase 4
    statusupdater/                     # Phase 5
    nodelabeller/                      # Phase 5
    controller/                        # Phase 6
  pkg/
    metrics/                           # AMD-namespaced Prometheus gauges
    simulate/                          # idle baseline + load-driven values
  api/v1alpha1/                        # DeviceConfig CRD types (Phase 6)
  chart/                               # Helm chart consumed by charts/sims-amd
  Dockerfile                           # multi-stage, one image, subcommand entrypoint
  Makefile                             # build, image, test, kind-load
  go.mod                               # own module, joined to the parent via go.work
```

## Build & test

From the repo root (the workspace knows about both modules):

```bash
go test -race ./...                                  # runs operator + parent tests
make -C operators/fake-rocm-gpu-operator image       # builds fake-rocm-gpu-operator:dev
make -C operators/fake-rocm-gpu-operator kind-load   # loads into the sims-amd kind cluster
```

Smoke-test the binary directly:

```bash
docker run --rm -p 5000:5000 -e NODE_NAME=test fake-rocm-gpu-operator:dev \
  metrics-exporter --listen :5000 --gpus-per-node 2
curl localhost:5000/metrics | grep amd_gpu_junction_temperature
```

## Metric naming

Metric names match the real ROCm exporter (`amd_gpu_*`, `amd_pcie_*`). Gauges and label sets are documented in [`../../docs/adding-metrics.md`](../../docs/adding-metrics.md).
