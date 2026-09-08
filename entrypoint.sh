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

# Production guard (§4.1, failure mode 1). The dev-mode fall-through below is a
# real convenience — local dev and CI must never need AWS credentials — but in
# production it is the most dangerous failure in the fleet: the game serves
# players normally while nothing replicates, and the loss stays silent until
# someone actually needs a restore. Setting FARM_REQUIRE_REPLICATION=true turns
# that fall-through into a crash loop, which gets noticed in seconds instead.
if [ "${FARM_REQUIRE_REPLICATION:-}" = "true" ] && [ -z "${LITESTREAM_REPLICA_URL:-}" ]; then
	echo "entrypoint: FATAL — FARM_REQUIRE_REPLICATION=true but LITESTREAM_REPLICA_URL is unset; refusing to serve unreplicated" >&2
	exit 1
fi

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

# §5.5 durable copy. The denylist exists in no repository, so if the host file
# is missing — a rebuilt host whose private/ directory has not been provisioned
# yet — the bucket is the only place left to get it. Restore into the data
# volume and point the app there; the host mount stays canonical whenever it is
# present. FARM_REQUIRE_MODERATION still decides the outcome when neither
# exists: the app refuses to boot rather than serve on the dev fixture.
#
# INERT until FARM_MODERATION_MC_PATH is set, which the compose file
# deliberately does not do yet: the MC_HOST_s3 credential is scoped to
# keys/* only, so it cannot read a private/ object without an IAM change.
if [ -n "${FARM_MODERATION_MC_PATH:-}" ] && [ -n "${FARM_MODERATION_PATH:-}" ] \
	&& [ ! -f "$FARM_MODERATION_PATH" ]; then
	fallback="$(dirname "$DB_PATH")/denylist.toml"
	echo "entrypoint: $FARM_MODERATION_PATH absent — restoring denylist from $FARM_MODERATION_MC_PATH"
	# Same promote-only-if-non-empty guard as the host key above, and for the
	# same reason: a bare redirect creates the target before mc runs, and an
	# empty denylist would parse fine and match nothing at all.
	if mc cat "$FARM_MODERATION_MC_PATH" >"$fallback.tmp" 2>/dev/null && [ -s "$fallback.tmp" ]; then
		mv "$fallback.tmp" "$fallback"
		chmod 400 "$fallback"
		FARM_MODERATION_PATH="$fallback"
		export FARM_MODERATION_PATH
		echo "entrypoint: denylist restored to $fallback"
	else
		rm -f "$fallback.tmp"
		echo "entrypoint: no denylist in the bucket either — boot will fail if FARM_REQUIRE_MODERATION=true, which is the intended outcome"
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
				# Seed the bucket only when it is genuinely empty. This used
				# to upload unconditionally, so a boot from a volume holding
				# a stale key overwrote the good key in S3 and gave every
				# player a host-key-changed warning. A failed `mc stat` also
				# skips the upload: not seeding is recoverable, clobbering is not.
				if mc stat "$HOST_KEY_MC_PATH" >/dev/null 2>&1; then
					echo "entrypoint: host key already in bucket at $HOST_KEY_MC_PATH — not overwriting"
				else
					mc pipe "$HOST_KEY_MC_PATH" <"$HOST_KEY_PATH" 2>/dev/null \
						&& echo "entrypoint: host key uploaded to $HOST_KEY_MC_PATH"
				fi
				break
			fi
			i=$((i + 1))
			sleep 1
		done
	) &
fi

exec litestream replicate -exec "/app/ssh-farm"
