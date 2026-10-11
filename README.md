# ssh-farm

**Idle Farmer v2** — the ssharcade-native rebuild of
[`ssh-idlefarmer`](../ssh-idlefarmer). Same beloved game (plant, sell, buy
land, rebirth), four big upgrades:

1. **Mouse everywhere.** Click plots, click market rows, click buttons —
   the keyboard still does everything, the mouse now does too.
2. **Leaderboards.** The richest farms in the world, live:
   `#13/261 · SUNNY HOLLOW ·k3v9Q · ◈ 84,210` — with real moderation so
   names stay decent.
3. **Durable data.** Every save is continuously replicated off the host to
   S3 (Litestream). A dead instance costs seconds of progress, never a
   farm.
4. **A farm that reads on any terminal.** A fully painted near-black
   canvas that drifts through a day/night cycle, a *Solid background*
   setting for players who want one fixed colour, and events that take
   over the frame with a draining countdown instead of one quiet line of
   text. It plays from 80×24 up; below the recommended 100×30 the Help tab
   turns into a yellow `⚠` and Help itself says what is cramped.
   See [docs/tui/03](docs/tui/03-theme-day-night-and-event-feedback.md).

Players reach it through the arcade:

```bash
ssh play.ssharcade.dev     # arcade menu → FARM
```

## Status

**Beta — 2.5.0.** The v2 port and its arcade integration, mouse controls,
leaderboard, durable storage, day/night clock, seasonal themes, and fixed
three-contract endgame campaign are implemented. See
[docs/gameplay/04-contracts.md](docs/gameplay/04-contracts.md).

[docs/README.md](docs/README.md) indexes the architecture, shipped feature
specifications, operational runbooks, and proposed work. The older phase
labels and unchecked acceptance lists are retained as design history, not as
the current project status.

## Stack

Go 1.26, wish v2, bubbletea v2 + lipgloss v2, modernc SQLite, and
**Litestream** wrapping the container for S3 replication.

## Commands

```bash
go build -o bin/ssh-farm ./cmd/ssh-farm   # build
go test ./...                              # test
go vet ./...                               # vet
```

The TUI assumes a terminal with 24-bit color support. The server forces Bubble
Tea's True Color profile for every SSH session, so players do not need to
forward `COLORTERM` with `SendEnv`.

## Note

This repo is public. The moderation **algorithm** lives here and is meant to
be readable; the moderation **denylist** does not. That list is host state
loaded from `FARM_MODERATION_PATH` and exists in no repository — see
[docs/gameplay/03](docs/gameplay/03-farm-names-and-moderation.md).

A clone builds, tests, and runs with no extra setup: with `FARM_MODERATION_PATH`
unset the server loads the placeholder fixture at
`internal/moderation/testdata/denylist.fixture.toml`, logs that it is in
moderation dev mode, and refuses player-set farm names in favour of generated
ones — a stub list must never be mistaken for a working filter. Production sets
`FARM_REQUIRE_MODERATION=true`, which turns a missing or malformed list into a
refusal to boot.

For local seasonal-theme testing outside the festival dates, set
`FARM_DEV_SEASON` before starting the server:

```bash
FARM_DEV_SEASON=halloween go run ./cmd/ssh-farm
FARM_DEV_SEASON=christmas go run ./cmd/ssh-farm
```

It forces the corresponding skin and seasonal seeds for that process only.
With the variable unset (or set to any other value), the normal UTC calendar
windows apply.


## Licence

MIT — see [`LICENSE`](LICENSE).

[`NOTICE`](NOTICE) covers third-party software redistributed inside the
published container image (currently Litestream, Apache-2.0). Update it if the
image ever gains another bundled binary — Apache-2.0 requires that attribution
to travel with the artifact, not merely with the source.

## Guest visits through the arcade

`FARM_ALLOW_GUESTS=true` enables **trusted-router guests only** (default `false`).
Direct SSH still requires a public key. A trusted proxy can send the version 2
username `guest.<32 lowercase hexadecimal characters>` without `ARCADE_PLAYER_KEY`.
The token identifies one disposable visit; it is never an account or save slot.

Guests see farm title `ssh-farm`, player `GUEST`, and `NOT SAVED`. Gameplay,
rebirth, contracts and comfort settings work in memory. Farm naming and
leaderboard identity require a key; guests may read the board but are never ranked.
Leaving, disconnecting or maintenance discards the farm without touching account,
save or rename-budget tables. Intentional exits ask for confirmation. Generate a
key **on your own computer** with `ssh-keygen -t ed25519`, then reconnect to save
future progress. Existing guest progress cannot be converted into a save.

Deploy this backend before enabling guest admission in the lobby. The fleet's
[guest architecture plan](../ssh-arcadelobby/docs/08-guest-access-plan.md) defines
the trust boundary and rollout order. No database migration is needed.
