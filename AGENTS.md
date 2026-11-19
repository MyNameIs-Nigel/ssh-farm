# Working on ssh-farm

Idle Farmer is ssharcade's SSH farming game. Players plant and harvest crops, trade, expand land, and progress through rebirths and contracts. It supports importing v1 saves and advances farm state across offline periods.

## Where to work

| Path | Responsibility |
| --- | --- |
| `cmd/ssh-farm/`, `internal/app/`, `internal/config/` | Bootstrap, v1 import command, and `FARM_*` settings |
| `internal/sim/`, `internal/gameclock/`, `internal/season/` | Progression, offline catch-up, time, and seasons |
| `internal/game/`, `internal/store/` | Save sessions, actor lifecycle, and SQLite persistence |
| `internal/content/`, `data/` | Content loading and balance definitions |
| `internal/leaderboard/`, `internal/moderation/` | Rankings and farm-name moderation |
| `internal/server/`, `internal/identity/`, `internal/tui/` | SSH and arcade identity, terminal screens, controls, and Help |

Start with [the docs index](docs/README.md). Preserve imported save behavior and deterministic offline progression when touching the sim. Keep state changes in the simulation and session layers. Use small and typical terminal sizes to check navigation, overlays, and mouse hit targets.

## Tools and verification

- Run `go build ./...`, `go vet ./...`, and `go test ./...` with the Go version in `go.mod`. Focus on `./internal/sim` for progression and `./internal/game` and `./internal/store` for save lifecycle. Use `scripts/restore-drill/` for changes to persistence and backup recovery.
- Use `go run ./cmd/dev-farm` for interactive development. Follow the [terminal tools guide](scripts/tui-tools/README.md) for an isolated SSH listener, onboarding and Help captures, and simulated network delay.
- Run `go test ./internal/sim -run '^$' -bench BenchmarkFarmCatchUp -benchmem` to measure the same planted save after several offline intervals.
- Work on a branch and open a PR against `main`. Check the release workflow and the lobby deployment runbook before any production release work.
