# syntax=docker/dockerfile:1.7

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" \
    -o /out/fake-rocm-gpu-operator ./cmd/fake-rocm-gpu-operator && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" \
    -o /out/rocm-smi-mock ./cmd/rocm-smi-mock

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/fake-rocm-gpu-operator /fake-rocm-gpu-operator
# /rocm-smi-mock is the AMD rocm-smi shim (#49). Mount it into user pods
# via an initContainer + emptyDir to make `rocm-smi` calls succeed inside
# workloads that shell out for sanity checks. See docs/adding-metrics.md.
COPY --from=build /out/rocm-smi-mock /rocm-smi-mock
# Numeric UID so K8s `runAsNonRoot: true` check (which verifies a numeric
# uid != 0) accepts the image — symbolic `nonroot:nonroot` triggers
# "image has non-numeric user, cannot verify user is non-root".
USER 65532:65532
ENTRYPOINT ["/fake-rocm-gpu-operator"]
