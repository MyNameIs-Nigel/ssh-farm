#!/bin/sh
# Confirms entrypoint.sh's dev-mode branch (docs/tests/02): with no
# LITESTREAM_REPLICA_URL set, local dev and CI must never require AWS/MinIO
# credentials or attempt any network call to S3 — they just run the game
# directly. Run with --network none: if the entrypoint ever tried an S3/MinIO
# call in dev mode, it would hang or fail here, since there is no network to
# reach it on — a stronger proof than grepping logs alone.
#
# Usage: ./no-credentials-check.sh
# Requires Docker. Exits non-zero on any failure.
set -eu
cd "$(dirname "$0")"

IMAGE="ssh-farm-devmode-check:latest"
CONTAINER="ssh-farm-devmode-check"
cleanup() {
	docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "== building the real ssh-farm image (same Dockerfile the fleet ships) =="
docker build -q -t "$IMAGE" ../.. >/dev/null

echo "== running with --network none and zero S3/MinIO env set =="
docker run -d --name "$CONTAINER" --network none "$IMAGE" >/dev/null

echo "== waiting for the dev-mode log line =="
i=0
until docker logs "$CONTAINER" 2>&1 | grep -q "running WITHOUT replication (dev mode)"; do
	i=$((i + 1))
	if [ "$i" -gt 15 ]; then
		echo "FAIL: dev-mode log line never appeared"
		docker logs "$CONTAINER"
		exit 1
	fi
	sleep 1
done

echo "== waiting for it to actually start serving (proves it never blocked trying to reach S3) =="
i=0
until docker logs "$CONTAINER" 2>&1 | grep -q "ssh server listening"; do
	i=$((i + 1))
	if [ "$i" -gt 15 ]; then
		echo "FAIL: server never reached listening state in dev mode"
		docker logs "$CONTAINER"
		exit 1
	fi
	sleep 1
done

# Exclude entrypoint's own dev-mode announcement: it names the
# LITESTREAM_REPLICA_URL env var it found unset, which would otherwise
# false-positive against the "litestream" keyword below. Real litestream/mc
# activity never carries this prefix.
if docker logs "$CONTAINER" 2>&1 | grep -v '^entrypoint:' | grep -qi "litestream\|s3\|minio\|mc:"; then
	echo "FAIL: dev mode logged S3/MinIO/litestream activity — it should never touch them"
	docker logs "$CONTAINER"
	exit 1
fi

echo "PASS: dev mode served with --network none (no S3/MinIO reachable at all) and never attempted replication"
