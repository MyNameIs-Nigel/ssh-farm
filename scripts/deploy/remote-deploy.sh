#!/bin/sh
# Deploy one immutable ssh-farm build onto the arcade host, verify it is
# actually serving, and put the previous build back if it is not.
#
# This runs ON the production host, shipped there by the Release workflow via
# SSM. It lives in the repository rather than inline in the workflow's SSM
# JSON for three reasons: it can be code-reviewed as a diff, it can be run by
# hand during an incident without reconstructing a JSON array, and a shell
# script in a `commands=[...]` list has no quoting story that survives
# contact with a conditional.
#
# Required environment:
#   FARM_IMAGE_DIGEST   ghcr.io/mynameis-nigel/ssh-farm@sha256:...  (immutable)
#   FARM_IMAGE_REPO     ghcr.io/mynameis-nigel/ssh-farm
#   FARM_EXPECT_VERSION the internal/version.Version this build should serve
# Optional:
#   FARM_COMPOSE_DIR    default /srv/ssharcade
#   FARM_SERVICE        default farm
#   FARM_PROBE_TIMEOUT  default 60 (seconds to wait for the banner)
set -eu

: "${FARM_IMAGE_DIGEST:?FARM_IMAGE_DIGEST is required}"
: "${FARM_IMAGE_REPO:?FARM_IMAGE_REPO is required}"
: "${FARM_EXPECT_VERSION:?FARM_EXPECT_VERSION is required}"
COMPOSE_DIR="${FARM_COMPOSE_DIR:-/srv/ssharcade}"
SERVICE="${FARM_SERVICE:-farm}"
PROBE_TIMEOUT="${FARM_PROBE_TIMEOUT:-60}"

cd "$COMPOSE_DIR"

log() { echo "[deploy] $*"; }

# --- the smoke test ---------------------------------------------------------
# internal/server sets wish's Version option from internal/version.Version, so
# the SSH identification string on the wire is literally "SSH-2.0-<version>".
# That single line is the strongest cheap check available: it proves the
# process started, bound its port, completed enough of its init to accept a
# TCP connection, and is the build we just shipped — none of which
# "the container is still running" tells you.
#
# Probed from inside the container against its own loopback, so it needs no
# published port (the fleet compose deliberately has none) and no extra image.
banner() {
	docker compose exec -T "$SERVICE" \
		sh -c 'timeout 5 nc 127.0.0.1 "${FARM_LISTEN_PORT:-2222}" </dev/null | head -1' \
		2>/dev/null | tr -d '\r\n'
}

wait_for_banner() {
	want="SSH-2.0-$1"
	waited=0
	while [ "$waited" -lt "$PROBE_TIMEOUT" ]; do
		got=$(banner || true)
		if [ "$got" = "$want" ]; then
			log "smoke test OK: serving $got"
			return 0
		fi
		if [ -n "$got" ]; then
			log "banner is '$got', waiting for '$want' (${waited}s)"
		fi
		sleep 2
		waited=$((waited + 2))
	done
	log "smoke test FAILED: after ${PROBE_TIMEOUT}s the banner is '${got:-<no response>}', wanted '$want'"
	return 1
}

# --- validate the instrument before trusting it -----------------------------
# A probe that cannot work will report every deploy as broken and roll back a
# perfectly good build — a worse outcome than having no smoke test at all,
# because it looks like a real failure. So before touching anything, prove the
# probe can read a banner from the container that is ALREADY running and
# known good. If it cannot, the probe is broken, not the build: stop, change
# nothing, and say so.
if docker compose ps --status running --quiet "$SERVICE" | grep -q .; then
	if [ -z "$(banner || true)" ]; then
		log "FATAL: the smoke-test probe got no banner from the currently running"
		log "       $SERVICE, which is serving players right now. That means the"
		log "       probe itself is broken (no busybox nc in the image? port moved?),"
		log "       not the new build. Refusing to deploy behind a broken health"
		log "       check. Nothing has been changed."
		exit 1
	fi
	log "probe validated against the running container"
else
	log "no running $SERVICE — first deploy on this host; skipping probe validation"
fi

# --- capture the rollback target before changing anything -------------------
PREVIOUS=$(docker image inspect "$FARM_IMAGE_REPO:latest" \
	--format '{{if .RepoDigests}}{{index .RepoDigests 0}}{{end}}' 2>/dev/null || true)
if [ -n "$PREVIOUS" ]; then
	log "current build: $PREVIOUS"
else
	log "WARNING: no local $FARM_IMAGE_REPO:latest to roll back to; this deploy is one-way"
fi

# --- deploy the exact digest CI built and tested ----------------------------
# Pulling :latest would deploy whatever the tag points at by the time the host
# gets round to pulling, which on two merges in quick succession is not
# necessarily the build this run tested. The digest is the build this run
# tested. Re-tagging it locally as :latest means the fleet compose file needs
# no change and stays the fleet's to own.
log "pulling $FARM_IMAGE_DIGEST"
docker pull --quiet "$FARM_IMAGE_DIGEST"
docker tag "$FARM_IMAGE_DIGEST" "$FARM_IMAGE_REPO:latest"

log "restarting $SERVICE (and only $SERVICE)"
docker compose up -d --no-deps --pull never "$SERVICE"

if wait_for_banner "$FARM_EXPECT_VERSION"; then
	log "deploy complete: $FARM_IMAGE_DIGEST serving $FARM_EXPECT_VERSION"
	docker image prune -f --filter "until=168h" >/dev/null 2>&1 || true
	exit 0
fi

# --- rollback ---------------------------------------------------------------
log "=== rolling back ==="
docker compose logs --no-color --tail 80 "$SERVICE" || true

if [ -z "$PREVIOUS" ]; then
	log "FATAL: nothing to roll back to. $SERVICE is down or serving the wrong build."
	exit 1
fi

docker tag "$PREVIOUS" "$FARM_IMAGE_REPO:latest"
docker compose up -d --no-deps --pull never "$SERVICE"

# The rollback target's version is not known here, so accept any well-formed
# SSH banner: the goal is "players can connect again", not "a specific build".
waited=0
while [ "$waited" -lt "$PROBE_TIMEOUT" ]; do
	case "$(banner || true)" in
	SSH-2.0-*)
		log "rolled back to $PREVIOUS and it is serving"
		exit 1
		;;
	esac
	sleep 2
	waited=$((waited + 2))
done

log "FATAL: rollback to $PREVIOUS did not come back up either. $SERVICE is DOWN."
exit 1
