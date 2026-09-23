# Farm Gameplay 04 — Contracts

**Status:** Implemented · **Depends on:** the completed base-game progression,
gameplay/02 (leaderboard), gameplay/03 (name identity), tui/02 (board screen)

## Goal

Give players who have exhausted the finite progression a short, deliberate
mastery campaign. A contract is a voluntary hard reset with rules that remove
specific conveniences. Completing it awards a permanent public cosmetic,
never more production power.

There are **exactly three contracts**. They are handcrafted, completed once,
and unlocked in this fixed order:

1. **Bare Hands**
2. **Lean Season**
3. **Clockwork Denied**

There are no rotating contracts, generated contracts, remixes, prestige tiers,
or implied fourth contract. The animated leaderboard name is the endpoint.

## Design principles

- **Longer-lived, not padded:** each contract changes how the existing economy
  is played. Difficulty comes from a legible restriction and a bounded goal,
  not an arbitrary order-of-magnitude increase.
- **Knowledge is the retained power:** contract starts remove economic power.
  Players keep what they learned about crops, timing, rebirth, and spending.
- **Public proof, private choice:** rewards are visible on the leaderboard but
  optional. No cosmetic changes ranking or earnings.
- **No accidental loss:** the terms and exact reset are shown before a
  contract starts, and acceptance requires deliberate confirmation.
- **Base saves remain valid:** existing saves decode with no active contract,
  zero completions, and the traditional leaderboard appearance.

## Unlock and sequence

The Contracts screen becomes available when the permanent base game is
complete:

- every StarShop lifetime upgrade is at its configured maximum;
- all non-seasonal prestige crop tiers are unlocked (currently rebirth 3);
- the save has met every permanent earnings-gated crop unlock.

Run-scoped purchases such as plots, the Greenhouse, multipliers, strains,
Scarecrow, and plot automation are not part of the unlock check because every
ordinary rebirth removes them. Seasonal crops are excluded because contract
access must not depend on the calendar. Achievements are not a gate.

This completion gate is checked only to enter the campaign for the first time.
Once the campaign has begun, resetting upgrades cannot relock Contracts or
force the player to max the ordinary game again between Contracts 1, 2, and 3.

Only the next incomplete contract can be started. The screen shows completed
contracts above it and later contracts as named but locked, so the three-step
campaign is visible without revealing every final target before it matters.

At 80 columns the existing navigation strip is already full. Contracts do not
add a ninth tab. Once eligible, the StarShop gains a `Contracts` row after its
lifetime upgrades, showing campaign or active-contract progress; activating it
(Enter, or a double-click) or pressing `c` anywhere in the StarShop opens the
Contracts screen while preserving the normal nav strip and Back behavior. The
Stats contract line likewise appears only once the campaign can be entered.

## The hard reset

Starting any contract irreversibly resets gameplay progression. It does not
create a parallel save or a restore point.

Reset to fresh-save values:

- spendable coins, planted crops, plots, and purchased land;
- run earnings and contract-local progress counters;
- Starseeds and every StarShop upgrade;
- zones, multipliers, Hardier Strains, Scarecrow, and all plot automation;
- pending gifts, active events, crop/replant memory, and other transient
  advantages;
- contract-local rebirth progress.

Preserve permanently:

- lifetime coin earnings used by the leaderboard;
- lifetime harvest count and achievements;
- total historical rebirth count used by the leaderboard;
- completed contracts and their cosmetic rewards;
- farm name and player settings.

The reset creates an important implementation distinction. The existing
`State.Rebirths` is both a lifetime statistic and a gameplay unlock today.
Contracts must retain that lifetime number without inheriting its crop unlocks.
While a contract is active, crop gates, StarShop access, gifts, and other
progression checks use a contract-local rebirth count that starts at zero.
Every rebirth still increments the historical total for the leaderboard and
increments the contract-local count for the active challenge.

Lifetime earnings never decrease. Contract goals use counters whose baseline
is the moment the contract begins, so old leaderboard earnings cannot complete
a new contract instantly.

