IMAGE      ?= fake-rocm-gpu-operator
TAG        ?= dev
KIND_CLUSTER ?= sims-amd

# controller-gen produces zz_generated_deepcopy.go + chart/crds/*.yaml from
# the kubebuilder markers in api/. Pinned so generation is reproducible.
CONTROLLER_GEN_VERSION ?= v0.16.5
CONTROLLER_GEN = go run sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION)

.PHONY: build image test kind-load lint clean generate manifests

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
		-o bin/fake-rocm-gpu-operator ./cmd/fake-rocm-gpu-operator

test:
	go test -race -count=1 ./...

image:
	docker buildx build --load -t $(IMAGE):$(TAG) .

kind-load: image
	kind load docker-image $(IMAGE):$(TAG) --name $(KIND_CLUSTER)

clean:
	rm -rf bin

# Regenerate api/v1alpha1/zz_generated_deepcopy.go from the type definitions.
generate:
	$(CONTROLLER_GEN) object paths=./api/...

# Regenerate the CRD manifest from the kubebuilder markers. Lives under
# chart/templates/ (not chart/crds/) so Helm installs it even when this
# chart ships as a subchart of sims-amd — Helm's auto-install only
# processes chart/crds/ on the TOP-LEVEL chart, not on subcharts.
manifests:
	$(CONTROLLER_GEN) crd paths=./api/... output:crd:dir=chart/templates
	@# controller-gen names files by group; standardise to crd.yaml.
	@if [ -f chart/templates/amd.sims.io_deviceconfigs.yaml ]; then \
		mv chart/templates/amd.sims.io_deviceconfigs.yaml chart/templates/crd.yaml; \
	fi
