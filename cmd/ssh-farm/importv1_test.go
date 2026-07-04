package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mynameis-nigel/ssh-farm/internal/content"
	"github.com/mynameis-nigel/ssh-farm/internal/sim"
	"github.com/mynameis-nigel/ssh-farm/internal/store"
)

// buildV1DB creates a SQLite file with v1 (ssh-idlefarmer)'s exact final
// schema — accounts + saves(with state_version), user_version=2 — and seeds
// it with the given accounts/saves, mirroring what a real v1 database looks
// like at cutover.
func buildV1DB(t *testing.T, path string, accounts []v1Account, saves []v1Save) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+url.PathEscape(path)+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`
		CREATE TABLE accounts (
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
		) WITHOUT ROWID;`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA user_version = 2"); err != nil {
		t.Fatal(err)
	}
	for _, a := range accounts {
		if _, err := db.Exec(`INSERT INTO accounts (fingerprint, public_key, first_seen, last_seen) VALUES (?, ?, ?, ?)`,
			a.Fingerprint, a.PublicKey, a.FirstSeen, a.LastSeen); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range saves {
		if _, err := db.Exec(`INSERT INTO saves (fingerprint, slot, created_at, last_active, state, state_version) VALUES (?, ?, ?, ?, ?, ?)`,
			s.Fingerprint, s.Slot, s.CreatedAt, s.LastActive, s.State, s.StateVersion); err != nil {
			t.Fatal(err)
		}
	}
}

// testContent reuses the minimal fixture under internal/sim/testdata —
// import-v1 itself never loads content (see doImportV1's doc comment); this
// is purely to build realistic encoded state blobs for the test fixtures.
func testContent(t *testing.T) *content.Content {
	t.Helper()
	c, err := content.Load("../../internal/sim/testdata")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func encodedState(t *testing.T, c *content.Content, name string, coins int64) []byte {
	t.Helper()
	st := sim.New(c, 1, 1000)
	if name != "" {
		if err := sim.SetFarmName(st, name); err != nil {
			t.Fatal(err)
		}
	}
	st.Coins = coins
	b, err := st.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 100}))
}

func TestImportV1HappyPath(t *testing.T) {
	c := testContent(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "v1.db")
	dst := filepath.Join(dir, "v2.db")

	buildV1DB(t, src,
		[]v1Account{{Fingerprint: "SHA256:a", PublicKey: "ka", FirstSeen: 10, LastSeen: 20}},
		[]v1Save{{Fingerprint: "SHA256:a", Slot: "farm", CreatedAt: 10, LastActive: 20,
			State: encodedState(t, c, "Old Mill", 500), StateVersion: 2}},
	)

	if err := doImportV1(context.Background(), discardLogger(), src, dst, false, false); err != nil {
		t.Fatalf("import failed: %v", err)
	}

	st, err := store.Open(context.Background(), dst)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	n, err := st.CountSaves(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("count = %d, err = %v, want 1", n, err)
	}
	has, err := st.HasSave(context.Background(), "SHA256:a", "farm")
	if err != nil || !has {
		t.Fatalf("has = %v, err = %v, want true", has, err)
	}
}

func TestImportV1FailsWhenSaveHasNoMatchingAccount(t *testing.T) {
	c := testContent(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "v1.db")
	dst := filepath.Join(dir, "v2.db")
	buildV1DB(t, src, nil, []v1Save{{Fingerprint: "SHA256:a", Slot: "farm", CreatedAt: 1, LastActive: 1,
		State: encodedState(t, c, "", 25), StateVersion: 2}})
	// Missing account row: FK constraint should surface as an import error,
	// not a silent partial write, when the account wasn't in the source.
	err := doImportV1(context.Background(), discardLogger(), src, dst, false, false)
	if err == nil {
		t.Fatal("expected an error importing a save with no matching account")
	}
}

func TestImportV1DryRunWritesNothing(t *testing.T) {
	c := testContent(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "v1.db")
	dst := filepath.Join(dir, "v2.db")
	buildV1DB(t, src,
		[]v1Account{{Fingerprint: "SHA256:a", PublicKey: "ka", FirstSeen: 1, LastSeen: 1}},
		[]v1Save{{Fingerprint: "SHA256:a", Slot: "farm", CreatedAt: 1, LastActive: 1,
			State: encodedState(t, c, "Dry Runner", 42), StateVersion: 2}},
	)

	if err := doImportV1(context.Background(), discardLogger(), src, dst, true, false); err != nil {
		t.Fatalf("dry run should not fail: %v", err)
	}
	if _, err := os.Stat(dst); err == nil {
		// store.Open creates the file even on a dry run since we open the
		// destination to count existing saves; assert it stayed empty
		// rather than asserting the file itself is absent.
		st, err := store.Open(context.Background(), dst)
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close()
		n, err := st.CountSaves(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("dry run wrote %d save(s), want 0", n)
		}
	}
}

