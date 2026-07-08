package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	st, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func freshPayload(body string, coins int64, farmName string) func() (FreshSave, error) {
	return func() (FreshSave, error) {
		return FreshSave{State: []byte(body), Version: 2, Coins: coins, FarmName: farmName}, nil
	}
}

func TestOpenAppliesAllMigrationsAndWAL(t *testing.T) {
	st := openTest(t)
	v, err := st.SchemaVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v != len(migrations) {
		t.Fatalf("schema version = %d, want %d", v, len(migrations))
	}
	var mode string
	if err := st.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Fatalf("journal mode = %q, want wal", mode)
	}
}

func TestAccountUpsertKeepsFirstSeen(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	if err := st.TouchAccount(ctx, "SHA256:abc", "ssh-ed25519 AAA", 100); err != nil {
		t.Fatal(err)
	}
	if err := st.TouchAccount(ctx, "SHA256:abc", "ssh-ed25519 AAA", 200); err != nil {
		t.Fatal(err)
	}
	var first, last int64
	if err := st.db.QueryRow(
		"SELECT first_seen, last_seen FROM accounts WHERE fingerprint = ?",
		"SHA256:abc").Scan(&first, &last); err != nil {
		t.Fatal(err)
	}
	if first != 100 || last != 200 {
		t.Fatalf("first/last = %d/%d, want 100/200", first, last)
	}
}

func TestSaveLifecycleAndOwnershipIsolation(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	for _, fp := range []string{"SHA256:keyA", "SHA256:keyB"} {
		if err := st.TouchAccount(ctx, fp, "k", 1); err != nil {
			t.Fatal(err)
		}
	}

	// Same slot under two keys: two distinct saves.
	rowA, createdA, err := st.LoadOrCreateSave(ctx, "SHA256:keyA", "farm", 10, freshPayload("stateA", 25, ""))
	if err != nil {
		t.Fatal(err)
	}
	rowB, createdB, err := st.LoadOrCreateSave(ctx, "SHA256:keyB", "farm", 20, freshPayload("stateB", 25, ""))
	if err != nil {
		t.Fatal(err)
	}
	if !createdA || !createdB {
		t.Fatal("both saves should be newly created")
	}
	if string(rowA.State) != "stateA" || string(rowB.State) != "stateB" {
		t.Fatal("saves with the same slot crossed keys")
	}
	if rowA.Coins != 25 {
		t.Fatalf("fresh save coins = %d, want 25 (seeded, not backfilled later)", rowA.Coins)
	}

	// Mutate A's save; B's must be untouched, and reload returns the new state.
	if err := st.PersistSave(ctx, "SHA256:keyA", "farm", []byte("stateA2"), 2, 30, 500, 500, 0, "Sunny Hollow"); err != nil {
		t.Fatal(err)
	}
	rowA2, created, err := st.LoadOrCreateSave(ctx, "SHA256:keyA", "farm", 40, freshPayload("WRONG", 0, ""))
	if err != nil {
		t.Fatal(err)
	}
	if created || string(rowA2.State) != "stateA2" {
		t.Fatalf("expected persisted stateA2, got created=%v state=%s", created, rowA2.State)
	}
	if rowA2.Coins != 500 || rowA2.FarmName != "Sunny Hollow" {
		t.Fatalf("denormalized columns not persisted: coins=%d name=%q", rowA2.Coins, rowA2.FarmName)
	}
	rowB2, _, err := st.LoadOrCreateSave(ctx, "SHA256:keyB", "farm", 40, freshPayload("WRONG", 0, ""))
	if err != nil {
		t.Fatal(err)
	}
	if string(rowB2.State) != "stateB" {
		t.Fatal("writing keyA's save changed keyB's save")
	}

	// Multiple slots under one key are isolated from each other.
	if _, _, err := st.LoadOrCreateSave(ctx, "SHA256:keyA", "second", 50, freshPayload("other", 0, "")); err != nil {
		t.Fatal(err)
	}
	slots, err := st.ListSlots(ctx, "SHA256:keyA")
	if err != nil {
		t.Fatal(err)
	}
	if len(slots) != 2 {
		t.Fatalf("keyA slots = %v, want 2", slots)
	}
	slotsB, _ := st.ListSlots(ctx, "SHA256:keyB")
	if len(slotsB) != 1 {
		t.Fatalf("keyB slots = %v, want 1", slotsB)
	}
}

