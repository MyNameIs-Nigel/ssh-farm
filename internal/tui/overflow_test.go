package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-farm/internal/leaderboard"
)

// designGame returns a game rendered at the maximum canvas size. The canvas
// clamps to canvasMaxWidth/Height, so this is the layout every terminal at or
// above that size sees — i.e. what essentially all real sessions render.
func designGame(t *testing.T) *Game {
	t.Helper()
	f := newFixture(t)
	base := time.Now().Unix()
	g := f.newGame(t, base)
	g = dismissIntro(t, g)
	g, _ = tick(t, g, base)
	return resize(t, g, canvasMaxWidth, canvasMaxHeight)
}

// assertFits fails if any line of s is wider than limit. The returned bool
// reports success so callers can add context.
func assertFits(t *testing.T, label, s string, limit int) {
	t.Helper()
	for i, line := range strings.Split(s, "\n") {
		if w := lipgloss.Width(line); w > limit {
			t.Errorf("%s: line %d overflows by %d (width %d > limit %d):\n%q",
				label, i, w-limit, w, limit, stripAnsi(line))
		}
	}
}

// TestScreenBodiesFitContentWidth guards every screen body against horizontal
// overflow at the design size. Overflow here is exactly what cuts text off:
// the frame clips any body line wider than contentWidth (the StarShop locked
// hint regression rendered a 141-wide line inside a 94-wide content area).
func TestScreenBodiesFitContentWidth(t *testing.T) {
	g := designGame(t)
	cw := g.contentWidth()

	cases := []struct {
		name  string
		setup func()
	}{
		{"farm", func() { g.scr = scrFarm }},
		{"market", func() { g.scr = scrMarket }},
		{"land", func() { g.scr = scrLand }},
		{"rebirth", func() { g.scr = scrRebirth }},
		{"starshop-locked", func() { g.scr = scrStarShop; g.snap.State.Rebirths = 0 }},
		{"starshop-unlocked", func() { g.scr = scrStarShop; g.snap.State.Rebirths = 2 }},
		{"stats", func() { g.scr = scrStats }},
		{"board-empty", func() { g.scr = scrBoard; g.lbErr = nil; g.lbBoard = leaderboard.Board{} }},
		{"board-error", func() { g.scr = scrBoard; g.lbErr = errBoardUnavailable }},
		{"board-populated", func() { g.scr = scrBoard; g.lbErr = nil; g.lbBoard = wideBoardFixture() }},
		{"help-controls", func() { g.scr = scrHelp; g.helpPage = 0 }},
		{"help-gameplay", func() { g.scr = scrHelp; g.helpPage = 1 }},
	}
	for _, c := range cases {
		c.setup()
		assertFits(t, "screen "+c.name, g.screenBody(), cw)
	}
}

var errBoardUnavailable = errors.New("board fixture: store unavailable")

// wideBoardFixture stresses boardRowLine's alignSides layout with the
// longest names moderation.MaxNameLen allows, the biggest coin totals
// money() formats, a three-digit rebirth count, top-3 accent styling, and a
// full Top+window with You outside Top so the divider renders too.
func wideBoardFixture() leaderboard.Board {
	row := func(rank int, name string, coins int64, isYou bool) leaderboard.Row {
		return leaderboard.Row{Rank: rank, DisplayName: name, Suffix: "wX9zK", Coins: coins, Rebirths: 999, IsYou: isYou}
	}
	top := make([]leaderboard.Row, 10)
	for i := range top {
		top[i] = row(i+1, "TWENTY CHARACTER NAME", 999_999_999_999-int64(i), false)
	}
	window := []leaderboard.Row{
		row(23, "TWENTY CHARACTER NAME", 4_200, false),
		row(24, "TWENTY CHARACTER NAME", 4_199, true),
		row(25, "TWENTY CHARACTER NAME", 4_198, false),
	}
	return leaderboard.Board{Top: top, Window: window, Total: 400, You: &window[1], AsOf: time.Now().Unix()}
}

// TestOverlayBoxesFitContentWidth guards modal overlays. Overlay content is
// clipped to contentWidth-4 before centering, so anything wider is truncated.
func TestOverlayBoxesFitContentWidth(t *testing.T) {
	g := designGame(t)
	limit := g.contentWidth() - 4

	overlays := []struct {
		name string
		ov   overlay
	}{
		{"tutorial", ovTutorial},
		{"away", ovAway},
		{"picker", ovPicker},
		{"upgrade", ovUpgrade},
		{"rebirth-confirm", ovRebirthConfirm},
		{"name", ovName},
		{"config", ovConfig},
		{"replant-warn", ovReplantWarn},
		{"kicked", ovKicked},
	}
	for _, o := range overlays {
		g.overlay = o.ov
		assertFits(t, "overlay "+o.name, g.overlayBox(), limit)
	}
}

// TestComposedCanvasIsRectangular verifies the final framed output is a clean
// rectangle (every line the same width, none wider than the canvas) for every
// screen. A ragged or over-wide line means something escaped the frame.
func TestComposedCanvasIsRectangular(t *testing.T) {
	g := designGame(t)
	canvasW, _ := g.canvasSize()

	for _, scr := range screenOrder {
		g.scr = scr
		out := g.composeCanvas(g.screenBody(), false)
		for i, line := range strings.Split(out, "\n") {
			if w := lipgloss.Width(line); w != canvasW {
				t.Errorf("screen %v: line %d width %d != canvas width %d:\n%q",
					scr, i, w, canvasW, stripAnsi(line))
			}
		}
	}
}

