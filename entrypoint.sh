#!/bin/sh
# Container entrypoint implementing the canonical fleet durability pattern
# (../ssh-arcadelobby/docs/06-fleet-data-durability.md): seed the SSH host key
# from the host's canonical copy if this volume has none, restore the SQLite
# file, then hand off to litestream, which replicates while ssh-farm runs as
# its child process. The game binary needs zero code changes for any of this.
set -eu

DB_PATH="${FARM_DB_PATH:-/var/lib/farm/farm.db}"
HOST_KEY_PATH="${FARM_HOST_KEY_PATH:-/var/lib/farm/ssh_host_key}"
# Read-only bind mount of the host's canonical copy of this service's SSH host
# key (/srv/ssharcade/private/keys/farm on the box).
#
# This replaces an `mc cat` from S3, and with it the static AWS key that used
# to sit in this container's environment as MC_HOST_s3. mc has no support for
# the EC2 instance role, so using it meant a long-lived access key pair in the
# environment of all four services — the one credential in the fleet that could
# not be replaced by the role litestream already uses. The host key is now host
# state, provisioned and backed up exactly like the proxy key and the denylist.
HOST_KEY_SOURCE="${FARM_HOST_KEY_SOURCE:-}"

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

# Seed the data volume from the host's copy when this volume has no key yet —
# a rebuilt host, or a volume recreated by a compose change. A normal boot
# skips this entirely, because the volume already holds the key.
#
# What is at stake here is identity, not data: with no key the app generates a
# fresh one, and every player who has ever connected gets their client's
# host-key-changed warning, which is indistinguishable from a MITM.
if [ -n "$HOST_KEY_SOURCE" ] && [ ! -f "$HOST_KEY_PATH" ]; then
	if [ -s "$HOST_KEY_SOURCE" ]; then
		echo "entrypoint: seeding host key from $HOST_KEY_SOURCE"
		# Write to a temp path and promote only once the copy is complete.
		# A partially written file at $HOST_KEY_PATH would still satisfy the
		# app's "does a key already exist" check and skip generation, which
		# crashes the server with "ssh: no key found".
		if ! cp "$HOST_KEY_SOURCE" "$HOST_KEY_PATH.tmp"; then
			rm -f "$HOST_KEY_PATH.tmp"
			echo "entrypoint: FATAL — $HOST_KEY_SOURCE is mounted but unreadable; refusing to start rather than serve under a different host key" >&2
			exit 1
		fi
		chmod 600 "$HOST_KEY_PATH.tmp"
		mv "$HOST_KEY_PATH.tmp" "$HOST_KEY_PATH"
	else
		# Deliberately not fatal: a genuine first-ever boot has no key
		# anywhere and the app generating one is the correct outcome. Loud,
		# because on any later boot it means the host mount is missing and
		# players are about to see a changed host key.
		echo "entrypoint: WARNING — $HOST_KEY_SOURCE is empty or absent; the app will generate a NEW host key and every returning player will see a host-key-changed warning" >&2
	fi
fi

# Dev mode: no replica configured, skip straight to the app with a loud log
# line. Local dev and CI must never require AWS/MinIO credentials.
if [ -z "${LITESTREAM_REPLICA_URL:-}" ]; then
	echo "entrypoint: LITESTREAM_REPLICA_URL not set — running WITHOUT replication (dev mode)"
	exec /app/ssh-farm
fi

# -config defaults to /etc/litestream.yml, which resolves $FARM_DB_PATH and
# $LITESTREAM_REPLICA_URL — the single source of truth for both this restore
# and the replicate call below, so they can never point at different places.
echo "entrypoint: restoring $DB_PATH from $LITESTREAM_REPLICA_URL if needed"
litestream restore -if-db-not-exists -if-replica-exists "$DB_PATH"

# There is deliberately no upload-the-host-key-back-to-S3 step any more, and no
# denylist fallback fetch. Both existed because the container was the only
# thing that could reach the bucket for small non-database files, and both
# needed write/read credentials inside the container to do it. Those files are
# host state now; the host backs them up with its own instance role (see
# ../ssh-arcadelobby/deploy/README.md), so this container authenticates to
# nothing except through litestream, which uses the instance role too.

exec litestream replicate -exec "/app/ssh-farm"
