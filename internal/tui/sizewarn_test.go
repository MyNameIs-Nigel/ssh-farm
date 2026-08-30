package tui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
)

func sizeGame(t *testing.T) *Game {
	t.Helper()
	f := newFixture(t)
	base := time.Now().Unix()
	g := f.newGame(t, base)
	g = dismissIntro(t, g)
	g, _ = tick(t, g, base)
	return g
}

// TestUndersizedTracksTheRecommendedSize pins the thresholds the whole
// feature hangs off. 80×24 (macOS default) is undersized; 120×30 (Windows
// default) is not — that is why the recommended height is 30 rather than the
// canvas maximum of 38.
func TestUndersizedTracksTheRecommendedSize(t *testing.T) {
	g := sizeGame(t)
	cases := []struct {
		name string
		w, h int
		want bool
	}{
		{"macOS default", 80, 24, true},
		{"windows default", 120, 30, false},
		{"narrow only", 80, recommendedHeight, true},
		{"short only", recommendedWidth, 24, true},
		{"one column short", recommendedWidth - 1, recommendedHeight, true},
		{"one row short", recommendedWidth, recommendedHeight - 1, true},
		{"exactly recommended", recommendedWidth, recommendedHeight, false},
		{"roomy", 140, 50, false},
	}
	for _, c := range cases {
		g = resize(t, g, c.w, c.h)
		if got := g.undersized(); got != c.want {
			t.Errorf("%s (%d×%d): undersized = %v, want %v", c.name, c.w, c.h, got, c.want)
		}
	}
}

// TestHelpTabCarriesTheWarningGlyph — the in-frame signal is the nav tab
// itself: "?" becomes "⚠" and picks up the Warn style.
func TestHelpTabCarriesTheWarningGlyph(t *testing.T) {
	g := resize(t, sizeGame(t), 80, 24)
	if got := g.helpNavLabel(); got != "⚠ Help" {
		t.Errorf("undersized help label = %q, want %q", got, "⚠ Help")
	}
	nav := g.viewNav()
	if !strings.Contains(stripAnsi(nav), "⚠ Help") {
		t.Errorf("nav strip does not show the warning glyph:\n%s", stripAnsi(nav))
	}
	warn := g.theme().Warn.Render("⚠ Help")
	if !strings.Contains(nav, warn) {
		t.Errorf("the Help tab is not rendered in the Warn style while undersized")
	}

	g = resize(t, g, recommendedWidth, recommendedHeight)
	if got := g.helpNavLabel(); got != "? Help" {
		t.Errorf("at the recommended size help label = %q, want %q", got, "? Help")
	}
	if strings.Contains(stripAnsi(g.viewNav()), "⚠") {
		t.Errorf("the warning glyph should not show at the recommended size")
	}
}

// TestHelpTabWidthIsTheSameEitherWay is the reason this indicator is free:
// the warning tab occupies exactly as many columns as the normal one, so the
// nav strip never shifts and no hitbox moves.
//
// Two things can break it, and both have: U+26A0 carrying a variation
// selector ("⚠️") measures two columns rather than one, and NavWarn losing
// the Padding(0, 1) that every other tab style carries.
func TestHelpTabWidthIsTheSameEitherWay(t *testing.T) {
	if w, q := lipgloss.Width("⚠ Help"), lipgloss.Width("? Help"); w != q {
		t.Errorf("warning label is %d columns but the normal label is %d — check for a variation selector", w, q)
	}

	// Measure the whole rendered strip, which is what actually has to hold
	// still: same game, same screen, only the size differs.
	small := resize(t, sizeGame(t), 80, 40)  // narrow: undersized
	large := resize(t, sizeGame(t), 120, 40) // roomy: not undersized
	if !small.undersized() || large.undersized() {
		t.Fatalf("fixture is wrong: undersized = %v (want true), %v (want false)",
			small.undersized(), large.undersized())
	}
	if w, q := lipgloss.Width(small.viewNav()), lipgloss.Width(large.viewNav()); w != q {
		t.Errorf("nav strip is %d columns while undersized but %d otherwise:\n warn: %q\nplain: %q",
			w, q, stripAnsi(small.viewNav()), stripAnsi(large.viewNav()))
	}
}

// TestNoSizeWarningInTheFrame — an earlier revision spent a header row and a
// toast on this. Both were removed: on the terminals that trip the warning,
// a permanent row costs one of only 24 rows to restate what the player can
// already see.
func TestNoSizeWarningInTheFrame(t *testing.T) {
	g := resize(t, sizeGame(t), 80, 24)
	for _, scr := range screenOrder {
		if scr == scrHelp {
			continue
		}
		g.scr = scr
		out := stripAnsi(view(g))
		if strings.Contains(out, "best at") || strings.Contains(out, "smaller than the farm") {
			t.Errorf("screen %v draws a size warning in the frame:\n%s", scr, out)
		}
	}
	if got := len(g.notices); got != 0 {
		t.Errorf("resizing raised %d notices, want 0 — the toast was removed", got)
	}
}

