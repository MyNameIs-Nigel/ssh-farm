package tui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
)

// startEvent puts a known event on the save so the bar has something to draw.
// Events roll randomly every 15-25 minutes in real play, which is far too
// coarse to test against.
func startEvent(t *testing.T, g *Game, id string, startedAt, endsAt int64) *Game {
	t.Helper()
	st := g.snap.State
	st.EventID = id
	st.EventStartedAt = startedAt
	st.EventEndsAt = endsAt
	return g
}

func eventGame(t *testing.T) (*Game, int64) {
	t.Helper()
	f := newFixture(t)
	base := time.Now().Unix()
	g := f.newGame(t, base)
	g = dismissIntro(t, g)
	g, _ = tick(t, g, base)
	return resize(t, g, canvasMaxWidth, canvasMaxHeight), base
}

func TestEventBarIsEmptyWithNoEvent(t *testing.T) {
	g, _ := eventGame(t)
	if got := g.viewEventBar(); got != "" {
		t.Fatalf("event bar rendered with no active event: %q", got)
	}
}

// TestEventBarShowsNameEffectAndCountdown — the point of the redesign: an
// event used to be one quiet line, easy to miss in a 15-25 minute idle loop.
func TestEventBarShowsNameEffectAndCountdown(t *testing.T) {
	g, base := eventGame(t)
	g = startEvent(t, g, "market_day", base, base+120)

	bar := stripAnsi(g.viewEventBar())
	if bar == "" {
		t.Fatal("no event bar rendered for an active event")
	}
	for _, want := range []string{"MARKET DAY", "▰", "▱", "2m 00s"} {
		if !strings.Contains(bar, want) {
			t.Errorf("event bar missing %q:\n%s", want, bar)
		}
	}
}

func TestEventBarFitsContentWidth(t *testing.T) {
	g, base := eventGame(t)
	for _, size := range [][2]int{{canvasMaxWidth, canvasMaxHeight}, {80, 24}} {
		g = resize(t, g, size[0], size[1])
		g = startEvent(t, g, "market_day", base, base+120)
		bar := g.viewEventBar()
		if w := lipgloss.Width(bar); w > g.contentWidth() {
			t.Errorf("at %d×%d the event bar is %d wide, over contentWidth %d:\n%s",
				size[0], size[1], w, g.contentWidth(), stripAnsi(bar))
		}
	}
}

// TestEventBarDrains is what makes the countdown feel live on the 1 Hz tick.
func TestEventBarDrains(t *testing.T) {
	g, base := eventGame(t)
	g = startEvent(t, g, "market_day", base, base+100)

	g.now = base
	full := g.eventPct()
	g.now = base + 50
	half := g.eventPct()
	g.now = base + 99
	nearly := g.eventPct()

	if !(full > half && half > nearly) {
		t.Fatalf("event bar did not drain: %d%% -> %d%% -> %d%%", full, half, nearly)
	}
	if full != 100 {
		t.Errorf("at the start the bar should be full, got %d%%", full)
	}
}

// TestEventPctToleratesMissingStartTime covers saves written before
// EventStartedAt existed: the span is unknown, so the bar must not divide by
// zero or render as permanently empty.
func TestEventPctToleratesMissingStartTime(t *testing.T) {
	g, base := eventGame(t)
	g = startEvent(t, g, "market_day", 0, base+60)
	g.now = base

	got := g.eventPct()
	if got < 0 || got > 100 {
		t.Fatalf("eventPct with no start time = %d, want a value in [0,100]", got)
	}
	if bar := stripAnsi(g.viewEventBar()); !strings.Contains(bar, "MARKET DAY") {
		t.Fatalf("event bar broke with no start time:\n%s", bar)
	}
}

func TestEventPctClampsOutsideTheSpan(t *testing.T) {
	g, base := eventGame(t)
	g = startEvent(t, g, "market_day", base, base+100)

	g.now = base - 50 // clock skew
	if got := g.eventPct(); got < 0 || got > 100 {
		t.Errorf("before the span: eventPct = %d, want [0,100]", got)
	}
	g.now = base + 500 // past the end
	if got := g.eventPct(); got < 0 || got > 100 {
		t.Errorf("after the span: eventPct = %d, want [0,100]", got)
	}
}

// TestActiveEventRecoloursTheFrame — the canvas-wide accent is what makes an
// event impossible to miss even if the player is looking at another screen.
func TestActiveEventRecoloursTheFrame(t *testing.T) {
	g, base := eventGame(t)
	plain := g.View().Content

	g = startEvent(t, g, "market_day", base, base+120)
	during := g.View().Content

	if plain == during {
		t.Fatal("an active event did not change the rendered canvas at all")
	}
}

func TestEventBarAppearsOnEveryScreen(t *testing.T) {
	g, base := eventGame(t)
	g = startEvent(t, g, "market_day", base, base+120)
	for _, scr := range screenOrder {
		g.scr = scr
		if !strings.Contains(stripAnsi(view(g)), "MARKET DAY") {
			t.Errorf("screen %v does not show the active event", scr)
		}
	}
}

// TestEventEndProducesANotice — Events.EventEnded was previously computed by
// the sim and then dropped on the floor: nothing told the player their bonus
// was over.
func TestEventEndProducesANotice(t *testing.T) {
	g, base := eventGame(t)
	g = startEvent(t, g, "market_day", base, base+2)

	g, _ = tick(t, g, base+5) // advance past the end
	joined := stripAnsi(strings.Join(noticeTexts(g), " | "))
	if !strings.Contains(strings.ToLower(joined), "market day") {
		t.Fatalf("no notice when the event ended; notices were: %s", joined)
	}
}

func noticeTexts(g *Game) []string {
	out := make([]string, 0, len(g.notices))
	for _, n := range g.notices {
		out = append(out, n.text)
	}
	return out
}
