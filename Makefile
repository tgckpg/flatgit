include build.env
-include build.env.work
export

OUT_DIR := out
BIN_DIR := bin

VERSION ?= dev
GIT_REV := $(shell git rev-parse HEAD)

BUILDINFO_FILE := internal/buildinfo/buildinfo_gen.go

all: test build

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

test:
	go test ./...

fmt:
	gofmt -w ./cmd ./internal

run: build
	$(BIN_DIR)/flatgit daemon -c examples/tinyproxy.json

vet:
	go vet ./...

clean:
	rm -f "$(BIN_DIR)/flatgit"
	rm -rf dist

.PHONY: all build test fmt vet clean run
