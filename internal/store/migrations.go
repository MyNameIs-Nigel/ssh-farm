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
