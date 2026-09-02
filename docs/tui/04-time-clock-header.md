# Farm TUI 04 — In-Game Time Clock

**Phase:** 6 (post-2.3) · **Depends on:** tui/03 (accelerated day/night
cycle and header layout) · **Blocks:** nothing

## Goal

Replace the moon-phase display in the primary farm header with a deterministic
24-hour in-game clock. The clock sits immediately to the right of the player's
slot name and before the wallet on the same top row:

```text
🌾 FARM NAME  ·  slot  ·  06:00                         ⛀ 25 coins
```

This is a display-only change. Moon phases and the Full Moon Moonberry bonus
remain gameplay mechanics; only their permanent header display is removed.

## References

| Source | Role |
| --- | --- |
| `internal/tui/theme/theme.go` | Owns `PhaseSeconds` and `PhaseAt`; the clock must agree with this cycle |
| `internal/tui/game.go` | Owns `Game.now`, advanced by the existing one-second tick |
| `internal/tui/views.go` | Owns `viewHeader` and the gameplay help copy |
| `internal/tui/layout.go` | Defines the 80-column and design-width canvas constraints |
| `internal/tui/timeclock_test.go` | Executable behavior contract; intentionally red before implementation |

## Clock contract

The existing visual cycle has four six-real-minute phases, so one complete
in-game day lasts 24 real minutes. The clock maps each real second to one
in-game minute. It does not display the player's wall clock, use a timezone, or
read `State.UpdatedAt`.

| Cycle position | Theme phase | Display |
| ---: | --- | --- |
| `0` | Dawn | `06:00` |
| `359` | Dawn | `11:59` |
| `360` | Day | `12:00` |
| `720` | Dusk | `18:00` |
| `1080` | Night | `00:00` |
| `1439` | Night | `05:59` |
| `1440` | next Dawn | `06:00` |

Equivalently, normalize `now` into the existing
`4 * theme.PhaseSeconds` cycle, treat that value as minutes elapsed, and add a
six-hour offset before formatting it as zero-padded `HH:MM`. Reuse `Game.now`
so the clock, background phase, tests, and golden renders all share one time
source. Do not call `time.Now` while rendering.

The player-facing format is exactly five printable ASCII columns: `HH:MM`.
There is no clock emoji or phase label. The compact format preserves room for
long farm and slot names and avoids ambiguous emoji widths across terminals.

## Header and help requirements

- Keep the clock on the primary row emitted by `viewHeader`; do not add a row
  in `composeCanvas`.
- Render the order as farm title, slot name, clock, wallet, and optional
  Starseed balance. Separate slot and clock with `  ·  `.
- Remove `moonGlyph` and `MoonPhaseName` from the primary header. Delete
  `moonGlyph` if it becomes unused.
- Preserve the slot's existing sanitization and semantic theme styles.
- The primary line must remain exactly `contentWidth()` columns at both the
  80-column supported size and the 100-column design size. Long valid names
  must not make it overflow; truncate lower-priority title content if needed.
- Keep the size warning, event bar, and Daily Furrow rows in their existing
  order and location so mouse hitboxes remain aligned.
- Update Gameplay Help: explain that Full Moon still gives the Moonberry
  bonus, but do not claim that moon phase appears in the header.

## Existing-test impact

The current suite was green before the red tests in this task were added.
These existing checks remain valid and must not be weakened:

- `TestHeaderGapUsesCanvasWidth` and the rectangular-canvas tests continue to
  enforce header geometry.
- `TestGameShowsTitleAndSlot` and `TestHostileSlotNameIsEscapedInView`
  continue to protect identity display and terminal escaping.
- Theme phase tests remain the source of truth for Dawn, Day, Dusk, and Night.
- Moon simulation and Moonberry bonus behavior are not stale; removing the
  header display does not authorize simulation changes.

All files in `internal/tui/testdata/golden/` currently contain the moon-phase
header. They are intentionally not refreshed in the tests-first commit:
refreshing them against unchanged production code would preserve the old UI.
After implementation, review and regenerate them with:

```bash
go test ./internal/tui -run TestGolden -update
```

`TestGoldenRendersAreDeterministic` will catch an implementation that reads the
wall clock during rendering.

## Implementation backlog

Complete these tasks on `feature/time-clock` in order:

- [ ] Read `internal/tui/timeclock_test.go` and run its focused tests to see
  the intended failures.
- [ ] Add a pure formatter in `internal/tui` that implements the table above,
  including cycle wrap. Keep implementation details private to the package.
- [ ] Use the formatter from `viewHeader` with `g.now`; replace the moon chip
  and preserve `slot  ·  HH:MM` ordering.
- [ ] Make the primary header width-safe with a 20-character farm name and a
  32-character slot at 80 and 100 terminal columns.
- [ ] Remove the now-unused moon header helper/imports without changing moon
  simulation or balance data.
- [ ] Update the Gameplay Help moon paragraph while retaining the Full Moon
  Moonberry bonus explanation.
- [ ] Run `go test ./internal/tui -run 'TestHeader|TestTimeClock|TestGameplayHelp'`
  until the focused contract is green.
- [ ] Run the full TUI suite and inspect any failure before changing a test;
  layout, escaping, event-row, and hitbox assertions are still authoritative.
- [ ] Regenerate the seven TUI goldens, inspect every first-line change, and
  commit the snapshots only after the implementation is correct.
- [ ] Run `go build ./...`, `go vet ./...`, and `go test ./...`.
- [ ] Update this backlog's completed boxes and open a PR to `main`; never
  commit directly to `main`, because `main` deploys production.

## Acceptance criteria

- [ ] The fixed timestamp cases in the clock table render exactly as specified.
- [ ] The clock is immediately right of the sanitized slot and left of wallet.
- [ ] No moon phase name or glyph remains on the primary header row.
- [ ] Full Moon still affects Moonberry sales and remains explained in Help.
- [ ] The header does not overflow at supported widths and existing hitboxes
  do not move.
- [ ] Golden renders use the fixed fixture clock and are deterministic.
- [ ] Build, vet, and the complete test suite pass after implementation.

## Out of scope

- Changing phase duration, palette, simulation time, moon math, or balance.
- Persisting a clock value in save data.
- Displaying wall-clock time, timezone, seconds, dates, or a configurable
  12-hour format.
- Moving the moon phase to another permanent HUD location.
- Reworking the known 80-column navigation-label clipping from tui/03.
