# Farm Gameplay 06 — Fertilizer

**Status:** Implemented · **Touches:** `internal/sim`, `internal/content`,
`internal/game`, `internal/tui`, `data/balance.toml`

## Why

Late-game farms plant slow, valuable crops and then wait. Fertilizer is a rare,
permanent "skip the wait" item: one press ripens the whole farm. It is scarce on
purpose — one to start, a few more from Starseeds or a lucky parcel — so it
reads as a treat to spend at the right moment rather than a routine speed-up.

## What it does (as requested)

| Rule | Detail |
| --- | --- |
| Effect | **F** ripens every growing crop on the farm at once. One-time use. |
| Starting stock | Every farm begins with **1**, including existing saves. |
| Cap | **3**, plus **1 per completed contract** (4 after Bare Hands, 5 after Lean Season, 6 after Clockwork Denied). |
| Contract completion | Raises the cap by one **and fills the sack** to the new cap, whatever you held. |
| Buying | StarShop → *Supplies* → Fertilizer, **✦ 5** each, never past the cap. |
| Parcels | A **5%** chance a parcel holds fertilizer instead of coins or Starseeds — rarer than the 20% Starseed parcel. |
| Parcel at the cap | Used **on the spot**: the farm ripens and the count stays full. |
| Persistence | Kept through rebirths, starting a contract, and abandoning one. |
| Prompt | F asks *"Use fertilizer?"* with a **Don't ask again (d)** box. Only `y` confirms — not `enter`, the farm's harvest key. Until the box is ticked *and confirmed*, F always asks. Settings → **Fertilizer prompt** turns it back on. |
| Header | `ϟ2/3` sits right of the Starseeds count, always visible. |

## Numbers (`[fertilizer]` in `data/balance.toml`)

| Key | Value | Meaning |
| --- | --- | --- |
| `cost_starseeds` | 5 | StarShop price; `0` (or no table) means not for sale |
| `drop_chance_pct` | 5 | share of parcels that hold one; `0` (or no table) means never |

The starter count (1) and base cap (3) are constants in `internal/sim/fertilizer.go`
rather than balance keys, because they define what an untouched save *means* —
see "Save format" below.

## Renames

The Market already had a run-scoped growth multiplier called **Fertilizer**, and
the StarShop had a permanent growth upgrade called **Rich Soil**. To free the name:

| Where | id (unchanged) | Old name | New name |
| --- | --- | --- | --- |
| Market (coins, this run) | `fertilizer` | Fertilizer | **Rich Soil** |
| StarShop (Starseeds, lifetime) | `growth` | Rich Soil | **Deep Roots** |

Only display names changed. Saves key owned levels by id, so renaming an id
would have silently wiped every player's levels.

## Judgement calls

- **Fertilizer only touches crops that are still growing.** A ripe crop keeps its
  ripe time; an empty plot stays empty. An auto-harvest plot is gathered by the
  next tick exactly as if it had ripened on its own.
- **F refuses instead of wasting one.** With nothing growing (or none held) F
  shows a notice and opens no prompt; the sim refuses too (`ErrNothingGrowing`,
  `ErrNoFertilizer`), so no client can spend one on a bare farm.
- **An at-cap parcel on a bare farm is wasted.** The request was "it just gets
  used instantly", so it is; the notice says it soaked away.
- **A fertilizer parcel holds only fertilizer** — no coins or Starseeds on top.
- **Fertilizer is allowed in every contract.** None of the three restrict it.
  Clockwork Denied disables parcels, so there it can only be bought.
- **Existing contract completers start at 1, not full.** The cap is derived
  (3 + completed contracts) so they get the higher cap immediately, but the
  refill happens at the moment of completion, and theirs predates this feature.
- **The prompt is skipped only after a confirmed tick.** Ticking the box and then
  cancelling saves nothing — "don't ask again" applies to the use it was ticked on.
- **Glyph:** `⚡︎` (U+26A1 + VS15) measures two cells in lipgloss and terminals
  disagree on honouring VS15, which would misalign the header. `ϟ` (Greek koppa,
  the classic text lightning bolt) is one cell like `✦` and `⛀`, and `⚡` was
  already the random-event marker in notices.

## Save format

Three omitempty fields, no `StateVersion` bump:

- `fertilizer` / `fertilizer_tracked` — the count, read only through
  `State.Fertilizer()`. *Untracked means "still holds the starter"*, so a save
  that never touched fertilizer writes neither key. That keeps the v1 parity
  goldens byte-identical and gives every existing save its starter without a
  migration write.
- `fertilizer_no_confirm` — the "don't ask again" box.

The parcel's fertilizer roll is derived from the save's RNG **without advancing
it**. Drawing from the stream would shift every later roll, and
`TestScriptedSequenceMatchesV1Golden` replays a parcel against a byte-exact v1
golden (verified: drawing from the stream fails it).

## Header wallet

The wallet (coins, Starseeds, fertilizer) never used to shrink, so a long balance
pushed the farm name off the row. It now steps down until it leaves the name
room: full labels → bare numbers → abbreviated numbers (`9.1T`, truncated so it
never overstates). The fertilizer count is never dropped.

## Footer

The farm footer drops `enter plant/harvest` (the tutorial's second page teaches
it) and `g gift` (a banner announces every waiting parcel and names the key),
and gains `F fertilizer` just before `q leave`. When the hints still overflow, the
self-explanatory `←↑↓→` goes first, so a stock 80-column terminal shows the full
row ending in `q leave` — before this change it was cut at `u upgrades …`.

## Tutorial

`enter` left the footer, so the tutorial's *Tending the farm* page carries it
("Arrow keys move; enter on a plot plants or harvests") along with a new `F`
bullet. Rendering it turned up two bugs that predate this change, both fixed
here because this is now the page that teaches enter:

- `rewrap` joined every single newline, so **every tutorial bullet list rendered
  as one run-on paragraph**. Bullets (`• `) now keep their own lines with a
  hanging indent.
- The Skip / Continue click targets used a fixed `cy+6` row and landed on the
  box's **bottom border** on every page at every size. They are now measured
  from the rendered box, like the confirm modals.

## Pre-existing issues noticed, not changed

- At the 36-column minimum the header overflows on `main` too (the slot and clock
  run is fixed priority) and the frame clips the wallet.
- At 80 columns, an open overlay pushes the nav row's `[x] Close` off screen.
- The StarShop's Inheritance description truncates at 80 columns.
- The Contracts screen's *Reward* lines still name only the cosmetic; the
  fertilizer refill is announced in the completion modal instead, to keep the
  accept modal inside its 80×24 budget.

## Acceptance

- `go test ./...` — sim rules (`internal/sim/fertilizer_test.go`), content
  validation, and TUI flows by key and mouse (`internal/tui/fertilizer_test.go`).
- Goldens: `fertilizer-confirm{,-80x24}`, `starshop-fertilizer{,-80x24}`,
  `header-wallet-compact-80x24`, `contract-reward`; all existing goldens
  regenerated for the header and footer.
