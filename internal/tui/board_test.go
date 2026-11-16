package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-farm/internal/game"
	"github.com/mynameis-nigel/ssh-farm/internal/leaderboard"
	"github.com/mynameis-nigel/ssh-farm/internal/sim"
	"github.com/mynameis-nigel/ssh-farm/internal/store"
	"github.com/mynameis-nigel/ssh-farm/internal/tui/hitbox"
)

// fakeBoardSource is a canned, call-counting leaderboard.Source: it lets
// these tests control exactly what the board screen renders (and, for the
// error-state test, force a failure) without seeding a real database.
type fakeBoardSource struct {
	mu    sync.Mutex
	rows  []store.LeaderboardRow
	err   error
	calls int
}

func (f *fakeBoardSource) LeaderboardSnapshot(context.Context) ([]store.LeaderboardRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return append([]store.LeaderboardRow(nil), f.rows...), nil
}

func (f *fakeBoardSource) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func boardRow(fp, name string, coins int64) store.LeaderboardRow {
	return store.LeaderboardRow{Fingerprint: fp, Slot: "farm", Coins: coins, FarmName: name, UpdatedAt: time.Now().Unix()}
}

// boardRowWithRebirths is boardRow plus an explicit rebirth count, for the
// tests that specifically care about that field.
func boardRowWithRebirths(fp, name string, coins, rebirths int64) store.LeaderboardRow {
	r := boardRow(fp, name, coins)
	r.Rebirths = rebirths
	return r
}

// newBoardGame wires a fresh Game to a leaderboard engine reading from src
// (a long TTL, since these tests drive the game-level refresh throttle
// directly rather than the engine's own caching — see engine_test.go for
// that), skips the intro overlay, and ticks once so the game is live.
func (f *fixture) newBoardGame(t *testing.T, now int64, src leaderboard.Source, activityWindow time.Duration, minCoins int64) *Game {
	t.Helper()
	eng := leaderboard.New(src, time.Hour, activityWindow, minCoins, func() time.Time { return time.Unix(now, 0) })
	res := f.attach(t, now)
	t.Cleanup(res.Session.Detach)
	g := NewGame(f.id, res, f.content, eng, 100, 35, now, 0)
	g = dismissIntro(t, g)
	g, _ = tick(t, g, now)
	return g
}

func TestBoardEntryTriggersOneGetAndShowsRank(t *testing.T) {
	f := newFixture(t)
	base := time.Now().Unix()
	src := &fakeBoardSource{rows: []store.LeaderboardRow{
		boardRow(f.id.Fingerprint, "TOP FARM", 500),
		boardRow("SHA256:other", "OTHER FARM", 100),
	}}
	g := f.newBoardGame(t, base, src, 0, 0)

	g = press(t, g, "7")
	if got := src.callCount(); got != 1 {
		t.Fatalf("entering the board should call Get exactly once, got %d calls", got)
	}
	out := view(g)
	if !strings.Contains(out, "YOU: #1/2") {
		t.Fatalf("expected rank header YOU: #1/2, got:\n%s", out)
	}
	if !strings.Contains(out, "TOP FARM") || !strings.Contains(out, "OTHER FARM") {
		t.Fatalf("expected both farms listed, got:\n%s", out)
	}
}

// TestBoardShowsRebirthCountAlongsideCoins is the direct regression for
// gameplay/02's lifetime-coins-and-rebirths enhancement: each row's rebirth
// count must render on the board next to its coin total.
func TestBoardShowsRebirthCountAlongsideCoins(t *testing.T) {
	f := newFixture(t)
	base := time.Now().Unix()
	src := &fakeBoardSource{rows: []store.LeaderboardRow{
		boardRowWithRebirths(f.id.Fingerprint, "TOP FARM", 500, 12),
		boardRowWithRebirths("SHA256:other", "OTHER FARM", 100, 0),
	}}
	g := f.newBoardGame(t, base, src, 0, 0)
	g = press(t, g, "7")

	out := view(g)
	if !strings.Contains(out, "↻ 12") {
		t.Fatalf("expected the top farm's rebirth count (↻ 12) on the board, got:\n%s", out)
	}
	if !strings.Contains(out, "↻ 0") {
		t.Fatalf("expected a zero rebirth count (↻ 0) rendered too, got:\n%s", out)
	}
}

