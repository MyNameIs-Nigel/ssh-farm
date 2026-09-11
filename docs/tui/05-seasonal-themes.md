# Farm TUI 05 — Seasonal Themes (Halloween & Christmas)

**Phase:** 5 (post-2.0) · **Depends on:** tui/03 (theme package, `configRows`,
golden harness), tui/04 (in-game clock) · **Blocks:** nothing

## Goal

Make the farm feel alive at the edges of the year. For the whole of October
the game wears a Halloween skin, and from November 25th through December 25th
it wears a Christmas skin: recoloured canvas and accents, a seasonal sky row,
reskinned text (title glyph, headlines, critter names), and limited-time
seasonal seeds that pay above the normal profit curve. A single
"Seasonal themes" toggle on the Settings overlay opts out of the *look*;
the seeds stay buyable in-season either way.

This task deliberately does **not** touch the random-event system, the
day/night phase formula, or the v1 parity goldens.

## References

| Source | Role |
| --- | --- |
| `tui/03-theme-day-night-and-event-feedback.md` | the theme package shape, the `Paint` nesting constraint, the `configRows` single-source-of-truth rule, the "no `StateVersion` bump" precedent |
| `tui/04-time-clock-header.md` | the accelerated in-game clock (`PhaseAt`, `formatClock`) the sky row reads for day/night |
| `internal/tui/theme/theme.go` | palette, `New`, `Paint`, per-style background rule |
| `internal/tui/config_rows.go` | where the new toggle row is appended (last, so existing toggle indices do not move) |
| `internal/sim/derive.go` (`Unlocked`, `VisibleCrops`) | progression gates the seasonal gate is added alongside, never inside |
| `internal/sim/actions.go` (`Plant`, `ReplantAll`, `SetAutoSowCrop`) | planting entry points that must refuse out-of-season seeds |
| `internal/sim/advance.go` (auto-sow tick) | replant path that must stop replanting seasonal crops once the window closes |

## Definitions

Seasons are **real calendar dates in UTC**, derived from the model's tick
`now` (which is wall-clock unix — the same counter `PhaseAt` already reads),
*not* from the accelerated in-game clock:

| Season | Window (UTC) | Special day |
| --- | --- | --- |
| Halloween | Oct 1 – Oct 31 | Oct 31 (big moon, day and night) |
| Christmas | Nov 25 – Dec 25 | Dec 25 (big star, day and night) |

Each lasts about one month. Outside both windows the season is None and every
render path behaves exactly as today.

## Deliverables

- `internal/season/` — `season.go` (pure calendar logic on `time.Time` plus
  an `AtUnix` helper), `season_test.go`
- `data/crops.toml` — four seasonal crops (two per festival), `unlock =
  { kind = "season", season = "halloween"|"christmas" }`
- `internal/content/content.go` — `Season` field on `Unlock`, validation for
  the `season` kind
- `internal/sim/` — `SeasonalOff` on `State` (`omitempty`, see below),
  `SeasonalEnabled()`, `SetSeasonal`, `Session.SetSeasonal`, seasonal gating
  in `Plant` / `ReplantAll` / auto-sow tick / picker visibility; persistence
  across rebirth
- `internal/tui/theme/` — `NewWithSeason` constructor, seasonal palettes and
  `Sky`/`SkyBright` styles (built in `New` too, so the all-styles background
  test keeps holding)
- `internal/tui/` — `season.go` (`g.season()`, `viewSky`, headline pool,
  critter display names, title glyph), sky row wired through `viewHeader`,
  new Settings row, Help + tutorial copy, golden cases
- `docs/tui/05-seasonal-themes.md` (this file) + index entry in `docs/README.md`

## Spec

### The season package (new, leaf)

`internal/season` depends only on the standard library so `sim`, `content`,
`theme` and `tui` can all import it:

```go
type Season int // SeasonNone, SeasonHalloween, SeasonChristmas
func At(t time.Time) Season
func AtUnix(now int64) Season      // time.Unix(now, 0).UTC()
func (s Season) Name() string      // "", "Halloween", "Christmas"
func (s Season) Glyph() string     // "", "🎃", "🎄"
func SpecialDay(t time.Time) bool  // Oct 31 or Dec 25
func SpecialDayUnix(now int64) bool
```

`At` uses UTC month/day only. No year logic, no timers, no state — every date
is reachable in a test by constructing it with `time.Date`.

### The `SeasonalOff` setting (no `StateVersion` bump)

Following the `ThemeSolid` precedent (tui/03: the v1 parity goldens in
`internal/sim/testdata/v1/` can never be regenerated, so new fields must be
`omitempty` with meaningful zero values):

- `State.SeasonalOff bool json:"seasonal_off,omitempty"` — zero value
  `false` means themes are **on**, so fresh saves, migrated saves and v1
  goldens all round-trip byte-identically.
