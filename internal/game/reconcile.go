package game

import (
	"context"
	"log/slog"

	"github.com/mynameis-nigel/ssh-farm/internal/sim"
	"github.com/mynameis-nigel/ssh-farm/internal/store"
)

// ReconcileDenormalized backfills the saves table's coins/lifetime_earnings/
// rebirths/farm_name columns for every row where coins/farm_name still look
// unpopulated — migration 002's zero defaults, or an import-v1 write that
// landed before this ran. It decodes each candidate blob with the ported
// sim and writes back only the denormalized columns, never the blob (the
// store never decodes blobs itself; that stays this package's job, since
// the state's shape is owned by internal/sim).
//
// Call this once at boot, after store.Open and before the server starts
// accepting sessions. It is idempotent: a save whose columns are already
// correct is not a candidate on the next boot, so this is a safe no-op on
// every boot after the first.
func ReconcileDenormalized(ctx context.Context, st *store.Store, logger *slog.Logger) error {
	pending, err := st.NeedingBackfill(ctx)
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}
	logger.Info("reconciling denormalized save columns", "count", len(pending))
	for _, row := range pending {
		state, err := sim.DecodeState(row.State)
		if err != nil {
			logger.Error("reconcile: undecodable save blob, skipping",
				"fingerprint", row.Fingerprint, "slot", row.Slot, "error", err)
			continue
		}
		if err := st.BackfillDenormalized(ctx, row.Fingerprint, row.Slot, state.Coins, state.LifetimeEarnings, state.Rebirths, state.FarmName); err != nil {
			logger.Error("reconcile: backfill failed",
				"fingerprint", row.Fingerprint, "slot", row.Slot, "error", err)
			continue
		}
	}
	return nil
}