func TestBoardRowLineKeepsRebirthCountSeparatedFromCoins(t *testing.T) {
	g := &Game{}
	row := leaderboard.Row{
		Rank:        123,
		DisplayName: "TWENTY CHARACTER NAME",
		Suffix:      "wX9zK",
		ShowSuffix:  true,
		Coins:       9_876_543_210,
		Rebirths:    1_234,
		IsYou:       true,
	}

	line := g.boardRowLine(row, 52, 3)
	plain := stripAnsi(line)
	if w := lipgloss.Width(plain); w != 52 {
		t.Fatalf("line width = %d, want 52:\n%q", w, plain)
	}
	for _, want := range []string{"#123", "(wX9zK)", "↻ 1,234", "← YOU", "◈ 9,876,543,210"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected %q to survive truncation, got:\n%q", want, plain)
		}
	}
	if strings.Index(plain, "↻ 1,234") > strings.Index(plain, "◈ 9,876,543,210") {
		t.Fatalf("rebirth count rendered after coins:\n%q", plain)
	}
}

func TestBoardRankedInTopTenShowsAllRowsNoWindow(t *testing.T) {
	f := newFixture(t)
	base := time.Now().Unix()
	src := &fakeBoardSource{rows: []store.LeaderboardRow{
		boardRow("SHA256:a", "ALPHA", 500),
		boardRow("SHA256:b", "BRAVO", 400),
		boardRow(f.id.Fingerprint, "CHARLIE", 300),
		boardRow("SHA256:d", "DELTA", 200),
	}}
	g := f.newBoardGame(t, base, src, 0, 0)
	g = press(t, g, "7")

	out := view(g)
	if !strings.Contains(out, "YOU: #3/4") {
		t.Fatalf("expected rank header YOU: #3/4, got:\n%s", out)
	}
	for _, want := range []string{"ALPHA", "BRAVO", "CHARLIE", "DELTA"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q on the board, got:\n%s", want, out)
		}
	}
	if strings.Count(out, "← YOU") != 1 {
		t.Fatalf("expected exactly one '← YOU' marker, got %d in:\n%s", strings.Count(out, "← YOU"), out)
	}
}

// A board that fits on screen is just the list: everyone, in rank order,
// with no divider — including a viewer outside the old "top 10".
func TestBoardShortListShowsEveryoneWithNoDivider(t *testing.T) {
	f := newFixture(t)
	g := f.newBoardGame(t, time.Now().Unix(), boardOfN(f, 15, 13), 0, 0)
	g = press(t, g, "7")

	out := view(g)
	if !strings.Contains(out, "YOU: #13/15") {
		t.Fatalf("expected rank header YOU: #13/15, got:\n%s", out)
	}
	for i := 1; i <= 15; i++ {
		if want := boardFarmName(i); !strings.Contains(out, want) {
			t.Fatalf("expected %s on the board, got:\n%s", want, out)
		}
	}
	if strings.Count(out, "← YOU") != 1 {
		t.Fatalf("expected exactly one '← YOU' marker, got %d", strings.Count(out, "← YOU"))
	}
	if n := dividers(g); n != 0 {
		t.Fatalf("a board that fits has no divider, got %d", n)
	}
	if g.boardScrollable() {
		t.Fatal("a board that fits is not scrollable")
	}
}

