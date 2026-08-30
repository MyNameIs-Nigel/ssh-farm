# ssh-farm (private)

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

This repo is **private** (game balance + moderation denylist stay out of
public view). CI/CD notes in `docs/framework/03` cover the private-image
pull auth this requires on the host.
