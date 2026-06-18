FROM golang:1.26-trixie AS build

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

COPY go.mod go.sum ./

RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY "internal" ./internal
COPY "cmd" ./cmd

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
	mkdir bin && \
    CGO_ENABLED=0 \
    GOOS=$TARGETOS \
    GOARCH=$TARGETARCH \
	go build -trimpath -o "bin/flatgit" ./cmd/flatgit

RUN mkdir -p /out/tmp && chmod 1777 /out/tmp

FROM debian:trixie-slim AS runtime

ARG TARGETOS
ARG TARGETARCH
ARG APT_PROXY

RUN if [ -n "${APT_PROXY}" ]; then \
      echo "Acquire::http::Proxy \"http://${APT_PROXY}\";" > /etc/apt/apt.conf.d/01proxy; \
    fi

RUN apt-get update \
 && apt-get install -y --no-install-recommends git ca-certificates \
 && rm -rf /var/lib/apt/lists/*

COPY --from=build /src/bin/flatgit /usr/local/bin/flatgit
COPY --from=build /out/tmp /tmp

RUN flatgit version

ENTRYPOINT ["/usr/local/bin/flatgit"]
