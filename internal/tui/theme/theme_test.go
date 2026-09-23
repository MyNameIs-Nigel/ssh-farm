package theme

import (
	"image/color"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripAnsi(s string) string { return ansiRe.ReplaceAllString(s, "") }

func TestPhaseAtWalksTheCycle(t *testing.T) {
	cases := []struct {
		name string
		now  int64
		want Phase
	}{
		{"epoch starts at dawn", 0, PhaseDawn},
		{"first second of dawn", 1, PhaseDawn},
		{"last second of dawn", PhaseSeconds - 1, PhaseDawn},
		{"first second of day", PhaseSeconds, PhaseDay},
		{"first second of dusk", 2 * PhaseSeconds, PhaseDusk},
		{"first second of night", 3 * PhaseSeconds, PhaseNight},
		{"last second of night", 4*PhaseSeconds - 1, PhaseNight},
		{"cycle wraps back to dawn", 4 * PhaseSeconds, PhaseDawn},
		{"second cycle day", 5 * PhaseSeconds, PhaseDay},
		{"far future wraps by cycle count", 987656 * PhaseSeconds, PhaseDawn},
		{"far future mid-cycle", 987654 * PhaseSeconds, PhaseDusk},
	}
	for _, c := range cases {
		if got := PhaseAt(c.now); got != c.want {
			t.Errorf("%s: PhaseAt(%d) = %v, want %v", c.name, c.now, got, c.want)
		}
	}
}

// TestPhaseAtClampsNegativeTime guards the pre-epoch case: Go's % keeps the
// sign of the dividend, so a naive (now/PhaseSeconds)%4 returns a negative
// phase and indexes the palette out of range.
func TestPhaseAtNegativeTimeStaysInRange(t *testing.T) {
	for _, now := range []int64{-1, -PhaseSeconds, -3 * PhaseSeconds, -1234567} {
		got := PhaseAt(now)
		if got < PhaseDawn || got > PhaseNight {
			t.Fatalf("PhaseAt(%d) = %v, want a phase in [%v,%v]", now, got, PhaseDawn, PhaseNight)
		}
	}
}

func TestEveryPhaseHasAName(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range []Phase{PhaseDawn, PhaseDay, PhaseDusk, PhaseNight} {
		name := p.Name()
		if name == "" {
			t.Errorf("phase %v has no name", p)
		}
		if seen[name] {
			t.Errorf("phase %v reuses the name %q", p, name)
		}
		seen[name] = true
	}
}

// styleFields returns every lipgloss.Style field on a Theme by name, so tests
// that must hold for *all* styles cannot be silently outgrown by a new field.
func styleFields(t *testing.T, th Theme) map[string]lipgloss.Style {
	t.Helper()
	out := map[string]lipgloss.Style{}
	v := reflect.ValueOf(th)
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if !f.CanInterface() {
			continue // unexported palette bookkeeping
		}
		if st, ok := f.Interface().(lipgloss.Style); ok {
			out[v.Type().Field(i).Name] = st
		}
	}
	if len(out) == 0 {
		t.Fatal("no lipgloss.Style fields found on Theme")
	}
	return out
}

// TestEveryStyleCarriesABackground is the anti-hole test. A foreground-only
// style punches the terminal's own background through the canvas, which is
// exactly the bug this whole feature exists to fix, so it must hold for every
// style in every phase — including any style added later.
func TestEveryStyleCarriesABackground(t *testing.T) {
	for _, solid := range []bool{false, true} {
		for _, p := range []Phase{PhaseDawn, PhaseDay, PhaseDusk, PhaseNight} {
			for _, ev := range []string{"", "market_day"} {
				th := New(p, solid, ev)
				for name, st := range styleFields(t, th) {
					if bg := st.GetBackground(); bg == (lipgloss.NoColor{}) {
						t.Errorf("phase=%v solid=%v event=%q: style %s has no background",
							p, solid, ev, name)
					}
				}
			}
		}
	}
}

func TestEveryStyleCarriesAForeground(t *testing.T) {
	th := New(PhaseNight, false, "")
	for name, st := range styleFields(t, th) {
		// Frame and Box colour their border, not their text.
		if name == "Frame" || name == "Box" || name == "PlotCard" || name == "PlotSel" {
			continue
		}
		if fg := st.GetForeground(); fg == (lipgloss.NoColor{}) {
			t.Errorf("style %s has no foreground", name)
		}
	}
}

