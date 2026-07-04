package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log/slog"
	"net/url"
	"os"

	_ "modernc.org/sqlite" // pure-Go, cgo-free driver; read-only source access

	"github.com/mynameis-nigel/ssh-farm/internal/moderation"
	"github.com/mynameis-nigel/ssh-farm/internal/sim"
	"github.com/mynameis-nigel/ssh-farm/internal/store"
)

// v1SchemaVersion is ssh-idlefarmer's final schema version (its two
// migrations: base accounts/saves, then the state_version column). Reading
// v1's tables with raw SQL here — rather than importing the v1 module —
// keeps ssh-farm from depending on a whole separate game's package tree
// just to read two tables; the row shapes are simple and stable.
const v1SchemaVersion = 2

type v1Account struct {
	Fingerprint string
	PublicKey   string
	FirstSeen   int64
	LastSeen    int64
}

type v1Save struct {
	Fingerprint  string
	Slot         string
	CreatedAt    int64
	LastActive   int64
	State        []byte
	StateVersion int
}

func runImportV1(args []string) {
	fs := flag.NewFlagSet("import-v1", flag.ExitOnError)
	from := fs.String("from", "", "path to the v1 (ssh-idlefarmer) SQLite database (required)")
	dryRun := fs.Bool("dry-run", false, "print the full report without writing anything")
	merge := fs.Bool("merge", false, "allow importing into a non-empty v2 store")
	toPath := fs.String("to", "", "path to the v2 database (default: FARM_DB_PATH env, else var/farm.db)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if *from == "" {
		logger.Error("import-v1: --from is required")
		os.Exit(1)
	}
	dest := *toPath
	if dest == "" {
		dest = envOr("FARM_DB_PATH", "var/farm.db")
	}

	ctx := context.Background()

	if err := doImportV1(ctx, logger, *from, dest, *dryRun, *merge); err != nil {
		logger.Error("import-v1 failed", "error", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// doImportV1 does not need game content: sim.DecodeState is a pure JSON
// decode (the state's shape, not its content-derived values, is what
// matters here), and moderation.Filter validates names without content
// either. Only the runtime server (serve.go) needs content.Load.
func doImportV1(ctx context.Context, logger *slog.Logger, from, dest string, dryRun, merge bool) error {
	srcDB, err := openReadOnly(from)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer srcDB.Close()

	if err := validateV1Schema(ctx, srcDB); err != nil {
		return fmt.Errorf("source schema: %w", err)
	}

	accounts, err := readV1Accounts(ctx, srcDB)
	if err != nil {
		return fmt.Errorf("read accounts: %w", err)
	}
	saves, err := readV1Saves(ctx, srcDB)
	if err != nil {
		return fmt.Errorf("read saves: %w", err)
	}
	logger.Info("import-v1: source read", "accounts", len(accounts), "saves", len(saves))

	dst, err := store.Open(ctx, dest)
	if err != nil {
		return fmt.Errorf("open destination: %w", err)
	}
	defer dst.Close()

	if !merge {
		n, err := dst.CountSaves(ctx)
		if err != nil {
			return fmt.Errorf("count destination saves: %w", err)
		}
		if n > 0 {
			return fmt.Errorf("destination store already has %d save(s); pass --merge to import into it anyway", n)
		}
	}

	// Collision pre-check: abort before writing anything if any (fingerprint,
	// slot) already exists in the destination. Collision never overwrites.
	var collisions []string
	for _, sv := range saves {
		has, err := dst.HasSave(ctx, sv.Fingerprint, sv.Slot)
		if err != nil {
			return fmt.Errorf("check collision for %s/%s: %w", truncateFingerprint(sv.Fingerprint), sv.Slot, err)
		}
		if has {
			collisions = append(collisions, fmt.Sprintf("%s/%s", truncateFingerprint(sv.Fingerprint), sv.Slot))
		}
	}
	if len(collisions) > 0 {
		return fmt.Errorf("collision(s) with existing v2 saves, aborting without writing: %v", collisions)
	}

	// Decode every blob up front with the ported sim. A blob that fails to
	// decode is a parity bug, not a data bug — abort the whole import rather
	// than silently drop a farm.
	type decoded struct {
		sv          v1Save
		state       *sim.State
		name        string
		locked      bool
		wasFiltered bool
	}
	plan := make([]decoded, 0, len(saves))
	for _, sv := range saves {
		state, err := sim.DecodeState(sv.State)
		if err != nil {
			return fmt.Errorf("decode save %s/%s: %w (aborting import, no partial writes)", truncateFingerprint(sv.Fingerprint), sv.Slot, err)
		}
		name, locked := moderation.Filter(state.FarmName)
		plan = append(plan, decoded{
			sv: sv, state: state, name: name, locked: locked,
			wasFiltered: locked && state.FarmName != "",
		})
	}

	if dryRun {
		logger.Info("import-v1 dry run: nothing will be written")
		for _, d := range plan {
			logger.Info("would import",
				"fingerprint", truncateFingerprint(d.sv.Fingerprint), "slot", d.sv.Slot,
				"coins", d.state.Coins, "farm_name", d.name, "name_locked", d.locked)
			if d.wasFiltered {
				logger.Info("v1 farm name denied by moderation, would import as anonymous",
					"fingerprint", truncateFingerprint(d.sv.Fingerprint), "slot", d.sv.Slot)
			}
		}
		logger.Info("import-v1 dry run complete", "accounts", len(accounts), "saves", len(plan))
		return nil
	}

	for _, a := range accounts {
		// Two touches preserve both timestamps exactly: the first insert
		// sets first_seen=last_seen=FirstSeen, the second call's ON CONFLICT
		// branch advances only last_seen to LastSeen.
		if err := dst.TouchAccount(ctx, a.Fingerprint, a.PublicKey, a.FirstSeen); err != nil {
			return fmt.Errorf("touch account %s: %w", truncateFingerprint(a.Fingerprint), err)
		}
		if a.LastSeen != a.FirstSeen {
			if err := dst.TouchAccount(ctx, a.Fingerprint, a.PublicKey, a.LastSeen); err != nil {
				return fmt.Errorf("touch account %s: %w", truncateFingerprint(a.Fingerprint), err)
			}
		}
	}

	imported := 0
	for _, d := range plan {
		if d.wasFiltered {
			logger.Info("v1 farm name denied by moderation, imported as anonymous",
				"fingerprint", truncateFingerprint(d.sv.Fingerprint), "slot", d.sv.Slot)
		}
		if err := dst.InsertSave(ctx, d.sv.Fingerprint, d.sv.Slot, d.sv.State, d.sv.StateVersion,
			d.sv.CreatedAt, d.sv.LastActive, d.state.Coins, d.name, d.locked); err != nil {
			return fmt.Errorf("insert save %s/%s: %w", truncateFingerprint(d.sv.Fingerprint), d.sv.Slot, err)
		}
		imported++
	}

	logger.Info("import-v1 complete", "accounts", len(accounts), "saves", imported)
	return nil
}

func openReadOnly(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	dsn := "file:" + url.PathEscape(path) + "?mode=ro&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func validateV1Schema(ctx context.Context, db *sql.DB) error {
	var v int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if v != v1SchemaVersion {
		return fmt.Errorf("unexpected v1 schema version %d, want %d (upgrade or downgrade v1 first)", v, v1SchemaVersion)
	}
	return nil
}

func readV1Accounts(ctx context.Context, db *sql.DB) ([]v1Account, error) {
	rows, err := db.QueryContext(ctx, `SELECT fingerprint, public_key, first_seen, last_seen FROM accounts`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []v1Account
	for rows.Next() {
		var a v1Account
		if err := rows.Scan(&a.Fingerprint, &a.PublicKey, &a.FirstSeen, &a.LastSeen); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func readV1Saves(ctx context.Context, db *sql.DB) ([]v1Save, error) {
	rows, err := db.QueryContext(ctx, `SELECT fingerprint, slot, created_at, last_active, state, state_version FROM saves`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []v1Save
	for rows.Next() {
		var s v1Save
		if err := rows.Scan(&s.Fingerprint, &s.Slot, &s.CreatedAt, &s.LastActive, &s.State, &s.StateVersion); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func truncateFingerprint(fp string) string {
	const keep = 14 // "SHA256:" + 7 chars is enough to spot in logs, not enough to dox a key
	if len(fp) <= keep {
		return fp
	}
	return fp[:keep] + "…"
}
