package leaderboard

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mynameis-nigel/ssh-farm/internal/store"
)

// mutableSource is a Source whose backing rows can be swapped concurrently
// with Engine.Get calls, standing in for the store under real concurrent
// coin-flush writes (Store.PersistSave) racing leaderboard reads.
type mutableSource struct {
	mu   sync.Mutex
	rows []store.LeaderboardRow
}

func (m *mutableSource) LeaderboardSnapshot(ctx context.Context) ([]store.LeaderboardRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]store.LeaderboardRow(nil), m.rows...), nil
}

func (m *mutableSource) setRows(rows []store.LeaderboardRow) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows = rows
}

// concurrentClock is a now() func safe to call from many goroutines while
// one goroutine advances it, standing in for the wall clock in production.
type concurrentClock struct {
	unix int64 // atomic
}

func newConcurrentClock() *concurrentClock {
	c := &concurrentClock{}
	atomic.StoreInt64(&c.unix, 1_700_000_000)
	return c
}

func (c *concurrentClock) now() time.Time {
	return time.Unix(atomic.LoadInt64(&c.unix), 0)
}

func (c *concurrentClock) advance(seconds int64) {
	atomic.AddInt64(&c.unix, seconds)
}

// boardRankInvariants reports the first rank-consistency violation found in
// a single Board, or nil if it's internally coherent. This is a per-call
// check — distinct concurrent callers may legitimately observe different
// snapshots in time, but each individual Board must always be self
// consistent: competition ranking (1224) never places two rows with
// differing coins at the same rank in the wrong order, and Window never
// repeats a row already present in Top.
//
// Deliberately returns an error instead of using testing.T assertions: it
// is called from reader goroutines, and t.Fatal/Fatalf is documented as
// unsafe outside the goroutine running the test.
func boardRankInvariants(board Board) error {
	if err := checkRankOrder("Top", board.Top); err != nil {
		return err
	}
	if err := checkRankOrder("Window", board.Window); err != nil {
		return err
	}
	for _, w := range board.Window {
		for _, top := range board.Top {
			if w.Suffix == top.Suffix && w.DisplayName == top.DisplayName && w.Coins == top.Coins {
				return fmt.Errorf("window row %+v duplicates a top row", w)
			}
		}
	}
	if board.You != nil && board.You.Rank < 1 {
		return fmt.Errorf("You.Rank = %d, want >= 1", board.You.Rank)
	}
	return nil
}

// checkRankOrder verifies a rank-ordered slice never decreases in rank,
// never increases in coins, and that equal coins imply equal rank while
// unequal coins imply strictly increasing rank (competition ranking).
func checkRankOrder(label string, rows []Row) error {
	for i := 1; i < len(rows); i++ {
		prev, cur := rows[i-1], rows[i]
		if cur.Coins > prev.Coins {
			return fmt.Errorf("%s[%d..%d] coins increased (%d -> %d) despite descending sort", label, i-1, i, prev.Coins, cur.Coins)
		}
		if cur.Coins == prev.Coins && cur.Rank != prev.Rank {
			return fmt.Errorf("%s[%d..%d] equal coins (%d) but ranks differ (%d vs %d)", label, i-1, i, cur.Coins, prev.Rank, cur.Rank)
		}
		if cur.Coins < prev.Coins && cur.Rank <= prev.Rank {
			return fmt.Errorf("%s[%d..%d] lower coins (%d < %d) but rank did not increase (%d -> %d)", label, i-1, i, cur.Coins, prev.Coins, prev.Rank, cur.Rank)
		}
	}
	return nil
}

// TestConcurrentReadersAndWritersProduceConsistentBoards runs 50
// writer-goroutines mutating the backing rows (standing in for concurrent
// coin-flush autosaves) against 50 reader-goroutines calling Get, all while
// the clock advances past the TTL to force interleaved rebuilds. Run with
// -race; the assertion is that every single Board returned is internally
// rank-consistent, never a torn read across a rebuild.
func TestConcurrentReadersAndWritersProduceConsistentBoards(t *testing.T) {
	const (
		nWriters  = 50
		nReaders  = 50
		nFarms    = 40
		writeIter = 200
		readIter  = 200
	)

	src := &mutableSource{}
	clock := newConcurrentClock()
	initial := make([]store.LeaderboardRow, nFarms)
	for i := range initial {
		initial[i] = row(fmt.Sprintf("F%02d", i), int64(1000-i), clock.now().Unix(), fmt.Sprintf("FARM%02d", i))
	}
	src.setRows(initial)

	// TTL of 0 forces every Get to attempt a rebuild if the clock has moved
	// at all since the last one — the tightest possible interleaving of
	// reads against writes.
	eng := New(src, 0, 0, 1, clock.now)

	var wg sync.WaitGroup

	wg.Add(nWriters)
	for w := 0; w < nWriters; w++ {
		go func(w int) {
			defer wg.Done()
			for i := 0; i < writeIter; i++ {
				rows := make([]store.LeaderboardRow, nFarms)
				for f := 0; f < nFarms; f++ {
					coins := int64((i*7+w*13+f*31)%1000) + 1
					rows[f] = row(fmt.Sprintf("F%02d", f), coins, clock.now().Unix(), fmt.Sprintf("FARM%02d", f))
				}
				// Store.LeaderboardSnapshot's contract (see rebuild's doc
				// comment) guarantees rows sorted coins DESC; rebuild()
				// trusts that rather than re-sorting, so the fake source
				// must uphold it too.
				sort.Slice(rows, func(a, b int) bool { return rows[a].Coins > rows[b].Coins })
				src.setRows(rows)
				clock.advance(1)
			}
		}(w)
	}

	errs := make(chan error, nReaders*readIter)
	wg.Add(nReaders)
	for r := 0; r < nReaders; r++ {
		go func(r int) {
			defer wg.Done()
			you := ref(fmt.Sprintf("F%02d", r%nFarms))
			for i := 0; i < readIter; i++ {
				board, err := eng.Get(context.Background(), you)
				if err != nil {
					errs <- fmt.Errorf("Get error: %w", err)
					return
				}
				if err := boardRankInvariants(board); err != nil {
					errs <- err
					return
				}
			}
		}(r)
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}