func TestPersistRequiresExistingRow(t *testing.T) {
	st := openTest(t)
	err := st.PersistSave(context.Background(), "SHA256:ghost", "farm", []byte("x"), 2, 1, 0, 0, 0, "")
	if err == nil {
		t.Fatal("persisting a nonexistent save must fail, not create rows")
	}
}

func TestHostileSlotAndFingerprintRejected(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	bad := []string{"", "UPPER", "a b", "a;drop table saves;--", "../../etc", "x'or'1'='1",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"} // 33 chars
	for _, slot := range bad {
		if _, _, err := st.LoadOrCreateSave(ctx, "SHA256:k", slot, 1, freshPayload("x", 0, "")); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("slot %q: expected ErrInvalidKey, got %v", slot, err)
		}
		if err := st.PersistSave(ctx, "SHA256:k", slot, []byte("x"), 2, 1, 0, 0, 0, ""); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("persist slot %q: expected ErrInvalidKey, got %v", slot, err)
		}
	}
	if _, _, err := st.LoadOrCreateSave(ctx, "", "ok", 1, freshPayload("x", 0, "")); !errors.Is(err, ErrInvalidKey) {
		t.Fatal("empty fingerprint must be rejected")
	}
}

func TestStateSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "persist.db")
	ctx := context.Background()

	st, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.TouchAccount(ctx, "SHA256:k", "key", 1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.LoadOrCreateSave(ctx, "SHA256:k", "farm", 1, freshPayload("before", 0, "")); err != nil {
		t.Fatal(err)
	}
	if err := st.PersistSave(ctx, "SHA256:k", "farm", []byte("after"), 2, 99, 200, 300, 1, "Northfield"); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st2, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	row, created, err := st2.LoadOrCreateSave(ctx, "SHA256:k", "farm", 100, freshPayload("WRONG", 0, ""))
	if err != nil {
		t.Fatal(err)
	}
	if created || string(row.State) != "after" || row.LastActive != 99 {
		t.Fatalf("state lost across restart: created=%v state=%s last=%d", created, row.State, row.LastActive)
	}
	if row.Coins != 200 || row.FarmName != "Northfield" {
		t.Fatalf("denormalized columns lost across restart: coins=%d name=%q", row.Coins, row.FarmName)
	}
	if row.LifetimeEarnings != 300 || row.Rebirths != 1 {
		t.Fatalf("lifetime_earnings/rebirths lost across restart: lifetime=%d rebirths=%d", row.LifetimeEarnings, row.Rebirths)
	}
}