- `func (s *State) SeasonalEnabled() bool { return !s.SeasonalOff }`.
- `sim.SetSeasonal(s, enabled)`, `Session.SetSeasonal(now, enabled)`,
  mirroring the `SetThemeSolid` chain exactly.
- New `configRows()` entry **appended last**: "Seasonal themes", hint
  "halloween & christmas looks (seasonal seeds stay)". Single toggle for
  both festivals ( Noël and spooky share one switch).
- Persists across rebirth like the other player settings; never cleared.
- What the toggle gates: palette, sky row, title glyph, seasonal headlines,
  critter display names. What it never gates: seasonal seed availability —
  seeds show and plant in-season even with themes off.

### Seasonal crops (content + sim)

```toml
[[crop]] # Halloween
id = "candycorn"
name = "Candycorn"
archetype = "fast"
seed_cost = 60
grow_seconds = 300
sell_value = 150
unlock = { kind = "season", season = "halloween" }

[[crop]] # Halloween
id = "witchhazel"
name = "Witchhazel"
archetype = "slow"
seed_cost = 400
grow_seconds = 7200
sell_value = 2600
unlock = { kind = "season", season = "halloween" }

[[crop]] # Christmas
id = "peppermint"
name = "Peppermint"
archetype = "fast"
seed_cost = 90
grow_seconds = 480
sell_value = 240
unlock = { kind = "season", season = "christmas" }

[[crop]] # Christmas
id = "tinseltree"
name = "Tinseltree"
archetype = "slow"
seed_cost = 800
grow_seconds = 14400
sell_value = 5200
unlock = { kind = "season", season = "christmas" }
```

Economy: each pays ≈0.30 coins/sec of profit, above the best permanent crop
(voidlotus ≈0.28/s) — the event premium. Time-limited availability is the
balancing lever, so no other progression gate is stacked on top.

`content.Unlock` gains `Season string`. `validateUnlock` case `"season"`:
`Season` must be `halloween` or `christmas`, `Value` must be 0, `Zone` empty.
`Unlocked()` is untouched (season is wall-clock, not save state — mixing it
into the pure progression predicate would poison every existing caller).

Sim gating (all take the `now` they already have — sim purity holds because
the timestamp is a parameter, never read from a clock):

- `func SeasonalPlantable(u content.Unlock, now int64) bool` — true unless
  `u.Kind == "season"` and `season.AtUnix(now)` does not match `u.Season`.
- `Plant`: seasonal-but-inactive → `ErrLocked` (same error as every other
  gate, so the UI needs no new message path).
- `ReplantAll`: seasonal-but-inactive remembered crops count as `Locked`
  (existing bucket, existing notice copy).
- Auto-sow tick (`advance.go`): the replant condition gains the seasonal
  check — a plot that harvested its last in-season cycle comes up **empty**
  afterwards, exactly like a crop whose zone was sold. Queuing a seasonal
  crop via `SetAutoSowCrop` stays allowed (it is a preference, settled at
  sow time).
- `VisibleCropsAt(s, c, now)` filters seasonal-but-inactive crops out of the
  picker, the Land seed catalog and the rebirth preview; `VisibleCrops`
  keeps its signature as the prestige-only filter for existing callers that
  do not have a clock. The TUI switches to the `At` variant with `g.now`.
- Already-planted seasonal crops are unaffected by the window closing: they
  keep growing, mature, and harvest for full payout (plus strains, bonuses,
  mercy rules all keep working — the crop row is ordinary content).
- `CheapestUnlockedCrop`/`MercyPlantEligible` gain `At` variants on the same
  pattern so a free mercy seed can never be an unplantable seasonal crop.

### Theme changes

`theme.NewWithSeason(p, solid, eventID, sn season.Season)` — `New` keeps its
signature and delegates with `SeasonNone`, so every existing caller and test
compiles unchanged.

- Backgrounds shift per season (still ANSI-256, still near-black readable
  under fg `253`):
  - Halloween: dawn `52` (oxblood), day `94` (dark amber), dusk `53`
    (dark magenta), night `16` (pitch black).
  - Christmas: dawn `17` (midnight blue), day `22` (pine), dusk `23`
    (frost teal), night `16` (pitch black, so the stars pop).
- Accents (lose to a live random event's accent, which keeps its existing
  priority): Halloween frame `208` (pumpkin), title `208`, section `183`
  (ghost lilac); Christmas frame `120` (pine light), title `210` (berry),
  section `159` (ice).
- `Sky` / `SkyBright` styles are constructed in `New` (neutral defaults) and
  tinted in `NewWithSeason` — every style still carries a background in every
  constructor, which `TestEveryStyleCarriesABackground` enforces.
- `solid` pins the seasonal background exactly like it pins the day/night
  drift and the event lift; accents, sky, and text still apply (same rule as
  events: pinning is about the canvas, not the feedback).

