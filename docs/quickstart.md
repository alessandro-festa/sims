# Quickstart

## Prerequisites

| Tool | Minimum version | Install |
|------|-----------------|---------|
| Docker (or Podman with Docker-compat socket) | 24+ | [docs](https://docs.docker.com/get-docker/) |
| `kind` | 0.23+ | `brew install kind` |
| `kubectl` | 1.28+ | `brew install kubectl` |
| `helm` | 3.13+ | `brew install helm` |
| Go (only for building from source) | 1.23+ | `brew install go` |

## Build the CLI

```bash
git clone https://github.com/alessandro-festa/sims.git
cd sims
go build -o bin/sims ./cmd/sims
./bin/sims --help
```

(Pre-built binaries will be published once Phase 1 lands a tagged release.)

## NVIDIA path

```bash
# Create a cluster with 2 workers, each advertising 2 fake NVIDIA GPUs, with monitoring on
./bin/sims gpu create --vendor nvidia --workers 2 --gpus-per-worker 2 --monitoring

# Verify capacity
kubectl get nodes -o jsonpath='{range .items[*]}{.metadata.name}{": "}{.status.capacity.nvidia\.com/gpu}{"\n"}{end}'

# Schedule a sample GPU pod
./bin/sims gpu sample --vendor nvidia | kubectl apply -f -
kubectl get pod sims-nvidia-sample -w   # Reaches Running → CrashLoopBackOff (expected: no real CUDA)

# Open Grafana → DCGM dashboard
./bin/sims gpu dashboard --open
# admin / prom-operator

# Clean up
./bin/sims gpu delete
```

## AMD path

> Available from Phase 3 onward.

```bash
./bin/sims gpu create --vendor amd --monitoring
./bin/sims gpu sample --vendor amd | kubectl apply -f -
./bin/sims gpu dashboard --open
./bin/sims gpu delete
```

## Common workflows

**Add monitoring to an existing cluster.**

```bash
./bin/sims gpu create --vendor nvidia            # no --monitoring
# ...later
./bin/sims gpu monitoring enable
./bin/sims gpu dashboard --open
```

**Load a locally built image into the cluster.**

```bash
docker build -t my-test-workload:dev .
./bin/sims gpu load-image my-test-workload:dev
kubectl run test --image=localhost:5001/my-test-workload:dev --restart=Never \
  --overrides='{"spec":{"containers":[{"name":"test","image":"localhost:5001/my-test-workload:dev","resources":{"limits":{"nvidia.com/gpu":"1"}}}]}}'
```

**Drive simulated GPU utilization from a pod annotation** *(Phase 5+ for AMD; works today for NVIDIA via the upstream `run.ai/simulated-gpu-utilization` annotation).*

```yaml
metadata:
  annotations:
    sims.io/simulated-gpu-utilization: "60-80"  # AMD (sims-emitted)
    run.ai/simulated-gpu-utilization: "60-80"   # NVIDIA (fake-gpu-operator)
```

Grafana panels for that pod's GPU will then show util in [60, 80] % and temperature derived from util.

**Inspect raw metrics.**

```bash
# NVIDIA (DCGM-shaped)
kubectl port-forward -n gpu-operator svc/nvidia-dcgm-exporter 9400 &
curl -s localhost:9400/metrics | grep '^DCGM_FI_DEV_'

# AMD (AMD-namespaced) — Phase 3+
kubectl port-forward -n gpu-operator svc/amd-device-metrics-exporter 5000 &
curl -s localhost:5000/metrics | grep '^gpu_'
```

## Troubleshooting

**`docker: Cannot connect to the Docker daemon`.** Start Docker Desktop (or for Podman, `systemctl --user enable --now podman.socket` on Linux).

**`kind` errors creating the cluster on macOS.** Check Docker Desktop's resource allocation — kind + monitoring needs ~4 GiB and 2 CPUs minimum.

**Grafana dashboard panels are empty (NVIDIA path).** `run-ai/fake-gpu-operator` only emits 3 of the ~20 DCGM metrics the upstream dashboard expects. Util, FB used, and FB free panels populate; temperature, power, clocks, ECC do not. Phase 7 plans a sidecar to fill these.
