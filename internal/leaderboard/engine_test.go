package leaderboard

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mynameis-nigel/ssh-farm/internal/store"
)

// fakeSource is a canned, call-counting Source for tests that don't need a
// real database — everything except the one integration test below.
type fakeSource struct {
	mu    sync.Mutex
	rows  []store.LeaderboardRow
	calls int
	delay time.Duration
}

func (f *fakeSource) LeaderboardSnapshot(ctx context.Context) ([]store.LeaderboardRow, error) {
	f.mu.Lock()
	f.calls++
	rows := append([]store.LeaderboardRow(nil), f.rows...)
	delay := f.delay
	f.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	return rows, nil
}

func (f *fakeSource) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// fakeClock gives tests explicit control over TTL expiry.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{t: time.Unix(1_700_000_000, 0)} }

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func row(fp string, coins, updatedAt int64, name string) store.LeaderboardRow {
	return store.LeaderboardRow{Fingerprint: fp, Slot: "farm", Coins: coins, FarmName: name, UpdatedAt: updatedAt}
}

// ref is shorthand for a SaveRef into the default "farm" slot used by row().
func ref(fp string) SaveRef { return SaveRef{Fingerprint: fp, Slot: "farm"} }

func TestRankMathTable(t *testing.T) {
	now := newFakeClock().now().Unix()

	tests := []struct {
		name         string
		rows         []store.LeaderboardRow
		you          SaveRef
		wantTotal    int
		wantTopRanks []int // Top[i].Rank for each i
		wantYouNil   bool
		wantYouRank  int
		wantWindow   []string // fingerprints, in order
	}{
		{
			name: "unique values",
			rows: []store.LeaderboardRow{
				row("A", 500, now, "A"), row("B", 400, now, "B"), row("C", 300, now, "C"),
				row("D", 200, now, "D"), row("E", 100, now, "E"),
			},
			you:          ref("A"),
			wantTotal:    5,
			wantTopRanks: []int{1, 2, 3, 4, 5},
			wantYouRank:  1,
		},
		{
			name: "ties use competition ranking (1224)",
			rows: []store.LeaderboardRow{
				row("A", 100, now, "A"), row("B", 100, now, "B"), row("C", 90, now, "C"),
				row("D", 80, now, "D"), row("E", 80, now, "E"), row("F", 80, now, "F"),
				row("G", 70, now, "G"),
			},
			you:          ref("C"),
			wantTotal:    7,
			wantTopRanks: []int{1, 1, 3, 4, 4, 4, 7},
			wantYouRank:  3,
		},
		{
			name: "you at top",
			rows: []store.LeaderboardRow{
				row("A", 500, now, "A"), row("B", 400, now, "B"),
			},
			you:          ref("A"),
			wantTotal:    2,
			wantTopRanks: []int{1, 2},
			wantYouRank:  1,
		},
		{
			name: "you unranked below the coin floor",
			rows: []store.LeaderboardRow{
				row("A", 500, now, "A"), row("B", 400, now, "B"), row("Z", 0, now, "Z"),
			},
			you:          ref("Z"),
			wantTotal:    2, // the floor drops Z entirely, not just from You
			wantYouNil:   true,
			wantTopRanks: []int{1, 2},
		},
		{
			name:       "empty board",
			rows:       nil,
			you:        ref("Nobody"),
			wantTotal:  0,
			wantYouNil: true,
		},
		{
			name: "exactly 10 farms, you last, no window needed",
			rows: []store.LeaderboardRow{
				row("A", 100, now, "A"), row("B", 90, now, "B"), row("C", 80, now, "C"),
				row("D", 70, now, "D"), row("E", 60, now, "E"), row("F", 50, now, "F"),
				row("G", 40, now, "G"), row("H", 30, now, "H"), row("I", 20, now, "I"),
				row("J", 10, now, "J"),
			},
			you:          ref("J"),
			wantTotal:    10,
			wantTopRanks: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			wantYouRank:  10,
		},
		{
			// 9 distinct leaders (ranks 1-9), then a tie for rank 10 that
			// spans index 9 and index 10. You are the *second* half of
			// that tie (index 10, outside Top's first 10 rows) but your
			// Rank is still 10, not 11 - per spec, Window only appears
			// when Rank > 10, so you get no window despite sitting just
			// past Top. Board.You still carries your correct rank.
			name: "you just past Top but tied at rank 10: no window",
			rows: []store.LeaderboardRow{
				row("A", 110, now, "A"), row("B", 109, now, "B"), row("C", 108, now, "C"),
				row("D", 107, now, "D"), row("E", 106, now, "E"), row("F", 105, now, "F"),
				row("G", 104, now, "G"), row("H", 103, now, "H"), row("I", 102, now, "I"),
				row("J", 101, now, "J"), row("K", 101, now, "K"),
			},
			you:          ref("K"),
			wantTotal:    11,
			wantTopRanks: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			wantYouRank:  10,
			wantWindow:   nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := &fakeSource{rows: tc.rows}
			clock := newFakeClock()
			eng := New(src, time.Minute, 0, 1, clock.now)

			board, err := eng.Get(context.Background(), tc.you)
			if err != nil {
				t.Fatal(err)
			}
			if board.Total != tc.wantTotal {
				t.Fatalf("Total = %d, want %d", board.Total, tc.wantTotal)
			}
			if len(board.Top) != len(tc.wantTopRanks) {
				t.Fatalf("len(Top) = %d, want %d", len(board.Top), len(tc.wantTopRanks))
			}
			for i, want := range tc.wantTopRanks {
				if board.Top[i].Rank != want {
					t.Fatalf("Top[%d].Rank = %d, want %d", i, board.Top[i].Rank, want)
				}
			}
			if tc.wantYouNil {
				if board.You != nil {
					t.Fatalf("You = %+v, want nil", board.You)
				}
				return
			}
			if board.You == nil {
				t.Fatal("You = nil, want a row")
			}
			if !board.You.IsYou {
				t.Fatal("You.IsYou = false, want true")
			}
			if board.You.Rank != tc.wantYouRank {
				t.Fatalf("You.Rank = %d, want %d", board.You.Rank, tc.wantYouRank)
			}
			gotWindow := namesOf(board.Window)
			if !equalStrings(gotWindow, tc.wantWindow) {
				t.Fatalf("Window names = %v, want %v", gotWindow, tc.wantWindow)
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestWindowAdjacencyAtBoundariesAndNoDuplicateOfTop(t *testing.T) {
	now := newFakeClock().now().Unix()
	// 15 distinct farms, ranks 1-15 by name AAA..OOO (three letters: a
	// single repeated letter would fail Validate's 3-char minimum).
	letters := "ABCDEFGHIJKLMNO"
	threeLetter := func(b byte) string { return strings.Repeat(string(b), 3) }
	var rows []store.LeaderboardRow
	for i := 0; i < len(letters); i++ {
		fp := string(letters[i])
		rows = append(rows, row(fp, int64(150-i), now, threeLetter(letters[i])))
	}
	src := &fakeSource{rows: rows}
	clock := newFakeClock()
	eng := New(src, time.Minute, 0, 1, clock.now)

	// Rank 11 (index 10, "K"): immediately adjacent to Top's last entry
	// (rank 10, "J") - window must start right after it, no gap, no dup.
	board, err := eng.Get(context.Background(), ref("K"))
	if err != nil {
		t.Fatal(err)
	}
	wantWindow := []string{"KKK", "LLL", "MMM", "NNN"} // ranks 11-14 (radius 3 from index10, clipped at Top on the low side)
	gotWindow := namesOf(board.Window)
	if !equalStrings(gotWindow, wantWindow) {
		t.Fatalf("rank-11 window = %v, want %v", gotWindow, wantWindow)
	}
	for _, w := range board.Window {
		for _, top := range board.Top {
			if w.DisplayName == top.DisplayName {
				t.Fatalf("window row %q duplicates a Top row", w.DisplayName)
			}
		}
	}

	// Rank 12 (index 11, "L"): one further out.
	board, err = eng.Get(context.Background(), ref("L"))
	if err != nil {
		t.Fatal(err)
	}
	wantWindow = []string{"KKK", "LLL", "MMM", "NNN", "OOO"} // index 8..14 clipped to >=10 on the low side -> 10..14
	gotWindow = namesOf(board.Window)
	if !equalStrings(gotWindow, wantWindow) {
		t.Fatalf("rank-12 window = %v, want %v", gotWindow, wantWindow)
	}
}

func namesOf(rows []Row) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.DisplayName
	}
	return out
}

func TestActivityWindowExcludesStaleFarmsFromListAndTotal(t *testing.T) {
	clock := newFakeClock()
	now := clock.now().Unix()
	const day = 24 * 60 * 60
	rows := []store.LeaderboardRow{
		row("Fresh", 500, now, "Fresh"),
		row("Stale", 400, now-100*day, "Stale"), // outside a 90-day window
	}
	src := &fakeSource{rows: rows}
	eng := New(src, time.Minute, 90*day*time.Second, 1, clock.now)

	board, err := eng.Get(context.Background(), ref("Stale"))
	if err != nil {
		t.Fatal(err)
	}
	if board.Total != 1 {
		t.Fatalf("Total = %d, want 1 (stale farm excluded)", board.Total)
	}
	if len(board.Top) != 1 || board.Top[0].DisplayName != "FRESH" {
		t.Fatalf("Top = %+v, want only Fresh", board.Top)
	}
	if board.You != nil {
		t.Fatal("You = non-nil for a stale, excluded farm")
	}

	// Disabling the window (<=0) must stop filtering entirely.
	eng2 := New(src, time.Minute, 0, 1, clock.now)
	board2, err := eng2.Get(context.Background(), ref("Stale"))
	if err != nil {
		t.Fatal(err)
	}
	if board2.Total != 2 {
		t.Fatalf("Total with disabled window = %d, want 2", board2.Total)
	}
}

func TestCacheServesOneDBPassForConcurrentGetsDuringRebuild(t *testing.T) {
	src := &fakeSource{
		rows:  []store.LeaderboardRow{row("A", 100, newFakeClock().now().Unix(), "A")},
		delay: 50 * time.Millisecond,
	}
	clock := newFakeClock()
	eng := New(src, time.Hour, 0, 1, clock.now)

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			_, err := eng.Get(context.Background(), ref("A"))
			errs[i] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("Get[%d] error: %v", i, err)
		}
	}
	if got := src.callCount(); got != 1 {
		t.Fatalf("LeaderboardSnapshot called %d times, want 1", got)
	}
}

