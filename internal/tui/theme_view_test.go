package tui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-farm/internal/tui/theme"
)

// bgRunLengths walks a rendered line and reports, for each printable cell,
// whether a background colour was in force. A cell with no background is a
// hole through which the terminal's own (possibly white) background shows.
func cellsWithoutBackground(line string) int {
	missing := 0
	bgActive := false
	i := 0
	for i < len(line) {
		if line[i] == 0x1b {
			// Consume one SGR sequence and update whether a bg is in force.
			j := i
			for j < len(line) && line[j] != 'm' {
				j++
			}
			if j >= len(line) {
				break
			}
			seq := line[i : j+1]
			switch {
			case seq == "\x1b[m" || seq == "\x1b[0m":
				bgActive = false
			case strings.Contains(seq, "48;5;") || strings.Contains(seq, "48;2;"):
				bgActive = true
			}
			i = j + 1
			continue
		}
		r := []rune(line[i:])[0]
		if !bgActive {
			missing++
		}
		i += len(string(r))
	}
	return missing
}

// TestEveryRenderedCellHasABackground is the anti-striping test, and the
// reason this feature needs more than a Background() on the frame. lipgloss
// closes every styled span with a reset, which drops the parent background
// for the rest of the line — so without a re-assert pass the canvas renders
// as bars of dark and bars of the user's own terminal colour.
func TestEveryRenderedCellHasABackground(t *testing.T) {
	f := newFixture(t)
	base := time.Now().Unix()
	g := f.newGame(t, base)
	g = dismissIntro(t, g)
	g, _ = tick(t, g, base)
	g = resize(t, g, canvasMaxWidth, canvasMaxHeight)

	for _, scr := range screenOrder {
		g.scr = scr
		g.overlay = ovNone
		for i, line := range strings.Split(view(g), "\n") {
			if n := cellsWithoutBackground(line); n > 0 {
				t.Fatalf("screen %v line %d: %d cells have no background:\n%q",
					scr, i, n, stripAnsi(line))
			}
		}
	}
}

func TestEveryOverlayRendersOnABackground(t *testing.T) {
	f := newFixture(t)
	base := time.Now().Unix()
	g := f.newGame(t, base)
	g, _ = tick(t, g, base)
	g = resize(t, g, canvasMaxWidth, canvasMaxHeight)

	for _, ov := range []overlay{ovTutorial, ovAway, ovPicker, ovUpgrade, ovRebirthConfirm, ovName, ovConfig, ovReplantWarn, ovKicked} {
		g.overlay = ov
		for i, line := range strings.Split(view(g), "\n") {
			if n := cellsWithoutBackground(line); n > 0 {
				t.Fatalf("overlay %v line %d: %d cells have no background:\n%q",
					ov, i, n, stripAnsi(line))
			}
		}
	}
}

// TestTinyTerminalGuardIsAlsoPainted — the "resize me" screen is exactly the
// screen a player on a bad terminal sees first, so it must not be the one
// place that renders unstyled on white.
func TestTinyTerminalGuardIsAlsoPainted(t *testing.T) {
	f := newFixture(t)
	base := time.Now().Unix()
	g := resize(t, f.newGame(t, base), 20, 6)
	for i, line := range strings.Split(view(g), "\n") {
		if n := cellsWithoutBackground(line); n > 0 {
			t.Fatalf("tiny guard line %d: %d cells have no background:\n%q", i, n, stripAnsi(line))
		}
	}
}

// TestViewSetsTerminalBackgroundOnEveryPath mirrors the AltScreen sweep: OSC
// 11 makes resets fall back to our dark instead of the terminal's default, so
// a path that forgets it renders differently from every other path.
func TestViewSetsTerminalBackgroundOnEveryPath(t *testing.T) {
	f := newFixture(t)
	base := time.Now().Unix()
	g := f.newGame(t, base)
	g, _ = tick(t, g, base)

	for _, ov := range []overlay{ovTutorial, ovAway, ovPicker, ovConfig, ovNone} {
		g.overlay = ov
		if g.View().BackgroundColor == nil {
			t.Errorf("overlay %v: View must set BackgroundColor", ov)
		}
		if g.View().ForegroundColor == nil {
			t.Errorf("overlay %v: View must set ForegroundColor", ov)
		}
	}
	g.overlay = ovNone
	for _, scr := range screenOrder {
		g.scr = scr
		if g.View().BackgroundColor == nil {
			t.Errorf("screen %v: View must set BackgroundColor", scr)
		}
	}
	tiny := resize(t, g, 20, 6)
	if tiny.View().BackgroundColor == nil {
		t.Error("tiny guard: View must set BackgroundColor")
	}
	if NewErrScreen().View().BackgroundColor == nil {
		t.Error("errScreen: View must set BackgroundColor")
	}
}

// TestBackgroundFollowsTheDayNightCycle — the whole point of the cycle is
// that it is visible, so two phases must not render identically.
func TestBackgroundFollowsTheDayNightCycle(t *testing.T) {
	f := newFixture(t)
	base := int64(0) // PhaseDawn
	g := f.newGame(t, base)
	g = dismissIntro(t, g)

	seen := map[string]theme.Phase{}
	for _, p := range []theme.Phase{theme.PhaseDawn, theme.PhaseDay, theme.PhaseDusk, theme.PhaseNight} {
		at := int64(p) * theme.PhaseSeconds
		g, _ = tick(t, g, at)
		if got := theme.PhaseAt(g.now); got != p {
			t.Fatalf("fixture error: at %d the phase is %v, want %v", at, got, p)
		}
		out := view(g)
		if prev, dup := seen[out]; dup {
			t.Fatalf("phase %v renders identically to phase %v — the cycle is invisible", p, prev)
		}
		seen[out] = p
	}
}

// TestSolidSettingStopsTheBackgroundChanging is the player's escape hatch.
func TestSolidSettingStopsTheBackgroundChanging(t *testing.T) {
	f := newFixture(t)
	g := f.newGame(t, 0)
	g = dismissIntro(t, g)
	g, _ = tick(t, g, 0)
	g.snap.State.ThemeSolid = true

	var want any
	for _, p := range []theme.Phase{theme.PhaseDawn, theme.PhaseDay, theme.PhaseDusk, theme.PhaseNight} {
		g, _ = tick(t, g, int64(p)*theme.PhaseSeconds)
		g.snap.State.ThemeSolid = true
		got := g.View().BackgroundColor
		if want == nil {
			want = got
			continue
		}
		if got != want {
			t.Fatalf("solid mode: phase %v changed the background to %v, want a pinned %v", p, got, want)
		}
	}
}

// TestPaintDoesNotDisturbGeometry pins the invariant the whole existing test
// suite rests on: painting adds SGR only, so widths and stripped text are
// unchanged. If this fails, expect assertFits and every hitbox test to fail too.
func TestPaintDoesNotDisturbGeometry(t *testing.T) {
	f := newFixture(t)
	base := time.Now().Unix()
	g := f.newGame(t, base)
	g = dismissIntro(t, g)
	g, _ = tick(t, g, base)
	g = resize(t, g, canvasMaxWidth, canvasMaxHeight)

	th := g.theme()
	for _, scr := range screenOrder {
		g.scr = scr
		body := g.screenBody()
		painted := th.Paint(body)
		if lipgloss.Width(painted) != lipgloss.Width(body) {
			t.Errorf("screen %v: paint changed width %d -> %d", scr, lipgloss.Width(body), lipgloss.Width(painted))
		}
		if stripAnsi(painted) != stripAnsi(body) {
			t.Errorf("screen %v: paint changed the text", scr)
		}
	}
}
