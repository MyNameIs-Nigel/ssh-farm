package leaderboard

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/mynameis-nigel/ssh-farm/internal/store"
)

// naiveBoard independently recomputes what Get should answer for a random
// population, by brute force rather than reusing any of engine.go's logic —
// the point is a second, deliberately naive implementation to check the
// real one against, not a faster path.
type naiveBoard struct {
	total int
	rank  map[string]int // fingerprint -> competition rank, filtered population only
	order []string       // fingerprints in filtered, sorted order
}

func naiveCompute(rows []store.LeaderboardRow, activityWindow time.Duration, minCoins int64, now int64) naiveBoard {
	var cutoff int64
	filterActivity := activityWindow > 0
	if filterActivity {
		cutoff = now - int64(activityWindow/time.Second)
	}

	var filtered []store.LeaderboardRow
	for _, r := range rows {
		if r.Coins < minCoins {
			continue
		}
		if filterActivity && r.UpdatedAt <= cutoff {
			continue
		}
		filtered = append(filtered, r)
	}
	// Independent sort by the same documented order (coins DESC, last_active
	// ASC, fingerprint ASC) rather than trusting input order, since this is
	// meant to be a from-scratch recount.
	sort.Slice(filtered, func(a, b int) bool {
		if filtered[a].Coins != filtered[b].Coins {
			return filtered[a].Coins > filtered[b].Coins
		}
		if filtered[a].UpdatedAt != filtered[b].UpdatedAt {
			return filtered[a].UpdatedAt < filtered[b].UpdatedAt
		}
		return filtered[a].Fingerprint < filtered[b].Fingerprint
	})

	nb := naiveBoard{total: len(filtered), rank: map[string]int{}}
	rank := 1
	for i, r := range filtered {
		if i > 0 && r.Coins < filtered[i-1].Coins {
			rank = i + 1
		}
		nb.rank[r.Fingerprint] = rank
		nb.order = append(nb.order, r.Fingerprint)
	}
	return nb
}

// TestLeaderboardPropertyAgainstNaiveRecount builds many random populations
// (0-5000 farms, random coins with deliberate duplicates, random ages
// relative to the activity window) and checks the engine's answer against
// naiveCompute's brute-force recount: competition ranking, You.Rank
// agreement, Total agreement, and a contiguous, top-deduped Window.
func TestLeaderboardPropertyAgainstNaiveRecount(t *testing.T) {
	seed := time.Now().UnixNano()
	t.Logf("property test seed: %d (reproduce by hardcoding this in rand.New(rand.NewSource(...)))", seed)
	rng := rand.New(rand.NewSource(seed))

	const trials = 200
	const day = 24 * 60 * 60
	const activityWindow = 90 * day * time.Second
	const minCoins = int64(1)
	now := int64(1_700_000_000)

	for trial := 0; trial < trials; trial++ {
		n := rng.Intn(120) // full 5000-row sweep is done in the size-tiered pass below
		rows := randomPopulation(rng, n, now)
		clock := &fakeClock{t: time.Unix(now, 0)}
		src := &fakeSource{rows: append([]store.LeaderboardRow(nil), rows...)}
		eng := New(src, time.Minute, activityWindow, minCoins, clock.now)

		nb := naiveCompute(rows, activityWindow, minCoins, now)

		you := SaveRef{}
		if n > 0 {
			you = ref(rows[rng.Intn(n)].Fingerprint)
		}

		board, err := eng.Get(context.Background(), you)
		if err != nil {
			t.Fatalf("trial %d (seed %d): Get error: %v", trial, seed, err)
		}
		if err := checkAgainstNaive(board, nb, you); err != nil {
			t.Fatalf("trial %d (seed %d, n=%d): %v", trial, seed, n, err)
		}
	}

	// A handful of large-population trials (up to 5000 rows) — kept separate
	// and few in number since each is O(n log n), to keep the suite fast.
	for _, n := range []int{500, 2000, 5000} {
		rows := randomPopulation(rng, n, now)
		clock := &fakeClock{t: time.Unix(now, 0)}
		src := &fakeSource{rows: append([]store.LeaderboardRow(nil), rows...)}
		eng := New(src, time.Minute, activityWindow, minCoins, clock.now)
		nb := naiveCompute(rows, activityWindow, minCoins, now)
		you := ref(rows[rng.Intn(n)].Fingerprint)

		board, err := eng.Get(context.Background(), you)
		if err != nil {
			t.Fatalf("large trial n=%d (seed %d): Get error: %v", n, seed, err)
		}
		if err := checkAgainstNaive(board, nb, you); err != nil {
			t.Fatalf("large trial n=%d (seed %d): %v", n, seed, err)
		}
	}
}

func randomPopulation(rng *rand.Rand, n int, now int64) []store.LeaderboardRow {
	const day = 24 * 60 * 60
	rows := make([]store.LeaderboardRow, n)
	for i := 0; i < n; i++ {
		coins := int64(rng.Intn(20)) // small range deliberately forces frequent ties
		ageDays := rng.Intn(200)     // spans both sides of the 90-day activity window
		rows[i] = row(fmt.Sprintf("FP%04d", i), coins, now-int64(ageDays*day), fmt.Sprintf("FARM%04d", i))
	}
	// Rows must arrive pre-sorted per Store.LeaderboardSnapshot's contract
	// (see engine.go rebuild's doc comment) — rebuild() trusts, not re-sorts.
	sort.Slice(rows, func(a, b int) bool {
		if rows[a].Coins != rows[b].Coins {
			return rows[a].Coins > rows[b].Coins
		}
		if rows[a].UpdatedAt != rows[b].UpdatedAt {
			return rows[a].UpdatedAt < rows[b].UpdatedAt
		}
		return rows[a].Fingerprint < rows[b].Fingerprint
	})
	return rows
}

func checkAgainstNaive(board Board, nb naiveBoard, you SaveRef) error {
	if board.Total != nb.total {
		return fmt.Errorf("Total = %d, want %d", board.Total, nb.total)
	}
	if err := boardRankInvariants(board); err != nil {
		return err
	}

	wantRank, wasRanked := nb.rank[you.Fingerprint]
	if you.Fingerprint == "" || !wasRanked {
		if board.You != nil {
			return fmt.Errorf("You = %+v, want nil (unranked/no viewer)", board.You)
		}
	} else {
		if board.You == nil {
			return fmt.Errorf("You = nil, want rank %d", wantRank)
		}
		if board.You.Rank != wantRank {
			return fmt.Errorf("You.Rank = %d, want %d (naive recount)", board.You.Rank, wantRank)
		}
	}

	// Window must be contiguous in the naive order and dedup'd against Top.
	if len(board.Window) > 0 {
		if board.You == nil || board.You.Rank <= topSize {
			return fmt.Errorf("Window non-empty (%d rows) but You is nil or within Top", len(board.Window))
		}
		youIdx := -1
		for i, fp := range nb.order {
			if fp == you.Fingerprint {
				youIdx = i
				break
			}
		}
		if youIdx == -1 {
			return fmt.Errorf("You.Fingerprint %q not found in naive order despite ranked", you.Fingerprint)
		}
		lo, hi := youIdx-windowRadius, youIdx+windowRadius
		if lo < topSize {
			lo = topSize
		}
		if hi > len(nb.order)-1 {
			hi = len(nb.order) - 1
		}
		wantLen := hi - lo + 1
		if wantLen < 0 {
			wantLen = 0
		}
		if len(board.Window) != wantLen {
			return fmt.Errorf("len(Window) = %d, want %d (naive contiguous window [%d,%d])", len(board.Window), wantLen, lo, hi)
		}
	}
	return nil
}