func TestBoardUnrankedShowsCallout(t *testing.T) {
	f := newFixture(t)
	base := time.Now().Unix()
	src := &fakeBoardSource{rows: []store.LeaderboardRow{
		boardRow("SHA256:a", "ALPHA", 500),
		boardRow(f.id.Fingerprint, "", 0), // never earned a coin yet
	}}
	g := f.newBoardGame(t, base, src, 0, 1) // gameplay/02 coin floor of 1
	g = press(t, g, "7")

	out := view(g)
	if !strings.Contains(out, "YOU: UNRANKED — EARN YOUR FIRST COIN") {
		t.Fatalf("expected unranked callout, got:\n%s", out)
	}
}

func TestBoardTieHeavyHighlightsYouExactlyOnce(t *testing.T) {
	f := newFixture(t)
	base := time.Now().Unix()
	src := &fakeBoardSource{rows: []store.LeaderboardRow{
		boardRow("SHA256:a", "AAA", 100),
		boardRow("SHA256:b", "BBB", 100),
		boardRow(f.id.Fingerprint, "CCC", 100),
		boardRow("SHA256:d", "DDD", 100),
	}}
	g := f.newBoardGame(t, base, src, 0, 0)
	g = press(t, g, "7")

	out := view(g)
	if !strings.Contains(out, "YOU: #1/4") {
		t.Fatalf("a 4-way tie for coins should all share rank 1, got:\n%s", out)
	}
	if strings.Count(out, "← YOU") != 1 {
		t.Fatalf("expected exactly one '← YOU' marker among tied rows, got %d in:\n%s", strings.Count(out, "← YOU"), out)
	}
}

func TestBoardErrorStateRendersUnavailable(t *testing.T) {
	f := newFixture(t)
	base := time.Now().Unix()
	src := &fakeBoardSource{err: errors.New("db unreachable")}
	g := f.newBoardGame(t, base, src, 0, 0)
	g = press(t, g, "7")

	out := view(g)
	if !strings.Contains(out, "LEADERBOARD UNAVAILABLE") {
		t.Fatalf("expected the board-unavailable message, got:\n%s", out)
	}
}

func TestBoardTickRefreshHonorsFifteenSecondWindow(t *testing.T) {
	f := newFixture(t)
	base := time.Now().Unix()
	src := &fakeBoardSource{rows: []store.LeaderboardRow{boardRow(f.id.Fingerprint, "AAA", 100)}}
	g := f.newBoardGame(t, base, src, 0, 0)

	g = press(t, g, "7")
	if g.lbNextRefresh != base+boardRefreshSeconds {
		t.Fatalf("lbNextRefresh after entry = %d, want %d", g.lbNextRefresh, base+boardRefreshSeconds)
	}

	// Within the window: no automatic refresh, so the schedule is untouched.
	g, _ = tick(t, g, base+10)
	if g.lbNextRefresh != base+boardRefreshSeconds {
		t.Fatalf("lbNextRefresh after an early tick = %d, want unchanged %d", g.lbNextRefresh, base+boardRefreshSeconds)
	}

	// Past the window: the tick loop refreshes and reschedules from now.
	g, _ = tick(t, g, base+16)
	if want := base + 16 + boardRefreshSeconds; g.lbNextRefresh != want {
		t.Fatalf("lbNextRefresh after a late tick = %d, want %d (refreshed)", g.lbNextRefresh, want)
	}
}

