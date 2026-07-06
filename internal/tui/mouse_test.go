package tui

import (
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
}
