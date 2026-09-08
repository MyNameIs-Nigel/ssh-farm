# syntax=docker/dockerfile:1

# ---- Build stage: compile a static, cgo-free binary -------------------------
# The pure-Go SQLite driver (modernc.org/sqlite) and embedded content files
# mean the result is a single self-contained executable.
FROM golang:1.26.4-alpine3.22 AS build

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

# ---- Litestream: pinned, pulled as a static binary --------------------------
FROM litestream/litestream:0.5.12 AS litestream

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

COPY --from=litestream /usr/local/bin/litestream /usr/local/bin/litestream
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
