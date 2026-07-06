package tui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripAnsi(s string) string { return ansiRe.ReplaceAllString(s, "") }

func resize(t *testing.T, g *Game, w, h int) *Game {
	t.Helper()
	m, _ := g.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m.(*Game)
}

func TestAltScreenOnEveryViewPath(t *testing.T) {
	f := newFixture(t)
	base := time.Now().Unix()
	g := f.newGame(t, base)
	g, _ = tick(t, g, base)

	check := func(name string, v tea.View) {
		t.Helper()
		if !v.AltScreen {
			t.Errorf("%s: AltScreen must be set", name)
		}
		if v.WindowTitle == "" {
			t.Errorf("%s: WindowTitle must be set", name)
		}
	}

	for _, ov := range []overlay{ovTutorial, ovAway, ovPicker, ovRebirthConfirm, ovName, ovConfig, ovReplantWarn, ovKicked, ovNone} {
		g.overlay = ov
		check("overlay", g.View())
	}
	for _, scr := range screenOrder {
		g.scr = scr
		check("screen", g.View())
	}

	g = resize(t, g, 20, 6) // tiny-terminal guard path
	check("tiny guard", g.View())

	check("errScreen", NewErrScreen().View())
}

func TestCanvasClamp(t *testing.T) {
	f := newFixture(t)
	base := time.Now().Unix()
	g := f.newGame(t, base)

	g = resize(t, g, 220, 70)
	if w, h := g.canvasSize(); w != canvasMaxWidth || h != canvasMaxHeight {
		t.Fatalf("canvasSize() = %d×%d, want %d×%d on a huge terminal", w, h, canvasMaxWidth, canvasMaxHeight)
	}

	g = resize(t, g, 80, 24)
	if w, h := g.canvasSize(); w != 80 || h != 24 {
		t.Fatalf("canvasSize() = %d×%d, want 80×24 (canvas follows small terminals)", w, h)
	}
}

func TestViewIsCenteredOnLargeTerminals(t *testing.T) {
	f := newFixture(t)
	base := time.Now().Unix()
	g := f.newGame(t, base)
	g = dismissIntro(t, g)
	g, _ = tick(t, g, base)
	g = resize(t, g, 200, 50)

	out := view(g)
	if w := lipgloss.Width(out); w != 200 {
		t.Fatalf("view width = %d, want padded to the full 200-col terminal", w)
	}
	if h := lipgloss.Height(out); h != 50 {
		t.Fatalf("view height = %d, want padded to the full 50-row terminal", h)
	}

	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "ssh-farm") {
			continue
		}
		// Canvas is 100 wide on a 200-col terminal: expect ~50 columns of
		// left margin (±2 for emoji-width slack).
		lead := len(line) - len(strings.TrimLeft(line, " "))
		if lead < 48 || lead > 52 {
			t.Fatalf("header line margin = %d leading spaces, want ~50", lead)
		}
		return
	}
	t.Fatal("header line not found in view")
}

func TestHeaderGapUsesCanvasWidth(t *testing.T) {
	f := newFixture(t)
	base := time.Now().Unix()
	g := f.newGame(t, base)
	g, _ = tick(t, g, base)
	g = resize(t, g, 200, 50)

	if w := lipgloss.Width(g.viewHeader()); w != g.contentWidth() {
		t.Fatalf("header width = %d, want contentWidth %d (not terminal width)", w, g.contentWidth())
	}
}

func TestCanvasHeightStableAcrossScreensAndNotices(t *testing.T) {
	f := newFixture(t)
	base := time.Now().Unix()
	g := f.newGame(t, base)
	g = dismissIntro(t, g)
	g, _ = tick(t, g, base)

	_, ch := g.canvasSize()
	farm := g.composeCanvas(g.screenBody(), false)
	if got := lipgloss.Height(farm); got != ch {
		t.Fatalf("farm canvas height = %d, want %d", got, ch)
	}

	g = press(t, g, "s")
	stats := g.composeCanvas(g.screenBody(), false)
	if got := lipgloss.Height(stats); got != ch {
		t.Fatalf("stats canvas height = %d, want %d (canvas must not resize per screen)", got, ch)
	}

	g.addNotice("a notice line")
	g.addNotice("another notice line")
	noisy := g.composeCanvas(g.screenBody(), false)
	if got := lipgloss.Height(noisy); got != ch {
		t.Fatalf("canvas height with notices = %d, want %d (body absorbs notice lines)", got, ch)
	}
}

func TestModalIsCenteredInBodyRegion(t *testing.T) {
	f := newFixture(t)
	base := time.Now().Unix()
	g := f.newGame(t, base)
	g = dismissIntro(t, g)
	g, _ = tick(t, g, base)
	g = resize(t, g, 100, 38)

	g = press(t, g, "enter") // open the crop picker on the empty plot
	if g.overlay != ovPicker {
		t.Fatal("picker should be open")
	}
	out := view(g)
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if !strings.Contains(line, "Plant on plot") {
			continue
		}
		if i < 6 {
			t.Fatalf("modal content on line %d — expected vertical centering below header", i)
		}
		// The modal text should sit well inside the 100-col canvas, not
		// flush against the frame (frame border + padding end at col 3).
		if at := strings.Index(stripAnsi(line), "Plant on plot"); at < 8 {
			t.Fatalf("modal text at column %d, expected horizontal centering", at)
		}
		return
	}
	t.Fatal("picker content not found in view")
}