func TestImportV1RefusesNonEmptyDestinationWithoutMerge(t *testing.T) {
	c := testContent(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "v1.db")
	dst := filepath.Join(dir, "v2.db")
	buildV1DB(t, src,
		[]v1Account{{Fingerprint: "SHA256:a", PublicKey: "ka", FirstSeen: 1, LastSeen: 1}},
		[]v1Save{{Fingerprint: "SHA256:a", Slot: "farm", CreatedAt: 1, LastActive: 1,
			State: encodedState(t, c, "", 25), StateVersion: 2}},
	)

	// Pre-populate the destination.
	st, err := store.Open(context.Background(), dst)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.TouchAccount(context.Background(), "SHA256:existing", "k", 1); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertSave(context.Background(), "SHA256:existing", "farm", []byte("{}"), 2, 1, 1, 0, "", false); err != nil {
		t.Fatal(err)
	}
	st.Close()

	if err := doImportV1(context.Background(), discardLogger(), src, dst, false, false); err == nil {
		t.Fatal("expected refusal to import into a non-empty store without --merge")
	}
	if err := doImportV1(context.Background(), discardLogger(), src, dst, false, true); err != nil {
		t.Fatalf("--merge should allow importing into a non-empty store: %v", err)
	}
}

func TestImportV1AbortsOnCollisionWithoutPartialWrites(t *testing.T) {
	c := testContent(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "v1.db")
	dst := filepath.Join(dir, "v2.db")
	buildV1DB(t, src,
		[]v1Account{
			{Fingerprint: "SHA256:new", PublicKey: "k1", FirstSeen: 1, LastSeen: 1},
			{Fingerprint: "SHA256:existing", PublicKey: "k2", FirstSeen: 1, LastSeen: 1},
		},
		[]v1Save{
			{Fingerprint: "SHA256:new", Slot: "farm", CreatedAt: 1, LastActive: 1, State: encodedState(t, c, "", 1), StateVersion: 2},
			{Fingerprint: "SHA256:existing", Slot: "farm", CreatedAt: 1, LastActive: 1, State: encodedState(t, c, "", 1), StateVersion: 2},
		},
	)

	st, err := store.Open(context.Background(), dst)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.TouchAccount(context.Background(), "SHA256:existing", "k2", 1); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertSave(context.Background(), "SHA256:existing", "farm", []byte("{}"), 2, 1, 1, 0, "", false); err != nil {
		t.Fatal(err)
	}
	st.Close()

	if err := doImportV1(context.Background(), discardLogger(), src, dst, false, true); err == nil {
		t.Fatal("expected collision abort")
	}

	st2, err := store.Open(context.Background(), dst)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	has, err := st2.HasSave(context.Background(), "SHA256:new", "farm")
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Fatal("collision abort must not partially write other, non-colliding saves")
	}
}

func TestImportV1AbortsOnUndecodableBlob(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "v1.db")
	dst := filepath.Join(dir, "v2.db")
	buildV1DB(t, src,
		[]v1Account{{Fingerprint: "SHA256:a", PublicKey: "ka", FirstSeen: 1, LastSeen: 1}},
		[]v1Save{{Fingerprint: "SHA256:a", Slot: "farm", CreatedAt: 1, LastActive: 1,
			State: []byte("not valid json"), StateVersion: 2}},
	)

	if err := doImportV1(context.Background(), discardLogger(), src, dst, false, false); err == nil {
		t.Fatal("expected abort on an undecodable state blob")
	}
	if _, err := os.Stat(dst); err == nil {
		st, err := store.Open(context.Background(), dst)
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close()
		n, _ := st.CountSaves(context.Background())
		if n != 0 {
			t.Fatalf("a decode failure must abort before any writes, but %d save(s) landed", n)
		}
	}
}

func TestImportV1LocksOverlongName(t *testing.T) {
	c := testContent(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "v1.db")
	dst := filepath.Join(dir, "v2.db")
	longName := strings.Repeat("x", 40)
	buildV1DB(t, src,
		[]v1Account{{Fingerprint: "SHA256:a", PublicKey: "ka", FirstSeen: 1, LastSeen: 1}},
		[]v1Save{{Fingerprint: "SHA256:a", Slot: "farm", CreatedAt: 1, LastActive: 1,
			State: encodedStateRawName(t, c, longName, 25), StateVersion: 2}},
	)

	if err := doImportV1(context.Background(), discardLogger(), src, dst, false, false); err != nil {
		t.Fatalf("import should succeed even with a name moderation denies: %v", err)
	}

	st, err := store.Open(context.Background(), dst)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	row, _, err := st.LoadOrCreateSave(context.Background(), "SHA256:a", "farm", 1, func() (store.FreshSave, error) {
		t.Fatal("save must already exist")
		return store.FreshSave{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !row.NameLocked || row.FarmName != "" {
		t.Fatalf("overlong v1 name should import locked and anonymized, got name=%q locked=%v", row.FarmName, row.NameLocked)
	}
}

// encodedStateRawName bypasses sim.SetFarmName's own length validation
// (which would reject the name before we could ever encode it) to simulate
// a v1 save that was written under looser rules, exercising import-v1's own
// moderation.Filter call independent of the sim layer's guardrail.
func encodedStateRawName(t *testing.T, c *content.Content, name string, coins int64) []byte {
	t.Helper()
	st := sim.New(c, 1, 1000)
	st.FarmName = name
	st.Coins = coins
	b, err := st.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return b
}
