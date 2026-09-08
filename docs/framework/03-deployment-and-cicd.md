# Farm Framework 03 — Deployment, CI/CD & the V1 Cutover

**Phase:** 3 · **Depends on:** framework/01–02 buildable ·
**Parallel-safe with:** gameplay/02, tui/*

## Goal

ssh-farm ships like every fleet service — PR tested, main auto-deployed,
only this service restarted — with two wrinkles the other repos don't have:
it **replaces a live game** (the idlefarmer cutover), and it is the only
service with a **denylist file that must be provisioned on the host before
first deploy**, because it refuses to boot without one.

## References

| Source | Role |
| --- | --- |
| `../../ssh-arcadelobby/docs/04-deployment-and-cicd.md` | fleet compose ownership, workflow template, secrets layout |
| `../../ssh-arcadelobby/docs/06-fleet-data-durability.md` | S3/IAM provisioning the deploy depends on |
| `../../ssh-moonminer/docs/framework/04-config-content-and-deployment.md` | the game-side deployment/CI-CD spec this mirrors |

## Deliverables

- `.github/workflows/ci.yml` + `release.yml`
- Dev-only `docker-compose.yml` (direct port, no proxy keys, optional
  local MinIO for litestream testing)
- Fleet compose service definition + `games.toml` entry (PRs against
  `../../ssh-arcadelobby/deploy/`)
- `docs/runbooks/cutover.md` — the v1→v2 switch, written before it's needed

## Spec

### CI/CD (per the fleet template)

- `ci.yml`: PRs + non-main pushes → `go vet`, `go build`, `go test -race`.
- `release.yml`: main → test → build/push
  `ghcr.io/mynameis-nigel/ssh-farm:latest` + `:sha-<short>`. It stops there.
  `docker compose pull farm && docker compose up -d farm` on the host is a
  manual step (`../ssh-arcadelobby/deploy/README.md` § "Deploying by hand"):
  the deploy job ran on a self-hosted runner on the production host, which
  cannot survive this repo going public, and all four were removed on
  2026-09-04.
- **No registry auth needed**: the GHCR package is public, so the host pulls
  anonymously and holds no Docker credential at all. CI uses the repo-scoped
  `GITHUB_TOKEN` to publish; no extra secrets.
- **Denylist provisioning is a first-deploy prerequisite.** `farm` runs with
  `FARM_REQUIRE_MODERATION=true` and will not start without
  `/srv/ssharcade/private/farm/denylist.toml` (0400, root, mounted read-only
  as a directory — see the compose file for why a directory and not a single
  file). Provision it before the first `up -d farm` on a new host, or the
  service crash-loops by design. Tighten the list later with
  `docker compose kill -s HUP farm`; no redeploy.
- Deploys are safe mid-session by construction (v1's ported shutdown
  flush); the lobby shows `○ OFFLINE` during the restart seconds.

### Fleet compose service (PR to `../../ssh-arcadelobby/deploy/`)

Mirrors the moonminer service shape (private network, **no `ports:`**,
read-only, non-root, `stop_grace_period: 45s`) plus the durability env from
the canonical doc:

```yaml
farm:
  image: ghcr.io/mynameis-nigel/ssh-farm:latest
  environment:
    FARM_LISTEN_PORT: "2222"
    FARM_DB_PATH: /var/lib/farm/farm.db
    FARM_HOST_KEY_PATH: /var/lib/farm/ssh_host_key
    FARM_PROXY_KEYS_PATH: /etc/farm/proxy_keys
    FARM_RATE_LIMIT_PER_SECOND: "50"
    LITESTREAM_REPLICA_URL: s3://<bucket>/farm/db
    ARCADE_KEYS_URL: s3://<bucket>/keys/farm/     # host-key restore prefix
  volumes:
    - farm-data:/var/lib/farm
    - ./proxy_keys:/etc/farm/proxy_keys:ro
  networks: [ssharcade]
  # credentials via EC2 instance role — no AWS keys in this file
```

### The cutover (runbook, executed once)

1. **Beta window**: `farm` joins `games.toml` as `FARM (V2 BETA)` with a
   fresh empty DB; `idlefarmer` stays. Players try v2; saves don't carry
   yet (message in the v2 onboarding explains the import date).
2. **Freeze + import**: announce in v1's MOTD; `docker compose stop
   idlefarmer`; copy its DB out of the volume; run
   `ssh-farm import-v1 --dry-run` then for real (beta DB is replaced —
   beta was throwaway, stated up front); restart `farm`.
3. **Swap**: `games.toml` — remove `idlefarmer`, rename `farm` to plain
   `FARM` (hot-reloads into the menu). v1 container stays stopped-but-
   present for two weeks (instant rollback), its DB archived to
   `s3://<bucket>/archive/idlefarmer/` with the date.
4. **Retire**: remove the v1 service + volume after the soak; keep the
   archive.

Rollback at any step = stop `farm`, start `idlefarmer`, revert
`games.toml` (v1 DB untouched since freeze).

## Acceptance criteria

- [ ] PR with a failing test blocks; main merge publishes an image, and a
  manual `up -d farm` restarts only `farm` (other services' uptimes
  untouched).
- [ ] Host pulls the public image anonymously, holding no registry credential.
- [ ] `farm` refuses to boot when the denylist file is absent, and boots when it is present.
- [ ] Dev compose: direct `ssh -p 2222 localhost` works with no AWS
  credentials present (litestream disabled or MinIO-pointed — the game
  must run without S3 in dev).
- [ ] Cutover runbook rehearsed end-to-end against throwaway data before
  the real event (dry-run documented with timings).
- [ ] Bucket shows fresh `farm/db` generations within seconds of play
  (durability live in production).

## Out of scope

- Litestream/S3/IAM provisioning details → canonical doc (arcade 06).
- Moonminer's adoption of the same pattern → its framework/04 (already
  updated to reference the canonical doc).