func TestAutoModeGivesEveryPhaseItsOwnBackground(t *testing.T) {
	seen := map[color.Color]Phase{}
	for _, p := range []Phase{PhaseDawn, PhaseDay, PhaseDusk, PhaseNight} {
		bg := New(p, false, "").Bg
		if prev, dup := seen[bg]; dup {
			t.Errorf("phase %v reuses phase %v's background %v — the cycle would be invisible", p, prev, bg)
		}
		seen[bg] = p
	}
}

// TestSolidModePinsTheBackground is the setting the player asked for: nothing
// moves the background, not the cycle and not an active event.
func TestSolidModePinsTheBackground(t *testing.T) {
	want := New(PhaseDawn, true, "").Bg
	for _, p := range []Phase{PhaseDawn, PhaseDay, PhaseDusk, PhaseNight} {
		for _, ev := range []string{"", "market_day", "warm_front", "bumper_demand"} {
			if got := New(p, true, ev).Bg; got != want {
				t.Errorf("solid mode: phase=%v event=%q background = %v, want a pinned %v", p, ev, got, want)
			}
		}
	}
}

// TestActiveEventLiftsTheCanvasInAutoMode: the event has to be visible on the
// canvas itself, not only in the banner.
func TestActiveEventLiftsTheCanvasInAutoMode(t *testing.T) {
	for _, p := range []Phase{PhaseDawn, PhaseDay, PhaseDusk, PhaseNight} {
		plain := New(p, false, "").Bg
		during := New(p, false, "market_day").Bg
		if plain == during {
			t.Errorf("phase %v: background unchanged during an event (%v)", p, plain)
		}
	}
}

// TestSolidModeStillShowsEventAccent — pinning the background must not also
// mute the event feedback; only the day/night drift is switched off.
func TestSolidModeStillShowsEventAccent(t *testing.T) {
	plain := New(PhaseDay, true, "").Frame.GetBorderTopForeground()
	during := New(PhaseDay, true, "market_day").Frame.GetBorderTopForeground()
	if plain == during {
		t.Fatalf("solid mode: frame border did not take the event accent (both %v)", plain)
	}
}

func TestEventAccentIsDistinctPerEvent(t *testing.T) {
	seen := map[color.Color]string{}
	for _, id := range []string{"market_day", "bumper_demand", "warm_front"} {
		c := EventAccent(id)
		if c == (lipgloss.NoColor{}) {
			t.Errorf("event %q has no accent colour", id)
		}
		if prev, dup := seen[c]; dup {
			t.Errorf("event %q reuses %q's accent %v", id, prev, c)
		}
		seen[c] = id
	}
}

func TestEventAccentFallsBackForUnknownEvent(t *testing.T) {
	// Content is data-driven, so an event id the theme has never heard of must
	// still get a usable accent rather than a hole.
	if c := EventAccent("some_future_event"); c == (lipgloss.NoColor{}) {
		t.Fatal("unknown event id produced no accent colour")
	}
}

// --- Paint -----------------------------------------------------------------

func TestPaintReassertsAfterEveryReset(t *testing.T) {
	th := New(PhaseNight, false, "")
	inner := lipgloss.NewStyle().Foreground(lipgloss.Color("120")).Render("ready")
	line := "before " + inner + " after"

	if strings.Count(line, "\x1b[m")+strings.Count(line, "\x1b[0m") == 0 {
		t.Fatalf("fixture does not contain a reset to re-assert: %q", line)
	}

	got := th.Paint(line)
	// Every reset must be immediately followed by the palette being re-set,
	// or the rest of that line renders on the terminal's own background.
	for _, reset := range []string{"\x1b[m", "\x1b[0m"} {
		idx := 0
		for {
			i := strings.Index(got[idx:], reset)
			if i < 0 {
				break
			}
			at := idx + i + len(reset)
			if !strings.HasPrefix(got[at:], "\x1b[") {
				t.Fatalf("reset at %d is not followed by a re-assert in %q", at, got)
			}
			idx = at
		}
	}
}

func TestPaintOpensWithThePalette(t *testing.T) {
	th := New(PhaseNight, false, "")
	got := th.Paint("plain")
	if !strings.HasPrefix(got, "\x1b[") {
		t.Fatalf("Paint output does not open with an SGR sequence: %q", got)
	}
	if stripAnsi(got) != "plain" {
		t.Fatalf("Paint changed the text: %q", stripAnsi(got))
	}
}

