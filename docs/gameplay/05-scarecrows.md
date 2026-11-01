# Farm Gameplay 05 — Scarecrows

**Status:** Implemented · **Replaces:** the single auto-shooing Scarecrow ·
**Touches:** `internal/sim`, `internal/content`, `internal/game`, `internal/tui`,
`data/balance.toml`

## Why

The old Scarecrow only earned when a critter wandered onto an **empty** plot.
Once a farm is fully planted — the state every mid- and late-game player is in —
critters never appear and the Scarecrow earns nothing online. Its only income
was a 60 coins/hour offline trickle. The upgrade went dead exactly when players
could afford it.

## What changes (as requested)

| Before | After |
| --- | --- |
| One Scarecrow, 1,200c | **Up to 4 Scarecrows**, each dearer than the last |
| Auto-shoos critters on empty plots for their bounty | **No auto-shoo.** Critters are always shooed by hand (`x`) |
| Income only when a critter shows up | Each Scarecrow **drops a random coin amount every 15 s while you are online** |
| Coins credited instantly | Coins pile up in the Scarecrow's **pouch** until you collect them |
| Offline: flat 60c/h | Offline: same random drops, **20× slower** (one every 5 min), still into the pouch |
| Shoo reward flat 3–12c | Shoo reward grows **+50% per rebirth** |
| Not shown on the farm | Scarecrow cards fill the empty column right of the plot grid |

Collecting works like a plot: move onto a Scarecrow card and press `enter` (or
click it). `a` (harvest all) also empties every pouch.

## Numbers (all in `[scarecrow]` / `[critters]` of `data/balance.toml`)

