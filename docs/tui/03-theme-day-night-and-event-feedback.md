# Farm TUI 03 — Theme, Day/Night Background & Event Feedback

**Phase:** 5 (post-2.0) · **Depends on:** tui/01 (hitbox registry, frame
layout), tui/02 (board styles) · **Blocks:** nothing

## Goal

Make the farm legible and alive on a *default* terminal. Today the game
paints no background at all, so on a stock light-profile 80×24 terminal it
renders pale text onto white and is effectively unplayable. This task adds a
real theme: a fully painted near-black canvas that drifts across a day/night
cycle, a player setting to pin it to one solid colour, a non-blocking warning
when the window is smaller than the layout wants, and an active event that
takes over the frame instead of whispering from one header line.

This supersedes tui/01's "Theme changes — v1's look ports as-is; restyling is
not v2.0."

## References

| Source | Role |
| --- | --- |
| `../../ssh-moonminer/internal/tui/theme/theme.go` | **the fleet's theme-package shape** — palette vars, semantic role constructors, `Glyph`. Copy the structure, not the space palette (fleet-size-2 rule: copy, don't share a module yet) |
| `../../ssh-moonminer/docs/tui/04-theme-components-and-tweaks.md` | the palette-table doc format and the "no screen hardcodes a hex value" rule |
| `charm.land/lipgloss/v2` `style.go` `Style.Render` | the nesting constraint below — parent backgrounds do not survive child spans |
| `charm.land/bubbletea/v2` `tea.go` `View.BackgroundColor` | OSC 11/10 terminal default colours |
| `internal/tui/format.go` | `progressBar`, `duration`, `truncate`, `alignSides` — the countdown bar is built from these, not from new code |

## Deliverables

- `internal/tui/theme/` — `theme.go` (palette, phases, styles, `Paint`),
  `theme_test.go`
- `internal/tui/` — theme threaded through `views.go`/`layout.go`,
  `viewSizeWarning`, the event bar, `configRows` as the settings
  single-source-of-truth
- `internal/tui/golden_test.go` + `internal/tui/testdata/golden/` — the
  golden-render harness tui/01 and tui/02 both called for and never got
- `internal/sim/` — `ThemeSolid` and `EventStartedAt` on `State`,
  `StateVersion` 4 → 5

## Spec

### The nesting constraint (why this is not a one-line change)

`lipgloss.Style.Render` wraps **each line** in the parent's SGR, but any inner
style closes with `ESC[0m`, which drops the background for the remainder of
that line. Setting `.Background()` on the frame alone yields stripes, not a
background. Three mechanisms cooperate, all inside `fullscreen()` — already
the single exit point every `View()` path goes through:

1. `View.BackgroundColor` / `View.ForegroundColor` set the terminal's
   *default* colours (OSC 11/10), so every reset falls back to dark rather
   than to the user's white.
2. `theme.Paint(s, fg, bg)` re-asserts the palette after every SGR reset in
   the composed string. **This is the load-bearing one** — it is
   terminal-independent and does not rely on OSC 11 support, which
   Terminal.app lacks. It only inserts SGR, so printable width is unchanged
   and every existing `lipgloss.Width`/`stripAnsi` assertion keeps holding.
3. `lipgloss.Place(..., WithWhitespaceStyle(bg))` paints the letterbox around
   the canvas. `Place` emits exactly `width × height` cells, so painting what
   we draw paints the whole terminal.

### Palette

Backgrounds are **ANSI-256 indices, not hex**. `wish/v2/bubbletea.MakeOptions`
forces a colour profile and Terminal.app reports `xterm-256color`, so
truecolour would be quantised and adjacent near-blacks could collapse onto the
same grey. Indices render identically everywhere.

| Role | Index | Use |
| --- | --- | --- |
| `bgNight` | `232` | night — almost pure black |
| `bgDusk` | `233` | dusk |
| `bgSolid` | `233` | the pinned colour when the cycle is off |
| `bgDawn` | `234` | dawn |
| `bgDay` | `235` | day — cool charcoal |
| event lift | `234`–`237` | two steps above the phase's own background, per phase |
| `fg` | `253` | default foreground, so resets land somewhere legible |

The four phase backgrounds are deliberately distinct indices: if two collapsed
onto the same grey the cycle would be invisible, which
`TestAutoModeGivesEveryPhaseItsOwnBackground` pins.

Foregrounds carry over from v1 unchanged except where they are too close to
near-black to read: `styleEmpty` and `styleLocked` (`240`/`241`) lift to
`244`/`243`, and the frame/rule lift for contrast.

### Day/night cycle

An **accelerated in-game cycle**, not the wall clock: SSH gives no reliable
player timezone, and a real-time cycle would never visibly change inside one
session. Four phases of 6 minutes, a 24-minute day, derived from the model's
`now` (already updated by the existing 1 Hz tick — no new timer):

```
phase = (now / 360) % 4      →  0 dawn · 1 day · 2 dusk · 3 night
```

`PhaseAt` is a pure function of a unix timestamp, so every phase is testable
by passing a constant. Negative timestamps clamp to dawn.

### Event feedback

While `State.EventActive(now)`:

- the frame border takes the event's accent — `market_day` gold,
  `bumper_demand` green, `warm_front` orange;
- the canvas background lifts to `bgEvent`;
- the header banner becomes a full-width bar, right-aligned countdown:

```
⚡ MARKET DAY · seeds -40%          ▰▰▰▰▰▰▱▱  1m 12s
```