// TestPaintPreservesWidthAndText is the test that protects every existing
// layout assertion in internal/tui. Paint runs over the fully composed frame,
// so if it changed printable width or text content it would break
// TestComposedCanvasIsRectangular, assertFits and every hitbox test at once.
func TestPaintPreservesWidthAndText(t *testing.T) {
	th := New(PhaseDay, false, "")
	cases := []string{
		"",
		"plain text",
		lipgloss.NewStyle().Foreground(lipgloss.Color("120")).Render("styled"),
		lipgloss.NewStyle().Bold(true).Background(lipgloss.Color("54")).Render("chip"),
		"multi\nline\ntext",
		lipgloss.NewStyle().Foreground(lipgloss.Color("222")).Render("a") + "\n" +
			lipgloss.NewStyle().Foreground(lipgloss.Color("114")).Render("bb"),
		"🌾 wide runes ▰▱ 🌕",
		strings.Repeat("─", 40),
	}
	for _, in := range cases {
		got := th.Paint(in)
		if lipgloss.Width(got) != lipgloss.Width(in) {
			t.Errorf("Paint(%q): width %d, want %d", in, lipgloss.Width(got), lipgloss.Width(in))
		}
		if lipgloss.Height(got) != lipgloss.Height(in) {
			t.Errorf("Paint(%q): height %d, want %d", in, lipgloss.Height(got), lipgloss.Height(in))
		}
		if stripAnsi(got) != stripAnsi(in) {
			t.Errorf("Paint(%q): stripped text = %q, want %q", in, stripAnsi(got), stripAnsi(in))
		}
	}
}

// TestPaintIsIdempotentInWidth — Paint may run over already-painted content
// during development; it must never accumulate printable characters.
func TestPaintIsIdempotentInWidth(t *testing.T) {
	th := New(PhaseDay, false, "")
	in := lipgloss.NewStyle().Foreground(lipgloss.Color("120")).Render("twice")
	once := th.Paint(in)
	twice := th.Paint(once)
	if stripAnsi(once) != stripAnsi(twice) || lipgloss.Width(once) != lipgloss.Width(twice) {
		t.Fatalf("Paint is not width-idempotent: %q vs %q", stripAnsi(once), stripAnsi(twice))
	}
}

func TestPaintUsesTheThemeBackground(t *testing.T) {
	night := New(PhaseNight, false, "").Paint("x")
	day := New(PhaseDay, false, "").Paint("x")
	if night == day {
		t.Fatal("Paint emitted identical output for night and day — the palette is not being used")
	}
}

// The leaderboard name styles are methods rather than fields, so the
// anti-hole test above cannot see them; they get the same guarantee here.
func TestLeaderboardNameStylesCarryTheCanvasBackground(t *testing.T) {
	for _, p := range []Phase{PhaseDawn, PhaseDay, PhaseDusk, PhaseNight} {
		th := New(p, false, "market_day")
		styles := map[string]lipgloss.Style{}
		for id := range leaderboardNameColors {
			st, ok := th.LeaderboardName(id)
			if !ok {
				t.Fatalf("LeaderboardName(%q) not found", id)
			}
			styles[id] = st
		}
		for step := range NameWavePeriod {
			styles["wave "+strconv.Itoa(step)] = th.NameWave(step)
		}
		for name, st := range styles {
			if st.GetBackground() != th.Bg {
				t.Errorf("phase %v: %s does not paint the canvas background", p, name)
			}
		}
	}
	if _, ok := New(PhaseDay, false, "").LeaderboardName(""); ok {
		t.Error("the traditional style must fall through to the row's own colours")
	}
}

// The wave runs out along the ramp and back, so neighbouring characters and
// neighbouring frames never jump more than one stop, and any step is valid.
func TestNameWaveIsAContinuousLoop(t *testing.T) {
	th := New(PhaseNight, false, "")
	stop := func(step int) int {
		fg := th.NameWave(step).GetForeground()
		for i, c := range nameWaveStops {
			if lipgloss.Color(c) == fg {
				return i
			}
		}
		t.Fatalf("step %d: colour is not a ramp stop", step)
		return -1
	}
	for step := -NameWavePeriod; step < 2*NameWavePeriod; step++ {
		if d := stop(step+1) - stop(step); d != 1 && d != -1 {
			t.Fatalf("step %d→%d jumps %d stops", step, step+1, d)
		}
		if stop(step) != stop(step+NameWavePeriod) {
			t.Fatalf("step %d does not repeat after one period", step)
		}
	}
}
