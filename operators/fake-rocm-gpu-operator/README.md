# fake-rocm-gpu-operator

A sister project to [`run-ai/fake-gpu-operator`](https://github.com/run-ai/fake-gpu-operator), targeting AMD ROCm instead of NVIDIA. Built incrementally across Phases 3-6 of [sims](../../README.md).

## What it provides

| Component | Phase | Description |
|-----------|:----:|-------------|
| `metrics-exporter` | 3 | Prometheus exporter emitting AMD-namespaced GPU metrics. Idle baseline + load-driven values. |
| `device-plugin` | 4 | Kubelet device-plugin API server advertising `amd.com/gpu`. |
| `status-updater` | 5 | Watches pods, writes `topology` ConfigMap (`node → [{gpu, pod}]`). |
| `node-labeller` | 5 | Patches node labels (`feature.node.kubernetes.io/amd-gpu=true`, `amd.com/gpu.product-name=...`). |
| `controller` | 6 | Reconciles `DeviceConfig` CRD (`amd.com/v1alpha1`), mirroring `ROCm/gpu-operator`'s surface. |

All five run from **one binary**, dispatched by subcommand. One image, multiple Kubernetes workloads.

## Layout

```
operators/fake-rocm-gpu-operator/
  cmd/
    device-plugin/main.go
    metrics-exporter/main.go
    status-updater/main.go
    node-labeller/main.go
    controller/main.go
  pkg/
    deviceplugin/    # k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1 server
    topology/        # ConfigMap schema + read/write helpers
    metrics/         # AMD-namespaced Prometheus gauges
    simulate/        # util/temp/power/memory value generators
  api/v1alpha1/      # DeviceConfig CRD types (Phase 6)
  chart/             # Helm chart consumed by charts/sims-amd
  Dockerfile         # multi-stage, one image, subcommand entrypoint
```

## Status

Skeleton — directories exist but source files land in Phase 3. See the parent [sims README](../../README.md#status) for the live phase table.