func TestCacheHonorsTTLWithInjectedClock(t *testing.T) {
	src := &fakeSource{rows: []store.LeaderboardRow{row("A", 100, newFakeClock().now().Unix(), "A")}}
	clock := newFakeClock()
	eng := New(src, 10*time.Second, 0, 1, clock.now)

	if _, err := eng.Get(context.Background(), ref("A")); err != nil {
		t.Fatal(err)
	}
	if got := src.callCount(); got != 1 {
		t.Fatalf("calls after first Get = %d, want 1", got)
	}

	clock.advance(5 * time.Second)
	if _, err := eng.Get(context.Background(), ref("A")); err != nil {
		t.Fatal(err)
	}
	if got := src.callCount(); got != 1 {
		t.Fatalf("calls within TTL = %d, want 1 (cache hit)", got)
	}

	clock.advance(6 * time.Second) // total 11s, past the 10s TTL
	if _, err := eng.Get(context.Background(), ref("A")); err != nil {
		t.Fatal(err)
	}
	if got := src.callCount(); got != 2 {
		t.Fatalf("calls past TTL = %d, want 2 (rebuilt)", got)
	}
}

func TestDeniedNameIsMaskedAtRenderTimeNotEmpty(t *testing.T) {
	// "moderation" content is loaded from the real embedded denylist, so
	// this test only asserts the *shape* of the masking (a non-empty,
	// generated substitute distinct from the raw stored value) rather
	// than depending on specific denylist terms - see
	// internal/moderation's own tests for content-check coverage.
	//
	// A name containing null bytes / control garbage fails Validate's
	// charset rule and is therefore always "locked" regardless of the
	// denylist's contents, which keeps this test independent of the data
	// file.
	const invalidRaw = "bad\x00name"
	clock := newFakeClock()
	src := &fakeSource{rows: []store.LeaderboardRow{row("A", 100, clock.now().Unix(), invalidRaw)}}
	eng := New(src, time.Minute, 0, 1, clock.now)

	board, err := eng.Get(context.Background(), ref("A"))
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Top) != 1 {
		t.Fatalf("Top len = %d, want 1", len(board.Top))
	}
	got := board.Top[0].DisplayName
	if got == "" || got == invalidRaw {
		t.Fatalf("DisplayName = %q, want a non-empty generated substitute", got)
	}
}

