# Farm Tests 01 — Parity, Import & Server Tests

**Phase:** 4 (start as targets land) · **Depends on:** gameplay/01,
framework/01–02

## Goal

Prove the three claims that make v2 safe to ship over a live v1: the sim
is identical, the import is lossless, and the server behaves like the
fleet expects. Most of this suite is inherited — port v1's tests, adopt
moonminer's specs — so this doc lists only what's new or different.

## References

| Source | Role |
| --- | --- |
| `../../ssh-idlefarmer/internal/{sim,store,game,server,tui}/**_test.go` | ported wholesale with the code |
| `../../ssh-moonminer/docs/tests/02-server-integration-tests.md` | the in-process SSH harness + proxied-identity matrix — implement it verbatim for `FARM_*` |
| gameplay/01 parity harness | already spec'd there; this task extends it |

## Deliverables

- Ported v1 test suites (land with each port task)
- `internal/sim/` parity goldens + generation README (per gameplay/01)
- `cmd/ssh-farm/` import tests; `internal/server/` integration suite

## Spec — the new tests

### Parity (extends gameplay/01's harness)

- **Long-run drift test**: a 10,000-step scripted session (plant/advance/
  sell/rebirth cycles) replayed against a committed v1-generated final
  state — catches accumulation bugs a short golden misses.
- **Decode-everything test hook**: a build-tagged test that takes a real
  v1 DB path via env (`FARM_TEST_V1_DB`) and decodes every blob in it —
  run manually against the production copy before cutover; skipped in CI.

### Import (framework/02's subcommand)

- Synthetic v1 DB fixtures (built by test code with v1's schema): happy
  path row counts + blob byte-equality; collision abort; corrupt-blob
  abort names the offending row; dry-run leaves zero writes (file hash
  compare); denied v1 name → generated default (uses a test-only
  denylist).
- Idempotence: a completed import re-run aborts cleanly (non-empty
  target).

### Server integration (mirror moonminer tests/02)

Full matrix: auth both modes, malformed proxied usernames, forged-direct,
caps by resolved fingerprint, takeover/refuse, shutdown flush, host-key
stability, no-PTY hint — plus one farm-specific row:

- **Offline catch-up over proxy**: connect proxied, disconnect, advance
  injected clock, reconnect proxied → away-summary reflects the gap
  (proves identity continuity carries the catch-up path).

## Acceptance criteria

- [ ] `go test ./... -race -count=2` green on Windows and Linux.
- [ ] Parity goldens regenerate reproducibly (documented command against
  the v1 module) and any sim edit fails at least one parity test
  (verified once, noted in PR).
- [ ] The pre-cutover manual checklist (decode-everything + import
  dry-run) is documented in framework/03's runbook and rehearsed.
- [ ] Suite < 90 s excluding the manual build-tagged tests.

## Out of scope

- Leaderboard/moderation/durability suites → tests/02.
- TUI tests → land inside tui/01 and tui/02 per their acceptance lists
  (pattern: `../../ssh-moonminer/docs/tests/03-tui-rendering-and-input-tests.md`).
