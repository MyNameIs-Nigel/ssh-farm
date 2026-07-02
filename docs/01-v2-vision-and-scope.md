# ssh-farm — V2 Vision & Scope

Shared context for every task; not itself a task. Read alongside
`../../ssh-idlefarmer/CLAUDE.md` (the v1 architecture summary).

## Why a v2

v1 (`ssh-idlefarmer`) works and has players, but it predates the fleet: it
was built to own port 22 on its own host, keyboard-only, with its SQLite
file living and dying with the instance. v2 makes the same game
arcade-native, clickable, competitive, and durable. It is a **port with
three additions**, not a rewrite.

## What stays exactly the same (the parity promise)

- **All gameplay**: crops, growth timing, market prices, land expansion,
  rebirth/stars, offline catch-up math, autosave cadence, session takeover
  policy, onboarding tutorial. `internal/sim` and `data/*.toml` port
  verbatim (gameplay/01) with golden tests proving v1 and v2 produce
  identical states from identical inputs.
- **Accounts**: same fingerprint model, same slot semantics. A v1 player's
  key opens their imported v2 farm.
- **The stack**: wish v2, bubbletea v2, lipgloss v2, modernc sqlite,
  TOML content — per the locked table in `../../ssh-moonminer/docs/README.md`.

## What changes

### 1. Arcade-native (framework/01, framework/03)

v2 lives behind `ssh-arcadelobby` on the private `ssharcade` network:
proxied identity per the canonical protocol
(`../../ssh-arcadelobby/docs/02-bridge-and-identity-protocol.md`), no
public ports in production, `FARM_*` env vars, per-repo CI/CD that
restarts only this service. Direct connections still work for dev.

### 2. Mouse support (tui/01)

v1 is keyboard-only. v2 adopts the fleet input contract
(`../../ssh-moonminer/docs/tui/01-app-shell-input-and-mouse.md`): click to
select, click-again/double-click to activate, wheel to scroll, buttons
clickable — with every v1 key binding intact.

### 3. Leaderboard (gameplay/02, gameplay/03, tui/02)

The richest farms, globally visible in-game:

```
◇ LEADERBOARD — RICHEST FARMS          YOU: #13/261
  #1   GOLDEN MEADOWS ·x9J2k        ◈ 1,204,551
  ...
```

Each row: rank, **farm name** (player-chosen, moderated) + a dim
**·5-char key-fingerprint suffix** that distinguishes same-named farms
without exposing anything sensitive. Name moderation follows the best
practices in gameplay/03 (normalization pipeline, tiered denylist,
render-time re-validation, no filter-probing feedback).

v1 already has farm naming (`ovName` overlay in
`../../ssh-idlefarmer/internal/tui/game.go`) — v2 keeps the UX and adds the
moderation gate.

### 4. Durable data (framework/02 — fleet-wide pattern)

Player data must survive the loss of the EC2 instance, its volumes, or the
whole AZ. The fleet pattern (canonical:
`../../ssh-arcadelobby/docs/06-fleet-data-durability.md`) is **Litestream**:
the game container runs under `litestream replicate -exec`, streaming every
SQLite write to a versioned S3 bucket within ~1s and auto-restoring on
boot when the local DB is missing. ssh-farm is the first implementation;
moonminer and the router adopt the same pattern.

## Cutover plan (v1 → v2, summarized; details in framework/03)

1. v2 ships to the fleet compose as service `farm` alongside `idlefarmer`
   (both visible in the arcade during the beta window, v2 labeled `FARM
   (V2 BETA)`).
2. One-time migration: `ssh-farm import-v1` copies v1's accounts + saves
   into v2's DB (framework/02). Run at cutover with v1 stopped; v1 stays
   deployed-but-hidden for rollback.
3. `games.toml` swap: `idlefarmer` entry removed, `farm` renamed plainly;
   v1 container retired after a safe soak (its DB archived to S3).

## Explicit non-goals for v2.0

- New crops/mechanics/balance changes (parity first — file TODOs).
- Cross-game currency or arcade-wide profiles.
- Web viewer for the leaderboard (fun later; the bucket + schema make it
  possible without touching the game).
- Multi-region/multi-host scaling.
