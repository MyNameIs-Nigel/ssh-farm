package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestMouseClickSelectsNavTab(t *testing.T) {
	f := newFixture(t)
	base := time.Now().Unix()
	g := f.newGame(t, base)
	g = dismissIntro(t, g)
	g, _ = tick(t, g, base)
	g = resize(t, g, 100, 38)

	g.registerHitboxes()
	box, ok := g.hits.At(g.layout.navX+2, g.layout.navY)
	if !ok {
		t.Fatal("expected nav hitbox")
	}
	if box.ID != "nav:0" {
		t.Fatalf("expected farm nav, got %q", box.ID)
	}

	m, _ := g.handleMouseClick(tea.Mouse(tea.MouseClickMsg{X: g.layout.navX + 2, Y: g.layout.navY, Button: tea.MouseLeft}))
	g = m.(*Game)
	if g.scr != scrMarket {
		// first tab is farm; click market tab offset
		g.registerHitboxes()
		// find market tab hitbox
		for _, b := range g.hits.Boxes() {
			if b.ID == "nav:1" {
				m, _ = g.handleMouseClick(tea.Mouse(tea.MouseClickMsg{X: b.X + 1, Y: b.Y, Button: tea.MouseLeft}))
				g = m.(*Game)
				break
			}
		}
	}
	if g.scr != scrMarket {
		t.Fatalf("expected market screen after nav click, got %d", g.scr)
	}
}

func TestMouseActivityResetsIdleTimer(t *testing.T) {
	f := newFixture(t)
	base := time.Now().Unix()
	g := f.newGame(t, base)
	g.idleTimeout = 60
	g.now = base
	g.lastInput = base - 30
	m, _ := g.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	g = m.(*Game)
	if g.lastInput != base {
		t.Fatalf("mouse wheel should set lastInput to now=%d, got %d", base, g.lastInput)
	}
}

func TestDoubleClickTimingWindow(t *testing.T) {
	f := newFixture(t)
	base := time.Now().Unix()
	g := f.newGame(t, base)
	g = dismissIntro(t, g)
	g.lastClickID = "plot:0"
	g.lastClickAt = g.now*1000 - 100
	g.registerHitboxes()
	box := g.hits.Boxes()[0]
	_, _ = g.dispatchHit(box, false)
	g.lastClickAt = g.now * 1000
	_, _ = g.dispatchHit(box, true)
}

func TestHitboxesEmptyOnTinyTerminal(t *testing.T) {
	f := newFixture(t)
	base := time.Now().Unix()
	g := f.newGame(t, base)
	g = resize(t, g, 20, 6)
	g.registerHitboxes()
	if len(g.hits.Boxes()) != 0 {
		t.Fatalf("expected no hitboxes on tiny terminal, got %d", len(g.hits.Boxes()))
	}
}

func TestOverlayBackgroundDismissHitbox(t *testing.T) {
	f := newFixture(t)
	base := time.Now().Unix()
	g := f.newGame(t, base)
	g = dismissIntro(t, g)
	g.overlay = ovPicker
	g.registerHitboxes()
	found := false
	for _, b := range g.hits.Boxes() {
		if b.ID == "overlay:bg" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected overlay background hitbox")
	}

	// The hitbox existing isn't enough — clicking it must actually close the
	// overlay (dismissOverlay used to leave ovPicker open on background click).
	m, _ := g.handleMouseClick(tea.Mouse(tea.MouseClickMsg{X: g.layout.ox, Y: g.layout.oy, Button: tea.MouseLeft}))
	g = m.(*Game)
	if g.overlay != ovNone {
		t.Fatalf("background click should close the picker overlay, got %v", g.overlay)
	}
}

