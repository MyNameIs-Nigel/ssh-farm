package store

import (
	"context"
	"database/sql"
	"net/url"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestContractColumnsPersistAndFeedLeaderboard(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	const fp = `SHA256:contractor`
	if err := st.TouchAccount(ctx, fp, `k`, 1); err != nil {
		t.Fatal(err)
	}
	row, _, err := st.LoadOrCreateSave(ctx, fp, `farm`, 2, func() (FreshSave, error) {
		return FreshSave{State: []byte(`{}`), Version: 4, ContractsCompleted: 1, LeaderboardNameStyle: `leaf`}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if row.ContractsCompleted != 1 || row.LeaderboardNameStyle != `leaf` {
		t.Fatalf(`fresh contract metadata = %d/%q, want 1/leaf`, row.ContractsCompleted, row.LeaderboardNameStyle)
	}

	if err := st.PersistSaveWithContracts(ctx, fp, `farm`, []byte(`{}`), 4, 3, 0, 0, 0, `Farm`, 3, `purple_wave`); err != nil {
		t.Fatal(err)
	}
	row, _, err = st.LoadOrCreateSave(ctx, fp, `farm`, 4, func() (FreshSave, error) { return FreshSave{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	if row.ContractsCompleted != 3 || row.LeaderboardNameStyle != `purple_wave` {
		t.Fatalf(`persisted contract metadata = %d/%q, want 3/purple_wave`, row.ContractsCompleted, row.LeaderboardNameStyle)
	}
	board, err := st.LeaderboardSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(board) != 1 || board[0].ContractsCompleted != 3 || board[0].LeaderboardNameStyle != `purple_wave` {
		t.Fatalf(`leaderboard metadata = %+v`, board)
	}
}

func TestContractMigrationBackfillsExistingJSONState(t *testing.T) {
	path := filepath.Join(t.TempDir(), `pre-contracts.db`)
	db, err := sql.Open(`sqlite`, `file:`+url.PathEscape(path)+`?_pragma=journal_mode(WAL)`)
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations[:5] {
		if _, err := db.Exec(migration); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`PRAGMA user_version = 5`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO accounts (fingerprint, public_key, first_seen, last_seen) VALUES ('SHA256:veteran', 'k', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	blob := "{\"contracts_completed\":2,\"leaderboard_name_style\":\"gold\"}"
	if _, err := db.Exec(`INSERT INTO saves (fingerprint, slot, created_at, last_active, state, state_version) VALUES ('SHA256:veteran', 'farm', 1, 1, ?, 4)`, blob); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	row, _, err := st.LoadOrCreateSave(context.Background(), `SHA256:veteran`, `farm`, 2, func() (FreshSave, error) { return FreshSave{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	if row.ContractsCompleted != 2 || row.LeaderboardNameStyle != `gold` {
		t.Fatalf(`migration backfill = %d/%q, want 2/gold`, row.ContractsCompleted, row.LeaderboardNameStyle)
	}
}
