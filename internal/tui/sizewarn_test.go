package tui

import (
	"strings"
	"testing"
	"time"
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

// TestSizeWarningAppearsBelowRecommended is the feedback the player asked
// for: at 80×24 the game plays, but cramped, and nothing used to say so.
func TestSizeWarningAppearsBelowRecommended(t *testing.T) {
	g := sizeGame(t)
	cases := []struct {
		name string
		w, h int
		want bool
	}{
		{"stock terminal", 80, 24, true},
		{"narrow only", 80, recommendedHeight, true},
		{"short only", recommendedWidth, 24, true},
		{"one column short", recommendedWidth - 1, recommendedHeight, true},
		{"one row short", recommendedWidth, recommendedHeight - 1, true},
		{"exactly recommended", recommendedWidth, recommendedHeight, false},
		{"roomy", 140, 50, false},
	}
	for _, c := range cases {
		g = resize(t, g, c.w, c.h)
		got := g.viewSizeWarning() != ""
		if got != c.want {
			t.Errorf("%s (%d×%d): warning present = %v, want %v", c.name, c.w, c.h, got, c.want)
		}
	}
}

func TestSizeWarningNamesBothSizes(t *testing.T) {
	g := resize(t, sizeGame(t), 80, 24)
	warn := stripAnsi(g.viewSizeWarning())
	for _, want := range []string{"80", "24", "100", "38"} {
		if !strings.Contains(warn, want) {
			t.Errorf("size warning %q does not mention %q", warn, want)
		}
	}
}

func TestSizeWarningIsVisibleInTheRenderedView(t *testing.T) {
	g := resize(t, sizeGame(t), 80, 24)
	for _, scr := range screenOrder {
		g.scr = scr
		if !strings.Contains(stripAnsi(view(g)), "80×24") {
			t.Errorf("screen %v does not surface the size warning", scr)
		}
	}
}

// TestSizeWarningAbsentOnTheBlockingPath — below minWidth/minHeight the game
// already shows a dedicated "needs a bigger window" screen; stacking a second
// warning on top of it would be noise.
func TestSizeWarningAbsentOnTheBlockingPath(t *testing.T) {
	g := resize(t, sizeGame(t), 20, 6)
	out := stripAnsi(view(g))
	if !strings.Contains(out, "bigger window") {
		t.Fatalf("expected the existing hard guard, got:\n%s", out)
	}
	if strings.Count(out, "best at") > 0 {
		t.Errorf("the size-warning chip should not stack on the blocking guard:\n%s", out)
	}
}

// TestSizeWarningFiresOneToast — a chip is easy to miss on first connect, so
// crossing the threshold also raises a notice. It must not re-fire on every
// subsequent resize inside the undersized range.
func TestSizeWarningFiresOneToast(t *testing.T) {
	g := resize(t, sizeGame(t), recommendedWidth, recommendedHeight)
	if got := countNotices(g, "smaller"); got != 0 {
		t.Fatalf("toast fired at the recommended size (%d notices)", got)
	}

	g = resize(t, g, 80, 24)
	if got := countNotices(g, "smaller"); got != 1 {
		t.Fatalf("crossing below the recommended size raised %d toasts, want 1", got)
	}

	g = resize(t, g, 82, 25) // still undersized
	if got := countNotices(g, "smaller"); got != 1 {
		t.Fatalf("resizing within the undersized range re-nagged: %d toasts, want 1", got)
	}
}

func countNotices(g *Game, needle string) int {
	n := 0
	for _, no := range g.notices {
		if strings.Contains(strings.ToLower(stripAnsi(no.text)), needle) {
			n++
		}
	}
	return n
}

// TestSizeWarningDoesNotBreakHitboxes guards the invariant that made this
// design choice: the warning is emitted from viewHeader(), which
// coords.go:computeLayout already measures, so adding a header row must not
// shift the body or the nav strip out from under the mouse.
func TestSizeWarningDoesNotBreakHitboxes(t *testing.T) {
	g := resize(t, sizeGame(t), 80, 24)
	view(g) // force a View pass so hitboxes are registered

	lines := strings.Split(view(g), "\n")
	navRow := -1
	for y, line := range lines {
		if strings.Contains(stripAnsi(line), "2 Market") && strings.Contains(stripAnsi(line), "1 Farm") {
			navRow = y
			break
		}
	}
	if navRow < 0 {
		t.Fatalf("nav strip not found in the rendered view:\n%s", stripAnsi(view(g)))
	}
	if g.layout.navY != navRow {
		t.Fatalf("nav hitboxes register at row %d but the tabs render at row %d — the size warning shifted the layout",
			g.layout.navY, navRow)
	}
}
