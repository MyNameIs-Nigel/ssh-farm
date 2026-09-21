# syntax=docker/dockerfile:1

# ---- Build stage: compile a static, cgo-free binary -------------------------
# The pure-Go SQLite driver (modernc.org/sqlite) and embedded content files
# mean the result is a single self-contained executable.
#
# The builder's alpine (3.23) intentionally no longer matches the runtime
# stage's (3.22): the golang image only publishes the two most recent alpine
# variants, and by the time 1.26.6 shipped — the patch carrying the
# encoding/asn1 fix, GO-2026-5972 — the 3.22 variant was gone. The mismatch
# is harmless here and nowhere near the shipped artifact: CGO_ENABLED=0 means
# the binary links no libc, so the builder's userland contributes nothing to
# the image beyond the Go toolchain that produced it.
FROM golang:1.27.1-alpine3.23 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
        -o /out/ssh-farm ./cmd/ssh-farm

# restore-check (docs/tests/02): a read-only integrity/decode verifier the
# durability drills run inside this same image against a restored database —
# built alongside the game so the drill never needs a second image.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
        -o /out/restore-check ./cmd/restore-check

# Pre-create the data directory with the runtime user's ownership so the
# named volume inherits writable permissions on first use.
RUN mkdir -p /out/data-dir && chown 65532:65532 /out/data-dir

# ---- Litestream: pinned, built from source ----------------------------------
# Upstream's prebuilt image is built against whatever Go patch release was
# current the day that tag shipped, which made it the only thing in this image
# we were not compiling ourselves — and, predictably, the only thing the
# release scan ever found. 0.5.12 shipped Go 1.25.11's stdlib and a grpc /
# x-crypto / x-net set with fixed HIGH advisories against them; `scan
# published image` fails the release on HIGH, so a stale upstream binary was
# holding up deploys of unrelated changes.
#
# Bumping the tag alone does not fix that: 0.5.17 still pins x/crypto v0.52.0,
# x/net v0.55.0 and grpc v1.82.1, all of which have fixed HIGH advisories.
# Building here puts litestream on the same toolchain as the game binary (so
# stdlib fixes arrive with the builder bump above, once, for both) and lets
# the transitive bumps that upstream has not taken yet be reviewed as a diff.
#
# The version and every transitive checksum live in build/litestream/go.mod
# and go.sum — a build-only module that nothing imports. Nothing is vendored
# and no source is patched; only dependency versions differ from upstream's.
FROM golang:1.27.1-alpine3.23 AS litestream

WORKDIR /litestream

# go.mod/go.sum alone, so the dependency download caches independently of the
# game's source tree — this stage rebuilds only when the pin changes.
COPY build/litestream/go.mod build/litestream/go.sum ./
RUN go mod download

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
        -o /out/litestream github.com/benbjohnson/litestream/cmd/litestream

# ---- Runtime stage --------------------------------------------------------
# alpine:3, not distroless: per the canonical fleet durability doc
# (../ssh-arcadelobby/docs/06-fleet-data-durability.md), the entrypoint needs
# a shell to seed the host key and hand off to litestream. The
# rest of the hardening (non-root, read-only rootfs, dropped capabilities)
# is unchanged and enforced in docker-compose.yml.
FROM alpine:3.22

RUN apk add --no-cache ca-certificates && \
    addgroup -g 65532 nonroot && \
    adduser -D -H -u 65532 -G nonroot nonroot

COPY --from=litestream /out/litestream /usr/local/bin/litestream
COPY --from=build /out/ssh-farm /app/ssh-farm
COPY --from=build /out/restore-check /app/restore-check
COPY --from=build --chown=65532:65532 /out/data-dir /var/lib/farm
COPY etc/litestream.yml /etc/litestream.yml
COPY entrypoint.sh /entrypoint.sh
RUN chmod 755 /entrypoint.sh

# The app's own default port is 22; inside the container it listens on an
# unprivileged port instead and the operator maps host 22 to it, so the
# process never needs root or CAP_NET_BIND_SERVICE.
#
# HOME: the nonroot user has no /home/nonroot and cannot create one (/home is
# root-owned). Nothing requires it now that mc is gone, but a read-only rootfs
# with HOME unset is a footgun for any tool added later, so it stays pointed at
# the tmpfs.
ENV FARM_LISTEN_PORT=2222 \
    FARM_HOST_KEY_PATH=/var/lib/farm/ssh_host_key \
    FARM_DB_PATH=/var/lib/farm/farm.db \
    HOME=/tmp

EXPOSE 2222

# Player saves and the SSH host key live here — always mount a volume, or
# both are lost (and clients see host-key warnings) on every redeploy. The
# volume is a cache: durability comes from litestream replicating it to S3
# (LITESTREAM_REPLICA_URL), not from the volume itself.
VOLUME ["/var/lib/farm"]

USER nonroot:nonroot

ENTRYPOINT ["/entrypoint.sh"]
