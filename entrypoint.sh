#!/bin/sh
# Container entrypoint implementing the canonical fleet durability pattern
# (../ssh-arcadelobby/docs/06-fleet-data-durability.md): restore the SQLite
# file and SSH host key from S3 if present, then hand off to litestream,
# which replicates while ssh-farm runs as its child process. The game
# binary needs zero code changes for any of this.
set -eu

DB_PATH="${FARM_DB_PATH:-/var/lib/farm/farm.db}"
HOST_KEY_PATH="${FARM_HOST_KEY_PATH:-/var/lib/farm/ssh_host_key}"
# mc's own alias/bucket/key path, e.g. "s3/<bucket>/keys/farm/ssh_host_key" —
# the alias's endpoint/credentials come from an MC_HOST_<alias> env var (mc's
# env-based alias mechanism), so this script has no bucket/region/credential
# logic of its own and works unchanged against real S3 or the MinIO used by
# local/CI durability drills (which uses alias "drill").
HOST_KEY_MC_PATH="${FARM_HOST_KEY_MC_PATH:-}"

# Dev mode: no replica configured, skip straight to the app with a loud log
# line. Local dev and CI must never require AWS/MinIO credentials.
if [ -z "${LITESTREAM_REPLICA_URL:-}" ]; then
	echo "entrypoint: LITESTREAM_REPLICA_URL not set — running WITHOUT replication (dev mode)"
	exec /app/ssh-farm
fi

if [ -n "$HOST_KEY_MC_PATH" ] && [ ! -f "$HOST_KEY_PATH" ]; then
	echo "entrypoint: restoring host key from $HOST_KEY_MC_PATH"
	# Write to a temp path first: on a genuinely first-ever boot the bucket
	# has no key yet and `mc cat` fails, but a bare `>"$HOST_KEY_PATH"`
	# redirect still creates/truncates its target before mc even runs —
	# leaving a 0-byte file at $HOST_KEY_PATH that satisfies the app's own
	# "does a key already exist" check and skips key generation entirely,
	# crashing the server with "ssh: no key found". Only promote the temp
	# file if mc actually produced non-empty content.
	if mc cat "$HOST_KEY_MC_PATH" >"$HOST_KEY_PATH.tmp" 2>/dev/null && [ -s "$HOST_KEY_PATH.tmp" ]; then
		mv "$HOST_KEY_PATH.tmp" "$HOST_KEY_PATH"
		chmod 600 "$HOST_KEY_PATH"
	else
		rm -f "$HOST_KEY_PATH.tmp"
		echo "entrypoint: no host key found in bucket yet — a new one will be generated and uploaded"
	fi
fi

# -config defaults to /etc/litestream.yml, which resolves $FARM_DB_PATH and
# $LITESTREAM_REPLICA_URL — the single source of truth for both this restore
# and the replicate call below, so they can never point at different places.
echo "entrypoint: restoring $DB_PATH from $LITESTREAM_REPLICA_URL if needed"
litestream restore -if-db-not-exists -if-replica-exists "$DB_PATH"

# The host key may not exist locally yet (first boot ever: wish generates
# one when the server starts). Upload it to the bucket in the background as
# soon as it appears, so this instance seeds the bucket for the next one
# without delaying startup.
if [ -n "$HOST_KEY_MC_PATH" ]; then
	(
		i=0
		while [ "$i" -lt 30 ]; do
			if [ -f "$HOST_KEY_PATH" ]; then
				mc pipe "$HOST_KEY_MC_PATH" <"$HOST_KEY_PATH" 2>/dev/null \
					&& echo "entrypoint: host key uploaded to $HOST_KEY_MC_PATH"
				break
			fi
			i=$((i + 1))
			sleep 1
		done
	) &
fi

exec litestream replicate -exec "/app/ssh-farm"
