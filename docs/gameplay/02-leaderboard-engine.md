# Farm Gameplay 02 — Leaderboard Engine

**Phase:** 3 · **Depends on:** framework/02 (columns + queries),
gameplay/01 (`Coins()`), gameplay/03 (display filtering) · **Blocks:**
tui/02

## Goal

Rank every farm in the world by lifetime coin earnings and answer three
questions fast and consistently: *where am I* (`#13/261`), *who's on top*
(the list), and *who's near me* (the window). All reads come from the
denormalized store columns — never from decoding blobs, never from sim
state.

## References

| Source | Role |
| --- | --- |
| framework/02 migration 002 | `coins`, `farm_name`, `name_locked` columns + `idx_saves_coins` |
| migration 004 | `lifetime_earnings`, `rebirths` columns + `idx_saves_lifetime_earnings` — the board's ranked metric moved here from `coins` (current balance) |
| gameplay/03 | render-time name filtering + suffix format |
| tui/02 | the consumer |

## Deliverables

- `internal/leaderboard/` — `engine.go` (queries + cache), `types.go`,
  tests

## Spec

### Data flow

1. Actor autosave/disconnect flush (framework/02) writes
   `coins = state.Coins()`, `farm_name = state.FarmName()` in the same
   transaction as the blob. The board is therefore at most one autosave
   interval (30 s) behind live play — an acceptable, explainable lag
   ("updated ~30s" hint in the UI).
2. The engine reads only the columns.

### Ranking rules

- **Metric**: lifetime coin earnings (`lifetime_earnings`) — monotonically
  increasing, unlike the current spendable balance, which rebirth resets
  (see gameplay/01's note). This was originally scoped as a "cross-game or
  lifetime-earnings boards" TODO (below); it shipped as the board's primary
  metric once rebirth made "current balance" a poor proxy for a farm's
  overall progress — a farm mid-rebirth-reset would otherwise vanish from
  the board despite having earned more than anyone.
- **Rank** = `1 + COUNT(saves WHERE lifetime_earnings > mine)` — standard
  competition ranking ("1224"): ties share a rank, next rank skips.
  Deterministic display order for ties: `lifetime_earnings DESC,
  updated_at ASC, fingerprint ASC` (first to the money shows first).
- **Rebirths**: each row also carries the save's permanent rebirth count
  (`rebirths`), shown alongside the coin total — informational, not part
  of the sort key.
- **Population**: every save counts, **per save** (a player with two slots
  legitimately has two farms on the board — each shows its own name +
  suffix). Two filters, both from `balance.toml`-style config
  (`FARM_LEADERBOARD_*` env or content file — pick one, document):
  - activity window: `updated_at > now − 90d` (dormant farms age off the
    board; keeps `#/total` meaningful as years pass);
  - floor: `lifetime_earnings ≥ 1` (a brand-new farm isn't "ranked last",
    it's unranked — the UI shows `UNRANKED — earn your first coin` — even
    though it may already hold a nonzero starting balance, since that
    balance was granted, not earned).

### Engine API (consumed by tui/02)

```go
type Row struct {
    Rank        int
    DisplayName string // moderated name; empty → "FARM ·suffix" fallback (gameplay/03)
    Suffix      string // 5-char fingerprint suffix, always shown
    Coins       int64  // lifetime coin earnings, not current balance
    Rebirths    int64
    IsYou       bool
}
type Board struct {
    You        *Row  // nil when unranked
    Total      int
    Top        []Row // top 10
    Window     []Row // ±3 around You when You.Rank > 10 (dedup'd vs Top)
    AsOf       int64 // snapshot time for the staleness hint
}
Get(ctx, you SaveRef) (Board, error)
```

### Caching (do not hammer the DB from renders)

- The engine holds one in-process snapshot of the ordered board (id,
  name, coins), rebuilt **at most** every `FARM_LEADERBOARD_TTL`
  (default 15 s) and only on demand. All `Get` calls between rebuilds are
  pure in-memory slices + one rank lookup. Single process (one actor per
  save, one server) makes this trivially correct.
- At thousands of farms the full snapshot is a few hundred KB — fine.
  Note the scaling cliff honestly in code comments: past ~100k saves,
  switch to query-per-Get with the index (the API doesn't change).
- Name filtering + suffix derivation happen **at snapshot build**
  (render-time re-validation per gameplay/03 — a denylist update masks
  old names on the next rebuild).

### Anti-gaming notes (document in code, keep simple)

- Coins are server-authoritative (sim owns them); there is nothing to
  spoof from a client.
- The only manipulation surface is *names* (gameplay/03's problem) and
  *slot multiplication* (accepted: more farms = more work, no advantage
  per farm).

### Planned contracts extension

`gameplay/04-contracts.md` owns the proposed leaderboard additions: contract
completion seals, earned name styles, and showing the fingerprint suffix only
when a rendered display name is duplicated in the ranked population. Until
that work lands, the current always-visible suffix remains the implemented
behavior.

## Acceptance criteria

- [ ] Rank math table tests: unique values, ties (competition ranking),
  you-at-top, you-unranked (0 coins), you-just-below-window-of-top,
  empty board, exactly-10 farms.
- [ ] Window never duplicates Top rows; adjacency correct at boundaries
  (#11, #12).
- [ ] Activity window excludes stale farms from both list and `Total`.
- [ ] Cache: N concurrent `Get`s during a rebuild → one DB pass
  (`-race` clean); TTL honored with injected clock.
- [ ] A save flush visibly moves the board after the next rebuild
  (integration test through actor → store → engine).

## Out of scope

- Screen rendering → tui/02. Name rules/filter → gameplay/03.
- Cross-game boards → TODO (lifetime-earnings-per-save is now in scope, see
  "Ranking rules" above).
