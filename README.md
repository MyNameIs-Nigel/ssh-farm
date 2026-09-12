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

**Planning.** `docs/` is the complete build plan:
[docs/README.md](docs/README.md) is the index. This plan leans on the
sibling repos hard — v1 (`../ssh-idlefarmer`) is the source being ported,
`../ssh-moonminer/docs/` defines the shared TUI/input contracts, and
`../ssh-arcadelobby/docs/` owns the fleet protocol, deployment, and the
data-durability pattern.

## Stack (planned)

Same as the fleet (see `../ssh-moonminer/docs/README.md`): Go 1.26+,
wish v2, bubbletea v2 + lipgloss v2, modernc sqlite — plus **Litestream**
wrapping the container for S3 replication.

## Commands

```bash
go build -o bin/ssh-farm ./cmd/ssh-farm   # build
go test ./...                              # test
go vet ./...                               # vet
```

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
