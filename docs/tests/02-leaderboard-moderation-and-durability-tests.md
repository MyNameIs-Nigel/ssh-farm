# Farm Tests 02 — Leaderboard, Moderation & Durability Tests

**Phase:** 4 · **Depends on:** gameplay/02, gameplay/03, framework/02

## Goal

The three v2-only systems, tested to the level their blast radius
deserves: the leaderboard is the most visible feature in the game, the
moderation layer is the reputational one, and the durability layer is the
one that must work on the worst day the fleet ever has.

## Deliverables

- `internal/leaderboard/` + `internal/moderation/` suites (much is already
  demanded by their task docs' acceptance lists — this doc adds the
  cross-cutting and adversarial layers)
- `scripts/restore-drill/` — the durability drill, runnable locally
  against MinIO and for-real against S3

## Spec

### Leaderboard (beyond gameplay/02's unit list)

- **Concurrency torture**: 50 goroutine-driven actors flushing coin
  updates while 50 readers call `Get` — `-race` clean, ranks always
  internally consistent (a snapshot never shows two farms with the same
  rank+different coins ordering violation).
- **Property test**: random populations (0–5,000 farms, random coins/
  ties/activity ages) → invariants: ranks are competition-ranked,
  `You.Rank` agrees with a naive O(n) recount, `Total` matches the
  filtered population, window is contiguous and dedup'd against top.
- **End-to-end**: scripted SSH session (tests/01 harness) earns coins,
  disconnects (flush), reconnects → board screen shows the new rank.

### Moderation (beyond gameplay/03's unit list)

- **Corpus regression harness**: two embedded test corpora —
  `must_block.txt` (placeholder-token patterns exercising every evasion
  class: leet, separators, repeats, embedding, mixed) and
  `must_allow.txt` (Scunthorpe words, farm vocabulary, the entire
  generated-name product). CI fails if any line crosses sides. Real slurs
  live only in the private denylist file; corpora use the injected
  test-token convention.
- **No-oracle check**: rejection message + response timing identical for
  invalid-charset vs denied names (coarse timing assertion — no
  distinguishable fast-path).
- **Retroactivity end-to-end**: name passes → lands on board → denylist
  gains the term → next snapshot masks it; `name_locked` survives a
  rename attempt through the real action path.

### Durability (the drills — mostly scripts, run on a schedule, not unit tests)

Local (CI-able, MinIO container):

- **Kill drill**: start the litestream-wrapped container against MinIO,
  play scripted sessions, `docker kill` (no graceful flush), delete the
  volume, restart → all farms present; loss window ≤ the autosave
  interval + sync lag; measure and log actual RPO.
- **Restore-to-scratch**: `litestream restore` into a fresh dir →
  `PRAGMA integrity_check` + decode-every-blob pass green.
- **No-credentials dev mode**: container without S3 env runs the game
  (durability off, loud log warning) — dev must never require AWS.

Production (runbook cadence, per the canonical doc
`../../ssh-arcadelobby/docs/06-fleet-data-durability.md`):

- Quarterly: restore the live bucket generation to a scratch container,
  integrity + decode pass, record RPO/RTO observed. The drill script is
  the same one CI runs — that's the point of writing it once.

## Acceptance criteria

- [ ] All of the above green locally (`-race`) on Windows and Linux;
  MinIO-based drills runnable in CI (Linux) and skipped cleanly where
  Docker isn't available.
- [ ] Drill script exits non-zero on any integrity/decode failure and
  prints the measured RPO — it's a monitoring tool, not just a test.
- [ ] Corpus files documented with the placeholder-token convention so
  contributors extend them without writing slurs into git history.
- [ ] A deliberate rank-math bug (off-by-one in the tie rule) and a
  deliberate filter bypass (drop the leet fold) each fail named tests —
  verified once, noted in PR.

## Out of scope

- S3 bucket/IAM correctness in AWS → canonical doc's provisioning
  checklist (framework/03 references it).
- v1 parity/import/server suites → tests/01.
