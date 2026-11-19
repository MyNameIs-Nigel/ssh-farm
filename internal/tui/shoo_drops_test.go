package tui

import (
	"strings"
	"testing"

	"github.com/mynameis-nigel/ssh-farm/internal/sim"
)

// shooFarm is a saved farm with a crow on plot 0 and a growing turnip on
// plot 1, with the shoo drop chances forced, so x goes through the real
// session and the notice is what a player would read.
func shooFarm(t *testing.T, rebirths int64, starseedBp, fertilizerBp int64, mutate func(st *sim.State)) *Game {
	t.Helper()
	g, _ := savedFarm(t, func(st *sim.State) {
		st.Rebirths = rebirths
		st.Plots[0] = sim.Plot{Critter: "crow"}
		st.Plots[1] = sim.Plot{Crop: "turnip", PlantedAt: st.UpdatedAt}
		if mutate != nil {
			mutate(st)
		}
	})
	g.content.Critters.StarseedChanceBp = starseedBp
	g.content.Critters.FertilizerChanceBp = fertilizerBp
	return g
}

func lastNotice(t *testing.T, g *Game) string {
	t.Helper()
	n := noticeTexts(g)
	if len(n) == 0 {
		t.Fatal("no notice after shooing")
	}
	return n[len(n)-1]
}

func TestShooNotices(t *testing.T) {
	t.Run("plain shoo says nothing extra", func(t *testing.T) {
		g := shooFarm(t, 1, 0, 0, nil)
		g = press(t, g, "x")
		n := lastNotice(t, g)
		if !strings.HasPrefix(n, "Shooed the crow (+") || !strings.HasSuffix(n, " coins).") || strings.Contains(n, "it dropped") {
			t.Fatalf("notice %q", n)
		}
		if g.snap.State.Plots[0].Critter != "" {
			t.Fatal("critter still on the plot")
		}
	})

	t.Run("fertilizer is banked and counted", func(t *testing.T) {
		g := shooFarm(t, 0, 0, 10000, nil)
		g = press(t, g, "x")
		if n := lastNotice(t, g); !strings.Contains(n, "it dropped Fertilizer! ϟ2/3 (F to use)") {
			t.Fatalf("notice %q", n)
		}
		if got := g.snap.State.Fertilizer(); got != 2 {
			t.Fatalf("holds %d, want 2", got)
		}
		if !strings.Contains(stripAnsi(g.viewHeader()), "ϟ2/3") {
			t.Fatal("header did not update to ϟ2/3")
		}
	})

	t.Run("full sack ripens the growing crop", func(t *testing.T) {
		g := shooFarm(t, 0, 0, 10000, func(st *sim.State) { st.FertilizerHeld, st.FertilizerTracked = 3, true })
		g = press(t, g, "x")
		if n := lastNotice(t, g); !strings.Contains(n, "Sack full, so 1 crop ripened.") {
			t.Fatalf("notice %q", n)
		}
		if !g.snap.State.PlotReady(g.content, 1, g.now) {
			t.Fatal("the turnip did not ripen")
		}
	})

	t.Run("starseed shows in the notice and the wallet", func(t *testing.T) {
		g := shooFarm(t, 1, 10000, 0, nil)
		before := g.snap.State.PrestigeCurrency
		g = press(t, g, "x")
		want := "dropped 1 " + g.starseedLabel() + "!"
		if n := lastNotice(t, g); !strings.Contains(n, want) {
			t.Fatalf("notice %q, want it to contain %q", n, want)
		}
		if got := g.snap.State.PrestigeCurrency; got != before+1 {
			t.Fatalf("starseeds %d→%d, want +1", before, got)
		}
	})
}
