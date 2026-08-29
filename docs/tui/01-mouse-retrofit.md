# Farm TUI 01 — Mouse Retrofit (+ Screen Port)

**Phase:** 2 · **Depends on:** gameplay/01 (state), framework/01 (mouse
program option) · **Blocks:** tui/02

## Goal

Port v1's TUI (Farm, Market, Land, Rebirth, Star Shop, Stats, Help, all
overlays) and make every interaction clickable. v1 is keyboard-only; v2
adopts the fleet input contract so the farm plays identically to moonminer
under the mouse: **click selects, click-again/double-click activates,
wheel scrolls, buttons click, and every existing key binding still works.**

## References

| Source | Role |
| --- | --- |
| `../../ssh-idlefarmer/internal/tui/*.go` | the screens/overlays being ported (model, views, layout, format) |
| `../../ssh-moonminer/docs/tui/01-app-shell-input-and-mouse.md` | **the fleet input contract**: mouse semantics, hitbox-registry design, min-size guard — implement its "Mouse contract" section as written |
| `../../ssh-moonminer/docs/tui/04-theme-components-and-tweaks.md` | the pattern of widgets registering their own hitboxes |

## Deliverables

- `internal/tui/` — ported v1 screens + `hitbox/` registry + mouse routing
  in the root model
- One new overlay entry: rename farm (v1's `ovName`) reachable by click

## Spec

### Port first

Screens/overlays port with v1's structure and key handling intact
(screen enum, overlay enum, tick/kick messages, idle enforcement, tutorial,
away-summary). Rename module paths; keep the farm voice.

### Hitbox registry

Implement the registry exactly per the fleet contract (rects registered
during `View()`, rebuilt every frame, topmost-wins `At(x,y)`, 400 ms
double-click). The moonminer doc is the spec; this repo gets its own
implementation (fleet-size-2 rule: copy, don't share a module yet — note
the extraction TODO once both exist).

### Per-screen click map (the retrofit work)

| Screen | Clickable things |
| --- | --- |
| **Farm** | each plot tile (click = select; click-selected/double = context action: harvest if ready, else open planting picker — mirror what Enter does in v1); nav tabs in the header; sow-all/harvest-all style buttons if v1 has them |
| **Planting picker (overlay)** | each crop row (click select, click-again confirm); wheel scrolls; cancel button |
| **Market** | each listing row; buy/sell quantity buttons (`-10 -1 +1 +10` style per v1's controls — every keyboard increment gets a clickable counterpart); confirm button |
| **Land** | each purchasable plot/expansion row + buy button |
| **Rebirth** | the confirm flow — **double-confirmation stays keyboard-equal**: click the confirm button twice with the same warning v1 shows; no accidental one-click rebirth |
| **Star Shop** | upgrade rows + buy buttons |
| **Stats / Help** | wheel + click-drag scrollbar optional (wheel is required, drag is stretch); help page tabs clickable |
| **All overlays** | dismiss buttons; overlays capture clicks (clicking the dimmed background = cancel, same as Esc) |
| **Header/nav** | screen tabs (FARM · MARKET · LAND · REBIRTH · SHOP · STATS · **BOARD** (tui/02) · HELP) clickable everywhere |

Rules that keep it consistent:

- Every click action has a key equivalent (accessibility + no-mouse
  terminals) — the retrofit adds input paths, never replaces them.
- Destructive/irreversible actions (rebirth) keep their confirmation
  regardless of input method.
- Wheel over any list moves selection/scroll; wheel elsewhere is ignored.
- Click during the min-size guard or a locked tutorial page is swallowed.

### Root-model routing

Mouse messages route through the same shell → overlay → screen precedence
as keys (an open overlay captures clicks). Update `lastInput` on mouse
activity too — mouse-only players must not idle out.

## Acceptance criteria

- [ ] Every v1 keyboard flow works unchanged (ported tests +
  `../../ssh-idlefarmer/internal/tui` test suite adapted).
- [ ] Every action in the click map reachable by mouse — checklist test
  file enumerating screen × action with a hitbox assertion for each
  (pattern: `../../ssh-moonminer/docs/tests/03-tui-rendering-and-input-tests.md`).
- [ ] Double-click timing, overlay capture, background-click-cancel,
  wheel-selection all unit-tested with synthetic messages.
- [ ] Mouse activity resets the idle timer.
- [ ] Golden renders unchanged vs the ported keyboard-only screens
  (hitboxes are invisible — the retrofit must not shift layout).

## Out of scope

- Leaderboard screen → tui/02 (it lands as one more nav tab here).
- Theme changes — v1's look ports as-is; restyling is not v2.0.
  (Superseded post-2.0 by tui/03, which adds the day/night background,
  the solid-colour setting, and the event bar.)