## Contract 1 — Bare Hands

**Premise:** prove that the farm can prosper without becoming a machine.

**Goal:** earn **100,000 contract coins** after accepting the contract.
Earnings accumulate across rebirths within the contract.

**Restriction:** plot automation is locked completely.

- Auto-harvest and auto-sow cannot be purchased, toggled, or inherited.
- The plot-upgrade overlay explains that Bare Hands has disabled both options.
- All other base systems remain available, including events, gifts,
  Scarecrow, land, Greenhouse, multipliers, strains, and rebirth upgrades.

**Why this comes first:** it is immediately understandable and asks for
attention without changing the rest of the economy. Its target is large enough
to require a plan but small enough to finish before manual play becomes the
entire experience.

**Reward:** the first contract seal. Leaderboard rows render one `◆` before
the farm name. The reward is permanent and selected automatically because it
adds identity without changing the existing name colors.

## Contract 2 — Lean Season

**Premise:** build a prestige economy on a deliberately small farm.

**Goal:** earn **100 total Starseeds** after accepting the contract. This is a
cumulative earned counter, not the current spendable balance, so buying
StarShop upgrades does not erase progress toward the goal.

**Restrictions:** land is capped at **six plots** and the Greenhouse is locked.

- The seventh plot cannot be purchased; Land explains the contract cap.
- The Greenhouse remains visible in the Market but is marked unavailable under
  Lean Season.
- Automation is available again. Choosing where to automate a smaller farm is
  part of the strategy and creates a deliberate change of pace after Bare
  Hands.
- All normal crop and prestige gates still apply using contract-local
  progression.

**Why this is different:** the player can choose quick rebirths or longer runs,
then decide how to spend earned Starseeds while working toward the same
cumulative target. It rewards economic planning rather than more clicking.

**Reward:** a second `◆` contract seal and access to a curated static
leaderboard-name color picker. Colors are semantic palette choices, not
arbitrary RGB input, so every option remains readable on day, night, solid,
event, and seasonal backgrounds. The traditional name treatment remains an
available option.

## Contract 3 — Clockwork Denied

**Premise:** complete the rebirth loop with every passive accelerator removed.

**Goal:** complete **five contract-local rebirths** after accepting the
contract.

**Restrictions:** automation, random events, gifts, and the Scarecrow are
locked.

- Auto-harvest and auto-sow cannot be purchased or used.
- Random events neither start nor grant effects.
- Gifts do not arrive online or offline, and no pending gift crosses into the
  contract.
- The Scarecrow cannot be purchased; critters remain cosmetic and may still be
  shooed manually for their ordinary small reward.
- Land, Greenhouse, multipliers, strains, and StarShop upgrades remain
  available. Spending each rebirth's Starseeds well is the acceleration path.

**Why this is the finale:** five bounded cycles create a mastery exam while
each cycle becomes faster through choices the player makes. It revisits the
manual restriction from Bare Hands, but the objective is now repeated rebirth
optimization and the ambient safety nets are gone.

**Reward:** a third `◆` contract seal and the animated leaderboard-name style.
The style is an optional two-color purple wave that travels left to right
through the farm-name characters. Players may switch back to any unlocked
static style.

## Contract modal and lifecycle

Selecting the available contract opens a modal before any state changes. It
must fit at 80×24 through wrapping or scrolling and show, in this order:

1. contract name and one-sentence premise;
2. exact completion goal and how progress is counted;
3. every disabled or capped system;
4. the permanent cosmetic reward;
5. a compact **RESET** list and **KEPT FOREVER** list;
6. the warning that the previous gameplay state cannot be restored.

Keyboard acceptance requires selecting the available contract, opening its
terms modal, then pressing `y`; Escape closes the modal unchanged. Mouse input
uses the same select-then-activate flow — click to select, double-click to
open the terms, then double-click the modal's accept button, exactly like
rebirth's — so a single click can never reset a farm.

