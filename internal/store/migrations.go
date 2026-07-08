package store

import (
	"context"
	"fmt"
)

// migrations are applied in order on startup. The schema version lives in
// SQLite's user_version pragma; each step runs inside a transaction together
// with the version bump, so a crash mid-migration leaves the database at the
// previous version. Steps must be forward-only, ordered, and never
// destructive to existing player data.
var migrations = []string{
	// v1: accounts by key fingerprint, saves by (fingerprint, slot). This is
	// v1 (ssh-idlefarmer)'s final schema shape byte-compatible with its own
	// accounts/saves tables (including the state_version column v1 added in
	// its own migration 2) — a fresh v2 database starts here directly rather
	// than replaying v1's historical two-step evolution.
	`CREATE TABLE accounts (
		fingerprint TEXT PRIMARY KEY,
		public_key  TEXT NOT NULL,
		first_seen  INTEGER NOT NULL,
		last_seen   INTEGER NOT NULL
	);
	CREATE TABLE saves (
		fingerprint   TEXT NOT NULL REFERENCES accounts(fingerprint),
		slot          TEXT NOT NULL,
		created_at    INTEGER NOT NULL,
		last_active   INTEGER NOT NULL,
		state         BLOB NOT NULL,
		state_version INTEGER NOT NULL DEFAULT 1,
		PRIMARY KEY (fingerprint, slot)
	) WITHOUT ROWID;`,

	// v2 (framework/02): leaderboard/moderation columns denormalized off the
	// state blob. The blob stays the source of truth; these are indexes into
	// it, always written in the same transaction as the blob (see
	// PersistSave) so the board and the blob can never disagree.
	`ALTER TABLE saves ADD COLUMN coins INTEGER NOT NULL DEFAULT 0;
	ALTER TABLE saves ADD COLUMN farm_name TEXT NOT NULL DEFAULT '';
	ALTER TABLE saves ADD COLUMN name_locked INTEGER NOT NULL DEFAULT 0;
	CREATE INDEX idx_saves_coins ON saves (coins DESC);`,

	// v3 (gameplay/03): rename abuse-friction counters. These live in their
	// own columns rather than the state blob because internal/sim's diff
	// from v1 is contractually frozen to the Coins/FarmName/SetFarmName
	// accessors gameplay/01 added (see docs/gameplay/01's acceptance
	// criteria) — this is moderation bookkeeping, not gameplay state. Zero
	// defaults are exactly correct for every existing row: "never renamed"
	// is what an empty history means.
	`ALTER TABLE saves ADD COLUMN rename_last_at INTEGER NOT NULL DEFAULT 0;
	ALTER TABLE saves ADD COLUMN rename_day_start INTEGER NOT NULL DEFAULT 0;
	ALTER TABLE saves ADD COLUMN rename_day_count INTEGER NOT NULL DEFAULT 0;`,

	// v4: the leaderboard now ranks and displays lifetime coin earnings and
	// rebirth count instead of the current spendable balance (gameplay/02's
	// "cross-game or lifetime-earnings boards" TODO). Unlike v2's
	// coins/farm_name (which needed a Go-side boot-time reconcile pass for
	// legacy rows), every row reaching this migration already carries a
	// valid-JSON state blob written by this same package, so the backfill
	// runs inline here via json_extract — json_valid guards a still
	// undecodable blob to 0 rather than aborting the migration (mirrors
	// ReconcileDenormalized's per-row tolerance, see internal/game/reconcile.go).
	`ALTER TABLE saves ADD COLUMN lifetime_earnings INTEGER NOT NULL DEFAULT 0;
	ALTER TABLE saves ADD COLUMN rebirths INTEGER NOT NULL DEFAULT 0;
	UPDATE saves SET
		lifetime_earnings = CASE WHEN json_valid(state) THEN COALESCE(json_extract(state, '$.lifetime_earnings'), 0) ELSE 0 END,
		rebirths = CASE WHEN json_valid(state) THEN COALESCE(json_extract(state, '$.rebirths'), 0) ELSE 0 END;
	CREATE INDEX idx_saves_lifetime_earnings ON saves (lifetime_earnings DESC);`,
}

func (st *Store) migrate(ctx context.Context) error {
	var current int
	if err := st.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("store: read schema version: %w", err)
	}
	if current > len(migrations) {
		return fmt.Errorf("store: database schema version %d is newer than supported %d", current, len(migrations))
	}

	for v := current; v < len(migrations); v++ {
		tx, err := st.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("store: begin migration %d: %w", v+1, err)
		}
		if _, err := tx.ExecContext(ctx, migrations[v]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: apply migration %d: %w", v+1, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", v+1)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: bump schema version to %d: %w", v+1, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("store: commit migration %d: %w", v+1, err)
		}
	}
	return nil
}

// SchemaVersion returns the database's current schema version.
func (st *Store) SchemaVersion(ctx context.Context) (int, error) {
	var v int
	err := st.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v)
	return v, err
}
