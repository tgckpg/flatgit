include build.env
-include build.env.work
export

OUT_DIR := out
BIN_DIR := bin

IMAGE_NAME ?= penguinade/flatgit
IMAGE_TAG ?= dev

VERSION ?= dev
GIT_REV := $(shell git rev-parse HEAD)

BUILDINFO_FILE := internal/buildinfo/buildinfo_gen.go
BUILDX_BUILDER ?= container-builder

all: test build

ensure-buildx:
	@if ! docker buildx inspect $(BUILDX_BUILDER) >/dev/null 2>&1; then \
		echo "Creating buildx builder $(BUILDX_BUILDER)..."; \
		docker buildx create \
			--name $(BUILDX_BUILDER) \
			--driver docker-container \
			--driver-opt network=host \
			--bootstrap --use; \
	else \
		echo "Using existing buildx builder $(BUILDX_BUILDER)"; \
		docker buildx use $(BUILDX_BUILDER); \
	fi

.buildinfo:
	@mkdir -p $(dir $(BUILDINFO_FILE))
	@printf '%s\n' \
		'package buildinfo' \
		'' \
		'const (' \
		'    Version     = "$(VERSION)"' \
		'    GitRevision = "$(GIT_REV)"' \
		'    Timestamp   = "'$$(TZ=UTC date +%Y%m%d.%H%M%S)'"' \
		')' \
	> $(BUILDINFO_FILE)

build: .buildinfo
	mkdir -p "$(OUT_DIR)" "$(BIN_DIR)"
	go build -o "$(BIN_DIR)/flatgit" ./cmd/flatgit

push: .buildinfo
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		--build-arg APT_PROXY=$(APT_PROXY) \
		-f dockerfiles/runtime.Dockerfile \
		-t $(IMAGE_NAME):$(IMAGE_TAG) \
		--push .

test:
	go test ./...

fmt:
	gofmt -w ./cmd ./internal

run: build
	$(BIN_DIR)/flatgit daemon -c examples/tinyproxy.json --v=2

vet:
	go vet ./...

clean:
	rm -f "$(BIN_DIR)/flatgit"
	rm -rf dist

.PHONY: all build test fmt vet clean run push ensure-buildx
