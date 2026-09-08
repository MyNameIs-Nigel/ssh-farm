package moderation

import "testing"

// TestFilterRetroactivelyLocksANameOnceTheDenylistGainsATerm proves the
// mechanism behind tests/02's "retroactivity" requirement: a name that was
// fine when a player set it (and is shown unmasked on the board) must come
// back locked the moment an operator adds a matching term to the denylist —
// with no code change and no re-processing of the stored name itself, since
// internal/leaderboard's render-time re-check (toRow) calls Filter fresh on
// every rebuild. That half of the story is covered by
// internal/leaderboard's TestDeniedNameIsMaskedAtRenderTimeNotEmpty (mask
// shape) and TestSaveFlushMovesBoardAfterRebuild (rebuilds happen on TTL
// expiry with no explicit invalidation) — this test exercises the actual
// "denylist gains a term" transition itself, which can only be done here:
// the active list is private to this package, so no other package can
// perform this swap. Since the migration to a host file it IS hot-swappable
// in production too, via Reload on SIGHUP — this test covers the same
// transition at the level below that.
func TestFilterRetroactivelyLocksANameOnceTheDenylistGainsATerm(t *testing.T) {
	old := loaded()
	t.Cleanup(func() { current.Store(old) })

	const name = "GIZMO VALE"

	empty, err := newDenylist(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	current.Store(empty)

	filtered, locked := Filter(name)
	if locked {
		t.Fatalf("Filter(%q) locked against an empty denylist, want it to pass", name)
	}
	if filtered == "" {
		t.Fatalf("Filter(%q) returned empty output for an accepted name", name)
	}

	// The denylist "gains the term" — a synthetic placeholder word, per the
	// package convention (see denylist_test.go's testDenylist), never a real
	// slur.
	updated, err := newDenylist([]term{{Word: "gizmo", Tier: "profanity", Match: "word"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	current.Store(updated)

	_, locked = Filter(name)
	if !locked {
		t.Fatalf("Filter(%q) still passes after the denylist gained a matching term", name)
	}
}
