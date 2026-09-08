// Package store is the SQLite persistence layer. It owns all SQL in the
// project: a versioned schema with forward migrations, accounts keyed by
// public-key fingerprint, and saves keyed by (fingerprint, slot).
//
// Ownership is enforced structurally: every query that touches a save takes
// the fingerprint as a parameter, there is no API that addresses a save by
// slot alone, and all access uses parameterized statements.
//
// Write serialization: the pool is capped at a single connection, so the
// database never sees concurrent writes from this process. Per-save mutation
// is additionally serialized by the save actor (internal/game), which is the
// sole writer of its save's row while active.
//
// The saves table additionally carries denormalized coins/farm_name/
// name_locked/lifetime_earnings/rebirths columns for the leaderboard
// (gameplay/02) and moderation (gameplay/03). The JSON state blob stays the
// source of truth; the store's job is only to keep the columns and the blob
// from ever disagreeing — every write goes through one statement inside one
// transaction. Decoding the blob is the caller's job (internal/game), since
// the state's shape is owned by internal/sim, not this package.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"

	_ "modernc.org/sqlite" // pure-Go, cgo-free driver (static builds)
)

// slotPattern re-validates slots at the storage boundary even though
// identity resolution sanitizes them at the door — defense in depth.
var slotPattern = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

// ErrInvalidKey is returned when a fingerprint or slot fails validation.
var ErrInvalidKey = errors.New("store: invalid fingerprint or slot")

// Store is the open database handle.
type Store struct {
	db *sql.DB
}

// SaveRow is one persisted save.
type SaveRow struct {
	Fingerprint      string
	Slot             string
	CreatedAt        int64
	LastActive       int64
	State            []byte
	StateVersion     int
	Coins            int64
	LifetimeEarnings int64
	Rebirths         int64
	FarmName         string
	NameLocked       bool
}

// FreshSave is what LoadOrCreateSave's fresh() callback returns to seed a
// brand-new row: the encoded state plus its denormalized leaderboard
// columns, extracted up front so a new farm never shows as a zero-coin
// ghost on the board before its first autosave.
type FreshSave struct {
	State            []byte
	Version          int
	Coins            int64
	LifetimeEarnings int64
	Rebirths         int64
	FarmName         string
}

// Open opens (creating if needed) the database at path, applies pending
// migrations, and returns the store. The file and its directory are created
// with restrictive permissions: the database holds all player state.
func Open(ctx context.Context, path string) (*Store, error) {
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("store: create db dir: %w", err)
		}
	}

	dsn := "file:" + url.PathEscape(path) +
		"?_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	// A single connection serializes all reads and writes (see package doc).
	db.SetMaxOpenConns(1)

	st := &Store{db: db}
	if err := st.verifyWAL(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := st.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	// Best-effort permission tightening (chmod is advisory on Windows).
	_ = os.Chmod(path, 0o600)
	return st, nil
}

// Close closes the underlying database.
func (st *Store) Close() error { return st.db.Close() }

func (st *Store) verifyWAL(ctx context.Context) error {
	var mode string
	if err := st.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		return fmt.Errorf("store: read journal mode: %w", err)
	}
	if mode != "wal" {
		return fmt.Errorf("store: WAL mode required, database reports %q", mode)
	}
	return nil
}

// TouchAccount records that fingerprint connected at now, creating the
// account row on first sight (trust-on-first-use). publicKey is stored in
// authorized_keys format for auditing.
func (st *Store) TouchAccount(ctx context.Context, fingerprint, publicKey string, now int64) error {
	if fingerprint == "" {
		return ErrInvalidKey
	}
	_, err := st.db.ExecContext(ctx, `
		INSERT INTO accounts (fingerprint, public_key, first_seen, last_seen)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (fingerprint) DO UPDATE SET last_seen = excluded.last_seen`,
		fingerprint, publicKey, now, now)
	if err != nil {
		return fmt.Errorf("store: touch account: %w", err)
	}
	return nil
}

