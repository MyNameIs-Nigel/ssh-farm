package game

import (
	"errors"
	"testing"
	"time"
)

func TestDevActionsGateAndPersist(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "production", true: "dev"}[enabled], func(t *testing.T) {
			m, path := testManager(t, PolicyTakeover, time.Hour)
			WithDevMode(enabled)(m)
			id := ident("SHA256:dev", "farm")
			res := mustAttach(t, m, id, 1000)
			if res.Session.DevMode() != enabled {
				t.Fatal("wrong gate")
			}
			snap, _, err := res.Session.DevApply(1000, "coins", 1234567)
			if !enabled {
				if !errors.Is(err, ErrDevModeDisabled) {
					t.Fatalf("gate: %v", err)
				}
				snap, _, err = res.Session.Advance(1000)
				if err != nil || snap.State.Coins != m.content.Start.Coins {
					t.Fatal("production state changed")
				}
			} else if err != nil || snap.State.Coins != 1234567 {
				t.Fatalf("edit failed: %v", err)
			}
			res.Session.Detach()
			reopened := reopenManager(t, path, PolicyTakeover, time.Hour)
			next := mustAttach(t, reopened, id, 1000)
			defer next.Session.Detach()
			saved, _, err := next.Session.Advance(1000)
			if err != nil || saved.State.Coins != snap.State.Coins {
				t.Fatal("edit lost after reopening database")
			}
			if _, _, err := res.Session.DevApply(1000, "coins", 1); enabled && !errors.Is(err, ErrSessionClosed) {
				t.Fatalf("closed session: %v", err)
			}
		})
	}
}
