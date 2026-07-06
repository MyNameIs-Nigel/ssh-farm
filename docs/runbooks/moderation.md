# Runbook: Farm-Name Moderation

Operator procedures for `internal/moderation` (gameplay/03). All commands
assume `sqlite3 var/farm.db` (adjust `FARM_DB_PATH` as configured) and that
you have shell access to the running host. The database uses WAL mode with
a single writer connection held by the game process — these are all plain
reads/writes against the `saves` table, which is safe to run alongside a
live server (SQLite serializes them), but prefer running during low traffic
if you're touching many rows at once.

## Force-rename a farm and lock it

Use this when a name slipped past the filter (a report, or something the
denylist doesn't cover yet) and you want it fixed **now**, permanently —
`name_locked=1` beats any future player rename attempt
(`Store.GateRename`/`Session.RenameFarm`, see `internal/game/session.go`).

```sql
UPDATE saves
SET farm_name = 'RENAMED FARM', name_locked = 1
WHERE fingerprint = 'SHA256:...' AND slot = '...';
```

- Find the row first with
  `SELECT fingerprint, slot, farm_name FROM saves WHERE farm_name LIKE '%...%';`
  if you only have the offending text, not the key.
- The replacement name should itself pass `internal/moderation.Check` (pick
  something boring) — the store does not re-validate what you write here.
- The leaderboard's next rebuild (at most `FARM_LEADERBOARD_TTL`, default
  15s) picks up the change automatically; there is no cache to bust by hand.

To unlock a farm (let the player rename it again), clear the flag:

```sql
UPDATE saves SET name_locked = 0 WHERE fingerprint = 'SHA256:...' AND slot = '...';
```

This also resets nothing about the rate-limit counters — a freshly-unlocked
player is still subject to the normal 1/minute, 10/day budget
(`rename_last_at`, `rename_day_start`, `rename_day_count` columns).

## Add a denylist term and confirm it takes effect retroactively

1. Edit `data/moderation/denylist.toml` (private repo — never copy real
   entries into public repos, commit messages, or test names; test
   fixtures use placeholder tokens like `badterm` instead, see
   `internal/moderation/denylist_test.go`).
2. Add a `[[term]]` block:
   ```toml
   [[term]]
   word = "newterm"    # normalized: lowercase, what Validate would produce
   tier = "slur"        # or "profanity"
   match = "substring"  # or "word"
   ```
   - `slur` + `substring`: zero tolerance, matches anywhere (leetspeak and
     separator evasion are folded away before matching — see
     `internal/moderation/normalize.go`).
   - `profanity` + `word`: whole-token only, so it can't trip on an
     innocent word that merely contains it (the Scunthorpe problem).
3. If the change introduces a false positive, add the *specific* affected
   name to the `allow` list at the bottom of the same file — never remove
   or weaken the term that caused it.
4. Run `go test ./internal/moderation/...` to confirm the new term parses
   and the existing corpus (including the Scunthorpe cases and the
   `Generate` all-pairs exhaustive test) still passes.
5. Rebuild and redeploy. This is a data change bundled into a normal
   binary release — there is no runtime hot-reload for the denylist.
6. **Verify the retroactive mask**: any *already-stored* name that now
   matches is never rewritten in the database (the blob and `farm_name`
   column are untouched), but `internal/leaderboard`'s render-time
   re-check (`Filter` called again at snapshot build) means the next
   leaderboard rebuild displays `moderation.Generate(fingerprint)` instead
   of the now-denied name. Confirm by checking the board after
   `FARM_LEADERBOARD_TTL` has elapsed, or by restarting the process to force
   an immediate rebuild.

## Taking a report from a player

1. Ask for (or find) the fingerprint/slot — `truncateFingerprint`-style
   partial fingerprints are fine for a first pass:
   `SELECT fingerprint, slot, farm_name FROM saves WHERE farm_name LIKE '%reported text%';`
2. Decide: is this a denylist gap (add a term, §above — fixes it for
   everyone, retroactively) or a one-off (force-rename + lock, §above —
   fixes just this row, immediately, without waiting for a deploy)?
   For anything urgent, force-rename + lock first, then consider a denylist
   addition afterward at your own pace.
3. Rejected *attempts* are logged by the rename action
   (`internal/game/session.go`) at the point moderation denies them — check
   application logs for the fingerprint (truncated) if you need history of
   what a player tried, keeping in mind the log line records that an
   attempt was denied, not full detail of why (no oracle, even in logs).

## Rename rate limits (abuse friction, not a moderation lever)

`rename_last_at`, `rename_day_start`, `rename_day_count` on `saves` track
gameplay/03's 1-per-minute, 10-per-day budget (`Store.GateRename`). These
are not moderation controls — do not edit them to "fix" a name; use the
force-rename procedure above instead. They exist purely to make probing the
filter tedious. To reset a legitimately-stuck player's budget (e.g. you
force-renamed them and they now want to pick their own name today):

```sql
UPDATE saves SET rename_last_at = 0, rename_day_count = 0
WHERE fingerprint = 'SHA256:...' AND slot = '...';
```