// LoadOrCreateSave returns the save for (fingerprint, slot), creating it
// with the payload from fresh() the first time this key uses this slot.
// It can only ever return a save owned by fingerprint.
func (st *Store) LoadOrCreateSave(ctx context.Context, fingerprint, slot string, now int64, fresh func() (FreshSave, error)) (SaveRow, bool, error) {
	if err := validateKeys(fingerprint, slot); err != nil {
		return SaveRow{}, false, err
	}

	row, err := st.loadSave(ctx, fingerprint, slot)
	if err == nil {
		return row, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return SaveRow{}, false, err
	}

	fs, err := fresh()
	if err != nil {
		return SaveRow{}, false, fmt.Errorf("store: build fresh save: %w", err)
	}
	res, err := st.db.ExecContext(ctx, `
		INSERT INTO saves (fingerprint, slot, created_at, last_active, state, state_version, coins, lifetime_earnings, rebirths, farm_name)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (fingerprint, slot) DO NOTHING`,
		fingerprint, slot, now, now, fs.State, fs.Version, fs.Coins, fs.LifetimeEarnings, fs.Rebirths, fs.FarmName)
	if err != nil {
		return SaveRow{}, false, fmt.Errorf("store: create save: %w", err)
	}
	inserted, err := res.RowsAffected()
	if err != nil {
		return SaveRow{}, false, fmt.Errorf("store: create save: %w", err)
	}
	// Re-read so a concurrent creator's row (rather than ours) wins cleanly.
	row, err = st.loadSave(ctx, fingerprint, slot)
	if err != nil {
		return SaveRow{}, false, err
	}
	return row, inserted == 1, nil
}

func (st *Store) loadSave(ctx context.Context, fingerprint, slot string) (SaveRow, error) {
	row := SaveRow{Fingerprint: fingerprint, Slot: slot}
	var nameLocked int
	err := st.db.QueryRowContext(ctx, `
		SELECT created_at, last_active, state, state_version, coins, lifetime_earnings, rebirths, farm_name, name_locked
		FROM saves WHERE fingerprint = ? AND slot = ?`,
		fingerprint, slot).
		Scan(&row.CreatedAt, &row.LastActive, &row.State, &row.StateVersion, &row.Coins, &row.LifetimeEarnings, &row.Rebirths, &row.FarmName, &nameLocked)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SaveRow{}, err
		}
		return SaveRow{}, fmt.Errorf("store: load save: %w", err)
	}
	row.NameLocked = nameLocked != 0
	return row, nil
}

// PersistSave writes the save payload for (fingerprint, slot) together with
// its denormalized leaderboard columns, in one transaction: the blob and
// the board can never disagree, because they are never written separately.
// It never creates rows: persisting a save that was deleted out from under
// us is an error, not a resurrection.
func (st *Store) PersistSave(ctx context.Context, fingerprint, slot string, state []byte, stateVersion int, lastActive int64, coins, lifetimeEarnings, rebirths int64, farmName string) error {
	if err := validateKeys(fingerprint, slot); err != nil {
		return err
	}
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: persist save: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after a successful Commit

	res, err := tx.ExecContext(ctx, `
		UPDATE saves SET state = ?, state_version = ?, last_active = ?, coins = ?, lifetime_earnings = ?, rebirths = ?, farm_name = ?
		WHERE fingerprint = ? AND slot = ?`,
		state, stateVersion, lastActive, coins, lifetimeEarnings, rebirths, farmName, fingerprint, slot)
	if err != nil {
		return fmt.Errorf("store: persist save: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: persist save: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("store: persist save: no row for this key/slot")
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: persist save: commit: %w", err)
	}
	return nil
}

// LockName marks a save's farm name as moderation-locked (gameplay/03):
// the store never interprets the flag, it only carries it so moderation can
// re-validate names at render time without re-reading the blob.
func (st *Store) LockName(ctx context.Context, fingerprint, slot string, locked bool) error {
	if err := validateKeys(fingerprint, slot); err != nil {
		return err
	}
	n := 0
	if locked {
		n = 1
	}
	res, err := st.db.ExecContext(ctx, `
		UPDATE saves SET name_locked = ? WHERE fingerprint = ? AND slot = ?`,
		n, fingerprint, slot)
	if err != nil {
		return fmt.Errorf("store: lock name: %w", err)
	}
	if affected, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("store: lock name: %w", err)
	} else if affected == 0 {
		return fmt.Errorf("store: lock name: no row for this key/slot")
	}
	return nil
}

