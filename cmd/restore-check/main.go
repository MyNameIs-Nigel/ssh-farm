// Command restore-check is the durability drills' verification step
// (docs/tests/02, scripts/restore-drill): given a restored ssh-farm SQLite
// file, it runs SQLite's own integrity_check, then decodes every save's
// state blob (proving the restore isn't just a well-formed file but
// actually-readable player data), and prints the latest last_active
// timestamp across all saves so the calling drill script can compute the
// observed RPO against its own recorded kill/restore timestamps.
//
// It never writes to the database — a read-only verification tool, safe to
// run against a live restore before deciding whether to serve it.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/mynameis-nigel/ssh-farm/internal/sim"
	"github.com/mynameis-nigel/ssh-farm/internal/store"
)

func main() {
	dbPath := flag.String("db", "", "path to the ssh-farm SQLite database to verify")
	flag.Parse()
	if *dbPath == "" {
		fmt.Fprintln(os.Stderr, "restore-check: -db is required")
		os.Exit(2)
	}

	if err := run(*dbPath); err != nil {
		fmt.Fprintf(os.Stderr, "restore-check: FAIL: %v\n", err)
		os.Exit(1)
	}
}

func run(dbPath string) error {
	ctx := context.Background()
	st, err := store.Open(ctx, dbPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", dbPath, err)
	}
	defer st.Close()

	if err := st.IntegrityCheck(ctx); err != nil {
		return fmt.Errorf("integrity check: %w", err)
	}
	fmt.Println("integrity: ok")

	rows, err := st.AllSaves(ctx)
	if err != nil {
		return fmt.Errorf("list saves: %w", err)
	}

	var latest int64
	for _, row := range rows {
		if _, err := sim.DecodeState(row.State); err != nil {
			return fmt.Errorf("decode save %s/%s: %w", row.Fingerprint, row.Slot, err)
		}
		if row.LastActive > latest {
			latest = row.LastActive
		}
	}

	fmt.Printf("saves: %d decoded ok\n", len(rows))
	fmt.Printf("latest_write: %d\n", latest)
	return nil
}