func TestNeverRenamedFarmHasEmptyDisplayName(t *testing.T) {
	clock := newFakeClock()
	src := &fakeSource{rows: []store.LeaderboardRow{row("A", 100, clock.now().Unix(), "")}}
	eng := New(src, time.Minute, 0, 1, clock.now)

	board, err := eng.Get(context.Background(), ref("A"))
	if err != nil {
		t.Fatal(err)
	}
	if got := board.Top[0].DisplayName; got != "" {
		t.Fatalf("DisplayName = %q, want empty (never renamed -> UI fallback)", got)
	}
}

// TestSaveFlushMovesBoardAfterRebuild is the integration test called for by
// gameplay/02's acceptance criteria: it writes through the exact same
// store.Store.PersistSave call the game actor's autosave/detach flush uses
// (see internal/game/actor.go), then confirms the engine's next rebuild
// (after its TTL, on the injected clock) reflects the change.
func TestSaveFlushMovesBoardAfterRebuild(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	seed := func(fp string, coins int64, name string) {
		t.Helper()
		if err := st.TouchAccount(ctx, fp, "k", 1); err != nil {
			t.Fatal(err)
		}
		if _, _, err := st.LoadOrCreateSave(ctx, fp, "farm", 1, func() (store.FreshSave, error) {
			return store.FreshSave{State: []byte("blob"), Version: 2, Coins: coins, FarmName: name}, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	seed("SHA256:a", 100, "Alpha")
	seed("SHA256:b", 500, "Bravo")

	clock := newFakeClock()
	eng := New(st, time.Minute, 0, 1, clock.now)

	board, err := eng.Get(ctx, ref("SHA256:a"))
	if err != nil {
		t.Fatal(err)
	}
	if board.You == nil || board.You.Rank != 2 {
		t.Fatalf("initial rank = %+v, want rank 2", board.You)
	}

	// Simulate the actor's autosave flush overtaking Bravo.
	if err := st.PersistSave(ctx, "SHA256:a", "farm", []byte("blob"), 4, clock.now().Unix(), 1_000, "Alpha"); err != nil {
		t.Fatal(err)
	}

	// Within TTL, the board must still be stale (cache honored).
	board, err = eng.Get(ctx, ref("SHA256:a"))
	if err != nil {
		t.Fatal(err)
	}
	if board.You.Rank != 2 {
		t.Fatalf("rank before TTL expiry = %d, want still 2 (cached)", board.You.Rank)
	}

	clock.advance(time.Minute + time.Second)
	board, err = eng.Get(ctx, ref("SHA256:a"))
	if err != nil {
		t.Fatal(err)
	}
	if board.You.Rank != 1 {
		t.Fatalf("rank after flush + rebuild = %d, want 1", board.You.Rank)
	}
}
