//go:build farm_test_v1_db

package sim

import (
	"database/sql"
	"net/url"
	"os"
	"testing"

	_ "modernc.org/sqlite"
)

// TestDecodeEverythingV1DB is a manual pre-cutover hook: point FARM_TEST_V1_DB
// at a copy of the production v1 SQLite file and run:
//
//	go test -tags farm_test_v1_db ./internal/sim -run TestDecodeEverythingV1DB -v
func TestDecodeEverythingV1DB(t *testing.T) {
	path := os.Getenv("FARM_TEST_V1_DB")
	if path == "" {
		t.Skip("FARM_TEST_V1_DB not set")
	}
	db, err := sql.Open("sqlite", "file:"+url.PathEscape(path)+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	rows, err := db.Query(`SELECT fingerprint, slot, state FROM saves`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	n := 0
	for rows.Next() {
		var fp, slot string
		var blob []byte
		if err := rows.Scan(&fp, &slot, &blob); err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeState(blob); err != nil {
			t.Fatalf("decode %s/%s: %v", fp, slot, err)
		}
		n++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("decoded %d saves from %s", n, path)
}
