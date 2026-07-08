# Farm TUI 02 — Leaderboard Screen

**Phase:** 4 · **Depends on:** tui/01 (shell + hitboxes), gameplay/02
(Board API), gameplay/03 (display rules)

## Goal

The screen that makes coins mean something: your rank in the world, the
top farms, and the farms just above and below you — the ones you can
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
│   #1  GOLDEN MEADOWS ·x9J2k ↻ 12         ◈ 1,204,551 │
│   #2  POTATO EMPIRE ·mQ04z ↻ 9             ◈ 981,300 │
│   #3  CLOVER & SONS ·77aBc ↻ 9             ◈ 954,110 │
│   …top 10…                                           │
│  ────────────────────────────────────────────────    │
│  #11  WINDMILL ACRES ·ff21x ↻ 2            ◈ 84,900  │
│  #12  MAPLE HOLLOW ·b8k2N ↻ 2              ◈ 84,551  │
│ ▸#13  SUNNY HOLLOW ·k3v9Q ↻ 1  ← YOU       ◈ 84,210  │
│  #14  BARLEYCORN ·z0q4T ↻ 1                ◈ 83,995  │
│  …                                                   │
│                                                      │
│  updated 12s ago                                     │
└ ↑↓/WHEEL SCROLL · N RENAME FARM · ESC BACK ────── █ ┘
```

- **Header rank** `YOU: #13/261` always visible — it's the number the
  whole feature exists for. Unranked: `YOU: UNRANKED — EARN YOUR FIRST
  COIN`.
- **Top 10**, then a divider, then the **±3 window** around you (only when
  you're outside the top 10; gameplay/02 already dedupes). Your row:
  `▸` + highlight + `← YOU`.
- Rows: rank (right-aligned, ties share numbers per gameplay/02), name in
  primary text, suffix in dim (`·k3v9Q` — always shown, it's the
  anti-impersonation signal), rebirth count (`↻ 12`), coins right-aligned
  with the ◈ glyph — **lifetime coin earnings** (gameplay/02), not the
  farm's current spendable balance. Top 3 ranks may take the
  gold/cyan/violet accent treatment — tasteful, not a rainbow.
- **Staleness hint** (`updated 12s ago`) from `Board.AsOf` — honest about
  the autosave + cache lag so "I just earned coins, why didn't I move"
  has a visible answer.
- If total rows exceed the panel, wheel/↑↓ scroll the combined list; the
  header rank never scrolls away.

### Behavior

- Entry: `BOARD` nav tab (click or key per tui/01's nav). On entry call
  `Get` (cache makes this cheap); refresh on a 15 s UI tick while the
  screen is open; `R` / click `updated…` forces a refresh (still
  TTL-bounded — no hammering).
- `N` (or clicking your own row) opens the rename overlay (tui/01 ports
  it) — the natural "I want a better name on the board" moment. Rejection
  shows gameplay/03's generic message.
- Board errors (store hiccup) render an in-panel `LEADERBOARD UNAVAILABLE
  — TRY AGAIN SOON` — never crash the session over a vanity feature.
- No pagination past the window in v2.0 (top + window is the product;
  full browsing is a TODO).

## Acceptance criteria

- [ ] Golden renders: ranked-in-top-10, ranked #13 (the sketch), unranked,
  tie-heavy board, error state — at 80×24 and 120×40.
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
