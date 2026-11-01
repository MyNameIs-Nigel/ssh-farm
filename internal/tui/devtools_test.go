package tui

import (
	"math"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mynameis-nigel/ssh-farm/internal/game"
)

func devGame(t *testing.T, enabled bool) *Game {
	t.Helper()
	f := newFixture(t)
	game.WithDevMode(enabled)(f.mgr)
	g := f.newGame(t, 1000)
	g, _ = tick(t, g, 1000)
	return dismissIntro(t, g)
}

func toggleDev(g *Game) {
	g.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
}

func TestDevPanelProductionGate(t *testing.T) {
	g := devGame(t, false)
	toggleDev(g)
	if g.overlay == ovDev || strings.Contains(g.viewFooter(), "dev tools") {
		t.Fatal("production exposes dev tools")
	}
	before := g.snap.State.Coins
	g.overlay = ovDev
	g.handleDevKey("right")
	if g.snap.State.Coins != before {
		t.Fatal("production changed balance")
	}
}

func TestDevPanelBalancesAndInput(t *testing.T) {
	g := devGame(t, true)
	if !strings.Contains(g.viewFooter(), "Ctrl+D") {
		t.Fatal("missing discovery hint")
	}
	toggleDev(g)
	if g.overlay != ovDev {
		t.Fatal("panel did not open")
	}
	for i := 0; i < 4; i++ {
		g.devSel = i
		press(t, g, "1", "2", "3", "backspace", "4", "enter")
		if g.devValue(i) != 124 {
			t.Fatalf("row %d: %d", i, g.devValue(i))
		}
		press(t, g, "right")
		if g.devValue(i) != 124+devRows[i].step {
			t.Fatal("right failed")
		}
		press(t, g, "left")
		if g.devValue(i) != 124 {
			t.Fatal("left failed")
		}
		press(t, g, "0", "enter", "left")
		if g.devValue(i) != 0 {
			t.Fatal("negative balance")
		}
	}
	g.devSel = 0
	g.devInput = strconv.FormatInt(math.MaxInt64, 10)
	press(t, g, "enter", "right")
	if g.snap.State.Coins != math.MaxInt64 {
		t.Fatal("overflowed balance")
	}
	g.devInput = "9999999999999999999"
	press(t, g, "enter")
	if g.snap.State.Coins != math.MaxInt64 {
		t.Fatal("invalid input changed balance")
	}
	press(t, g, "esc")
	if g.overlay != ovNone {
		t.Fatal("escape failed")
	}
	toggleDev(g)
	if g.devInput != "" {
		t.Fatal("stale input")
	}
	toggleDev(g)
	if g.overlay != ovNone {
		t.Fatal("toggle close failed")
	}
}

func TestDevPanelFixtureActionsAndEvents(t *testing.T) {
	g := devGame(t, true)
	toggleDev(g)
	g.devSel = 5
	press(t, g, "enter")
	if !g.snap.State.GiftPending {
		t.Fatal("parcel missing")
	}
	g.devSel = 7
	press(t, g, "enter")
	if !g.snap.State.ContractsUnlocked {
		t.Fatal("contracts locked")
	}
	g.devSel = 6
	for i := range g.content.Events {
		press(t, g, "right")
		if g.snap.State.EventID != g.content.Events[i].ID {
			t.Fatal("wrong event")
		}
	}
	press(t, g, "right")
	if g.snap.State.EventID != "" {
		t.Fatal("event off failed")
	}
	press(t, g, "left")
	if len(g.content.Events) > 0 && g.snap.State.EventID != g.content.Events[len(g.content.Events)-1].ID {
		t.Fatal("event reverse failed")
	}
}

func TestDevPanelVisibleRowsAndMouse(t *testing.T) {
	g := devGame(t, true)
	toggleDev(g)
	for _, size := range [][2]int{{100, 38}, {80, 24}, {36, 10}} {
		g = resize(t, g, size[0], size[1])
		for selected := range devRows {
			g.devSel = selected
			output := view(g)
			assertFits(t, "dev panel", output, size[0])
			lines := strings.Split(output, "\n")
			found := false
			for _, box := range g.hits.Boxes() {
				if box.ID != "dev:"+itoa(selected) {
					continue
				}
				found = true
				if box.Y < 0 || box.Y >= len(lines) || !strings.Contains(stripAnsi(lines[box.Y]), "▸") {
					t.Fatalf("row %d at %v: hitbox missed selected row at %d:\n%s", selected, size, box.Y, stripAnsi(output))
				}
				g.dispatchHit(box, false)
			}
			if !found {
				t.Fatalf("selected row missing at %v", size)
			}
		}
	}
	g = resize(t, g, 100, 38)
	g.devSel = 0
	g.devInput = "123"
	g.handleMouseWheel(tea.Mouse{Button: tea.MouseWheelDown})
	if g.devSel != 1 || g.devInput != "" {
		t.Fatal("wheel selection failed")
	}
	g.dismissOverlay()
	if g.overlay != ovNone {
		t.Fatal("mouse close failed")
	}
}
