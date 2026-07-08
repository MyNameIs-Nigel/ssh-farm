package game

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/mynameis-nigel/ssh-farm/internal/content"
	applog "github.com/mynameis-nigel/ssh-farm/internal/log"
	"github.com/mynameis-nigel/ssh-farm/internal/sim"
	"github.com/mynameis-nigel/ssh-farm/internal/store"
)

func TestReconcileDenormalizedBackfillsAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "reconcile.db")
	st, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	c, err := content.Load("../sim/testdata")
	if err != nil {
		t.Fatal(err)
	}
	logger := applog.New("error", "text")

	// Build a real state with non-default coins/name/lifetime earnings/
	// rebirths, but insert it with zeroed denormalized columns — simulating
	// a migration-002 upgrade or a pre-reconcile import-v1 write.
	state := sim.New(c, 1, 1000)
	if err := sim.SetFarmName(state, "Reconciled Acres"); err != nil {
		t.Fatal(err)
	}
	state.Coins = 555
	state.LifetimeEarnings = 4321
	state.Rebirths = 2
	payload, err := state.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.TouchAccount(ctx, "SHA256:reconcile", "k", 1); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertSave(ctx, "SHA256:reconcile", "farm", payload, state.Version, 1, 1, 0, 0, 0, "", false); err != nil {
		t.Fatal(err)
	}

	pending, err := st.NeedingBackfill(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 row needing backfill, got %d", len(pending))
	}

	if err := ReconcileDenormalized(ctx, st, logger); err != nil {
		t.Fatal(err)
	}

	row, _, err := st.LoadOrCreateSave(ctx, "SHA256:reconcile", "farm", 1, func() (store.FreshSave, error) {
		t.Fatal("save must already exist")
		return store.FreshSave{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if row.Coins != 555 || row.FarmName != "Reconciled Acres" {
		t.Fatalf("reconcile did not backfill correctly: coins=%d name=%q", row.Coins, row.FarmName)
	}
	if row.LifetimeEarnings != 4321 || row.Rebirths != 2 {
		t.Fatalf("reconcile did not backfill lifetime_earnings/rebirths: lifetime=%d rebirths=%d", row.LifetimeEarnings, row.Rebirths)
	}

	// Idempotent: nothing left pending, and a second run is a clean no-op.
	pending, err = st.NeedingBackfill(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("still pending after reconcile: %+v", pending)
	}
	if err := ReconcileDenormalized(ctx, st, logger); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileDenormalizedSkipsUndecodableBlobsWithoutFailing(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "reconcile-bad.db")
	st, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	logger := applog.New("error", "text")

	if err := st.TouchAccount(ctx, "SHA256:garbage", "k", 1); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertSave(ctx, "SHA256:garbage", "farm", []byte("not json"), 2, 1, 1, 0, 0, 0, "", false); err != nil {
		t.Fatal(err)
	}

	if err := ReconcileDenormalized(ctx, st, logger); err != nil {
		t.Fatalf("a single undecodable blob must not fail the whole reconcile pass: %v", err)
	}
}
