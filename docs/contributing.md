# Contributing

## Development setup

```bash
git clone https://github.com/alessandro-festa/sims.git
cd sims
go mod download
go build ./...
go test ./...
go build -o bin/sims ./cmd/sims && ./bin/sims gpu doctor   # validates Docker, insecure-registries, GHCR
```

## Repository layout

```
sims/
  cmd/sims/main.go             # cobra entrypoint
  pkg/
    cli/                       # cobra command implementations
    cluster/                   # kind orchestration + local registry lifecycle
    helm/                      # helm SDK wrapper (install/upgrade/uninstall + EnsureDependencies)
    kube/                      # client-go helpers: wait-for-capacity / -deployment, namespace, vendor detection
    config/                    # per-vendor kind config templating
  charts/
    sims-nvidia/               # wraps run-ai/fake-gpu-operator (OCI dep)
    sims-amd/                  # wraps our fake-rocm-gpu-operator (Phase 3+)
    sims-monitoring/
      dashboards/              # vendored Grafana dashboard JSON (lives here so Files.Get can reach it)
      templates/               # ServiceMonitor + dashboard ConfigMap, conditional on .Values.vendor
  operators/
    fake-rocm-gpu-operator/    # new: AMD-side sister to fake-gpu-operator (Phase 3+)
      cmd/{device-plugin,metrics-exporter,status-updater,node-labeller,controller}/
      pkg/{deviceplugin,topology,metrics,simulate}/
      api/v1alpha1/            # DeviceConfig CRD (Phase 6)
      chart/                   # Helm chart shipped via charts/sims-amd dependency
      Dockerfile               # one image, subcommand entrypoint
  dashboards/                  # discovery README (the JSON lives under the chart that consumes it)
  hack/update-dashboards.sh    # refresh script for the vendored Grafana JSON
  e2e/                         # end-to-end tests driving the sims CLI; gated by E2E=1
  docs/
```

## Coding conventions

- **Go style:** standard `gofmt` + `goimports`. CI runs `go vet ./...` and `golangci-lint run`.
- **Errors:** return errors; do not log-and-continue at library boundaries. CLI commands at the top level translate errors to exit codes.
- **Comments:** only when the *why* is non-obvious (a hidden constraint, a subtle invariant). Don't comment what the code already says.
- **Tests:**
  - Unit tests live next to the code (`foo.go` ↔ `foo_test.go`).
  - `pkg/simulate` tests must use a seeded RNG for determinism.
  - E2E tests in `e2e/` drive the `sims` CLI itself; gated by `E2E=1`. They need Docker (Docker Desktop on macOS, native on Linux). Run with `E2E=1 go test -timeout 20m ./e2e/...`; without the env they skip cleanly.

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