// TestLockedStarShopHintCenteredAndComplete is the focused regression for the
// reported bug: the hint must render as one intact, centered line — not split
// across lines (the embedded-newline collision) nor cut off (overflow).
func TestLockedStarShopHintCenteredAndComplete(t *testing.T) {
	const sentence = "The cosmos keeps its deeper rewards for those who begin anew."

	g := designGame(t)
	g.scr = scrStarShop
	g.snap.State.Rebirths = 0
	cw := g.contentWidth()

	var found bool
	for _, line := range strings.Split(g.screenBody(), "\n") {
		plain := stripAnsi(line)
		if strings.TrimSpace(plain) != sentence {
			continue
		}
		found = true
		if w := lipgloss.Width(line); w > cw {
			t.Fatalf("hint overflows contentWidth: width %d > %d:\n%q", w, cw, plain)
		}
		lead := len(plain) - len(strings.TrimLeft(plain, " "))
		want := (cw - lipgloss.Width(sentence)) / 2
		if lead < want-1 || lead > want+1 {
			t.Fatalf("hint not centered: leading spaces %d, want ~%d", lead, want)
		}
	}
	if !found {
		t.Fatalf("hint sentence not found intact — it was split or truncated:\n%s", stripAnsi(g.screenBody()))
	}

	// And it survives the full compose pipeline (the step that previously
	// reflowed the centered line and cut it off).
	out := g.composeCanvas(g.screenBody(), false)
	if !strings.Contains(collapseRun(stripAnsi(out)), sentence) {
		t.Fatalf("hint sentence missing from composed canvas:\n%s", stripAnsi(out))
	}
}

// collapseRun squeezes runs of spaces to one so a centered/padded line can be
// matched against its source sentence.
func collapseRun(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// stockGame renders at 80×24 — the stock macOS Terminal.app size, and the
// terminal the day/night theme work was prompted by. designGame only ever
// exercised 100×38, so until now the size most likely to overflow had no
// coverage at all.
func stockGame(t *testing.T) *Game {
	t.Helper()
	f := newFixture(t)
	base := time.Now().Unix()
	g := f.newGame(t, base)
	g = dismissIntro(t, g)
	g, _ = tick(t, g, base)
	return resize(t, g, 80, 24)
}

func TestScreenBodiesFitContentWidthAtStockSize(t *testing.T) {
	g := stockGame(t)
	cw := g.contentWidth()

	cases := []struct {
		name  string
		setup func()
	}{
		{"farm", func() { g.scr = scrFarm }},
		{"market", func() { g.scr = scrMarket }},
		{"land", func() { g.scr = scrLand }},
		{"rebirth", func() { g.scr = scrRebirth }},
		{"starshop-locked", func() { g.scr = scrStarShop; g.snap.State.Rebirths = 0 }},
		{"starshop-unlocked", func() { g.scr = scrStarShop; g.snap.State.Rebirths = 2 }},
		{"stats", func() { g.scr = scrStats }},
		{"board-empty", func() { g.scr = scrBoard; g.lbErr = nil; g.lbBoard = leaderboard.Board{} }},
		{"board-error", func() { g.scr = scrBoard; g.lbErr = errBoardUnavailable }},
		{"board-populated", func() { g.scr = scrBoard; g.lbErr = nil; g.lbBoard = wideBoardFixture() }},
		{"help", func() { g.scr = scrHelp }},
	}
	for _, c := range cases {
		c.setup()
		assertFits(t, "stock screen "+c.name, g.screenBody(), cw)
	}
}

func TestOverlayBoxesFitContentWidthAtStockSize(t *testing.T) {
	g := stockGame(t)
	limit := g.contentWidth() - 4

	for _, o := range []struct {
		name string
		ov   overlay
	}{
		{"tutorial", ovTutorial},
		{"away", ovAway},
		{"picker", ovPicker},
		{"upgrade", ovUpgrade},
		{"rebirth-confirm", ovRebirthConfirm},
		{"name", ovName},
		{"config", ovConfig},
		{"replant-warn", ovReplantWarn},
		{"kicked", ovKicked},
	} {
		g.overlay = o.ov
		assertFits(t, "stock overlay "+o.name, g.overlayBox(), limit)
	}
}

// TestComposedCanvasIsRectangularAtStockSize — the frame must stay a clean
// rectangle at 80×24 too, including while the size warning and an event bar
// are both occupying header rows.
func TestComposedCanvasIsRectangularAtStockSize(t *testing.T) {
	g := stockGame(t)
	canvasW, _ := g.canvasSize()

	g.snap.State.EventID = "market_day"
	g.snap.State.EventStartedAt = g.now
	g.snap.State.EventEndsAt = g.now + 120

	for _, scr := range screenOrder {
		g.scr = scr
		out := g.composeCanvas(g.screenBody(), false)
		for i, line := range strings.Split(out, "\n") {
			if w := lipgloss.Width(line); w != canvasW {
				t.Errorf("screen %v: line %d width %d != canvas width %d:\n%q",
					scr, i, w, canvasW, stripAnsi(line))
			}
		}
	}
}