`Game.theme()` becomes:

```go
sn := season.SeasonNone
if st != nil && st.SeasonalEnabled() {
    sn = season.AtUnix(g.now)
}
return theme.NewWithSeason(theme.PhaseAt(g.now), solid, eventID, sn)
```

### The sky row (`viewSky`, via `viewHeader`)

One row, emitted from `viewHeader()` beside the event bar and banner — the
tui/03 placement rule (anything routed through the header is mirrored into
`coords.go:computeLayout` for free; a row added in `composeCanvas` would
silently shift every hitbox). Empty string when season is None or themes are
off, so header height is unchanged eleven months of the year.

- Christmas night (in-game `PhaseNight`, not the 25th): a deterministic star
  field picked by day-of-year from three variants, e.g.
  `· ✦ ·   · ✧ ·   ✦ ·` in `Sky`. Stars never appear by day.
- Christmas Day (Dec 25, any phase): one large gold star —
  `★ ⋯ THE STARLIGHT ⋯ ★` in `SkyBright`, day and night.
- Halloween night: bats and moon, e.g. `🦇 · 🌙 · 🦇`, variants by date.
- Halloween day: pumpkins and leaves, e.g. `🎃 · 🍂 · 🎃`.
- Oct 31 (any phase): a big orange moon —
  `🌕 ⋯ HALLOWEEN NIGHT ⋯ 🌕` in `SkyBright`, day and night.

All patterns are pure functions of (month, day, phase) — no RNG, no save
state, golden-deterministic.

### Text changes

- Title glyph: `🌾` → season glyph (`🎃` / `🎄`) while a season is active.
- Headlines: seasonal pools rotate by day-of-year when active, e.g.
  Halloween `SPOOKY HARVEST: CANDYCORN PRICES SOAR`, Christmas
  `STARLIGHT FESTIVAL: TINSELTREES FETCH A FORTUNE`. Gift-pending keeps its
  existing priority; `NewsEnabled=false` still silences the ticker.
- Critter display names (render-only — saves keep `crow`/`rabbit`/`mole`):
  Halloween crow→bat, rabbit→ghost, mole→gremlin; Christmas crow→robin,
  rabbit→hare, mole→mouse. Applied in plot cards, compact rows, visit/shoo
  notices. Rewards and mechanics untouched.
- Picker + Land catalog rows for seasonal crops carry a `🎃`/`🎄` marker and
  a "gone Nov 1" / "gone Dec 26" suffix so the window is visible at the point
  of purchase.
- Help gains a "Seasonal festivals" section (windows, toggle location,
  seed premium + keep-after-season rule, sky notes); tutorial page 3 gains
  seasonal themes in its settings line. All copy flows through the existing
  wrap helpers.

## Acceptance criteria

- [ ] `season.At` covers Oct 1, Oct 31, Nov 1, Nov 24/25, Dec 25/26, Jan 1,
  plus a leap-day (Feb 29 → None).
- [ ] Seasonal goldens render styled at 100×38: Halloween day, Halloween
  night (bats+moon), Christmas night (stars), Christmas Day (big star, day
  phase); existing goldens byte-identical (their fixture date, Nov 14, is
  outside both windows).
- [ ] `Paint` width/text invariants hold for seasonal themes (existing test
  cases plus a seasonal theme instance).
- [ ] Solid mode pins the seasonal background but keeps seasonal accents.
- [ ] `Plant` out-of-season → `ErrLocked`; in-season plants; planted crops
  harvest full value after the window closes.
- [ ] `ReplantAll` counts out-of-season remembered crops as `Locked`;
  auto-sow tick leaves the plot empty after the last in-season harvest.
- [ ] Picker/catalog hide out-of-season seeds; show them with markers
  in-season even with themes toggled off.
- [ ] Sky row: absent off-season and when toggled off; stars only at
  Christmas night; big star all of Dec 25; big moon all of Oct 31.
- [ ] New save fields omitted at defaults (v1 parity goldens byte-identical,
  no `StateVersion` bump); setting persists across rebirth.
- [ ] Settings golden regen covers the 5th row; config click/wheel tests
  reach it; existing toggle indices (0–2) untouched.
- [ ] `go test ./...` green; `go vet ./...` clean.

## Out of scope

- Random-event interplay (seasonal events, boosted event rates) — the event
  system is untouched; its accent wins ties.
- Per-crop Halloween/Christmas sprites on plot cards beyond the catalog
  markers.
- Timezone selection — UTC month/day only; a player near the dateline may see
  a window edge up to a day early/late.
- Server-side (`FARM_*`) configuration for seasons — dates are fixed in code.
- Reflowing any screen for the extra header row — the sky row is one row and
  vanishes off-season; `assertFits` coverage at 80×24 applies as usual.
