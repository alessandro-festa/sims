# Quickstart

## Prerequisites

| Tool | Minimum version | Install |
|------|-----------------|---------|
| Docker (or Podman with Docker-compat socket) | 24+ | [docs](https://docs.docker.com/get-docker/) |
| `kind` | 0.23+ | `brew install kind` |
| `kubectl` | 1.28+ | `brew install kubectl` |
| `helm` | 3.13+ | `brew install helm` |
| Go (only for building from source) | 1.25+ | `brew install go` |

## Build the CLI

```bash
git clone https://github.com/alessandro-festa/sims.git
cd sims
go build -o bin/sims ./cmd/sims
./bin/sims --help
```

(Pre-built binaries will be published with the first tagged release.)

## Pre-flight check

```bash
./bin/sims gpu doctor
```

Validates Docker, kind clusters, and GHCR reachability. Exit 0 means you're good to go; failures print a one-line remediation hint.

## Config file (recommended)

Instead of passing every option as a CLI flag, you can write a `SimsConfig` YAML file and pass it with `--config`:

```yaml
# sims.yaml
apiVersion: sims.io/v1
kind: SimsConfig
vendor: nvidia          # "nvidia" or "amd"
name: my-cluster        # optional, defaults to sims-<vendor>
workers: 2              # number of worker nodes (default: 2)
k8sVersion: v1.31.0     # optional
taint: false            # add GPU taints to workers (default: false)
gpu:
  family: H100          # GPU family from the catalog below
  perWorker: 4          # fake GPUs per worker (default: 2)
  memoryBytes: 85899345920  # override catalog memory (optional)
  features:
    mig: 1g.10gb        # NVIDIA-only: MIG profile label
    partition:           # AMD-only: compute partition
      mode: cpx          # "spx" (single) or "cpx" (compute)
      count: 4           # 1-8 partitions
workload:
  defaultUtilization: "5-15"  # simulated GPU util range (future)
monitoring: true        # install Prometheus + Grafana
```

```bash
./bin/sims gpu create --config sims.yaml
```

CLI flags override config values — useful for one-off tweaks:

```bash
./bin/sims gpu create --config sims.yaml --workers 1 --gpus-per-worker 8
```

### GPU family catalog

When you set `gpu.family`, sims auto-fills the product name and memory from a built-in catalog. You only need to specify the family name — everything else is optional.

**NVIDIA:**

| Family | Product Name | Memory | Type |
|--------|-------------|--------|------|
| `GB300` | B300 | 288 GiB | HBM3e |
| `GB200` | B200 | 192 GiB | HBM3e |
| `H200` | H200 | 141 GiB | HBM3e |
| `H100` | H100 | 80 GiB | HBM3 |
| `A100` | A100 | 40 GiB | HBM2e |
| `L40S` | L40S | 48 GiB | GDDR6 |
| `L40` | L40 | 48 GiB | GDDR6 |
| `L4` | L4 | 24 GiB | GDDR6 |
| `T4` | Tesla T4 | 16 GiB | GDDR6 |

**AMD:**

| Family | Product Name | Memory | Type |
|--------|-------------|--------|------|
| `MI440X` | MI440X | 432 GiB | HBM4 |
| `MI430X` | MI430X | 432 GiB | HBM4 |
| `MI325X` | MI325X | 256 GiB | HBM3e |
| `MI300X` | MI300X | 192 GiB | HBM3 |
| `MI300A` | MI300A | 128 GiB | HBM3 |
| `MI250X` | MI250X | 128 GiB | HBM2e |
| `MI250` | MI250 | 128 GiB | HBM2e |
| `MI210` | MI210 | 64 GiB | HBM2e |
| `MI100` | MI100 | 32 GiB | HBM2 |

The product name flows into node labels (NVIDIA) and controller args (AMD). Memory flows into the chart values so Grafana dashboards show realistic memory usage. Override with `gpu.memoryBytes` if needed.

### Example configs

**NVIDIA H100 with monitoring:**

```yaml
apiVersion: sims.io/v1
kind: SimsConfig
vendor: nvidia
gpu:
  family: H100
  perWorker: 4
monitoring: true
```

**AMD MI300X with CPX partitioning:**

```yaml
apiVersion: sims.io/v1
kind: SimsConfig
vendor: amd
gpu:
  family: MI300X
  features:
    partition:
      mode: cpx
      count: 4
```

**Minimal — just specify vendor and family:**

```yaml
apiVersion: sims.io/v1
kind: SimsConfig
vendor: nvidia
gpu:
  family: L40S
```

## NVIDIA path

```bash
# 1. Create cluster: 2 workers × 2 fake NVIDIA GPUs each, with monitoring.
#    Using flags:
./bin/sims gpu create --vendor nvidia --workers 2 --gpus-per-worker 2 --monitoring
#    Or using a config file:
#    ./bin/sims gpu create --config examples/nvidia-h100.yaml

# 2. Verify capacity (or `./bin/sims gpu status` for a fuller summary)
kubectl get nodes -o jsonpath='{range .items[*]}{.metadata.name}{": "}{.status.capacity.nvidia\.com/gpu}{"\n"}{end}'

# 3. Schedule the canned sample pod.
./bin/sims gpu sample --vendor nvidia | kubectl apply -f -
kubectl get pod sims-nvidia-sample -w   # Reaches Running

# 4. Open Grafana → DCGM Exporter Dashboard.
./bin/sims gpu dashboard                # http://localhost:3000  (admin / prom-operator)

# 5. Clean up.
./bin/sims gpu dashboard --stop
./bin/sims gpu delete
```

## AMD path

```bash
# 1. Create cluster: 2 workers × 2 fake AMD GPUs, with monitoring.
#    Using flags:
./bin/sims gpu create --vendor amd --workers 2 --gpus-per-worker 2 --monitoring
#    Or using a config file:
#    ./bin/sims gpu create --config examples/amd-mi300x-cpx.yaml

# 2. Within ~30 s the operator's device-plugin advertises capacity:
kubectl get nodes -o jsonpath='{range .items[*]}{.metadata.name}{": "}{.status.capacity.amd\.com/gpu}{"\n"}{end}'

# 3. Schedule the canned sample pod.
./bin/sims gpu sample --vendor amd | kubectl apply -f -
kubectl get pod sims-amd-sample -w      # Reaches Running

# 4. Open Grafana → AMD Instinct Single Node Dashboard.
./bin/sims gpu dashboard                # http://localhost:3000  (admin / prom-operator)

# 5. Clean up.
./bin/sims gpu dashboard --stop
./bin/sims gpu delete
```

## Real-vendor-container demo

The canned `sims gpu sample` uses a busybox image — enough to schedule and exercise the metrics pipeline. To prove sims accepts a **real** vendor container the way a production workload would, run the official ROCm or CUDA image directly. The vendor's GPU diagnostic tool (`rocm-smi` / `nvidia-smi`) fails authentically inside because there's no real device, but the pod stays Running and Grafana shows its metrics.

**AMD (rocm/dev-ubuntu-22.04):**

```bash
cat <<'YAML' | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: rocm-workload
  annotations:
    sims.io/simulated-gpu-utilization: "85-95"   # drives Grafana values
spec:
  restartPolicy: Never
  containers:
    - name: rocm
      image: rocm/dev-ubuntu-22.04:6.0
      command: ["bash", "-c"]
      args:
        - |
          rocm-smi 2>&1 || true       # fails authentically: no /dev/kfd
          sleep 7200
      resources:
        limits:
          amd.com/gpu: 1
YAML
```

**NVIDIA (nvidia/cuda):**

```bash
cat <<'YAML' | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: cuda-workload
  annotations:
    run.ai/simulated-gpu-utilization: "85-95"    # drives Grafana values
spec:
  restartPolicy: Never
  containers:
    - name: cuda
      image: nvidia/cuda:12.4.0-base-ubuntu22.04
      command: ["bash", "-c"]
      args:
        - |
          nvidia-smi 2>&1 || true     # intercepted by fake-gpu-operator
          sleep 7200
      resources:
        limits:
          nvidia.com/gpu: 1
YAML
```

After ~30 s both pods will be `Running` and Grafana panels will show their simulated load in the [85, 95]% band (temperature ~90°C, power ~250W, SM/clock ramped up accordingly).

## Common workflows

**Add monitoring to an existing cluster.**

```bash
./bin/sims gpu create --vendor nvidia            # no --monitoring
# ...later
./bin/sims gpu monitoring enable
./bin/sims gpu dashboard
```

**Load a locally built workload image into the cluster.**

```bash
docker build -t my-test-workload:dev .
kind load docker-image my-test-workload:dev --name sims-nvidia
kubectl run test --image=my-test-workload:dev --restart=Never \
  --overrides='{"spec":{"containers":[{"name":"test","image":"my-test-workload:dev","imagePullPolicy":"Never","resources":{"limits":{"nvidia.com/gpu":"1"}}}]}}'
```

**Drive simulated GPU utilization from a pod annotation.**

```yaml
metadata:
  annotations:
    sims.io/simulated-gpu-utilization: "60-80"   # AMD (Phase 5+)
    run.ai/simulated-gpu-utilization: "60-80"    # NVIDIA (fake-gpu-operator)
```

Grafana panels for that pod's GPU will show util in [60, 80]% and derived temperature/power/clock values within one scrape interval (~15-30 s). On the AMD path, status-updater also stamps `sims.io/assigned-gpus: gpu-N,gpu-M` on the pod so you can see which GPU IDs it holds.

**Inspect raw metrics.**

```bash
# NVIDIA: 3 metrics from fake-gpu-operator's DCGM exporter + 9 from the Phase 7 sidecar
kubectl port-forward -n gpu-operator svc/nvidia-dcgm-exporter 9400 &
kubectl port-forward -n gpu-operator svc/dcgm-extras-exporter 9401 &
curl -s localhost:9400/metrics | grep '^DCGM_FI_DEV_GPU_UTIL'
curl -s localhost:9401/metrics | grep '^DCGM_FI_DEV_GPU_TEMP'

# AMD: 10 amd_gpu_*/amd_pcie_* gauges
kubectl port-forward -n gpu-operator svc/amd-device-metrics-exporter 5000 &
curl -s localhost:5000/metrics | grep '^amd_'
```

**Scale GPU count via helm upgrade (proves the device-plugin is dynamic).**

```bash
helm upgrade sims-amd charts/sims-amd \
  --namespace gpu-operator --reuse-values \
  --set gpusPerNode=4 --set fake-rocm-gpu-operator.gpusPerNode=4
# Within ~30 s capacity reflects the new count:
kubectl get nodes -o jsonpath='{.items[*].status.capacity.amd\.com/gpu}'
```

## Troubleshooting

**`docker: Cannot connect to the Docker daemon`.** Start Docker Desktop (or for Podman, `systemctl --user enable --now podman.socket` on Linux).

**`kind` errors creating the cluster on macOS.** Check Docker Desktop's resource allocation — kind + monitoring needs ~4 GiB and 2 CPUs minimum.

**Grafana dashboard panels are empty.** Make sure (a) the `instance` template variable at the top of the dashboard is set to **All** — fresh browsers sometimes load with nothing selected; (b) `kubectl get pods -n gpu-operator` shows no `ImagePullBackOff` — operator images are pulled from GHCR; (c) for the AMD dashboard, the pod that's supposed to drive the panel actually has the `sims.io/simulated-gpu-utilization` annotation.

**Pod stays in `ImagePullBackOff`.** Operator images are published to `ghcr.io/alessandro-festa/...`. Check that the GHCR packages are public and the image tag matches the chart's `values.yaml`.

**NVIDIA temperature/power/SM-clock panels are empty.** The `fake-dcgm-extras` sidecar image is pulled from GHCR. Verify the pod is Running: `kubectl get pods -n gpu-operator -l app=dcgm-extras-exporter`.
