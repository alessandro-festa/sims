# Contributing

## Development setup

```bash
git clone https://github.com/alessandro-festa/sims.git
cd sims
go mod download
go build ./...
go test ./...
```

## Repository layout

```
sims/
  cmd/sims/main.go             # cobra entrypoint
  pkg/
    cli/                       # cobra command implementations
    cluster/                   # kind orchestration (sigs.k8s.io/kind/pkg/cluster)
    helm/                      # helm SDK wrapper (helm.sh/helm/v3/pkg/action)
    kube/                      # client-go helpers (port-forward, wait-for-ready)
    config/                    # per-vendor kind config templating
  charts/
    sims-nvidia/               # wraps run-ai/fake-gpu-operator
    sims-amd/                  # wraps our fake-rocm-gpu-operator
    sims-monitoring/           # wraps kube-prometheus-stack + vendor ServiceMonitor + dashboard CM
  operators/
    fake-rocm-gpu-operator/    # new: AMD-side sister to fake-gpu-operator
      cmd/{device-plugin,metrics-exporter,status-updater,node-labeller,controller}/
      pkg/{deviceplugin,topology,metrics,simulate}/
      api/v1alpha1/            # DeviceConfig CRD (Phase 6)
      chart/                   # Helm chart shipped via charts/sims-amd dependency
      Dockerfile               # one image, subcommand entrypoint
  dashboards/
    nvidia-dcgm.json           # Grafana ID 12239, vendored
    amd-gpu.json               # Grafana ID 23715, vendored
  hack/                        # update-dashboards.sh and other dev scripts
  e2e/                         # end-to-end tests driving the CLI itself
  docs/
```

## Coding conventions

- **Go style:** standard `gofmt` + `goimports`. CI runs `go vet ./...` and `golangci-lint run`.
- **Errors:** return errors; do not log-and-continue at library boundaries. CLI commands at the top level translate errors to exit codes.
- **Comments:** only when the *why* is non-obvious (a hidden constraint, a subtle invariant). Don't comment what the code already says.
- **Tests:**
  - Unit tests live next to the code (`foo.go` ↔ `foo_test.go`).
  - `pkg/simulate` tests must use a seeded RNG for determinism.
  - E2E tests in `e2e/` drive the `sims` CLI itself; they only run on Linux (kind needs the Linux kernel).

## Adding a new phase

Each phase should land as a single PR that:

1. Updates the phase table in [README.md](../README.md#status) (mark the phase complete).
2. Adds verification steps in [docs/quickstart.md](quickstart.md) so users can confirm it works.
3. Includes at least one e2e test under `e2e/` that drives the new functionality through the CLI.
4. Updates `docs/architecture.md` if the design changes.

## Working on the fake-rocm-gpu-operator

The fake operator lives in-monorepo (`operators/fake-rocm-gpu-operator/`) until Phase 6, at which point we may extract it to its own repo for an independent release cadence. While in-monorepo:

- Build the image with `docker build -t localhost:5001/fake-rocm-gpu-operator:dev operators/fake-rocm-gpu-operator/`.
- Load it into the cluster via `./bin/sims gpu load-image localhost:5001/fake-rocm-gpu-operator:dev`.
- Helm dependency in `charts/sims-amd/Chart.yaml` points at `file://../../operators/fake-rocm-gpu-operator/chart` during dev; switches to OCI once published.

## Releasing

- Tagged releases via `git tag vX.Y.Z && git push --tags`.
- GoReleaser builds `sims` binaries for darwin/arm64, darwin/amd64, linux/amd64, linux/arm64.
- The fake-rocm-gpu-operator image is published to `ghcr.io/alessandro-festa/fake-rocm-gpu-operator:vX.Y.Z`.
- The umbrella charts are published as OCI artifacts to `ghcr.io/alessandro-festa/charts/sims-{nvidia,amd,monitoring}`.

## License

Apache 2.0. All contributions are accepted under the same license. By submitting a PR you affirm you have the right to do so.