func TestBoardRenameOpensFromKeyAndClick(t *testing.T) {
	f := newFixture(t)
	base := time.Now().Unix()
	src := &fakeBoardSource{rows: []store.LeaderboardRow{boardRow(f.id.Fingerprint, "AAA", 100)}}
	g := f.newBoardGame(t, base, src, 0, 0)
	g = press(t, g, "7")

	g = press(t, g, "n")
	if g.overlay != ovName {
		t.Fatalf("n on the board should open the rename overlay, overlay = %v", g.overlay)
	}
	if g.nameInput != g.snap.State.FarmName {
		t.Fatalf("nameInput = %q, want current farm name %q", g.nameInput, g.snap.State.FarmName)
	}
	g = press(t, g, "esc")
	if g.overlay != ovNone {
		t.Fatal("esc should close the rename overlay")
	}

	// And the same overlay opens by clicking your own row.
	view(g) // force a View pass so hitboxes are registered
	var yourRow *hitbox.Box
	for _, b := range g.hits.Boxes() {
		if b.ID == "board:yourow" {
			bb := b
			yourRow = &bb
		}
	}
	if yourRow == nil {
		t.Fatal("expected a board:yourow hitbox")
	}
	m, _ := g.handleMouseClick(tea.Mouse(tea.MouseClickMsg{X: yourRow.X, Y: yourRow.Y, Button: tea.MouseLeft}))
	g = m.(*Game)
	if g.overlay != ovName {
		t.Fatalf("clicking your own row should open the rename overlay, overlay = %v", g.overlay)
	}
}

// TestBoardRefreshFromKeyAndClick uses a zero-TTL engine (unlike
// newBoardGame's default long TTL) specifically so every Get actually
// re-queries the source — otherwise "r"/click would call Get correctly but
// the engine's own cache (a separate, deliberately TTL-bounded concern
// covered by internal/leaderboard's own tests) would mask it from this
// call-count assertion.
func TestBoardRefreshFromKeyAndClick(t *testing.T) {
	f := newFixture(t)
	base := time.Now().Unix()
	src := &fakeBoardSource{rows: []store.LeaderboardRow{boardRow(f.id.Fingerprint, "AAA", 100)}}
	eng := leaderboard.New(src, 0, 0, 0, func() time.Time { return time.Unix(base, 0) })
	res := f.attach(t, base)
	t.Cleanup(res.Session.Detach)
	g := NewGame(f.id, res, f.content, eng, 100, 35, base, 0)
	g = dismissIntro(t, g)
	g, _ = tick(t, g, base)

	g = press(t, g, "7") // call 1
	g = press(t, g, "r") // call 2
	if got := src.callCount(); got != 2 {
		t.Fatalf("calls after entry+r = %d, want 2", got)
	}

	view(g)
	var refresh *hitbox.Box
	for _, b := range g.hits.Boxes() {
		if b.ID == "board:refresh" {
			bb := b
			refresh = &bb
		}
	}
	if refresh == nil {
		t.Fatal("expected a board:refresh hitbox")
	}
	m, _ := g.handleMouseClick(tea.Mouse(tea.MouseClickMsg{X: refresh.X, Y: refresh.Y, Button: tea.MouseLeft}))
	g = m.(*Game)
	if got := src.callCount(); got != 3 {
		t.Fatalf("calls after clicking updated… = %d, want 3", got)
	}
}

