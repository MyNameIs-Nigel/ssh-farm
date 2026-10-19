# syntax=docker/dockerfile:1

# Test-only replacements for the retired upstream Docker Hub images. Build
# the same releases from immutable source commits; no registry login needed.
FROM golang:1.26.9-alpine3.23 AS builder
RUN apk add --no-cache git
ENV CGO_ENABLED=0 GOBIN=/out

FROM builder AS minio-build
# RELEASE.2025-04-08T15-41-24Z
RUN go install -trimpath -ldflags="-s -w" github.com/minio/minio@d0cada583fce88f60cb276ddfb06f5cb16820069

FROM builder AS mc-build
# RELEASE.2025-08-13T08-35-41Z
RUN go install -trimpath -ldflags="-s -w" github.com/minio/mc@7394ce0dd2a80935aded936b09fa12cbb3cb8096

FROM alpine:3.24 AS runtime
RUN apk add --no-cache ca-certificates

FROM runtime AS mc
COPY --from=mc-build /out/mc /usr/local/bin/mc
ENTRYPOINT ["mc"]

FROM runtime AS minio
COPY --from=minio-build /out/minio /usr/local/bin/minio
# The server healthcheck uses mc, just like the former upstream image.
COPY --from=mc-build /out/mc /usr/local/bin/mc
ENTRYPOINT ["minio"]
