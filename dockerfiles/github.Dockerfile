# syntax=docker/dockerfile:1.7

FROM golang:1.26-trixie AS build

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

COPY go.mod go.sum ./

RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY internal ./internal
COPY cmd ./cmd

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p /out/bin && \
    CGO_ENABLED=0 \
    GOOS=${TARGETOS:-linux} \
    GOARCH=${TARGETARCH:-amd64} \
    go build \
      -trimpath \
      -o /out/bin/flatgit \
      ./cmd/flatgit

FROM debian:trixie-slim AS runtime

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      ca-certificates \
      git \
      tzdata && \
    rm -rf /var/lib/apt/lists/*

RUN groupadd -r -g 10001 flatgit && \
    useradd -r -u 10001 -g 10001 -d /var/lib/flatgit -s /usr/sbin/nologin flatgit && \
    mkdir -p /var/lib/flatgit /tmp && \
    chown -R 10001:10001 /var/lib/flatgit && \
    chmod 1777 /tmp

COPY --from=build /out/bin/flatgit /usr/local/bin/flatgit

USER 10001:10001

VOLUME ["/var/lib/flatgit"]

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/flatgit"]