// TestBoardRefreshBypassesEngineTTL is the regression for "r does nothing":
// with the production-shaped engine (15s TTL) a refresh pressed mid-window
// used to re-read the cached snapshot, so a newly-ranked farm never appeared
// and "updated Ns ago" kept counting. The earlier refresh test used a zero
// TTL, which hid this.
func TestBoardRefreshBypassesEngineTTL(t *testing.T) {
	for _, via := range []string{"key", "click"} {
		t.Run(via, func(t *testing.T) {
			f := newFixture(t)
			base := time.Now().Unix()
			src := &fakeBoardSource{rows: []store.LeaderboardRow{boardRow(f.id.Fingerprint, "AAA", 100)}}
			clock := base
			eng := leaderboard.New(src, 15*time.Second, 0, 0, func() time.Time { return time.Unix(clock, 0) })
			res := f.attach(t, base)
			t.Cleanup(res.Session.Detach)
			g := NewGame(f.id, res, f.content, eng, 100, 35, base, 0)
			g = dismissIntro(t, g)
			g, _ = tick(t, g, base)
			g = press(t, g, "7")

			src.mu.Lock()
			// The Source contract is coins DESC, so the richer farm goes first.
			src.rows = append([]store.LeaderboardRow{boardRow("SHA256:new", "NEWBIE", 999)}, src.rows...)
			src.mu.Unlock()

			clock = base + 5 // inside the TTL, past the engine's refresh floor
			g, _ = tick(t, g, base+5)
			if out := view(g); strings.Contains(out, "NEWBIE") || !strings.Contains(out, "updated 5s ago") {
				t.Fatalf("precondition: the tick must not refresh early, got:\n%s", out)
			}

			if via == "key" {
				g = press(t, g, "r")
			} else {
				view(g)
				var refresh *hitbox.Box
				for _, b := range g.hits.Boxes() {
					if b.ID == "board:refresh" {
						bb := b
						refresh = &bb
					}
				}
				if refresh == nil {
					t.Fatal("expected a board:refresh hitbox")
				}
				m, _ := g.handleMouseClick(tea.Mouse(tea.MouseClickMsg{X: refresh.X, Y: refresh.Y, Button: tea.MouseLeft}))
				g = m.(*Game)
			}

			out := view(g)
			if !strings.Contains(out, "NEWBIE") || !strings.Contains(out, "YOU: #2/2") {
				t.Fatalf("refresh must show the farm added since entry, got:\n%s", out)
			}
			if !strings.Contains(out, "updated 0s ago") {
				t.Fatalf("refresh must reset the staleness line, got:\n%s", out)
			}
		})
	}
}

func boardFarmName(rank int) string {
	return "FARM" + strings.Repeat("0", 3-len(itoa(rank))) + itoa(rank)
}

// boardOfN seeds n farms ranked 1..n by coins, with the viewer at youRank
// (0 = the viewer is not on the board at all).
func boardOfN(f *fixture, n, youRank int) *fakeBoardSource {
	var rows []store.LeaderboardRow
	for r := 1; r <= n; r++ {
		fp := "SHA256:" + boardFarmName(r)
		if r == youRank {
			fp = f.id.Fingerprint
		}
		rows = append(rows, boardRow(fp, boardFarmName(r), int64(10_000-r*10)))
	}
	return &fakeBoardSource{rows: rows}
}

// dividers counts the divider lines in the board's frame.
func dividers(g *Game) int {
	n := 0
	for _, l := range g.boardFrame(g.contentWidth()) {
		if l.row == nil {
			n++
		}
	}
	return n
}

// frameRanks is the rank of every row the board frame draws, in order.
func frameRanks(g *Game) []int {
	var out []int
	for _, l := range g.boardFrame(g.contentWidth()) {
		if l.row != nil {
			out = append(out, l.row.Rank)
		}
	}
	return out
}

// boardWith opens the board for a viewer ranked youRank of n on a short
// terminal, so the list overflows and scrolling is real.
func boardWith(t *testing.T, n, youRank int) *Game {
	t.Helper()
	f := newFixture(t)
	g := f.newBoardGame(t, time.Now().Unix(), boardOfN(f, n, youRank), 0, 0)
	g = resize(t, g, 100, 24)
	return press(t, g, "7")
}