The bar needs elapsed-vs-total, but only `EventEndsAt` is stored, so
`EventStartedAt` joins it on `State` (`omitempty`, set in `rollOnlineEvent`
alongside `EventEndsAt`; sim purity holds since `to` is a parameter). It is
cleared wherever the event is cleared — expiry and rebirth — so the bar never
draws against a span that no longer exists. A save written before the field
existed reports a full bar rather than dividing by a zero span.

`Events.EventEnded` is currently never surfaced anywhere; it becomes a notice.

### Size warning

`recommendedWidth`/`recommendedHeight` = the canvas maxima (100×38). Below
either, `viewSizeWarning()` renders a chip in the `Warn` style:

```
⚠ 80×24 · best at 100×38
```

It is **non-blocking** and separate from the existing hard guard at 36×10,
which keeps its current behaviour. A one-shot toast also fires when a
`WindowSizeMsg` crosses from adequate to undersized.

**Placement rule:** the warning and the event bar are emitted from
`viewHeader()`. `coords.go:computeLayout()` duplicates `composeCanvas()`'s
height arithmetic to keep mouse hitboxes aligned, and it already calls
`viewHeader()` — so anything routed through the header is mirrored for free.
Adding a row directly in `composeCanvas` would silently shift every hitbox.

### The `ThemeSolid` setting

A fourth Config toggle, following the `NewsEnabled` chain exactly:
`State.ThemeSolid bool`, default `false` (cycle on), `sim.SetThemeSolid`,
`Session.SetThemeSolid`.

**No `StateVersion` bump.** The plan originally called for 4 → 5 with a
migration, but `internal/sim/testdata/v1/` holds byte-identical goldens
produced by the retired `ssh-idlefarmer` module, which is not checked out and
cannot be re-run — so they can never be regenerated, and a version bump would
break `TestGoldensRoundTripByteIdentical` permanently. Both new fields
(`ThemeSolid`, `EventStartedAt`) have meaningful zero values, so they are
`omitempty` instead: old saves round-trip unchanged and no migration is
needed. `TestNewFieldsAreOmittedAtTheirDefaults` pins that property.

The settings count is currently hardcoded in four places (`const settings`,
the wheel clamp, the hitbox loop, the render slice). Rather than edit three of
four and ship the classic off-by-one, `configRows()` becomes the single source
of truth for render, hitboxes, clamp and toggle — the pattern `marketLines()`
already establishes.

## Acceptance criteria

- [ ] Every line of the composed view carries a background — the anti-striping
  assertion, at every screen and overlay.
- [ ] `Paint` leaves printable width and ANSI-stripped text byte-identical.
- [ ] `fullscreen` sets `BackgroundColor` on every `View()` path, including
  the tiny-terminal guard and `errScreen`.
- [ ] `PhaseAt` covers all four phase boundaries, cycle wrap, and `now <= 0`.
- [ ] Solid mode returns one background for every phase; auto mode returns
  distinct ones.
- [ ] Golden renders at 100×38 and 80×24, stored **styled** so the background
  ANSI is pinned, regenerated by `-update`, deterministic under a fixed clock.
- [ ] `assertFits` runs at 80×24 as well as 100×38 — the user's real terminal
  size previously had no overflow coverage at all.
- [ ] The event bar shows name, effect, a draining `progressBar` and a
  `duration`; the border accent reverts when the event ends; an "ended"
  notice fires.
- [ ] The size warning appears below the recommended size, is absent at or
  above it, and never appears on the blocking path.
- [ ] `configRows()` pinned as single source of truth by a test mirroring
  `TestMarketLinesMatchViewOutput`; clicking and wheeling reach row 4.
- [ ] New save fields are omitted at their defaults, so `parity_test.go` still
  passes byte-identically and no `StateVersion` bump is needed.
- [ ] Existing geometry guards unchanged and passing:
  `TestComposedCanvasIsRectangular`, `TestViewIsCenteredOnLargeTerminals`,
  `TestCanvasHeightStableAcrossScreensAndNotices`,
  `TestMarketRowHitboxesMatchRenderedText`.

## Out of scope

- **Reflowing screens for small terminals.** 80×24 gets a warning and test
  coverage, not a redesigned layout. If the cramped layout is still the
  complaint after this lands, that is its own task. In particular the **nav
  strip is still clipped at 80 columns** (`? Help` renders as `? H`): the
  labels would have to shorten, which changes the look at every size, so it is
  left for a follow-up.
- Per-crop or per-season palettes; the cycle is time-based only.
- Extracting a shared fleet theme module — fleet-size-2 rule still applies.
- The dead `farm:harvest-all` / `farm:replant` mouse dispatch
  (`mouse.go`, IDs never registered) — a real pre-existing bug, but unrelated.

## Pre-existing bugs this work uncovered and fixed

Adding 80×24 coverage (`assertFits` previously only ever ran at 100×38) turned
up four truncation bugs that predate this task:

- **Settings rows were clickable on the wrong line.** `registerConfigHits`
  used a fixed `bodyY+4+i*2`, but rows render one per line inside a box that
  is *centred* in the body region. Hitboxes are now measured from the rendered
  box, the same way the market rows were fixed in `c6a1445`.
- **Help and tutorial copy had hard line breaks sized for the design canvas**,
  and interpolated variable-length labels (`starseedLabel`). Both now reflow
  via `rewrap`/`helpBody`.
- **Market and land lock reasons ran past the frame** and were silently
  clipped; they are now cut with `fitWidth`, which measures display columns so
  a two-column 🔒 cannot slip past the limit.
- The replant-warning and kicked overlays had the same hard-break problem,
  masked until the tutorial was fixed.
