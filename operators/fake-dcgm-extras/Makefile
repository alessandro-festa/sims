IMAGE      ?= fake-dcgm-extras
TAG        ?= dev
KIND_CLUSTER ?= sims-nvidia

.PHONY: build image test kind-load lint clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
		-o bin/fake-dcgm-extras ./cmd/fake-dcgm-extras

test:
	go test -race -count=1 ./...

image:
	docker buildx build --load -t $(IMAGE):$(TAG) .

kind-load: image
	kind load docker-image $(IMAGE):$(TAG) --name $(KIND_CLUSTER)

clean:
	rm -rf bin
