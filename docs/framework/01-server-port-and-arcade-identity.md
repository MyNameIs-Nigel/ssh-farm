# Farm Framework 01 — Server Port & Arcade Identity

**Phase:** 1 · **Blocks:** framework/02, framework/03, tui/01 ·
**Parallel-safe with:** gameplay/01

## Goal

Port v1's SSH/server layer and make it fleet-native: proxied identity from
the arcade router, per-player (not per-wire-key) session caps, and the
mouse program option the TUI retrofit needs. This is a **port task** — v1's
code is the starting point, not just a reference.

## Sources & references

| Source | Role |
| --- | --- |
| `../../ssh-idlefarmer/internal/server/*.go` | port as-is: middleware chain, `RequirePTY`, limits, shutdown hooks, **`teaprogram.go` Windows PTY fixes** |
| `../../ssh-idlefarmer/internal/identity/identity.go` | port + extend with the proxied branch |
| `../../ssh-idlefarmer/cmd/ssh-idlefarmer/main.go` | port wiring (rename `IDLEFARM_*` → `FARM_*`) |
| `../../ssh-arcadelobby/docs/02-bridge-and-identity-protocol.md` | **canonical** protocol v1 spec to implement |
| `../../ssh-moonminer/docs/framework/01-ssh-server-and-identity.md` | the same task already specified for moonminer — mirror its "Authentication & identity (two modes)" section, its caps/rate-limit notes, and its acceptance list |

## Deliverables

- `internal/server/`, `internal/identity/`, `internal/log/`,
  `cmd/ssh-farm/main.go` (serve path; `import-v1` lands in framework/02)
- `internal/config/` — `FARM_*` loader (same table as v1's README plus
  `FARM_PROXY_KEYS_PATH` and the leaderboard/moderation knobs other tasks
  register)

## Spec

Moonminer's framework/01 spec applies wholesale with `MOONMINER_` →
`FARM_`; the deltas that matter:

1. **Identity gains the proxied branch** exactly per protocol v1
   (`<hex64>.<slot>` username from a key in `FARM_PROXY_KEYS_PATH`,
   canonical fingerprint reconstruction, `ARCADE_PLAYER_KEY` env capture,
   malformed-username refusal, direct connections unchanged). v1 saves must
   be reachable from both modes — the fingerprint format is already
   identical, which is what makes the v1 import (framework/02) seamless.
2. **Session caps bucket by resolved fingerprint** (v1 buckets by wire
   key — that breaks behind the router where everyone shares the proxy
   key). Port `limits.go`, then move its keying after identity resolution.
3. **Mouse program option**: `newTeaProgram` appends mouse cell-motion
   (verify exact bubbletea v2 API name) after `bubbletea.MakeOptions(s)` +
   the Windows PTY options — the one server-side line tui/01 depends on.
4. **Session policy, idle-in-UI enforcement, graceful shutdown flush**:
   port unchanged; they are load-bearing (see v1 `CLAUDE.md` gotchas).
5. Player-facing strings keep v1's farm voice (🌾/🌧 messages, `\r\n`
   endings).

## Acceptance criteria

Moonminer framework/01's full list, plus:

- [ ] A v1 save file dropped into a v2 store (or imported) opens for the
  same key via **both** direct and proxied connections.
- [ ] `go test ./internal/identity ./internal/server` includes the proxied
  identity table tests (valid, malformed, forged-direct).
- [ ] Windows-host rendering verified (ported `teaprogram.go` intact).

## Out of scope / handoffs

- Store/leaderboard columns, Litestream, `import-v1` → framework/02.
- Game manager/actor port — v1's `internal/game` ports with the store task
  (framework/02) since its only changes are store-facing.
- The TUI itself → tui/01 (it receives the same constructor signature v1
  uses, plus content/leaderboard handles registered later).
