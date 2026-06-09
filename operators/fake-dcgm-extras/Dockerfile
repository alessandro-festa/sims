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
    -o /out/fake-dcgm-extras ./cmd/fake-dcgm-extras

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/fake-dcgm-extras /fake-dcgm-extras
# Numeric UID — see fake-rocm-gpu-operator/Dockerfile for the reason.
USER 65532:65532
ENTRYPOINT ["/fake-dcgm-extras"]