// The board is the whole list. A viewer who is not on the first screen sees
// their own ±3 pinned under the list, behind a divider, until scrolling brings
// them into the list itself.
func TestBoardPinsYouPlusMinusThreeThenMergesWhenYouScrollIntoView(t *testing.T) {
	g := boardWith(t, 60, 40)
	V := g.boardVisibleRows()

	// Opening: list from #1, divider, then #37..#43 with You in the middle.
	ranks := frameRanks(g)
	if len(ranks) != V-1 {
		t.Fatalf("pinned frame should be %d rows plus one divider, got %d rows", V, len(ranks))
	}
	if dividers(g) != 1 {
		t.Fatalf("want exactly one divider while You is off-screen, got %d", dividers(g))
	}
	tail := ranks[len(ranks)-7:]
	for i, want := range []int{37, 38, 39, 40, 41, 42, 43} {
		if tail[i] != want {
			t.Fatalf("pinned block = %v, want 37..43", tail)
		}
	}
	if ranks[0] != 1 {
		t.Fatalf("list should start at #1, got %v", ranks)
	}
	out := view(g)
	if !strings.Contains(out, "← YOU") || !strings.Contains(out, boardFarmName(1)) {
		t.Fatalf("both the top of the list and You must be on screen, got:\n%s", out)
	}

	// Scroll until You is inside the list: the divider goes and ranks run on
	// unbroken, You included.
	for g.boardPlan().pinned {
		g = press(t, g, "down")
	}
	if dividers(g) != 0 {
		t.Fatalf("divider must disappear once You is in the list, got %d", dividers(g))
	}
	ranks = frameRanks(g)
	if len(ranks) != V {
		t.Fatalf("merged frame = %d rows, want %d", len(ranks), V)
	}
	for i := 1; i < len(ranks); i++ {
		if ranks[i] != ranks[i-1]+1 {
			t.Fatalf("merged list must be contiguous, got %v", ranks)
		}
	}
	if !strings.Contains(view(g), "← YOU") {
		t.Fatalf("You must be visible after merging, got:\n%s", view(g))
	}
}

// Scrolling reaches everyone, down to the last farm, and the last farm is
// drawn at the bottom of the list with no divider.
func TestBoardScrollsToTheVeryLastFarm(t *testing.T) {
	g := boardWith(t, 60, 5)
	if strings.Contains(view(g), boardFarmName(60)) {
		t.Fatal("the last farm should start off-screen")
	}
	g = press(t, g, "end")
	ranks := frameRanks(g)
	if ranks[len(ranks)-1] != 60 || dividers(g) != 0 {
		t.Fatalf("end should show #60 last with no divider, got %v dividers=%d", ranks, dividers(g))
	}
	if !strings.Contains(view(g), boardFarmName(60)) {
		t.Fatal("#60 must be rendered")
	}
	g = press(t, g, "home")
	if frameRanks(g)[0] != 1 {
		t.Fatalf("home should return to #1, got %v", frameRanks(g))
	}
}

// You at the very bottom: "visually obvious they are on the bottom" — the
// last row is You, the list runs unbroken above them.
func TestBoardLastPlaceViewerSitsAtTheBottomOfAnUnbrokenList(t *testing.T) {
	g := boardWith(t, 60, 60)
	if tail := frameRanks(g); tail[len(tail)-1] != 60 || dividers(g) != 1 {
		t.Fatalf("opening view pins #57..#60 behind a divider, got %v dividers=%d", tail, dividers(g))
	}
	g = press(t, g, "end")
	ranks := frameRanks(g)
	if ranks[len(ranks)-1] != 60 || dividers(g) != 0 {
		t.Fatalf("scrolled to the end: want #60 last and no divider, got %v dividers=%d", ranks, dividers(g))
	}
	if !g.lbBoard.Rows[59].IsYou {
		t.Fatal("fixture: #60 should be You")
	}
}

// Near the top you never need a pin, and an unranked viewer never gets one.
func TestBoardNeverPinsWhenYouAreVisibleOrUnranked(t *testing.T) {
	if g := boardWith(t, 60, 3); dividers(g) != 0 {
		t.Fatalf("a viewer in the first screen needs no pin, got %d dividers", dividers(g))
	}
	g := boardWith(t, 60, 0)
	if g.lbBoard.You != nil {
		t.Fatal("fixture: viewer should be unranked")
	}
	for _, key := range []string{"", "down", "pgdown", "end"} {
		if key != "" {
			g = press(t, g, key)
		}
		if dividers(g) != 0 {
			t.Fatalf("an unranked viewer must never get a pinned block (after %q)", key)
		}
	}
}