At 80×24 the body can shrink to about fourteen rows (a notice and an event bar
each take one), so the modal uses a slim box and gives each term one labelled
line rather than a paragraph. Each list is one line — RESET: coins, crops,
land, Starseeds, upgrades, automation; KEPT FOREVER: name, settings,
achievements, seals, lifetime stats. The overflow tests hold every contract's
modal to that size.

Once accepted, reconnecting or going offline does not cancel the contract.
The Contracts screen shows the active terms and progress. Completing the goal
immediately records the completion, clears its restrictions, and opens a reward
modal. The current farm state remains; starting the next contract performs the
next hard reset.

A goal can be crossed outside any action: a Bare Hands scarecrow bounty on a
live tick, or its offline trickle while away. `sim.Events.ContractCompleted` and
`game.Snapshot.ContractCompleted` carry the completion explicitly, so the reward
modal opens on a tick too. A completion earned offline opens once the
welcome-back summary is dismissed. The UI never infers completion by diffing
snapshots, because its first snapshot is a placeholder, and diffing against it
would re-announce every earned seal on each connect.

A player may abandon an active contract from its details modal. Abandoning
does not restore the pre-contract farm: it clears the contract and starts a
fresh ordinary farm while preserving lifetime statistics, previous contract
rewards, name, and settings. The same contract remains the next required one.
Abandonment uses the same deliberate confirmation as starting.

## Save model

The exact representation may change during implementation, but the state needs
these concepts:

```go
type ContractState struct {
    ActiveID          string
    Completed         int   // 0..3; strict sequence is the source of truth
    LocalRebirths     int64 // gameplay tier while a contract is active
    Earnings          int64 // earned since this contract started
    StarseedsEarned   int64 // cumulative, unaffected by spending
    NameStyle         string
}
```

`Completed` is sufficient because contracts are fixed, ordered, and one-time.
Do not introduce a generic map, scheduler, expiry, or content rotation system.
Completion and selected style persist in the save blob. The store additionally
denormalizes contract completion count and selected name style beside the
existing leaderboard columns so board rebuilds never decode save blobs.

The contract fields are additive `omitempty` fields under save-state version
4. This deliberately avoids rewriting old payloads and preserves the parity
suite's byte-identical round trips. Store schema migration 6 adds and
backfills the two leaderboard columns from JSON when present; ordinary old
saves get no active contract and zero completions. Completion is never inferred
from lifetime earnings or rebirths.

## Leaderboard identity and cosmetics

Contract cosmetics augment identity without changing rank. Rank remains based
only on lifetime earnings, and the historical rebirth count remains visible.

### Row shape

- Zero completions: `SUNNY HOLLOW`
- One completion: `◆ SUNNY HOLLOW`
- Two completions: `◆◆ SUNNY HOLLOW`
- Three completions: `◆◆◆ SUNNY HOLLOW`
- Duplicate name: `◆◆ SUNNY HOLLOW (k3v9Q)`

One seal is shown per completed contract, to a maximum of three. Seals use a
stable, single-column glyph and the normal board style; the earned name style
applies only to the farm name.

The fingerprint suffix is retained but becomes conditional:

- Compute collisions across the complete active ranked population at snapshot
  rebuild time, after moderation and fallback-name generation.
- A unique rendered farm name omits its suffix.
- Every row in a duplicate-name group displays the suffix in parentheses.
- Generic fallback names always display the suffix even if the current
  snapshot happens to contain only one.
- The suffix, rank, rebirth count, earnings, `YOU` marker, and contract seals
  never inherit the player's selected name color.

This keeps ordinary rows cleaner while preserving the suffix for exactly the
case it exists to solve.

### Static colors

Contract 2 unlocks a small curated set of semantic name colors. Store a stable
style ID, not a hex value. Theme code (`theme.LeaderboardName`) maps that ID to
a readable color; because every day/night, event, solid, and seasonal canvas
is near-black, one bright tone per style reads under all of them. Color choice
lives with the farm save and is configured from Stats alongside naming and
other presentation settings.

A styled name is its own span between two spans in the row's style. Nesting it
inside one row-wide `Render` would end it in an SGR reset, and the suffix,
rebirths, `YOU` marker, and coins after it would lose the row's highlight.

