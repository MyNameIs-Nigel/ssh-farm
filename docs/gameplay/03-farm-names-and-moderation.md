# Farm Gameplay 03 — Farm Names & Moderation

**Phase:** 2 · **Depends on:** gameplay/01 (`SetFarmName` action point) ·
**Blocks:** gameplay/02 (display filtering), framework/02 (import filter)

## Goal

Farm names appear on a public leaderboard, which makes them user-generated
content read by strangers — including the strangers' kids. This task builds
the naming system and the moderation layer: permissive enough that
`SUNNY HOLLOW` and `XX_POTATO_LORD_XX` sail through, strict enough that
slurs and their leetspeak mutations don't, and designed so the filter can
be tightened later **and apply retroactively**.

There is no perfect word filter. The design goal is: nothing hateful
survives casual effort, determined trolls get bored (rate limits, no
feedback), and the operator can fix anything that slips through in one SQL
statement — with a lock so it stays fixed.

## References

| Source | Role |
| --- | --- |
| `../../ssh-idlefarmer/internal/tui/game.go` (`ovName`, `nameInput`) | v1's naming UX being kept |
| gameplay/02 | consumes `DisplayName`/suffix at snapshot build |
| framework/02 | `farm_name`, `name_locked` columns; import-time filtering |

## Deliverables

- `internal/moderation/` — `validate.go` (charset/length),
  `normalize.go` (the pipeline), `denylist.go` (tiered matching),
  `generate.go` (default names), exhaustive tests
- `data/moderation/namewords.toml` (embedded — wholesome by construction,
  and keeping it embedded is what lets a clone generate names out of the box)
- `internal/moderation/testdata/denylist.fixture.toml` — the placeholder
  fixture used by dev and the test suite. Invented tokens only, forever.
- The real denylist is **not a deliverable of this repo**. It is host state at
  `FARM_MODERATION_PATH`, loaded at boot and reloadable on SIGHUP — see
  "Where the denylist lives" below.
- The `RenameFarm` action wrapping `sim.SetFarmName`

## Spec

### Name rules (validation, before any filtering)

- Charset: `[A-Za-z0-9 _-]`, stored/displayed uppercase, 3–20 chars.
  **ASCII only** — rejecting all other Unicode isn't unfriendly, it's the
  single most effective anti-abuse measure (kills homoglyph evasion:
  Cyrillic lookalikes, zalgo, RTL tricks) and matches the terminal
  aesthetic anyway.
- Collapse runs of spaces; trim; no leading/trailing separators; not
  purely digits/separators.
- Uniqueness is **not** required — the always-visible fingerprint suffix
  disambiguates (below).

### The suffix (identity without exposure)

