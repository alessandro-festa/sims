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

Pre-alpha. Phased delivery:

| Phase | Scope | Status |
|------:|:------|:------:|
| 1 | NVIDIA cluster end-to-end (no monitoring) | ✅ done |
| 2 | Monitoring chart + `sims gpu dashboard` UX | ✅ done |
| 3 | AMD metrics exporter standalone | planned |
| 4 | AMD device plugin (kubelet gRPC) | planned |
| 5 | AMD topology + node labeller + pod-driven metrics | planned |
| 6 | `DeviceConfig` CRD reconciler (optional) | planned |
| 7 | Parity polish (compute partitions, `rocm-smi` shim) | planned |

**What works today (Phases 1 + 2):** `sims gpu create --vendor nvidia [--monitoring]`, `sims gpu sample`, `sims gpu status`, `sims gpu load-image`, `sims gpu dashboard [--open|--stop]`, `sims gpu monitoring enable|disable`, `sims gpu delete`, `sims gpu doctor`. The AMD path returns a "Phase 3+" error until the operator lands.

See [docs/architecture.md](docs/architecture.md) for the full design.

## Quickstart

Requires Docker (or Podman with the Docker-compat socket), [`kind`](https://kind.sigs.k8s.io/docs/user/quick-start/#installation), [`helm`](https://helm.sh/docs/intro/install/), and [`kubectl`](https://kubernetes.io/docs/tasks/tools/).

```bash
# Build the CLI
go build -o bin/sims ./cmd/sims

# Create an NVIDIA-flavored cluster with monitoring
./bin/sims gpu create --vendor nvidia --monitoring

# Schedule a sample GPU pod
./bin/sims gpu sample --vendor nvidia | kubectl apply -f -

# Open Grafana (port-forwards on :3000)
./bin/sims gpu dashboard --open

# Tear it all down
./bin/sims gpu delete
```

See [docs/quickstart.md](docs/quickstart.md) for the longer walkthrough.

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