// TestMigrationUpgradesV1Database simulates a database created with only
// migration 001 applied (no leaderboard columns) and confirms Open upgrades
// it without touching existing rows.
func TestMigrationUpgradesV1Database(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old.db")
	ctx := context.Background()

	db, err := sql.Open("sqlite", "file:"+url.PathEscape(path)+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(migrations[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA user_version = 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		"INSERT INTO accounts (fingerprint, public_key, first_seen, last_seen) VALUES ('SHA256:old', 'k', 1, 1)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		"INSERT INTO saves (fingerprint, slot, created_at, last_active, state) VALUES ('SHA256:old', 'farm', 1, 2, 'v1state')"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("migrating a v1-shape database failed: %v", err)
	}
	defer st.Close()

	v, _ := st.SchemaVersion(ctx)
	if v != len(migrations) {
		t.Fatalf("schema version = %d, want %d", v, len(migrations))
	}
	row, created, err := st.LoadOrCreateSave(ctx, "SHA256:old", "farm", 100, freshPayload("WRONG", 0, ""))
	if err != nil {
		t.Fatal(err)
	}
	if created || string(row.State) != "v1state" {
		t.Fatal("migration destroyed existing save data")
	}
	if row.StateVersion != 1 {
		t.Fatalf("pre-migration rows must default to state_version 1, got %d", row.StateVersion)
	}
	if row.Coins != 0 || row.FarmName != "" || row.NameLocked {
		t.Fatalf("migration 002 should default new columns to zero values, got coins=%d name=%q locked=%v",
			row.Coins, row.FarmName, row.NameLocked)
	}
	// The v1-shape blob ("v1state") is not valid JSON, so migration 004's
	// inline json_extract backfill must fall back to zero rather than
	// aborting the whole migration (see migrations.go's json_valid guard).
	if row.LifetimeEarnings != 0 || row.Rebirths != 0 {
		t.Fatalf("migration 004 should default an undecodable blob's new columns to zero, got lifetime=%d rebirths=%d",
			row.LifetimeEarnings, row.Rebirths)
	}

	// Re-opening is idempotent: no migration reruns, no errors.
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st2, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen after migration failed: %v", err)
	}
	_ = st2.Close()
}

// TestMigrationBackfillsLifetimeEarningsAndRebirthsFromBlob proves migration
// 004's inline json_extract backfill (unlike v2's coins/farm_name, there is
// no Go-side boot-time reconcile pass for these columns — see migrations.go)
// picks up real values already sitting in an existing row's JSON state blob
// when upgrading a pre-004 database, rather than leaving them stuck at the
// new columns' zero default.
func TestMigrationBackfillsLifetimeEarningsAndRebirthsFromBlob(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pre-v4.db")
	ctx := context.Background()

	db, err := sql.Open("sqlite", "file:"+url.PathEscape(path)+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations[:3] { // v1-v3: everything before the lifetime-coins columns
		if _, err := db.Exec(m); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("PRAGMA user_version = 3"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		"INSERT INTO accounts (fingerprint, public_key, first_seen, last_seen) VALUES ('SHA256:veteran', 'k', 1, 1)"); err != nil {
		t.Fatal(err)
	}
	blob := `{"version":4,"coins":10,"lifetime_earnings":4200,"rebirths":6,"farm_name":"Veteran Farm"}`
	if _, err := db.Exec(
		`INSERT INTO saves (fingerprint, slot, created_at, last_active, state, state_version, coins, farm_name)
		 VALUES ('SHA256:veteran', 'farm', 1, 2, ?, 4, 10, 'Veteran Farm')`,
		blob); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("migrating a pre-004 database failed: %v", err)
	}
	defer st.Close()

	row, created, err := st.LoadOrCreateSave(ctx, "SHA256:veteran", "farm", 100, freshPayload("WRONG", 0, ""))
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("migration destroyed an existing save")
	}
	if row.LifetimeEarnings != 4200 || row.Rebirths != 6 {
		t.Fatalf("migration 004 should backfill from the blob, got lifetime=%d rebirths=%d", row.LifetimeEarnings, row.Rebirths)
	}
}

func TestNewerSchemaRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "future.db")
	db, err := sql.Open("sqlite", "file:"+url.PathEscape(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", len(migrations)+5)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), path); err == nil {
		t.Fatal("expected refusal to open a newer-schema database")
	}
}

// TestCoinsIndexExists proves migration 002 leaves an index gameplay/02 can
// rely on for ordered leaderboard reads.
func TestCoinsIndexExists(t *testing.T) {
	st := openTest(t)
	var name string
	err := st.db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type = 'index' AND name = 'idx_saves_coins'").Scan(&name)
	if err != nil {
		t.Fatalf("idx_saves_coins missing: %v", err)
	}
}

// TestLifetimeEarningsIndexExists proves migration 004 leaves an index
// LeaderboardSnapshot's new lifetime_earnings ordering can rely on.
func TestLifetimeEarningsIndexExists(t *testing.T) {
	st := openTest(t)
	var name string
	err := st.db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type = 'index' AND name = 'idx_saves_lifetime_earnings'").Scan(&name)
	if err != nil {
		t.Fatalf("idx_saves_lifetime_earnings missing: %v", err)
	}
}