// Scrolling past your own row drops the pin too: the list is plain, and the
// header keeps saying where you are.
func TestBoardScrolledPastYouIsPlainListAgain(t *testing.T) {
	g := boardWith(t, 60, 20)
	g = press(t, g, "end")
	ranks := frameRanks(g)
	if dividers(g) != 0 || ranks[0] <= 20 {
		t.Fatalf("scrolled past You should be a plain list below #20, got %v dividers=%d", ranks, dividers(g))
	}
	if !strings.Contains(view(g), "YOU: #20/60") {
		t.Fatal("the pinned header should still show your rank")
	}
}

func TestBoardPagingKeysMoveByAViewport(t *testing.T) {
	g := boardWith(t, 100, 0)
	V := g.boardVisibleRows()
	g = press(t, g, "pgdown")
	if g.lbScroll != V {
		t.Fatalf("pgdown scrolled to %d, want %d", g.lbScroll, V)
	}
	g = press(t, g, "pgup", "pgup")
	if g.lbScroll != 0 {
		t.Fatalf("pgup should clamp at 0, got %d", g.lbScroll)
	}
	g = press(t, g, "end")
	if g.lbScroll != 100-V {
		t.Fatalf("end scrolled to %d, want %d", g.lbScroll, 100-V)
	}
}

// The footer should only promise scrolling when there is more list than room.
func TestBoardScrollHintOnlyWhenThereIsSomethingToScroll(t *testing.T) {
	f := newFixture(t)
	g := f.newBoardGame(t, time.Now().Unix(), boardOfN(f, 8, 3), 0, 0)
	g = press(t, g, "7")
	if out := view(g); strings.Contains(out, "scroll") {
		t.Fatalf("8 farms fit; no scroll hint expected, got:\n%s", out)
	}
	g = boardWith(t, 60, 3)
	if out := view(g); !strings.Contains(out, "wheel scroll") || !strings.Contains(out, "scroll for more") {
		t.Fatalf("60 farms overflow; the scroll hints are expected, got:\n%s", out)
	}
}

// The "← YOU" hitbox must land on the row that is drawn there — including in
// the pinned block, where it sits below the list and its divider — and the
// refresh target must stay put when the pin appears or goes.
func TestBoardHitboxesFollowThePinnedLayout(t *testing.T) {
	check := func(t *testing.T, g *Game) (refreshY int) {
		t.Helper()
		view(g)
		lines := strings.Split(stripAnsi(view(g)), "\n")
		var you, refresh *hitbox.Box
		for _, b := range g.hits.Boxes() {
			bb := b
			switch b.ID {
			case "board:yourow":
				you = &bb
			case "board:refresh":
				refresh = &bb
			}
		}
		if you == nil || refresh == nil {
			t.Fatalf("missing hitboxes: you=%v refresh=%v", you, refresh)
		}
		if !strings.Contains(lines[you.Y], "← YOU") {
			t.Fatalf("yourow hitbox at y=%d lands on %q", you.Y, lines[you.Y])
		}
		if !strings.Contains(lines[refresh.Y], "updated") {
			t.Fatalf("refresh hitbox at y=%d lands on %q", refresh.Y, lines[refresh.Y])
		}
		return refresh.Y
	}
	g := boardWith(t, 60, 40)
	pinnedY := check(t, g)
	for g.boardPlan().pinned {
		g = press(t, g, "down")
	}
	if mergedY := check(t, g); mergedY != pinnedY {
		t.Fatalf("refresh line moved from y=%d to y=%d when the pin dropped", pinnedY, mergedY)
	}
}

