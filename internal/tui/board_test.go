package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mynameis-nigel/ssh-farm/internal/leaderboard"
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

func TestBoardOutsideTopTenShowsWindowWithDivider(t *testing.T) {
	f := newFixture(t)
	base := time.Now().Unix()
	letters := "ABCDEFGHIJKLMNO" // 15 distinct farms, ranks 1-15
	var rows []store.LeaderboardRow
	for i, l := range letters {
		fp := "SHA256:" + string(l)
		name := strings.Repeat(string(l), 3)
		coins := int64(1000 - i*10)
		if i == 12 { // rank 13 (the doc's sketch position) is "you"
			fp = f.id.Fingerprint
		}
		rows = append(rows, boardRow(fp, name, coins))
	}
	src := &fakeBoardSource{rows: rows}
	g := f.newBoardGame(t, base, src, 0, 0)
	g = press(t, g, "7")

	out := view(g)
	if !strings.Contains(out, "YOU: #13/15") {
		t.Fatalf("expected rank header YOU: #13/15, got:\n%s", out)
	}
	// Top 10: ranks 1-10 (letters A-J).
	for _, l := range "ABCDEFGHIJ" {
		want := strings.Repeat(string(l), 3)
		if !strings.Contains(out, want) {
			t.Fatalf("expected top-10 farm %q on the board, got:\n%s", want, out)
		}
	}
	// Window around rank 13 (index 12, radius 3, clipped at Top): ranks 11-15.
	for _, l := range "KLMNO" {
		want := strings.Repeat(string(l), 3)
		if !strings.Contains(out, want) {
			t.Fatalf("expected window farm %q on the board, got:\n%s", want, out)
		}
	}
	if strings.Count(out, "← YOU") != 1 {
		t.Fatalf("expected exactly one '← YOU' marker, got %d", strings.Count(out, "← YOU"))
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
