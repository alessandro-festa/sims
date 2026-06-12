# sims

**kind-based GPU cluster simulator for NVIDIA and AMD ROCm.**

Spin up a local Kubernetes cluster (via [kind](https://kind.sigs.k8s.io/)) that *thinks* it has NVIDIA or AMD GPUs. Pods requesting `nvidia.com/gpu` or `amd.com/gpu` schedule successfully. Optional Prometheus + Grafana stack lights up vendor dashboards with simulated utilization, memory, temperature, and power. No physical GPU required.

> **Pod containers will fail at runtime** — there is no real CUDA / ROCm driver. The goal is scheduling, manifests, operators, and observability dashboards, not workload execution.

## Why

Testing GPU-aware Kubernetes manifests and operators without GPU hardware is painful. The NVIDIA ecosystem has [`run-ai/fake-gpu-operator`](https://github.com/run-ai/fake-gpu-operator), but no equivalent exists for AMD. `sims` wraps the NVIDIA fake operator and adds a sister project — `fake-rocm-gpu-operator` — so both vendors get the same UX:

```
sims gpu create --vendor nvidia --monitoring
sims gpu create --vendor amd    --monitoring
```

## Status

| Phase | Scope | Status |
|------:|:------|:------:|
| 1 | NVIDIA cluster end-to-end (no monitoring) | ✅ done |
| 2 | Monitoring chart + `sims gpu dashboard` UX | ✅ done |
| 3 | AMD metrics exporter standalone | ✅ done |
| 4 | AMD device plugin (kubelet gRPC) | ✅ done |
| 5 | AMD topology + node labeller + pod-driven metrics | ✅ done |
| 6 | `DeviceConfig` CRD reconciler (optional) | optional / skipped |
| 7 | Parity polish — NVIDIA DCGM extras sidecar | ✅ done (main thrust); `rocm-smi` shim + CPX/SPX deferred |

**End-to-end validated** on 2026-06-09 against real vendor containers (`rocm/dev-ubuntu-22.04:6.0` for AMD, `nvidia/cuda:12.4.0-base-ubuntu22.04` for NVIDIA). Pods schedule + reach Running; ROCm / CUDA tools fail authentically inside (no real GPU); Grafana panels show per-pod metrics driven by `sims.io/simulated-gpu-utilization` annotations. See [docs/quickstart.md](docs/quickstart.md#real-vendor-container-demo) for the demo flow.

**Commands:** `sims gpu create --vendor {nvidia|amd} [--monitoring]`, `sims gpu sample --vendor {nvidia|amd}`, `sims gpu status`, `sims gpu dashboard [--name N] [--stop]`, `sims gpu monitoring enable|disable`, `sims gpu delete`, `sims gpu doctor`.

See [docs/architecture.md](docs/architecture.md) for the full design.

## Quickstart

Requires Docker (or Podman with the Docker-compat socket), [`kind`](https://kind.sigs.k8s.io/docs/user/quick-start/#installation), [`helm`](https://helm.sh/docs/intro/install/), and [`kubectl`](https://kubernetes.io/docs/tasks/tools/).

**NVIDIA:**

```bash
go build -o bin/sims ./cmd/sims
./bin/sims gpu create --vendor nvidia --workers 2 --gpus-per-worker 2 --monitoring
./bin/sims gpu sample --vendor nvidia | kubectl apply -f -
./bin/sims gpu dashboard                # http://localhost:3000 (admin / prom-operator)
./bin/sims gpu delete
```

**AMD:**

```bash
./bin/sims gpu create --vendor amd --workers 2 --gpus-per-worker 2 --monitoring
./bin/sims gpu sample --vendor amd | kubectl apply -f -
./bin/sims gpu dashboard                # http://localhost:3000 (admin / prom-operator)
./bin/sims gpu delete
```

See [docs/quickstart.md](docs/quickstart.md) for the longer walkthrough including the real-vendor-container demo.

## Documentation

- [Architecture](docs/architecture.md) — components, data flow, design decisions
- [Quickstart](docs/quickstart.md) — install, common workflows
- [Adding metrics](docs/adding-metrics.md) — how the simulated AMD/NVIDIA metrics work
- [Contributing](docs/contributing.md) — dev setup, code layout, how to add a phase

## Acknowledgements

- [`run-ai/fake-gpu-operator`](https://github.com/run-ai/fake-gpu-operator) — the NVIDIA path is built on this. We mirror their pod-annotation-driven simulation model for AMD.
- [`maryamtahhan/kind-gpu-sim`](https://github.com/maryamtahhan/kind-gpu-sim) — reference for the kind + node-status-patch pattern.
- [`ROCm/gpu-operator`](https://github.com/ROCm/gpu-operator), [`ROCm/k8s-device-plugin`](https://github.com/ROCm/k8s-device-plugin), [`ROCm/device-metrics-exporter`](https://github.com/ROCm/device-metrics-exporter) — the real AMD stack we mimic.

## License

Apache 2.0 — see [LICENSE](LICENSE).
