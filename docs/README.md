# ssh-farm — Build Plan Index (Idle Farmer v2)

This folder specifies **ssh-farm**, the v2 rebuild of `../ssh-idlefarmer`
as a native citizen of the ssharcade fleet. Each doc under `framework/`,
`gameplay/`, `tui/`, and `tests/` is a self-contained task an agent can
pick up independently. Read this index first, then your task file, then the
referenced sibling material.

## What v2 is

The **same game** — plant crops, sell at market, buy land, rebirth for
stars — with three upgrades and zero gameplay regressions:

| Pillar | Summary | Specs |
| --- | --- | --- |
| **Arcade-native** | played via `play.ssharcade.dev`, proxied identity, private network | framework/01, framework/03 |
| **Mouse support** | every action clickable, wheel scrolling, same keyboard bindings as v1 | tui/01 |
| **Leaderboard** | richest farms, `#13/261`, farm name + key suffix, moderated names | gameplay/02, gameplay/03, tui/02 |
| **Durable data** | continuous S3 replication (Litestream) — fleet-wide pattern | framework/02 |

**The parity promise:** a v1 player whose save is imported into v2 sees the
same farm, the same numbers, and the same offline catch-up behavior. The
sim is ported, not redesigned (gameplay/01).

## Required sibling reading (the docs this plan builds on)

| Where | What it owns for us |
| --- | --- |
| `../ssh-idlefarmer/` | **the v1 source being ported** — `CLAUDE.md` is the architecture summary; `internal/sim`, `internal/tui`, `internal/game`, `internal/store`, `internal/server` are the porting sources; `data/*.toml` comes across unchanged |
| `../ssh-moonminer/docs/README.md` | task-doc conventions, locked stack, project-wide conventions (sim purity, `\r\n`, middleware order, Windows PTY fixes, TOML balance) — **all apply here** |
| `../ssh-moonminer/docs/tui/01-app-shell-input-and-mouse.md` | the fleet's keyboard+mouse input contract and hitbox-registry design that tui/01 retrofits onto v1's screens |
| `../ssh-arcadelobby/docs/02-bridge-and-identity-protocol.md` | **canonical** proxied-identity protocol (v1) this game implements |
| `../ssh-arcadelobby/docs/04-deployment-and-cicd.md` | fleet compose, CI/CD workflow template, secrets layout |
| `../ssh-arcadelobby/docs/06-fleet-data-durability.md` | **canonical** Litestream/S3 durability pattern framework/02 implements |

## Package layout (the contract)

Identical to v1/moonminer with two additions:

| Path | Purpose |
| --- | --- |
| `cmd/ssh-farm/` | entry point + `import-v1` subcommand (one-time v1 save migration) |
| `internal/config/` | `FARM_*` env vars |
| `internal/server/`, `internal/identity/`, `internal/game/`, `internal/sim/`, `internal/tui/`, `internal/store/`, `internal/content/`, `data/` | as in v1 (ported) — same responsibilities as the table in `../ssh-moonminer/docs/README.md` |
| `internal/leaderboard/` | **new** — rank engine, cached board snapshots (gameplay/02) |
| `internal/moderation/` | **new** — name validation/normalization + embedded denylist (gameplay/03) |
| `data/moderation/` | denylist + generated-name word lists (one reason this repo is **private**) |

## Build order

```
Phase 1 (parallel):  gameplay/01 (sim port)     framework/01 (server port + identity)
Phase 2 (parallel):  framework/02 (store + durability + v1 import)
                     tui/01 (mouse retrofit)     gameplay/03 (moderation)
Phase 3 (parallel):  gameplay/02 (leaderboard engine)   framework/03 (deploy + CI/CD)
Phase 4:             tui/02 (leaderboard screen)        tests/01, tests/02
```

## Document map

| Doc | Task |
| --- | --- |
| [01-v2-vision-and-scope.md](01-v2-vision-and-scope.md) | What changes, what must not, v1 cutover plan (context, not a task) |
| [framework/01-server-port-and-arcade-identity.md](framework/01-server-port-and-arcade-identity.md) | Port v1 server; add proxied identity, mouse option, fp-keyed caps |
| [framework/02-store-durability-and-v1-import.md](framework/02-store-durability-and-v1-import.md) | Store port + leaderboard columns, Litestream integration, `import-v1` |
| [framework/03-deployment-and-cicd.md](framework/03-deployment-and-cicd.md) | Private-repo CI/CD, fleet compose service, idlefarmer→farm cutover |
| [gameplay/01-sim-port-and-parity.md](gameplay/01-sim-port-and-parity.md) | Port `internal/sim` + content verbatim; parity goldens vs v1 |
| [gameplay/02-leaderboard-engine.md](gameplay/02-leaderboard-engine.md) | Coins ranking, schema, caching, activity window |
| [gameplay/03-farm-names-and-moderation.md](gameplay/03-farm-names-and-moderation.md) | Display names, normalization pipeline, denylist best practices |
| [tui/01-mouse-retrofit.md](tui/01-mouse-retrofit.md) | Hitbox registry + click/wheel semantics across every v1 screen |
| [tui/02-leaderboard-screen.md](tui/02-leaderboard-screen.md) | The board: rank header, top list, window around you |
| [tests/01-parity-and-import-tests.md](tests/01-parity-and-import-tests.md) | v1↔v2 sim parity, migration, store |
| [tests/02-leaderboard-moderation-and-durability-tests.md](tests/02-leaderboard-moderation-and-durability-tests.md) | Rank correctness, filter suite, restore drills |

## Conventions

Everything in `../ssh-moonminer/docs/README.md` § "Project-wide
conventions" applies verbatim (substituting `FARM_*`), plus:

1. **Parity beats improvement.** During the port, resist "while I'm here"
   gameplay changes — v1 players are watching their own farms cross over.
   File ideas as TODOs; ship parity first.
2. **The denylist is content, not code** — `data/moderation/*` — and this
   repo stays **private** partly so the filter isn't trivially studied.
   Never copy denylist contents into public repos, commit messages, or
   test names.
3. **Names are re-validated at render time**, not just at write time, so a
   denylist update retroactively masks old names (gameplay/03).
4. **Every new feature keeps the sim pure** — leaderboard reads are store
   queries, not sim state; moderation is a pure function over strings.