// ListSlots returns the slot names owned by fingerprint, newest activity
// first. Used for diagnostics and the stats screen; never crosses keys.
func (st *Store) ListSlots(ctx context.Context, fingerprint string) ([]string, error) {
	if fingerprint == "" {
		return nil, ErrInvalidKey
	}
	rows, err := st.db.QueryContext(ctx, `
		SELECT slot FROM saves WHERE fingerprint = ? ORDER BY last_active DESC`,
		fingerprint)
	if err != nil {
		return nil, fmt.Errorf("store: list slots: %w", err)
	}
	defer rows.Close()
	var slots []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, fmt.Errorf("store: list slots: %w", err)
		}
		slots = append(slots, s)
	}
	return slots, rows.Err()
}

// IntegrityCheck runs SQLite's own consistency check (PRAGMA
// integrity_check) and reports the first problem found, if any. Used by the
// durability drills (scripts/restore-drill, docs/tests/02) after a restore,
// to catch corruption a WAL-frame replay could in principle leave behind —
// it says nothing about the *content* of any save, only that the file
// itself decodes as a well-formed SQLite database.
func (st *Store) IntegrityCheck(ctx context.Context) error {
	var result string
	if err := st.db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return fmt.Errorf("store: integrity check: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("store: integrity check failed: %s", result)
	}
	return nil
}

// AllSaves returns every save row, blob included — unlike NeedingBackfill,
// with no filter. This exists for the durability drills' decode-every-save
// pass (scripts/restore-drill, docs/tests/02): the store still does not
// decode the blob itself (see the package doc), it only hands the caller
// every row so it can.
func (st *Store) AllSaves(ctx context.Context) ([]SaveRow, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT fingerprint, slot, created_at, last_active, state, state_version, coins, lifetime_earnings, rebirths, farm_name, name_locked
		FROM saves`)
	if err != nil {
		return nil, fmt.Errorf("store: all saves: %w", err)
	}
	defer rows.Close()
	var out []SaveRow
	for rows.Next() {
		var r SaveRow
		var nameLocked int
		if err := rows.Scan(&r.Fingerprint, &r.Slot, &r.CreatedAt, &r.LastActive, &r.State, &r.StateVersion, &r.Coins, &r.LifetimeEarnings, &r.Rebirths, &r.FarmName, &nameLocked); err != nil {
			return nil, fmt.Errorf("store: all saves: %w", err)
		}
		r.NameLocked = nameLocked != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

// NeedingBackfill returns every save whose denormalized columns look
// un-populated (coins = 0 AND farm_name = ”): the set the boot-time
// reconcile pass and import-v1 need to backfill by decoding the blob. A
// save that legitimately has zero coins and no name (a genuinely fresh,
// unnamed farm) is backfilled as a harmless no-op — its columns are already
// correct. lifetime_earnings/rebirths ride along with coins/farm_name in
// this same set: any row still un-populated on coins/farm_name is, by
// construction, also still at migration 004's zero defaults for the newer
// columns (see BackfillDenormalized).
func (st *Store) NeedingBackfill(ctx context.Context) ([]SaveRow, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT fingerprint, slot, created_at, last_active, state, state_version, coins, lifetime_earnings, rebirths, farm_name, name_locked
		FROM saves WHERE coins = 0 AND farm_name = ''`)
	if err != nil {
		return nil, fmt.Errorf("store: needing backfill: %w", err)
	}
	defer rows.Close()
	var out []SaveRow
	for rows.Next() {
		var r SaveRow
		var nameLocked int
		if err := rows.Scan(&r.Fingerprint, &r.Slot, &r.CreatedAt, &r.LastActive, &r.State, &r.StateVersion, &r.Coins, &r.LifetimeEarnings, &r.Rebirths, &r.FarmName, &nameLocked); err != nil {
			return nil, fmt.Errorf("store: needing backfill: %w", err)
		}
		r.NameLocked = nameLocked != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

// BackfillDenormalized sets a save's coins/lifetime_earnings/rebirths/
// farm_name columns without touching the blob or last_active — used by the
// reconcile pass and import-v1, which both derive these values by decoding
// the blob themselves (store treats the blob as opaque bytes).
func (st *Store) BackfillDenormalized(ctx context.Context, fingerprint, slot string, coins, lifetimeEarnings, rebirths int64, farmName string) error {
	if err := validateKeys(fingerprint, slot); err != nil {
		return err
	}
	res, err := st.db.ExecContext(ctx, `
		UPDATE saves SET coins = ?, lifetime_earnings = ?, rebirths = ?, farm_name = ? WHERE fingerprint = ? AND slot = ?`,
		coins, lifetimeEarnings, rebirths, farmName, fingerprint, slot)
	if err != nil {
		return fmt.Errorf("store: backfill: %w", err)
	}
	if affected, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("store: backfill: %w", err)
	} else if affected == 0 {
		return fmt.Errorf("store: backfill: no row for this key/slot")
	}
	return nil
}

