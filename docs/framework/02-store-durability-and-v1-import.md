# Farm Framework 02 — Store Port, Durable Data & V1 Import

**Phase:** 2 · **Depends on:** framework/01, gameplay/01 (state types) ·
**Blocks:** gameplay/02 (leaderboard queries), framework/03

## Goal

Three jobs in one storage-owning task: (1) port v1's SQLite store and game
manager, (2) make the data durable per the fleet pattern — continuous S3
replication so **no instance/volume failure can lose player progress**, and
(3) the one-time `import-v1` migration that carries every existing farm
into v2.

## Sources & references

| Source | Role |
| --- | --- |
| `../../ssh-idlefarmer/internal/store/*.go` | port: WAL single-connection store, append-only migrations |
| `../../ssh-idlefarmer/internal/game/*.go` | port: manager/actor/session lifecycle (unchanged semantics) |
| `../../ssh-arcadelobby/docs/06-fleet-data-durability.md` | **canonical** durability pattern (Litestream + S3, entrypoint, IAM, runbooks) — this task implements the game side |
| `../../ssh-moonminer/docs/framework/02-persistence-and-save-model.md` | schema/API conventions shared across the fleet |

## Deliverables

- `internal/store/` — ported store + **migration 002** (leaderboard/name
  columns below)
- `internal/game/` — ported manager/actor (store-facing changes only)
- `cmd/ssh-farm/` — `import-v1` subcommand
- `Dockerfile` + `entrypoint.sh` — the Litestream-wrapped image (shape
  defined in the canonical doc; this repo ships the first real one)
- `etc/litestream.yml` — replication config template

## Spec

### Store port + migration 002

v1 schema (`accounts`, `saves`) ports as migration 001, byte-compatible.
Migration 002 adds what the leaderboard needs as **denormalized columns on
`saves`** (the JSON state blob stays the source of truth; these are
indexes into it):

```sql
ALTER TABLE saves ADD COLUMN coins INTEGER NOT NULL DEFAULT 0;
ALTER TABLE saves ADD COLUMN farm_name TEXT NOT NULL DEFAULT '';
ALTER TABLE saves ADD COLUMN name_locked INTEGER NOT NULL DEFAULT 0;  -- moderation lock
CREATE INDEX idx_saves_coins ON saves (coins DESC);
```

- `SaveState` gains the denormalized values (extracted by the caller — the
  actor — from the sim state) and writes them **in the same transaction**
  as the blob: board and blob can never disagree.
- Backfill inside migration 002 is impossible (blob decoding lives above
  the store), so `import-v1` and a boot-time reconcile pass (below) do it.
- New store queries for gameplay/02: `TopSaves(ctx, n, activeSince)`,
  `RankAndTotal(ctx, coins, activeSince)`, `Window(ctx, rank, radius,
  activeSince)` — exact signatures owned by gameplay/02; the store just
  promises indexed answers.

### Durability (implements the canonical fleet doc)

Per `../../ssh-arcadelobby/docs/06-fleet-data-durability.md`:

- The container entrypoint is Litestream:
  `restore -if-db-not-exists -if-replica-exists` then
  `replicate -exec /app/ssh-farm` — the game process never manages
  replication and needs **zero code changes**; this task's job is the
  image, config template, and the discipline below.
- **All persistent state lives in exactly one SQLite file.** Nothing else
  writable matters except the host key, which the entrypoint
  restores/uploads via the bucket's `keys/farm/` prefix (canonical doc).
- WAL mode + single connection port unchanged — Litestream's supported
  configuration.
- **Boot-time reconcile pass**: after `store.Open`, iterate saves where
  `coins = 0 AND farm_name = ''`, decode blobs, backfill denormalized
  columns (covers imports and the 001→002 upgrade). Idempotent, logged.
- Config: `FARM_DB_PATH` default `var/farm.db`; replication is entirely
  outside the binary (env consumed by litestream.yml:
  `LITESTREAM_REPLICA_URL=s3://<bucket>/farm/db`).

### `import-v1` (one-time migration)

`ssh-farm import-v1 --from /path/idlefarm.db [--dry-run]`:

1. Opens the v1 DB read-only; validates its schema version.
2. Copies `accounts` and `saves` rows into the v2 store (which must be
   empty or `--merge` explicitly passed; collision = same
   `(fingerprint, slot)` → abort with a report, never overwrite).
3. Decodes every state blob with the ported sim (gameplay/01) — a blob
   that fails to decode **aborts the import** (parity bug, not a data
   bug), reported with its fingerprint truncated.
4. Backfills `coins` (from state) and `farm_name` (from v1's stored name,
   passed through the gameplay/03 filter — a now-denied v1 name imports as
   the generated default with a log line).
5. `--dry-run` prints the full report without writing.

Run at cutover with v1 stopped (framework/03 owns the runbook).

## Acceptance criteria

- [ ] Ported store passes v1's test suite (adapted names) + migration 002
  round-trip; blob and columns update atomically (crash-injection test:
  fail between → transaction proves both-or-neither).
- [ ] Fresh container with empty volume + populated bucket restores and
  serves the restored farms (integration script; also the tests/02 drill).
- [ ] `import-v1` on a copy of a real v1 DB: all rows land, every blob
  decodes, coins/name backfilled, dry-run writes nothing, collision
  aborts.
- [ ] Reconcile pass backfills and is a no-op on second boot.
- [ ] The image contains litestream + game binary, runs non-root,
  read-only rootfs except volume + `/tmp`.

## Out of scope / handoffs

- Bucket/IAM provisioning + fleet rollout → canonical doc + framework/03.
- Rank math, caching, activity window → gameplay/02.
- Name filtering rules → gameplay/03 (import calls its exported filter).
