# Architecture

## Goal

Run a Kubernetes cluster on a developer laptop (or CI runner) that behaves as if it has NVIDIA or AMD GPUs:

- Pods requesting `nvidia.com/gpu` / `amd.com/gpu` schedule successfully.
- A Prometheus-scraped exporter emits vendor-shaped GPU metrics.
- A Grafana dashboard renders those metrics with realistic-looking values driven by which pods are running.

Workload execution (CUDA / HIP kernels) is *not* a goal — containers crash because there's no real driver. We only need the cluster's view to be coherent enough to exercise manifests, operators, scheduling policies, and dashboards.

## Why not "install the official operator on faked hardware"

kind nodes are containers. They share the host kernel and cannot load kernel modules from inside the container. The official `ROCm/gpu-operator` uses KMM to load the `amdgpu` kernel module; the official NVIDIA GPU Operator validates `/dev/nvidia*` and refuses to deploy without a real driver. Both refuse to come up in kind.

`run-ai/fake-gpu-operator` solves this for NVIDIA by *replacing* the official operator with a stack of fake components that satisfy the same Kubernetes-facing contract (device plugin API, DCGM-shaped metrics endpoint, topology ConfigMap). `sims` follows the same replacement pattern for AMD via a sister project `fake-rocm-gpu-operator`.

## Components

```
┌─────────────────────────────── kind cluster (one vendor) ─────────────────────────────┐
│                                                                                       │
│   control-plane node                                                                  │
│   ┌─────────────────┐                                                                 │
│   │ kube-apiserver  │                                                                 │
│   │ scheduler       │                                                                 │
│   │ controllers     │                                                                 │
│   └─────────────────┘                                                                 │
│                                                                                       │
│   worker nodes (× N)                                                                  │
│   ┌─────────────────────────────────────────────────────────────────────────────┐    │
│   │ kubelet                                                                     │    │
│   │   ↑ device-plugin gRPC                                                      │    │
│   │ ┌───────────────────────┐   ┌──────────────────────────┐                    │    │
│   │ │ device-plugin (DS)    │   │ metrics-exporter (DS)    │ :9400 / :5000      │    │
│   │ │  advertises N GPUs    │   │  reads topology CM       │ /metrics           │    │
│   │ │  Allocate() →         │   │  + pod annotations       │                    │    │
│   │ │    pod annotation     │   │  emits vendor gauges     │                    │    │
│   │ │   sims.io/assigned-   │   └──────────────────────────┘                    │    │
│   │ │   gpus                │                                                   │    │
│   │ └───────────────────────┘                                                   │    │
│   │ ┌───────────────────────┐   ┌──────────────────────────┐                    │    │
│   │ │ status-updater (Dep)  │   │ node-labeller (DS)       │                    │    │
│   │ │  pod watcher          │   │  feature.node.kubernetes │                    │    │
│   │ │  → topology CM        │   │  .io/<vendor>-gpu=true   │                    │    │
│   │ └───────────────────────┘   └──────────────────────────┘                    │    │
│   └─────────────────────────────────────────────────────────────────────────────┘    │
│                                                                                       │
│   namespace: monitoring (only if --monitoring)                                        │
│   ┌─────────────────┐   ┌──────────────┐   ┌─────────────────────────────────┐       │
│   │ Prometheus      │ ← │ ServiceMon.  │ ← │ Grafana                         │       │
│   │ Operator        │   │ (vendor)     │   │  + vendor dashboard CM          │       │
│   └─────────────────┘   └──────────────┘   │  exposed via `sims gpu          │       │
│                                            │  dashboard` port-forward         │       │
│                                            └─────────────────────────────────┘       │
│                                                                                       │
└───────────────────────────────────────────────────────────────────────────────────────┘
                                          ▲
                                          │
                                  ┌───────┴────────┐
                                  │ sims (Go CLI)  │
                                  │  kind + helm   │
                                  │  orchestration │
                                  └────────────────┘
```

## Data flow

1. User applies a Pod with `resources.limits["<vendor>.com/gpu"] = K`.
2. Scheduler binds based on capacity advertised by the device plugin.
3. kubelet calls `Allocate` on the device plugin; the plugin returns dummy device IDs and patches the Pod with `sims.io/assigned-gpus: "gpu-0,gpu-1"`.
4. `status-updater` watches Pods, writes `gpu-operator/topology` ConfigMap (`node → [{gpu_id, pod_ns, pod_name, container}]`).
5. `metrics-exporter` reads the topology CM + each pod's `sims.io/simulated-gpu-utilization: "60-80"` annotation. For each scrape it generates plausible values (util sampled in the annotated range; temperature/power derived from util; memory derived from util × node memory).
6. Prometheus scrapes via the vendor `ServiceMonitor`.
7. Grafana renders the vendor dashboard mounted as a `grafana_dashboard: "1"` ConfigMap.
8. The pod's actual container errors out (no real driver). That is fine — scheduling and metrics are the deliverable.

## Two independent Helm releases per cluster

- **GPU stack** (always installed) — namespace `gpu-operator`. Either `sims-nvidia` (wraps `run-ai/fake-gpu-operator`) or `sims-amd` (wraps `fake-rocm-gpu-operator`).
- **Monitoring stack** (opt-in via `--monitoring`) — namespace `monitoring`. `sims-monitoring` chart wraps `kube-prometheus-stack` and contributes a vendor-specific `ServiceMonitor` + dashboard CM.

Keeping them independent means:

- Spin-ups without `--monitoring` are fast (no Prometheus, no Grafana).
- `sims gpu monitoring enable --name foo` can add monitoring to an existing cluster without recreating.
- Failures in one stack don't block the other.

## Grafana access

`sims gpu dashboard` runs `kubectl port-forward svc/sims-monitoring-grafana 3000:80 -n monitoring` in the background, stores the PID under `~/.cache/sims/<cluster>/grafana.pid`, prints the URL + credentials (`admin` / `prom-operator` default), and optionally opens the browser. `sims gpu dashboard --stop` kills the forward. No kind `extraPortMappings` and no ingress controller are required.

## Local container registry

On first `sims gpu create`, a local `kind-registry` container is started (idempotent — checked via `docker inspect`). The kind config registers it as a containerd mirror at `localhost:5001`. This lets `sims gpu load-image` push our `fake-rocm-gpu-operator` image into the cluster without rebuilding kind nodes.

## Phased delivery

See [README.md#status](../README.md#status) for the current phase table. Each phase delivers something verifiable end-to-end — Phase 1 is "NVIDIA scheduling works"; Phase 2 adds Grafana; Phases 3-5 build out AMD; Phases 6-7 are polish.