// TestNavTabRowMatchesRenderedText renders the real view and locates "1 Farm"
// by text search, then checks it lines up with where the nav hitboxes are
// registered. A prior off-by-one in navY put every nav hitbox one row below
// the visible tabs, so clicking a tab did nothing.
func TestNavTabRowMatchesRenderedText(t *testing.T) {
	f := newFixture(t)
	base := time.Now().Unix()
	g := f.newGame(t, base)
	g = dismissIntro(t, g)
	g, _ = tick(t, g, base)
	g = resize(t, g, 100, 38)

	lines := strings.Split(view(g), "\n")
	row := -1
	for i, line := range lines {
		if strings.Contains(stripAnsi(line), "1 Farm") {
			row = i
			break
		}
	}
	if row == -1 {
		t.Fatal("\"1 Farm\" not found in rendered view")
	}

	g.registerHitboxes()
	if row != g.layout.navY {
		t.Fatalf("\"1 Farm\" renders on row %d but nav hitboxes register at row %d", row, g.layout.navY)
	}
	box, ok := g.hits.At(g.layout.navX, row)
	if !ok || box.ID != "nav:0" {
		t.Fatalf("expected nav:0 hitbox at the rendered tab's position, got %+v ok=%v", box, ok)
	}
}

// TestOverlayCloseButtonDismissesMenu covers the mouse-only way to leave a
// menu: a visible "[x] Close" control on the nav row, clickable wherever it
// actually renders (not just at a self-referential layout coordinate).
func TestOverlayCloseButtonDismissesMenu(t *testing.T) {
	f := newFixture(t)
	base := time.Now().Unix()
	g := f.newGame(t, base)
	g = dismissIntro(t, g)
	g, _ = tick(t, g, base)
	g = resize(t, g, 100, 38)

	g.overlay = ovConfig
	g.registerHitboxes()

	var boxX, boxY int
	found := false
	for _, b := range g.hits.Boxes() {
		if b.ID == "overlay:close" {
			boxX, boxY, found = b.X, b.Y, true
		}
	}
	if !found {
		t.Fatal("expected an overlay:close hitbox while a menu is open")
	}

	lines := strings.Split(view(g), "\n")
	if boxY < 0 || boxY >= len(lines) || !strings.Contains(stripAnsi(lines[boxY]), "Close") {
		t.Fatalf("overlay:close hitbox at row %d doesn't line up with visible \"Close\" text", boxY)
	}

	m, _ := g.handleMouseClick(tea.Mouse(tea.MouseClickMsg{X: boxX, Y: boxY, Button: tea.MouseLeft}))
	g = m.(*Game)
	if g.overlay != ovNone {
		t.Fatalf("clicking [x] Close should close the overlay, got %v", g.overlay)
	}
}

// TestReplantWarningClickDismissesAndArms covers the mouse path for the
// first-use replant warning: any click on it (like any key) should
// acknowledge the warning and close it, not just redisplay it.
func TestReplantWarningClickDismissesAndArms(t *testing.T) {
	f := newFixture(t)
	base := time.Now().Unix()
	g := f.newGame(t, base)
	g = dismissIntro(t, g)
	g, _ = tick(t, g, base)
	g = resize(t, g, 100, 38)

	g = press(t, g, "enter", "enter") // plant plot 0 so replant has a remembered crop
	if g.snap.State.Plots[0].Crop != "turnip" {
		t.Fatalf("setup plant failed: %+v", g.snap.State.Plots[0])
	}
	g = press(t, g, "r")
	if g.overlay != ovReplantWarn {
		t.Fatalf("expected replant warning overlay, got %v", g.overlay)
	}

	g.registerHitboxes()
	box, ok := g.hits.At(g.layout.cx, g.layout.bodyY)
	if !ok {
		t.Fatal("expected a hitbox within the replant warning overlay")
	}
	m, _ := g.handleMouseClick(tea.Mouse(tea.MouseClickMsg{X: box.X, Y: box.Y, Button: tea.MouseLeft}))
	g = m.(*Game)
	if g.overlay != ovNone || !g.snap.State.ReplantWarned {
		t.Fatalf("click should dismiss and arm replant, overlay=%v warned=%v", g.overlay, g.snap.State.ReplantWarned)
	}
}