// A wave name has to animate wherever it is on the list, not just in the
// first ten rows — and not tick for rows that are scrolled out of view.
func TestBoardWaveAnimatesOnlyWhenOnScreen(t *testing.T) {
	rows := make([]leaderboard.Row, 100)
	for i := range rows {
		rows[i] = leaderboard.Row{Rank: i + 1, DisplayName: "FARM"}
	}
	rows[14].NameStyle = sim.NameStylePurpleWave
	g := &Game{scr: scrBoard, width: 100, height: 40, snap: game.Snapshot{State: &sim.State{}}, lbBoard: leaderboard.Board{Rows: rows, Total: 100}}
	if cmd := g.startBoardAnimation(); cmd == nil {
		t.Fatal("a wave row at #15 on screen should start the tick chain")
	}
	g.boardAnimRunning = false
	g.lbScroll = 60
	if cmd := g.startBoardAnimation(); cmd != nil {
		t.Fatal("a wave row scrolled out of view must not run the tick")
	}
}

func TestBoardWheelScrollsWithoutLosingHeader(t *testing.T) {
	f := newFixture(t)
	base := time.Now().Unix()
	letters := "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	var rows []store.LeaderboardRow
	for i, l := range letters {
		fp := "SHA256:" + string(l)
		if i == len(letters)-1 {
			fp = f.id.Fingerprint // rank last: window kicks in
		}
		rows = append(rows, boardRow(fp, strings.Repeat(string(l), 3), int64(1000-i)))
	}
	src := &fakeBoardSource{rows: rows}
	g := f.newBoardGame(t, base, src, 0, 0)
	g = resize(t, g, 80, 24) // small canvas -> few visible rows -> scrolling required
	g = press(t, g, "7")

	before := view(g)
	if !strings.Contains(before, "LEADERBOARD") || !strings.Contains(before, "YOU: #") {
		t.Fatalf("expected the pinned header before scrolling, got:\n%s", before)
	}
	startScroll := g.lbScroll

	m, _ := g.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	g = m.(*Game)
	if g.lbScroll <= startScroll {
		t.Fatalf("wheel down should advance lbScroll past %d, got %d", startScroll, g.lbScroll)
	}
	after := view(g)
	if !strings.Contains(after, "LEADERBOARD") || !strings.Contains(after, "YOU: #") {
		t.Fatalf("header must survive scrolling, got:\n%s", after)
	}

	// Scrolling far past the end must clamp, not go out of bounds.
	for i := 0; i < 100; i++ {
		m, _ = g.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
		g = m.(*Game)
	}
	clamped := g.lbScroll
	m, _ = g.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	g = m.(*Game)
	if g.lbScroll != clamped {
		t.Fatalf("scroll should clamp at the end of the list, went from %d to %d", clamped, g.lbScroll)
	}
}

func TestBoardHitboxesForTabRenameRefresh(t *testing.T) {
	f := newFixture(t)
	base := time.Now().Unix()
	src := &fakeBoardSource{rows: []store.LeaderboardRow{boardRow(f.id.Fingerprint, "AAA", 100)}}
	g := f.newBoardGame(t, base, src, 0, 0)
	g = press(t, g, "7")
	view(g)

	want := map[string]bool{"nav:6": false, "board:yourow": false, "board:refresh": false}
	for _, b := range g.hits.Boxes() {
		if _, ok := want[b.ID]; ok {
			want[b.ID] = true
		}
	}
	for id, found := range want {
		if !found {
			t.Errorf("expected hitbox %q to be registered", id)
		}
	}
}

func TestBoardEscReturnsToFarm(t *testing.T) {
	f := newFixture(t)
	base := time.Now().Unix()
	src := &fakeBoardSource{rows: []store.LeaderboardRow{boardRow(f.id.Fingerprint, "AAA", 100)}}
	g := f.newBoardGame(t, base, src, 0, 0)
	g = press(t, g, "7", "esc")
	if g.scr != scrFarm {
		t.Fatalf("esc on the board should return to farm, scr = %v", g.scr)
	}
	if !strings.Contains(view(g), "Plot 1") {
		t.Fatalf("expected the farm screen after esc, got:\n%s", view(g))
	}
}
