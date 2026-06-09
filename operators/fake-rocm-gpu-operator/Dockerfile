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
    -o /out/fake-rocm-gpu-operator ./cmd/fake-rocm-gpu-operator

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/fake-rocm-gpu-operator /fake-rocm-gpu-operator
USER nonroot:nonroot
ENTRYPOINT ["/fake-rocm-gpu-operator"]