// TestHelpScreenExplainsTheSize — the ⚠ is only a pointer; Help is where the
// player finds out what is actually wrong and what to do about it.
func TestHelpScreenExplainsTheSize(t *testing.T) {
	g := resize(t, sizeGame(t), 80, 24)
	g.scr = scrHelp
	g.helpPage = 0
	out := stripAnsi(view(g))
	for _, want := range []string{"80×24", "100×30"} {
		if !strings.Contains(out, want) {
			t.Errorf("help screen does not mention %q:\n%s", want, out)
		}
	}
}

// TestHelpScreenIsSilentAtTheRecommendedSize — Help must not carry a
// permanent scold for players whose window is already fine.
func TestHelpScreenIsSilentAtTheRecommendedSize(t *testing.T) {
	g := resize(t, sizeGame(t), 120, 40)
	g.scr = scrHelp
	g.helpPage = 0
	if n := g.viewHelpSizeNotice(); n != "" {
		t.Errorf("help size notice rendered at 120×40: %q", stripAnsi(n))
	}
	if strings.Contains(stripAnsi(view(g)), "smaller than the farm") {
		t.Errorf("help screen mentions the window size at a fine size")
	}
}

// TestNoWarningOnTheBlockingPath — below minWidth/minHeight the game already
// shows a dedicated "needs a bigger window" screen; a second signal on top of
// it would be noise.
func TestNoWarningOnTheBlockingPath(t *testing.T) {
	g := resize(t, sizeGame(t), 20, 6)
	if g.undersized() {
		t.Errorf("undersized() is true on the blocking path")
	}
	out := stripAnsi(view(g))
	if !strings.Contains(out, "bigger window") {
		t.Fatalf("expected the existing hard guard, got:\n%s", out)
	}
	if strings.Contains(out, "⚠") {
		t.Errorf("the warning glyph should not stack on the blocking guard:\n%s", out)
	}
}

// TestNavLabelsMatchRenderedNav pins navLabels() as the single source of
// truth, mirroring TestMarketLinesMatchViewOutput. viewNav and
// registerNavHits used to hold separate copies of this list, so a conditional
// label drifted the rendered row away from the click targets.
func TestNavLabelsMatchRenderedNav(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {120, 40}} {
		g := resize(t, sizeGame(t), sz[0], sz[1])
		nav := stripAnsi(g.viewNav())
		for _, l := range g.navLabels() {
			text := l.text
			if l.lock {
				text += " 🔒"
			}
			if !strings.Contains(nav, text) {
				t.Errorf("at %d×%d navLabels has %q but the rendered nav is %q",
					sz[0], sz[1], text, nav)
			}
		}
	}
}

// TestHelpTabHitboxLandsOnTheRenderedTab is the drift guard: the warning
// label must not move the Help tab's click target away from where it draws.
// It locates the tab by searching the rendered row, so it does not re-derive
// the layout arithmetic it is checking.
func TestHelpTabHitboxLandsOnTheRenderedTab(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {120, 40}} {
		g := resize(t, sizeGame(t), sz[0], sz[1])
		view(g) // force a View pass so hitboxes are registered

		label := g.helpNavLabel()
		lines := strings.Split(view(g), "\n")
		if g.layout.navY >= len(lines) {
			t.Fatalf("at %d×%d nav row %d is off the view", sz[0], sz[1], g.layout.navY)
		}
		row := stripAnsi(lines[g.layout.navY])
		col := displayCol(row, label)
		if col < 0 {
			// At 80 columns the nav strip is clipped mid-label (see the doc's
			// out-of-scope note); the glyph still has to survive.
			if sz[0] < 100 && strings.Contains(row, "⚠") {
				continue
			}
			t.Fatalf("at %d×%d %q not found on the nav row %q", sz[0], sz[1], label, row)
		}
		box, ok := g.hits.At(col, g.layout.navY)
		if !ok || box.ID != "nav:"+itoa(int(scrHelp)) {
			t.Errorf("at %d×%d the Help tab renders at column %d but the hitbox there is %+v (ok=%v)",
				sz[0], sz[1], col, box, ok)
		}
	}
}

// displayCol returns the display column at which needle starts in an
// ANSI-stripped row, or -1. It counts columns rather than bytes because the
// nav strip can contain a two-column 🔒 before the Help tab.
func displayCol(row, needle string) int {
	i := strings.Index(row, needle)
	if i < 0 {
		return -1
	}
	return lipgloss.Width(row[:i])
}
