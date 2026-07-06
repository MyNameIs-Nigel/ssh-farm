#!/bin/sh
# The full durability kill drill (docs/tests/02), extending framework/02's
# minimal restore-drill/run.sh with the parts tests/02 owns: a real scripted
# session (so there is genuine save data to lose), integrity_check +
# decode-every-save on the restored database (scripts/restore-check, built
# into this same image), and a measured, enforced RPO — this is meant to run
# on a schedule as a monitoring tool, not just as a one-off test, so it exits
# non-zero on any integrity/decode failure or an RPO budget overrun and
# prints the measured number either way.
#
# Usage: ./kill-drill.sh
# Requires Docker with compose v2 and an ssh client. Exits non-zero on any
# failure.
set -eu
cd "$(dirname "$0")"

# Autosave interval (30s default, see internal/config) plus a generous sync
# and drill-overhead buffer — see docs/06-fleet-data-durability.md's RPO
# claim ("seconds" litestream lag + one autosave interval).
MAX_RPO_SECONDS="${MAX_RPO_SECONDS:-60}"

KEYDIR="$(mktemp -d)"
cleanup() {
	docker compose exec -T farm true >/dev/null 2>&1 || true
	docker compose down -v --remove-orphans >/dev/null 2>&1 || true
	rm -rf "$KEYDIR"
}
trap cleanup EXIT

echo "== kill drill: fresh boot =="
docker compose up -d --build minio minio-setup
docker compose up -d farm

echo "== waiting for ssh-farm to accept connections =="
# `docker compose exec -T farm true` only proves the container's shell is
# execable — it goes green a second or so before ssh-farm itself starts
# listening (entrypoint.sh's mc host-key check + litestream restore run
# first), so it let the scripted connect below race the real listener and
# silently fail (its stderr is discarded). Wait for the actual log line
# instead, same signal the post-restore wait already uses further down.
i=0
until docker compose logs farm 2>&1 | grep -q "ssh server listening"; do
	i=$((i + 1))
	if [ "$i" -gt 30 ]; then
		echo "FAIL: farm container never started listening"
		docker compose logs farm
		exit 1
	fi
	sleep 1
done

echo "== playing a real scripted session (creates a genuine save to lose) =="
ssh-keygen -t ed25519 -N "" -f "$KEYDIR/drill_key" -q -C "restore-drill"
# The remote TUI never exits on its own (it's a full-screen bubbletea
# program, not a shell) — closing stdin after `sleep 2` doesn't close the
# session, so without an outer timeout this hangs forever. attachSave
# creates the save row on connect (trust-on-first-sight), before any
# keystrokes, so a bounded connect-and-drop is enough to create genuine data.
( sleep 2 ) | timeout 10 ssh -tt \
	-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=5 \
	-i "$KEYDIR/drill_key" -p 2222 drillplayer@127.0.0.1 >/dev/null 2>&1 || true

echo "== waiting for the session's save to land =="
sleep 2 # litestream's ~1s sync lag (docs/06) plus a buffer

echo "== killing the container without a graceful flush =="
kill_ts=$(date +%s)
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
i=0
until docker compose logs farm 2>&1 | grep -q "ssh server listening"; do
	i=$((i + 1))
	if [ "$i" -gt 30 ]; then
		echo "FAIL: server never reached listening state after restore"
		docker compose logs farm
		exit 1
	fi
	sleep 1
done

echo "== verifying the restored database: integrity_check + decode-every-save =="
check_out="$(docker compose exec -T farm sh -c '/app/restore-check -db "$FARM_DB_PATH"')" || {
	echo "FAIL: restore-check reported a problem:"
	echo "$check_out"
	exit 1
}
echo "$check_out"

saves_line="$(echo "$check_out" | grep '^saves: ')"
saves_count="$(echo "$saves_line" | sed -n 's/^saves: \([0-9]*\).*/\1/p')"
if [ -z "$saves_count" ] || [ "$saves_count" -lt 1 ]; then
	echo "FAIL: expected at least 1 decoded save, restore-check reported: $saves_line"
	exit 1
fi

latest_write="$(echo "$check_out" | sed -n 's/^latest_write: \([0-9]*\).*/\1/p')"
if [ -z "$latest_write" ] || [ "$latest_write" -le 0 ]; then
	echo "FAIL: restore-check reported no latest_write timestamp"
	exit 1
fi

rpo=$((kill_ts - latest_write))
echo "== measured RPO: ${rpo}s (kill_ts=${kill_ts}, latest_write=${latest_write}) =="
if [ "$rpo" -gt "$MAX_RPO_SECONDS" ]; then
	echo "FAIL: measured RPO ${rpo}s exceeds budget ${MAX_RPO_SECONDS}s"
	exit 1
fi
if [ "$rpo" -lt 0 ]; then
	echo "FAIL: measured RPO ${rpo}s is negative — clock skew between host and container?"
	exit 1
fi

echo "PASS: kill drill restored ${saves_count} save(s), integrity+decode clean, RPO ${rpo}s <= ${MAX_RPO_SECONDS}s budget"
