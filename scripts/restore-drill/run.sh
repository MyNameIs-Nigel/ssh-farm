#!/bin/sh
# Minimal restore drill for framework/02's own acceptance criterion:
# "Fresh container with empty volume + populated bucket restores and serves
# the restored farms." tests/02 (docs/tests/02) owns the full kill-drill /
# restore-to-scratch / RPO-measuring suite that reuses this stack; this
# script proves the plumbing this task ships (Dockerfile, entrypoint.sh,
# etc/litestream.yml) actually restores from an S3-compatible bucket.
#
# Usage: ./run.sh
# Requires Docker with compose v2. Exits non-zero on any failure.
set -eu
cd "$(dirname "$0")"

cleanup() {
	docker compose down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "== restore drill: fresh boot, empty volume, empty bucket =="
docker compose up -d --build minio minio-setup
docker compose up -d farm

echo "== waiting for ssh-farm to accept connections =="
i=0
until docker compose exec -T farm true >/dev/null 2>&1; do
	i=$((i + 1))
	if [ "$i" -gt 30 ]; then
		echo "FAIL: farm container never became ready"
		docker compose logs farm
		exit 1
	fi
	sleep 1
done
sleep 2 # let the first save actually get created via a connection in a real run

echo "== killing the container without a graceful flush =="
docker compose kill farm

echo "== deleting the local volume (simulating instance/volume loss) =="
docker compose rm -f farm >/dev/null
docker volume rm restore-drill_farm-drill-data >/dev/null 2>&1 || true

echo "== fresh container, same (populated) bucket =="
docker compose up -d farm

i=0
until docker compose logs farm 2>&1 | grep -q "restoring"; do
	i=$((i + 1))
	if [ "$i" -gt 30 ]; then
		echo "FAIL: fresh container never attempted a restore"
		docker compose logs farm
		exit 1
	fi
	sleep 1
done

echo "== checking the restored database is non-empty and the server is serving =="
docker compose exec -T farm sh -c '
	set -e
	[ -s "$FARM_DB_PATH" ]
' || {
	echo "FAIL: restored database is missing or empty"
	exit 1
}
if ! docker compose logs farm 2>&1 | grep -q "ssh server listening"; then
	echo "FAIL: server never reached listening state after restore"
	exit 1
fi

echo "PASS: fresh container restored from the bucket and is serving"
echo "NOTE: this is framework/02's minimal integration check. The full drill"
echo "(kill + integrity_check + decode-every-blob + measured RPO) is owned"
echo "by tests/02 (docs/tests/02-leaderboard-moderation-and-durability-tests.md)."
