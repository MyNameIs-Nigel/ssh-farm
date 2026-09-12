package tui

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mynameis-nigel/ssh-farm/internal/sim"
	"github.com/mynameis-nigel/ssh-farm/internal/tui/theme"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/golden")

// goldenBase is a fixed timestamp so every golden is reproducible. It is a
// multiple of the phase length, so goldenBase itself sits at the start of
// PhaseDawn and each phase is reachable by adding whole phases.
const goldenBase int64 = 1_700_000_000 - (1_700_000_000 % (4 * theme.PhaseSeconds))

// goldenGame builds a fully deterministic game. A fresh save is created with a
// random RNG seed and the fixture clock is time.Now(), so the snapshot is
// replaced wholesale with a known state — goldens must not depend on when the
// suite happens to run.
func goldenGame(t *testing.T, w, h int) *Game {
	t.Helper()
	f := newFixture(t)
	g := f.newGame(t, goldenBase)
	g = dismissIntro(t, g)
	g, _ = tick(t, g, goldenBase)

	g.snap.State = sim.New(f.content, 7, goldenBase)
	g.snap.Now = goldenBase
	g.now = goldenBase
	g.notices = nil
	g.scr = scrFarm
	g.overlay = ovNone
	return resize(t, g, w, h)
}

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".txt")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v\n\nrun: go test ./internal/tui -run TestGolden -update", err)
	}
	if got == string(want) {
		return
	}
	// Show the visible diff first — a styled golden is unreadable raw.
	gotLines := strings.Split(stripAnsi(got), "\n")
	wantLines := strings.Split(stripAnsi(string(want)), "\n")
	for i := 0; i < len(gotLines) || i < len(wantLines); i++ {
		var gl, wl string
		if i < len(gotLines) {
			gl = gotLines[i]
		}
		if i < len(wantLines) {
			wl = wantLines[i]
		}
		if gl != wl {
			t.Errorf("%s: first visible difference on line %d:\n got: %q\nwant: %q", name, i, gl, wl)
			break
		}
	}
	if stripAnsi(got) == stripAnsi(string(want)) {
		t.Errorf("%s: text matches but styling changed (this is a palette change)", name)
	}
	t.Fatalf("%s differs from its golden; re-run with -update to accept", name)
}

// TestGoldenRenders pins the styled output of the screens this feature
// changes. Goldens store the ANSI, not just the text, because the whole point
// of the change is what colour each cell is — a text-only golden would pass
// even if the background vanished entirely.
func TestGoldenRenders(t *testing.T) {
	cases := []struct {
		name  string
		w, h  int
		setup func(g *Game)
	}{
		{"farm-dawn", canvasMaxWidth, canvasMaxHeight, nil},
		{"farm-day", canvasMaxWidth, canvasMaxHeight, func(g *Game) {
			g.now = goldenBase + theme.PhaseSeconds
		}},
		{"farm-night", canvasMaxWidth, canvasMaxHeight, func(g *Game) {
			g.now = goldenBase + 3*theme.PhaseSeconds
		}},
		{"farm-solid", canvasMaxWidth, canvasMaxHeight, func(g *Game) {
			g.now = goldenBase + theme.PhaseSeconds
			g.snap.State.ThemeSolid = true
		}},
		{"farm-event", canvasMaxWidth, canvasMaxHeight, func(g *Game) {
			st := g.snap.State
			st.EventID = "market_day"
			st.EventStartedAt = goldenBase
			st.EventEndsAt = goldenBase + 120
		}},
		// The reported environment: a stock 80x24 terminal, which is below
		// the recommended 100x30 and so renders the Help tab as a warning.
		{"farm-night-80x24", 80, 24, func(g *Game) {
			g.now = goldenBase + 3*theme.PhaseSeconds
		}},
		{"settings-80x24", 80, 24, func(g *Game) {
			g.overlay = ovConfig
		}},
		// Festival goldens pin the seasonal palette, sky row and glyphs. The
		// dates are fixed, so headlines and sky variants are deterministic.
		{"farm-halloween", canvasMaxWidth, canvasMaxHeight, func(g *Game) {
			g.SetTrueColor(true)
			g.now = alignToPhase(festivalUnix(2024, 10, 15), theme.PhaseDay)
		}},
		{"farm-halloween-night", canvasMaxWidth, canvasMaxHeight, func(g *Game) {
			g.SetTrueColor(true)
			g.now = alignToPhase(festivalUnix(2024, 10, 15), theme.PhaseNight)
		}},
		{"farm-christmas-night", canvasMaxWidth, canvasMaxHeight, func(g *Game) {
			g.SetTrueColor(true)
			g.now = alignToPhase(festivalUnix(2024, 12, 10), theme.PhaseNight)
		}},
		{"farm-christmas-day", canvasMaxWidth, canvasMaxHeight, func(g *Game) {
			g.SetTrueColor(true)
			g.now = alignToPhase(festivalUnix(2024, 12, 25), theme.PhaseDay)
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := goldenGame(t, c.w, c.h)
			if c.setup != nil {
				c.setup(g)
			}
			assertGolden(t, c.name, view(g))
		})
	}
}

// TestGoldenRendersAreDeterministic catches the failure mode that makes a
// golden suite worthless: a render that varies run to run would otherwise
// only show up as a mysterious CI failure later.
func TestGoldenRendersAreDeterministic(t *testing.T) {
	a := view(goldenGame(t, canvasMaxWidth, canvasMaxHeight))
	b := view(goldenGame(t, canvasMaxWidth, canvasMaxHeight))
	if a != b {
		t.Fatal("two identically-built games rendered differently; the golden fixture is not deterministic")
	}
}