// CountSaves reports the total number of save rows (used by import-v1 to
// require an empty store unless --merge is passed).
func (st *Store) CountSaves(ctx context.Context) (int, error) {
	var n int
	err := st.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM saves").Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: count saves: %w", err)
	}
	return n, nil
}

// HasSave reports whether a row already exists for (fingerprint, slot),
// used by import-v1 to detect collisions before writing.
func (st *Store) HasSave(ctx context.Context, fingerprint, slot string) (bool, error) {
	if err := validateKeys(fingerprint, slot); err != nil {
		return false, err
	}
	var n int
	err := st.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM saves WHERE fingerprint = ? AND slot = ?`,
		fingerprint, slot).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("store: has save: %w", err)
	}
	return n > 0, nil
}

// InsertSave inserts a brand-new save row directly with every column
// populated, for import-v1 (which already has the fully-decoded state and
// its denormalized values in hand — there's no "fresh" payload to build).
// It fails if the row already exists; import-v1's collision check runs
// first, but this is a second line of defense.
func (st *Store) InsertSave(ctx context.Context, fingerprint, slot string, state []byte, stateVersion int, createdAt, lastActive int64, coins, lifetimeEarnings, rebirths int64, farmName string, nameLocked bool) error {
	if err := validateKeys(fingerprint, slot); err != nil {
		return err
	}
	locked := 0
	if nameLocked {
		locked = 1
	}
	res, err := st.db.ExecContext(ctx, `
		INSERT INTO saves (fingerprint, slot, created_at, last_active, state, state_version, coins, lifetime_earnings, rebirths, farm_name, name_locked)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (fingerprint, slot) DO NOTHING`,
		fingerprint, slot, createdAt, lastActive, state, stateVersion, coins, lifetimeEarnings, rebirths, farmName, locked)
	if err != nil {
		return fmt.Errorf("store: insert save: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: insert save: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("store: insert save: row already exists for this key/slot")
	}
	return nil
}

// RenameGate is the outcome of GateRename: whether a rename attempt may
// proceed, and if not, why — so the action layer (internal/game) can
// return the right sentinel error without a second query.
type RenameGate int

const (
	// RenameAllowed means the attempt was recorded and may proceed to
	// content moderation.
	RenameAllowed RenameGate = iota
	// RenameLocked means an operator has locked this save's name
	// (docs/runbooks/moderation.md); no attempt is recorded.
	RenameLocked
	// RenameRateLimited means the save's abuse-friction budget (a burst of
	// RenameBurstLimit, then renameCooldownSeconds; renameDailyLimit/day) is
	// exhausted; no attempt is recorded.
	RenameRateLimited
)

const (
	// RenameBurstLimit is how many attempts a player may make back to back
	// before the cooldown applies. Naming is iterative — you try one, you
	// dislike it, you try another — and moderation gives no reason for a
	// denial, so a player who trips the denylist is guessing. One attempt per
	// minute turned that guessing into an unusable feature. A burst absorbs
	// the honest iteration; the cooldown behind it still bounds a prober to
	// RenameBurstLimit guesses per renameCooldownSeconds, which is far too
	// slow to map a denylist of any size.
	RenameBurstLimit = 5

	// renameCooldownSeconds is how long a save waits, after spending its
	// burst, before the allowance refills.
	renameCooldownSeconds = 60

	renameDayWindowSeconds = 86400

	// renameDailyLimit is the backstop against someone grinding the burst all
	// day. It has to stay well clear of RenameBurstLimit or the burst is
	// decorative: at the old value of 10 a player got two bursts and then
	// nothing until tomorrow, which is a worse experience than the per-minute
	// gate this replaces.
	renameDailyLimit = 60
)

// GateRename checks and, if allowed, atomically consumes one unit of
// gameplay/03's rename abuse-friction budget for (fingerprint, slot) at
// now. An operator lock (name_locked) always wins and costs nothing to
// check. Otherwise the budget is consumed for every attempt that clears
// the gate — regardless of what the caller does with it next, since
// content moderation runs after this and a denied name must not be free to
// retry — so a rate-limited or content-denied attempt looks identical from
// the outside (no oracle on why a given attempt failed).
//
// The budget is a burst of RenameBurstLimit attempts, refilled in full once
// renameCooldownSeconds have passed since the last allowed attempt, under a
// renameDailyLimit-per-day ceiling.
//
// This lives in Store rather than internal/sim (whose diff from v1 is
// frozen) or internal/moderation (which stays a pure function over
// strings, with no per-save state): a check-and-increment against one row
// is exactly the kind of thing a transaction is for.
func (st *Store) GateRename(ctx context.Context, fingerprint, slot string, now int64) (RenameGate, error) {
	if err := validateKeys(fingerprint, slot); err != nil {
		return RenameRateLimited, err
	}
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return RenameRateLimited, fmt.Errorf("store: gate rename: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after a successful Commit

	var locked int
	var lastAt, dayStart int64
	var dayCount, burstCount int
	err = tx.QueryRowContext(ctx, `
		SELECT name_locked, rename_last_at, rename_day_start, rename_day_count, rename_burst_count
		FROM saves WHERE fingerprint = ? AND slot = ?`,
		fingerprint, slot).Scan(&locked, &lastAt, &dayStart, &dayCount, &burstCount)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RenameRateLimited, fmt.Errorf("store: gate rename: no row for this key/slot")
		}
		return RenameRateLimited, fmt.Errorf("store: gate rename: %w", err)
	}
	if locked != 0 {
		return RenameLocked, nil
	}

	// The burst refills in full once the cooldown has elapsed since the last
	// attempt that was actually allowed. Measuring from the last *allowed*
	// attempt, not the last attempt of any kind, is what stops a player who
	// keeps hammering during the cooldown from pushing their own unlock
	// further away every time they try.
	if lastAt != 0 && now >= lastAt+renameCooldownSeconds {
		burstCount = 0
	}
	if burstCount >= RenameBurstLimit {
		return RenameRateLimited, nil
	}
	if now >= dayStart+renameDayWindowSeconds {
		dayStart = now
		dayCount = 0
	}
	if dayCount >= renameDailyLimit {
		return RenameRateLimited, nil
	}
	dayCount++
	burstCount++

	if _, err := tx.ExecContext(ctx, `
		UPDATE saves SET rename_last_at = ?, rename_day_start = ?, rename_day_count = ?, rename_burst_count = ?
		WHERE fingerprint = ? AND slot = ?`,
		now, dayStart, dayCount, burstCount, fingerprint, slot); err != nil {
		return RenameRateLimited, fmt.Errorf("store: gate rename: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return RenameRateLimited, fmt.Errorf("store: gate rename: commit: %w", err)
	}
	return RenameAllowed, nil
}

// LeaderboardRow is one save's denormalized board data (gameplay/02): only
// the columns the leaderboard is allowed to read, never the blob.
type LeaderboardRow struct {
	Fingerprint string
	Slot        string
	// Coins is the amount ranked and displayed on the board: lifetime coin
	// earnings (the lifetime_earnings column), which only ever grows —
	// never the save's current spendable balance, which rebirth resets.
	Coins     int64
	Rebirths  int64
	FarmName  string
	UpdatedAt int64 // last_active: when this row was last confirmed accurate
}

// LeaderboardSnapshot returns every save's denormalized board columns,
// ordered by lifetime coin earnings DESC, last_active ASC, fingerprint ASC
// — gameplay/02's competition-ranking tie-break ("first to the money shows
// first"). The leaderboard engine is the only intended caller; it holds the
// result in memory and rebuilds on its own TTL rather than querying per
// request (see docs/gameplay/02 "Caching").
func (st *Store) LeaderboardSnapshot(ctx context.Context) ([]LeaderboardRow, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT fingerprint, slot, lifetime_earnings, rebirths, farm_name, last_active
		FROM saves
		ORDER BY lifetime_earnings DESC, last_active ASC, fingerprint ASC`)
	if err != nil {
		return nil, fmt.Errorf("store: leaderboard snapshot: %w", err)
	}
	defer rows.Close()
	var out []LeaderboardRow
	for rows.Next() {
		var r LeaderboardRow
		if err := rows.Scan(&r.Fingerprint, &r.Slot, &r.Coins, &r.Rebirths, &r.FarmName, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("store: leaderboard snapshot: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func validateKeys(fingerprint, slot string) error {
	if fingerprint == "" || !slotPattern.MatchString(slot) {
		return ErrInvalidKey
	}
	return nil
}