### Animated final style

Contract 3 unlocks an optional purple wave. For each rendered rune `i`, blend
between two theme-approved purples using `i + phase`; increment `phase` on a
lightweight animation tick. Apply a background to every Lip Gloss span just as
the rest of the painted canvas does, or the terminal's default background will
show through between colored characters.

Animation is presentation-only:

- tick at roughly 100–150 ms only while the Board is visible and at least one
  animated row is on screen;
- never rebuild or query the leaderboard on an animation tick;
- keep display width identical to the unstyled name so alignment and hitboxes
  do not move;
- use one synchronized phase for deterministic rendering;
- fall back to a static purple treatment when the active color profile cannot
  render the gradient cleanly. The ramp (`theme.NameWave`) is built from
  xterm-256 indices, not RGB: the server forces the TrueColor profile on every
  session, so hex colours would reach 256-colour terminals as 24-bit escapes,
  while an index renders the same on both. A profile below 256 colours, as
  reported by Bubble Tea's `ColorProfileMsg`, holds the name on one purple and
  starts no tick chain;
- honor a future reduced-motion setting if one is added.

## Enforcement rules

Restrictions belong in `internal/sim`, not only in the TUI. Every keyboard,
mouse, session, offline-advance, and future caller must receive the same
result. The UI may hide or mark disabled actions, but sim actions are the
authority.

- Starting, abandoning, and completing contracts are actor-serialized intents.
- Starting a contract advances the old state to `now` before resetting it, as
  every other intent does.
- Contract completion is checked after successful actions and `Advance`, like
  achievements, and fires exactly once.
- Offline catch-up honors active restrictions: no automated harvests in Bare
  Hands or Clockwork Denied, and no offline gift/event/scarecrow effects in
  Clockwork Denied.
- Saturating arithmetic remains mandatory for contract counters.
- Server restarts, reconnects, and session takeover cannot duplicate a reward.

## Acceptance criteria

- [x] Exactly three contracts exist, in the fixed order documented here.
- [x] Contract access derives from permanent base-game completion and ignores
      temporary run state, achievements, and calendar seasons.
- [x] Start and abandon confirmations cannot be triggered by one accidental key
      press or click.
- [x] Each start resets every listed gameplay field and preserves every listed
      lifetime, identity, setting, and cosmetic field.
- [x] Historical rebirths remain on the leaderboard while contract gameplay
      starts from rebirth tier zero.
- [x] Old lifetime earnings cannot satisfy a contract; new contract earnings
      continue increasing the lifetime leaderboard value.
- [x] Bare Hands refuses every automation entry point and completes at 100,000
      contract earnings.
- [x] Lean Season refuses plot seven and the Greenhouse, allows automation, and
      completes at 100 cumulative earned Starseeds even when Starseeds are spent.
- [x] Clockwork Denied blocks automation, events, gifts, and Scarecrow effects
      online and offline, then completes after five local rebirths.
- [x] Completion is durable and idempotent across save/reload and reconnect.
- [x] Abandoning restores no old economic state and leaves the same contract as
      the next required contract.
- [x] Board snapshots carry denormalized completion/style data without decoding
      save blobs.
- [x] Duplicate rendered names receive `(suffix)` on every matching row; unique
      named farms omit it; generic fallback names retain it.
- [x] Contract seals and static name styles render correctly at 80×24 and
      120×40 under every theme.
- [x] The final wave animates without DB reads, width drift, hitbox drift, or
      multiple tick chains, and has a static limited-color fallback.
- [x] Existing saves, ranking, moderation, base rebirth behavior, and parity
      goldens remain valid when no contract is active.

## Out of scope

- More than three contracts, repeatable contracts, rotations, seasons, remixes,
  procedural goals, or a contract currency/shop.
- Production bonuses from contract rewards.
- Arbitrary player-entered colors or animation parameters.
- Changing the lifetime-earnings ranking metric.
- Removing the fingerprint suffix or exposing a full SSH fingerprint.