| Key | Value | Meaning |
| --- | --- | --- |
| `max_count` | 4 | Scarecrows per run |
| `cost`, `cost_growth_pct` | 1,200, 300% | 1,200 → 3,600 → 10,800 → 32,400 |
| `drop_interval_sec` | 15 | online seconds between drops |
| `offline_slowdown` | 20 | offline drops take 20× longer |
| `coins_min`, `coins_max` | 2, 8 | base roll per Scarecrow per drop |
| `synergy_pct` | 25 | each Scarecrow beyond the first adds +25% to **every** Scarecrow's drop (4 owned = ×1.75 each, 7× one alone) |
| `rebirth_bonus_pct` | 10 | +10% to drops per rebirth |
| `unlock_rebirths` | [0, 0, 1, 2] | rebirths needed to **buy** the 1st, 2nd, 3rd and 4th (see [Rebirth gates](#rebirth-gates)) |
| `[critters] rebirth_bonus_pct` | 50 | +50% to shoo rewards per rebirth |

"Rebirths" means `ProgressionRebirths()` — contract runs count their own
rebirths from zero, as every other gameplay gate does.

## Judgement calls

1. **One shared 15 s clock, independent rolls.** All Scarecrows tick together
   (one progress bar to read), but each rolls its own amount, so the pouches
   drift apart and the column doesn't look copy-pasted.
2. **Offline = the same mechanic, slower.** Instead of a separate per-hour
   formula, the clock just advances at 1/20 speed offline. One code path, so
   offline can never accidentally out-earn online.
3. **"Online" uses the sim's existing definition** (an Advance of ≤ 2 s). A
   lagged 3 s tick runs at offline speed for those 3 s — at most a few coins lost.
4. **No pouch cap.** Not requested, and a cap would punish exactly the
   away-from-keyboard play the offline rate exists for.
5. **Rebirth banks the pouches first.** Uncollected Scarecrow coins count toward
   the run's earnings and the Starseed preview, so nothing is silently lost on
   rebirth. Contract start/abandon wipes them along with coins, as before.
6. **Scarecrows also scale with rebirths** (+10% each). Without it they are
   irrelevant by the time a player owns Voidlotus.
7. **A "for sale" card.** When you own fewer than 4 (and no contract forbids
   them) one dim card shows the next price; clicking it opens the Market. It
   only appears once the run has earned the first price or owns a scarecrow,
   so a brand-new farm looks exactly as it did before. It is not part of
   keyboard navigation.
8. **Navigation is row-major.** `→` off the end of a plot row lands on that
   row's Scarecrow, `→` again continues to the next plot row; `←` reverses it;
   `↑/↓` move within the Scarecrow column. Compact (narrow) mode lists
   Scarecrows after the plots.
9. **No per-drop notices.** A notice every 15 s would bury everything else. The
   card updates in place; the welcome-back screen reports offline gains.
10. **Old saves migrate in place.** A save with the old `scarecrow: true` gets
    one Scarecrow with an empty pouch. New fields are `omitempty`, so saves
    without Scarecrows round-trip byte-identically and no `StateVersion` bump
    is needed. Rolling the binary back drops the Scarecrows (run-scoped; nothing
    permanent is lost).
11. **Notices make room for the grid, not the other way round.** On the farm's
    card grid, notices show only the newest lines that fit below the cards
    (at least one). A full 16-plot farm with four Scarecrows therefore keeps
    its card grid at the recommended 100×30 instead of losing its bottom row.
12. **Contracts.** Clockwork Denied still locks Scarecrows. Bare Hands allows
    them, but a drop is no longer a credit, so a tick can no longer complete a
    contract by itself — completion now happens on the collect action. The
    `Events.ContractCompleted` plumbing stays as a guard for future passive
    income.

## Rebirth gates

Requested after launch: the first two Scarecrows are open to every run, the
3rd needs **1 rebirth** and the 4th **2**, so a full column of four is
something rebirths earn.

13. **One list, one entry per Scarecrow.** `unlock_rebirths` must be empty (no
    gates) or have exactly `max_count` entries, each ≥ 0 and never decreasing,
    because Scarecrows are bought in order. A bad list fails the server at load
    like every other content error.
14. **Progression rebirths, as every gate counts them.** Inside a contract the
    contract's own rebirths count, so Bare Hands and Lean Season start back at
    two. Clockwork Denied still disables Scarecrows outright, and its lock
    takes precedence over the rebirth gate.
15. **The gate only ever refuses a purchase.** Scarecrows a save already owns
    (bought before the gates shipped) are kept and keep dropping; no save is
    touched or migrated. Scarecrows reset on rebirth, so such a column is back
    under the rule after the player's next rebirth anyway.
16. **The lock outranks the price.** A gated buy is refused as `ErrLocked` even
    when the player could not afford it. The Market shows `🔒 rebirth 1×`
    (the wording crop and zone gates use) instead of a price, and refusing it
    says `Scarecrow 3 needs 1 rebirth (you have 0).` (`this contract has 0`
    inside a contract, where "you have 0" would baffle a veteran).
17. **The for-sale card turns into a locked card** that counts the rebirths
    still needed (`1 more` / `rebirth`). The absolute "rebirth 2×" does not
    fit the card's 9 text cells, and the remaining count is what the player
    acts on, correct inside a contract too. It still opens the Market when
    clicked.
18. **The Rebirth screen's "Next rebirth unlocks" list names the next
    Scarecrow** (`⛀ Scarecrow 3 in the Market`) alongside the crop tiers, except
    under Clockwork Denied, and Help spells the gates out from the live config.

## Card layout

At the standard 100-column canvas the body is 94 columns: four 20-column plot
cards (80) + a 1-column gap + a **13-column** Scarecrow card = 94 exactly.

```
╭───────────╮
│ Scarecrow │   title
│ ⛀ 1,234   │   pouch (compact above 99,999: 1.2M)
│ ▰▰▰▱ 9s   │   time to next drop
╰───────────╯
```

While `unlock_rebirths` gates the next Scarecrow, the column's last card
counts the rebirths still needed instead of quoting a price:

```
╭───────────╮
│ Scarecrow │
│ 1 more    │
│ rebirth   │
╰───────────╯
```

On narrower terminals the plot grid drops a column before the Scarecrow column
is squeezed. The farm switches to the compact list when the taller of the two
columns would not fit the body height.

## Bugs found in existing code along the way

Each was reproduced with a failing test against the untouched code first.

1. **Grid plot hitboxes were the wrong size.** They assumed 22-wide cards with a
   blank row between card rows; cards are 20×5 with no gap. Row 3 of a full
   farm was unclickable and row 4 clicked row 3's plots.
2. **The grid clipped its bottom row.** `compactFarm` budgeted 3 lines per
   5-line card row, so at 100×30 a 16-plot farm with notices showing drew only
   12 of 16 cards whole.
3. **Compact-list hitboxes disagreed with the renderer** about how many rows
   fit, so once the list scrolled every click landed 3 plots off.

## Docs and help updated

- Help → Keys (`enter`, `a`, `x`), Help → "Critters & Scarecrows", rebirth
  "Lost:" list, tutorial pages 2–3, Market row copy, farm footer hint.
- `gameplay/04-contracts.md` lines that described the Scarecrow bounty.

## Acceptance

- [x] Up to 4 Scarecrows, escalating cost, Market shows `n/4` and next price.
- [x] Online drops every 15 s, offline 20× slower, random, into the pouch only.
- [x] More Scarecrows → higher per-Scarecrow drop.
- [x] Enter/click collects one; `a` collects all.
- [x] No auto-shoo; shoo reward rises per rebirth.
- [x] Cards fit the 94-column body with no clipping; goldens committed.
- [x] The 3rd Scarecrow needs 1 progression rebirth and the 4th 2, refused
      in the sim and explained in the Market, on the farm, in Help and on the
      Rebirth screen; owned Scarecrows are never taken away.
