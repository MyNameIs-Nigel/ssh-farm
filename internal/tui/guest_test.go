package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-farm/internal/game"
	"github.com/mynameis-nigel/ssh-farm/internal/identity"
)

func TestGuestIdentityLocksAndExit(t *testing.T) {
	f := newFixture(t)
	f.id = identity.SessionIdentity{Kind: identity.Guest, GuestID: strings.Repeat("c", 32)}
	res, err := f.mgr.Attach(t.Context(), f.id, "", 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Session.Detach()
	g := NewGame(f.id, res, f.content, f.board, 100, 35, 100, 0)
	g, _ = tick(t, g, 100)
	g.overlay = ovNone
	if h := stripAnsi(g.viewHeader()); !strings.Contains(h, "ssh-farm") || !strings.Contains(h, "GUEST") || !strings.Contains(h, "NOT SAVED") {
		t.Fatal(h)
	}
	g.scr = scrStats
	press(t, g, "n")
	if g.overlay == ovName {
		t.Fatal("guest rename opened")
	}
	if !strings.Contains(g.viewNotices(), "SSH key") {
		t.Fatal("missing rename explanation")
	}
	g.refreshBoard()
	if g.lbBoard.You != nil || !strings.Contains(g.boardHeaderLine(g.contentWidth()), "Guests are not ranked") {
		t.Fatal("guest rank")
	}
	press(t, g, "q")
	if g.overlay != ovGuestQuit {
		t.Fatal("missing discard confirmation")
	}
	if !strings.Contains(g.overlayBox(), "discarded") {
		t.Fatal("missing discard warning")
	}
	g.View()
	press(t, g, "esc")
	if g.overlay != ovNone {
		t.Fatal("cannot cancel exit")
	}
	snap, err := res.Session.SetThemeSolid(101, true)
	if err != nil || !snap.State.ThemeSolid {
		t.Fatal("guest settings unavailable", err)
	}
	if _, err = res.Session.SetFarmName(102, "NAME"); !errors.Is(err, game.ErrGuestRequiresKey) {
		t.Fatal(err)
	}
}

func TestSeasonalCanvasRemainsRectangular(t *testing.T) {
	f := newFixture(t)
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC).Unix()
	g := f.newGame(t, now)
	g, _ = tick(t, g, now)
	g.overlay = ovNone
	for _, size := range [][2]int{{80, 24}, {100, 38}} {
		g = resize(t, g, size[0], size[1])
		out := g.composeCanvas(g.screenBody(), false)
		for _, line := range strings.Split(out, "\n") {
			if width := lipgloss.Width(line); width != size[0] {
				t.Fatalf("seasonal row width=%d", width)
			}
		}
		assertFits(t, "seasonal", out, size[0])
	}
}

func TestKickedGuestCtrlCExitsWithoutDiscardDialog(t *testing.T) {
	f := newFixture(t)
	f.id = identity.SessionIdentity{Kind: identity.Guest, GuestID: strings.Repeat("e", 32)}
	res, err := f.mgr.Attach(t.Context(), f.id, "", 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Session.Detach()
	g := NewGame(f.id, res, f.content, f.board, 100, 35, 100, 0)
	g.overlay = ovKicked
	_, cmd := g.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("kicked guest was offered a cancelable exit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("kicked guest did not quit")
	}
	if g.overlay != ovKicked {
		t.Fatal("terminal kick notice was replaced")
	}
}
