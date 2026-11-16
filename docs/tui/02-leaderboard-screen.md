# Farm TUI 02 — Leaderboard Screen

**Phase:** 4 · **Depends on:** tui/01 (shell + hitboxes), gameplay/02
(Board API), gameplay/03 (display rules)

## Goal

The screen that makes coins mean something: your rank in the world, the
whole ranked list to scroll through, and — when you are further down than
the screen reaches — the farms just above and below you, the ones you can
actually catch.

## References

| Source | Role |
| --- | --- |
| gameplay/02 `Board`/`Row` | the entire data source — this screen computes nothing |
| gameplay/03 | names arrive pre-moderated with suffixes; render as given |
| tui/01 | nav tab (`BOARD`), hitboxes, wheel |

## Spec

### Layout (80×24)

```
┌◇ LEADERBOARD — RICHEST FARMS ────────── YOU: #13/261 ┐
│                                                      │
│   #1  ◆◆ GOLDEN MEADOWS ↻ 12              ◈ 1,204,551 │
│   #2  POTATO EMPIRE ·mQ04z ↻ 9             ◈ 981,300 │
│   #3  CLOVER & SONS ·77aBc ↻ 9             ◈ 954,110 │
│   …the list, from wherever you've scrolled to…       │
│  ────────────────────────────────────────────────    │
│  #11  WINDMILL ACRES ·ff21x ↻ 2            ◈ 84,900  │
│  #12  MAPLE HOLLOW ·b8k2N ↻ 2              ◈ 84,551  │
│ ▸#13  SUNNY HOLLOW (k3v9Q) ↻ 1  ← YOU      ◈ 84,210  │
│  #14  BARLEYCORN ·z0q4T ↻ 1                ◈ 83,995  │
│  …                                                   │
│                                                      │
│  updated 12s ago                                     │
└ ↑↓/PGUP/PGDN/WHEEL SCROLL · N RENAME FARM · ESC BACK ┘
```

(The sketch is the pinned state below: the list, the divider, then you ±3.)

- **Header rank** `YOU: #13/261` always visible — it's the number the
  whole feature exists for. Unranked: `YOU: UNRANKED — EARN YOUR FIRST
  COIN`.
- **The whole ranked list** (`Board.Rows`), scrollable, starting at #1. Your
  row: `▸` + highlight + `← YOU`.
- **Your neighbourhood stays in view.** When you are ranked but below the
  visible rows, a divider and your **±3** neighbours are pinned under the
  list. The frame is the same height either way: the list gives up the
  rows the block takes, so nothing below it (scroll hint, `updated…`,
  hitboxes) moves. Scrolling down until you are inside the visible list
  drops the pin — the **divider disappears and the ranks run on
  unbroken**, so it is obvious where you are (rank last = the bottom row of
  the list). You are never pinned when you are already visible, unranked, a
  guest, or scrolled past (the list is then plain; the header still says
  where you are). On a short terminal the block shrinks (±2, ±1, just you)
  and then disappears before the list is squeezed below three rows. The
  layout is the pure function `planBoard` (`internal/tui/board_plan.go`).
- Rows: rank (right-aligned, ties share numbers per gameplay/02), up to three
  contract seals, name in its selected earned style, a parenthesized suffix
  only for duplicate names and generic fallbacks, rebirth count (`↻ 12`), coins right-aligned
  with the ◈ glyph — **lifetime coin earnings** (gameplay/02), not the
  farm's current spendable balance. Top 3 ranks may take the
  gold/cyan/violet accent treatment — tasteful, not a rainbow.
- **Staleness hint** (`updated 12s ago`) from `Board.AsOf` — honest about
  the autosave + cache lag so "I just earned coins, why didn't I move"
  has a visible answer.
- If the list exceeds the panel, wheel / ↑↓ scroll a row, PgUp/PgDn a page,
  Home/End jump to either end; the header rank never scrolls away. The
  `scroll` footer hint and `↑↓ scroll for more` line appear only when
  there is something to scroll. Only the visible rows are rendered, and
  only a visible wave-styled name runs the animation tick.

### Behavior

- Entry: `BOARD` nav tab (click or key per tui/01's nav). On entry call
  `Get` (cache makes this cheap); refresh on a 15 s UI tick while the
  screen is open; `R` / click `updated…` forces a refresh that **bypasses
  the TTL** (`Engine.Refresh`) — without that the key would re-read the
  cached snapshot and visibly do nothing — but only once the snapshot is at
  least `minRefreshInterval` (2 s) old, so a held key can't hammer the
  database.
- `N` (or clicking your own row) opens the rename overlay (tui/01 ports
  it) — the natural "I want a better name on the board" moment. Rejection
  shows gameplay/03's generic message.
- Board errors (store hiccup) render an in-panel `LEADERBOARD UNAVAILABLE
  — TRY AGAIN SOON` — never crash the session over a vanity feature.
- No jump-to-me key and no search yet; PgUp/PgDn/Home/End are the way
  around a long list.

## Acceptance criteria

- [ ] Golden renders: whole list fits (no divider), pinned-you, merged-you
  (scrolled into the list), last place, name waves, unranked, tie-heavy
  board, error state.
- [ ] `planBoard` invariants swept over a grid: constant frame height, no row
  drawn twice, a divider always hides ≥ 2 rows, never pinned when unranked or
  visible, every row reachable by scrolling.
- [ ] Update-loop tests: entry triggers one `Get`; tick refresh honors
  TTL; rename opens from key and click; wheel scrolls without losing the
  header.
- [ ] `IsYou` row renders highlighted exactly once even when ties place
  duplicates of your coin value adjacent.
- [ ] All hitboxes (tab, rename, refresh, your-row) asserted per the
  tui/01 checklist pattern.

## Out of scope

- Rank computation, windows, ties → gameplay/02.
- Name rules → gameplay/03. Rename overlay internals → tui/01.

## Contract cosmetics

The three-contract campaign in `../gameplay/04-contracts.md` is implemented:
rows carry completion seals and selectable name colors, each with an animated
wave once Contract 3 is complete. The animation has its own presentation-only
tick; rank, earnings, board refresh, and scrolling behavior remain unchanged.
