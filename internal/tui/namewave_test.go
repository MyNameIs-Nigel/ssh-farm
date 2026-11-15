package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/mynameis-nigel/ssh-farm/internal/game"
	"github.com/mynameis-nigel/ssh-farm/internal/leaderboard"
	"github.com/mynameis-nigel/ssh-farm/internal/sim"
)

func waveStyles() []string {
	var out []string
	for _, s := range (&sim.State{ContractsCompleted: 3}).AvailableLeaderboardNameStyles() {
		if sim.IsWaveNameStyle(s) {
			out = append(out, s)
		}
	}
	return out
}

// Every colour's wave (issue #59) must animate on the Board, not only
// purple's: the tick chain starts for it and the name changes with phase.
func TestEveryWaveStyleAnimatesOnTheBoard(t *testing.T) {
	waves := waveStyles()
	if len(waves) != 5 {
		t.Fatalf("wave styles = %v, want one per colour", waves)
	}
	seen := map[string]string{}
	for _, style := range waves {
		g := &Game{scr: scrBoard, snap: game.Snapshot{State: &sim.State{}}, lbBoard: leaderboard.Board{Top: []leaderboard.Row{{NameStyle: style}}}}
		if cmd := g.startBoardAnimation(); cmd == nil {
			t.Errorf("%s: a visible wave row should start the tick chain", style)
		}
		first := g.renderBoardName("WAVY FARM", style, g.theme().Value)
		if stripAnsi(first) != "WAVY FARM" {
			t.Fatalf("%s: wave changed the name's text: %q", style, stripAnsi(first))
		}
		static, _ := g.theme().LeaderboardName(sim.NameStyleBase(style))
		if first == static.Render("WAVY FARM") {
			t.Errorf("%s: renders as its static colour, not a wave", style)
		}
		g.boardAnimPhase = 3
		if g.renderBoardName("WAVY FARM", style, g.theme().Value) == first {
			t.Errorf("%s: the wave did not move with the animation phase", style)
		}
		if other, dup := seen[first]; dup {
			t.Errorf("%s draws the same wave as %s", style, other)
		}
		seen[first] = style
	}
}

// A 16-colour terminal holds every wave on its own static colour.
func TestLimitedColourHoldsEachWaveOnItsColour(t *testing.T) {
	for _, style := range waveStyles() {
		g := &Game{scr: scrBoard, snap: game.Snapshot{State: &sim.State{}}, lbBoard: leaderboard.Board{Top: []leaderboard.Row{{NameStyle: style}}}}
		m, _ := g.Update(tea.ColorProfileMsg{Profile: colorprofile.ANSI})
		g = m.(*Game)
		static, _ := g.theme().LeaderboardName(sim.NameStyleBase(style))
		for _, phase := range []int{0, 5} {
			g.boardAnimPhase = phase
			if got := g.renderBoardName("WAVY", style, g.theme().Value); got != static.Render("WAVY") {
				t.Errorf("%s phase %d: limited colour should show the static %s", style, phase, sim.NameStyleBase(style))
			}
		}
	}
}

// On Stats, l cycles colours and L turns the wave off and on again; both
// go through the session, so the choice is saved.
func TestStatsShiftLTogglesTheWave(t *testing.T) {
	g := contractGame(t, 3)
	g.scr = scrStats
	if out := stripAnsi(view(g)); !strings.Contains(out, "L wave") {
		t.Fatalf("Stats does not offer the wave toggle:\n%s", out)
	}
	g = press(t, g, `l`)
	if got := g.snap.State.LeaderboardNameStyle; got != sim.NameStyleLeafWave {
		t.Fatalf("first colour after contract 3 = %q, want the leaf wave", got)
	}
	if out := stripAnsi(view(g)); !strings.Contains(out, "Board style: leaf wave") {
		t.Fatalf("Stats does not name the wave style:\n%s", out)
	}
	g = press(t, g, `L`)
	if got := g.snap.State.LeaderboardNameStyle; got != sim.NameStyleLeaf {
		t.Fatalf("L on the leaf wave = %q, want static leaf", got)
	}
	g = press(t, g, `l`)
	if got := g.snap.State.LeaderboardNameStyle; got != sim.NameStyleGold {
		t.Fatalf("l after turning the wave off = %q, want static gold", got)
	}
	g = press(t, g, `L`)
	if got := g.snap.State.LeaderboardNameStyle; got != sim.NameStyleGoldWave {
		t.Fatalf("L on static gold = %q, want the gold wave", got)
	}
}

// Before contract 3 there is no wave to toggle: L is not offered and does
// nothing, rather than surfacing a "locked" error for an unadvertised key.
func TestStatsShiftLIsInertBeforeTheWave(t *testing.T) {
	g := contractGame(t, 2)
	g.scr = scrStats
	if out := stripAnsi(view(g)); strings.Contains(out, "L wave") {
		t.Fatalf("Stats offers the wave before contract 3:\n%s", out)
	}
	g = press(t, g, `l`)
	before := strings.Join(noticeTexts(g), "|")
	g = press(t, g, `L`)
	if got := g.snap.State.LeaderboardNameStyle; got != sim.NameStyleLeaf {
		t.Fatalf("style = %q, want leaf", got)
	}
	if after := strings.Join(noticeTexts(g), "|"); after != before {
		t.Fatalf("L before contract 3 raised notices: %q", after)
	}
}