Every public display appends a dim suffix: the **last 5 characters of the
key's base64 fingerprint** — e.g. `SUNNY HOLLOW ·k3v9Q`. Properties worth
a code comment: stable for the life of the key, ~30 bits so collisions are
rare and harmless (it's a discriminator, not an identifier), derived from
the already-public-ish fingerprint so it leaks nothing, and it makes
impersonating a famous farm pointless — the suffix won't match.

### Default names (safe by construction)

New/unnamed farms get a generated name: `<adjective> <noun>` from curated
embedded lists (~60×60 wholesome farm words: `MAPLE`, `CLOVER`, `HOLLOW`,
`ORCHARD`…), seeded by fingerprint so it's stable. Naming is opt-in
polish, so the board is never blocked on moderation for players who never
touch the feature.

### The normalization pipeline (run before every denylist check)

Filters that check raw input are decorative. Normalize first:

1. lowercase;
2. leetspeak fold: `0→o 1→i 3→e 4→a 5→s 6→g 7→t 8→b @→a $→s !→i +→t`;
3. strip separators (spaces, `-`, `_`) → catches `s-l-u-r`;
4. produce **two** candidates: separator-stripped, and additionally
   repeat-collapsed (`niiice→nice`) — check both (collapsing can both
   create and destroy matches; checking both sides closes the gap).

### Where the denylist lives

The list is a file on the production host, mounted read-only into the
container. It is in no repository and never has been part of the public tree.

| Env var | Meaning |
| --- | --- |
| `FARM_MODERATION_PATH` | Path to the denylist TOML. Unset means the dev fixture. |
| `FARM_REQUIRE_MODERATION` | `true` in production: refuse to boot without a real list. |

Boot is fail-closed, and deliberately so in both directions:

- Path set but missing, unreadable, or malformed → **exit non-zero**, in dev as
  well as production. Falling back to the stub because the real list failed to
  parse is the silent downgrade this design exists to prevent.
- Path unset with `FARM_REQUIRE_MODERATION=true` → **exit non-zero**.
- Path unset otherwise → the fixture, logged loudly, with player-set names
  refused outright and generated names used instead.

`SIGHUP` re-reads the file in place, which is what makes the retroactive
tightening below a file edit plus a signal rather than a rebuild and a
redeploy. A reload that fails to parse leaves the previously loaded list in
force and logs the error: a typo in a live edit must never drop the filter.

### Tiered denylist (file format)

```toml
[[term]]
word = "..."          # normalized form
tier = "slur"         # slur | profanity
match = "substring"   # substring | word
```

- **`slur` tier → substring match** on the normalized candidates. Zero
  tolerance, embedded-anywhere counts. Seed from established open lists
  (LDNOOBW's multi-language lists are the usual base) then **hand-review**
  — machine-copied lists carry false positives you'll regret.
- **`profanity` tier → whole-word match** (on the separator-preserved
  normalized form). Mild words embedded in bigger words are fine —
  this is what prevents the Scunthorpe problem (`CLASS ACT FARM` contains
  "ass"; word-boundary matching doesn't care).
- Explicit `allow = [...]` exception list for the residual false positives
  the tests discover.
- Keep the list in TOML (content, not code) so tightening it is a data PR;
  the render-time re-check (below) makes it retroactive.

### Enforcement points (three, all through one function)

`moderation.Check(name) (ok bool)` is called at:

1. **Write** — the `RenameFarm` action: invalid/denied → generic
   `THAT NAME ISN'T AVAILABLE` (never say *why* — explaining teaches
   evasion), farm keeps its current name. Skipped when `name_locked`
   (operator lock beats player rename).
2. **Import** — framework/02's `import-v1` filters v1 names; failures get
   the generated default + a log line.
3. **Render** — gameplay/02's snapshot build re-checks every name against
   the *current* list; failures display as the generated default. A
   denylist update thus cleans the board on the next rebuild without
   touching stored data.

### Abuse friction

- Rename rate limit: 1 per minute, 10 per day per save (state-tracked) —
  makes oracle-probing the filter tedious.
- Rejected attempts logged (fingerprint truncated + the rejected name) for
  operator review — the feedback loop for list tightening.
- **Operator runbook** (`docs/runbooks/moderation.md`, written in this
  task): one SQL statement to force-rename + set `name_locked=1`; how to
  add a denylist term and confirm the render-time mask picked it up;
  taking a report from a player.

## Acceptance criteria

- [ ] Table tests: happy names pass; each pipeline stage has cases proving
  it (leet, separators, repeats, embedding); Scunthorpe cases pass
  (`SCUNTHORPE`, `CLASS ACT`, `GRASS VALLEY`); tier semantics differ as
  spec'd. Test inputs for slur-tier use **placeholder tokens** injected
  via a test-only list — real slurs never appear in test source
  (convention: the denylist file is the only place they exist).
- [ ] Rejection is generic and identical for invalid vs denied (timing
  too — no oracle).
- [ ] Rate limiting: 2nd rename inside a minute refused; counters survive
  save/load.
- [ ] Render-time mask: add a term, rebuild snapshot, existing name masked
  to default; `name_locked` blocks player rename but not display.
- [ ] Generated names: deterministic per fingerprint, all-pairs pass the
  filter (exhaustive product test).
- [ ] Fuzz `Check` with random bytes/Unicode — never panics, non-ASCII
  always rejected at validation.

## Out of scope

- The rename UI (v1's overlay + a rename entry point) → tui/01 keeps it.
- Chat/messages — there is no free-text anywhere else; keep it that way.
- Automated ML moderation, report queues — massive overkill at this scale.