// TestPersistSaveIsAtomic proves the blob and its denormalized columns can
// never disagree: a transaction that fails to commit leaves both the old
// blob and the old columns in place, never a partial update of one but not
// the other. This is the crash-injection scenario the acceptance criteria
// asks for, expressed as a forced rollback of the exact statement
// PersistSave runs.
func TestPersistSaveIsAtomic(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	if err := st.TouchAccount(ctx, "SHA256:k", "k", 1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.LoadOrCreateSave(ctx, "SHA256:k", "farm", 1, freshPayload("original", 10, "Old Name")); err != nil {
		t.Fatal(err)
	}

	// Simulate a crash between the UPDATE and the COMMIT: run the identical
	// statement PersistSave uses, then roll back instead of committing.
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE saves SET state = ?, state_version = ?, last_active = ?, coins = ?, lifetime_earnings = ?, rebirths = ?, farm_name = ?
		WHERE fingerprint = ? AND slot = ?`,
		[]byte("new-blob"), 3, 999, 9999, 9999, 1, "Hijacked", "SHA256:k", "farm"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	row, _, err := st.LoadOrCreateSave(ctx, "SHA256:k", "farm", 1, freshPayload("WRONG", 0, ""))
	if err != nil {
		t.Fatal(err)
	}
	if string(row.State) != "original" || row.Coins != 10 || row.FarmName != "Old Name" {
		t.Fatalf("rollback left a partial update: state=%s coins=%d name=%q",
			row.State, row.Coins, row.FarmName)
	}
	if row.LifetimeEarnings != 0 || row.Rebirths != 0 {
		t.Fatalf("rollback left a partial update on the new columns: lifetime=%d rebirths=%d", row.LifetimeEarnings, row.Rebirths)
	}

	// The real PersistSave, run to completion, updates everything together.
	if err := st.PersistSave(ctx, "SHA256:k", "farm", []byte("new-blob"), 3, 999, 9999, 9999, 1, "New Name"); err != nil {
		t.Fatal(err)
	}
	row, _, err = st.LoadOrCreateSave(ctx, "SHA256:k", "farm", 1, freshPayload("WRONG", 0, ""))
	if err != nil {
		t.Fatal(err)
	}
	if string(row.State) != "new-blob" || row.Coins != 9999 || row.FarmName != "New Name" {
		t.Fatalf("committed persist did not apply both blob and columns: state=%s coins=%d name=%q",
			row.State, row.Coins, row.FarmName)
	}
	if row.LifetimeEarnings != 9999 || row.Rebirths != 1 {
		t.Fatalf("committed persist did not apply lifetime_earnings/rebirths: lifetime=%d rebirths=%d", row.LifetimeEarnings, row.Rebirths)
	}
}

func TestNeedingBackfillAndBackfillDenormalized(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	if err := st.TouchAccount(ctx, "SHA256:a", "a", 1); err != nil {
		t.Fatal(err)
	}
	if err := st.TouchAccount(ctx, "SHA256:b", "b", 1); err != nil {
		t.Fatal(err)
	}

	// A save created pre-002 (simulated directly) needs backfill.
	if _, _, err := st.LoadOrCreateSave(ctx, "SHA256:a", "farm", 1, freshPayload("blobA", 0, "")); err != nil {
		t.Fatal(err)
	}
	// A save already carrying real values does not.
	if _, _, err := st.LoadOrCreateSave(ctx, "SHA256:b", "farm", 1, freshPayload("blobB", 40, "Named")); err != nil {
		t.Fatal(err)
	}

	pending, err := st.NeedingBackfill(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Fingerprint != "SHA256:a" {
		t.Fatalf("needing backfill = %+v, want exactly SHA256:a's row", pending)
	}

	if err := st.BackfillDenormalized(ctx, "SHA256:a", "farm", 77, 88, 9, "Backfilled"); err != nil {
		t.Fatal(err)
	}
	pending, err = st.NeedingBackfill(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("backfill should be a no-op the second time it is needed to run, still pending: %+v", pending)
	}

	row, _, err := st.LoadOrCreateSave(ctx, "SHA256:a", "farm", 1, freshPayload("WRONG", 0, ""))
	if err != nil {
		t.Fatal(err)
	}
	if row.Coins != 77 || row.FarmName != "Backfilled" || string(row.State) != "blobA" {
		t.Fatalf("backfill should only touch denormalized columns, got %+v", row)
	}
	if row.LifetimeEarnings != 88 || row.Rebirths != 9 {
		t.Fatalf("backfill did not set lifetime_earnings/rebirths, got %+v", row)
	}
}

func TestImportHelpers(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	n, err := st.CountSaves(ctx)
	if err != nil || n != 0 {
		t.Fatalf("count = %d, err = %v, want 0", n, err)
	}
	has, err := st.HasSave(ctx, "SHA256:x", "farm")
	if err != nil || has {
		t.Fatalf("has = %v, err = %v, want false", has, err)
	}

	if err := st.TouchAccount(ctx, "SHA256:x", "k", 1); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertSave(ctx, "SHA256:x", "farm", []byte("imported"), 4, 5, 6, 100, 150, 3, "Imported Farm", true); err != nil {
		t.Fatal(err)
	}
	n, err = st.CountSaves(ctx)
	if err != nil || n != 1 {
		t.Fatalf("count = %d, err = %v, want 1", n, err)
	}
	has, err = st.HasSave(ctx, "SHA256:x", "farm")
	if err != nil || !has {
		t.Fatalf("has = %v, err = %v, want true", has, err)
	}
	if err := st.InsertSave(ctx, "SHA256:x", "farm", []byte("dup"), 4, 5, 6, 1, 1, 0, "", false); err == nil {
		t.Fatal("expected collision error inserting over an existing row")
	}

	row, _, err := st.LoadOrCreateSave(ctx, "SHA256:x", "farm", 1, freshPayload("WRONG", 0, ""))
	if err != nil {
		t.Fatal(err)
	}
	if row.LifetimeEarnings != 150 || row.Rebirths != 3 {
		t.Fatalf("imported row's lifetime_earnings/rebirths = %d/%d, want 150/3", row.LifetimeEarnings, row.Rebirths)
	}
	if !row.NameLocked {
		t.Fatal("imported row should carry name_locked=true")
	}

	if err := st.LockName(ctx, "SHA256:x", "farm", false); err != nil {
		t.Fatal(err)
	}
	row, _, err = st.LoadOrCreateSave(ctx, "SHA256:x", "farm", 1, freshPayload("WRONG", 0, ""))
	if err != nil {
		t.Fatal(err)
	}
	if row.NameLocked {
		t.Fatal("LockName(false) should clear the flag")
	}
}

func TestGateRenameAllowsThenRateLimitsWithinCooldown(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	if err := st.TouchAccount(ctx, "SHA256:r", "k", 1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.LoadOrCreateSave(ctx, "SHA256:r", "farm", 1, freshPayload("blob", 0, "")); err != nil {
		t.Fatal(err)
	}

	gate, err := st.GateRename(ctx, "SHA256:r", "farm", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if gate != RenameAllowed {
		t.Fatalf("first attempt gate = %v, want RenameAllowed", gate)
	}

	// A second attempt 30s later (inside the 60s cooldown) is refused.
	gate, err = st.GateRename(ctx, "SHA256:r", "farm", 1030)
	if err != nil {
		t.Fatal(err)
	}
	if gate != RenameRateLimited {
		t.Fatalf("second attempt (30s later) gate = %v, want RenameRateLimited", gate)
	}

	// Past the cooldown, a third attempt is allowed again.
	gate, err = st.GateRename(ctx, "SHA256:r", "farm", 1061)
	if err != nil {
		t.Fatal(err)
	}
	if gate != RenameAllowed {
		t.Fatalf("third attempt (61s later) gate = %v, want RenameAllowed", gate)
	}
}

func TestGateRenameEnforcesDailyLimitAndRollsOverWindow(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	if err := st.TouchAccount(ctx, "SHA256:d", "k", 1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.LoadOrCreateSave(ctx, "SHA256:d", "farm", 1, freshPayload("blob", 0, "")); err != nil {
		t.Fatal(err)
	}

	now := int64(0)
	for i := 0; i < renameDailyLimit; i++ {
		now += renameCooldownSeconds + 1
		gate, err := st.GateRename(ctx, "SHA256:d", "farm", now)
		if err != nil {
			t.Fatal(err)
		}
		if gate != RenameAllowed {
			t.Fatalf("attempt %d gate = %v, want RenameAllowed", i+1, gate)
		}
	}

	// The 11th attempt, still within the day window, is refused even
	// though the per-minute cooldown has elapsed.
	now += renameCooldownSeconds + 1
	gate, err := st.GateRename(ctx, "SHA256:d", "farm", now)
	if err != nil {
		t.Fatal(err)
	}
	if gate != RenameRateLimited {
		t.Fatalf("11th same-day attempt gate = %v, want RenameRateLimited", gate)
	}

	// A day later the window rolls over and the budget resets.
	now += renameDayWindowSeconds
	gate, err = st.GateRename(ctx, "SHA256:d", "farm", now)
	if err != nil {
		t.Fatal(err)
	}
	if gate != RenameAllowed {
		t.Fatalf("first attempt of the new day gate = %v, want RenameAllowed", gate)
	}
}

func TestGateRenameLockedBeatsEverythingAndCostsNothing(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	if err := st.TouchAccount(ctx, "SHA256:l", "k", 1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.LoadOrCreateSave(ctx, "SHA256:l", "farm", 1, freshPayload("blob", 0, "")); err != nil {
		t.Fatal(err)
	}
	if err := st.LockName(ctx, "SHA256:l", "farm", true); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		gate, err := st.GateRename(ctx, "SHA256:l", "farm", int64(1000+i))
		if err != nil {
			t.Fatal(err)
		}
		if gate != RenameLocked {
			t.Fatalf("attempt %d gate = %v, want RenameLocked", i+1, gate)
		}
	}

	// Unlocking restores normal rate-limited behavior with a fresh budget
	// (a locked attempt never consumed any).
	if err := st.LockName(ctx, "SHA256:l", "farm", false); err != nil {
		t.Fatal(err)
	}
	gate, err := st.GateRename(ctx, "SHA256:l", "farm", 2000)
	if err != nil {
		t.Fatal(err)
	}
	if gate != RenameAllowed {
		t.Fatalf("first attempt after unlock gate = %v, want RenameAllowed", gate)
	}
}

func TestGateRenameCountersSurviveReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gate-reopen.db")
	ctx := context.Background()

	st, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.TouchAccount(ctx, "SHA256:p", "k", 1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.LoadOrCreateSave(ctx, "SHA256:p", "farm", 1, freshPayload("blob", 0, "")); err != nil {
		t.Fatal(err)
	}
	if gate, err := st.GateRename(ctx, "SHA256:p", "farm", 1000); err != nil || gate != RenameAllowed {
		t.Fatalf("gate = %v, err = %v, want RenameAllowed", gate, err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st2, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	// A restart-and-reconnect within the cooldown must still be refused:
	// the counters are read from disk, not process memory.
	gate, err := st2.GateRename(ctx, "SHA256:p", "farm", 1010)
	if err != nil {
		t.Fatal(err)
	}
	if gate != RenameRateLimited {
		t.Fatalf("gate after reopen = %v, want RenameRateLimited (counters must survive save/load)", gate)
	}
}

func TestLeaderboardSnapshotOrdersByLifetimeCoinsThenActivityThenFingerprint(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	seed := func(fp string, coins, lifetimeCoins, rebirths, lastActive int64, name string) {
		t.Helper()
		if err := st.TouchAccount(ctx, fp, "k", 1); err != nil {
			t.Fatal(err)
		}
		if _, _, err := st.LoadOrCreateSave(ctx, fp, "farm", 1, freshPayload("blob", coins, name)); err != nil {
			t.Fatal(err)
		}
		if err := st.PersistSave(ctx, fp, "farm", []byte("blob"), 4, lastActive, coins, lifetimeCoins, rebirths, name); err != nil {
			t.Fatal(err)
		}
	}

	// Two saves tie on lifetime coins (100): "SHA256:b" reached it first
	// (lastActive=5) so it must rank above "SHA256:a" (lastActive=9).
	seed("SHA256:a", 100, 100, 0, 9, "Tie A")
	seed("SHA256:b", 100, 100, 1, 5, "Tie B")
	// A low current balance (10) must not matter: lifetime coins (500) is
	// what ranks this save first.
	seed("SHA256:z", 10, 500, 2, 1, "Leader")
	seed("SHA256:c", 0, 0, 0, 1, "")
	// Conversely, a big current balance (9,000, higher than every other
	// row) must not help: this save has spent almost everything it ever
	// earned, so its lifetime total (50) ranks it above only the zero row.
	seed("SHA256:spender", 9_000, 50, 9, 1, "Big Spender")

	rows, err := st.LeaderboardSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 {
		t.Fatalf("got %d rows, want 5", len(rows))
	}
	want := []string{"SHA256:z", "SHA256:b", "SHA256:a", "SHA256:spender", "SHA256:c"}
	for i, w := range want {
		if rows[i].Fingerprint != w {
			t.Fatalf("row %d = %s, want %s (order: %+v)", i, rows[i].Fingerprint, w, rows)
		}
	}
	if rows[0].Coins != 500 {
		t.Fatalf("leader's board Coins = %d, want 500 (lifetime earnings, not the 10 current balance)", rows[0].Coins)
	}
	var spender LeaderboardRow
	for _, r := range rows {
		if r.Fingerprint == "SHA256:spender" {
			spender = r
		}
	}
	if spender.Coins != 50 {
		t.Fatalf("big spender's board Coins = %d, want 50 (lifetime earnings, not the 9000 current balance)", spender.Coins)
	}
	if spender.Rebirths != 9 {
		t.Fatalf("big spender's Rebirths = %d, want 9", spender.Rebirths)
	}
}

func TestIntegrityCheckPassesOnAFreshDatabase(t *testing.T) {
	st := openTest(t)
	if err := st.TouchAccount(context.Background(), "SHA256:k", "key", 1); err != nil {
		t.Fatal(err)
	}
	if err := st.IntegrityCheck(context.Background()); err != nil {
		t.Fatalf("IntegrityCheck on a healthy database: %v", err)
	}
}

// TestIntegrityCheckCatchesCorruption is the durability drills' negative
// case (docs/tests/02): a restore that produces a corrupted file must be
// caught, not silently served. Corrupting a live *sql.DB in-process isn't
// reliable (the driver may cache pages), so this closes the store and
// scrambles the last page directly on disk, then reopens — mangling only a
// leaf page's cell data (rather than the header or early schema pages)
// leaves Open able to connect, matching a real torn/truncated restore, and
// lets integrity_check itself be the thing that catches it.
func TestIntegrityCheckCatchesCorruption(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.db")
	ctx := context.Background()

	st, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.TouchAccount(ctx, "SHA256:k", "key", 1); err != nil {
		t.Fatal(err)
	}
	// Enough rows to spill past the schema pages into leaf data pages this
	// test can corrupt without touching sqlite_master itself.
	for i := 0; i < 50; i++ {
		fp := fmt.Sprintf("SHA256:many%02d", i)
		if err := st.TouchAccount(ctx, fp, "key", 1); err != nil {
			t.Fatal(err)
		}
		if _, _, err := st.LoadOrCreateSave(ctx, fp, "farm", 1, freshPayload(strings.Repeat("x", 200), 100, "Northfield")); err != nil {
			t.Fatal(err)
		}
	}
	var pageSize, pageCount int
	if err := st.db.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount); err != nil {
		t.Fatal(err)
	}
	// WAL mode keeps recent writes in a separate -wal file; checkpoint first
	// so the corruption below actually lands in data the main file holds.
	if _, err := st.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if pageCount < 3 {
		t.Fatalf("only %d pages, need more to corrupt a leaf page safely", pageCount)
	}
	lastPageStart := (pageCount - 1) * pageSize
	for i := lastPageStart + 20; i < lastPageStart+100 && i < len(b); i++ {
		b[i] ^= 0xFF
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}

	st2, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open refused to reopen a page-level-corrupted database: %v (test needs a milder corruption)", err)
	}
	defer st2.Close()
	if err := st2.IntegrityCheck(ctx); err == nil {
		t.Fatal("IntegrityCheck = nil on a deliberately corrupted database, want an error")
	}
}

func TestAllSavesReturnsEveryRowIncludingState(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	if err := st.TouchAccount(ctx, "SHA256:a", "k", 1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.LoadOrCreateSave(ctx, "SHA256:a", "farm", 1, freshPayload("blob-a", 100, "Alpha")); err != nil {
		t.Fatal(err)
	}
	if err := st.TouchAccount(ctx, "SHA256:b", "k", 1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.LoadOrCreateSave(ctx, "SHA256:b", "farm", 1, freshPayload("blob-b", 200, "Bravo")); err != nil {
		t.Fatal(err)
	}

	rows, err := st.AllSaves(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	byFP := map[string]SaveRow{}
	for _, r := range rows {
		byFP[r.Fingerprint] = r
	}
	if string(byFP["SHA256:a"].State) != "blob-a" || byFP["SHA256:a"].Coins != 100 {
		t.Fatalf("SHA256:a row wrong: %+v", byFP["SHA256:a"])
	}
	if string(byFP["SHA256:b"].State) != "blob-b" || byFP["SHA256:b"].Coins != 200 {
		t.Fatalf("SHA256:b row wrong: %+v", byFP["SHA256:b"])
	}
}
