package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// The game renders as a fixed-size canvas centered on the terminal: a framed
// window that never exceeds canvasMaxWidth×canvasMaxHeight, placed in the
// middle of whatever space the player gives us. canvasMaxWidth is the single
// knob for wider layouts (138 would fit six plot-card columns instead of four).
const (
	canvasMaxWidth  = 100
	canvasMaxHeight = 38
	minWidth        = 36
	minHeight       = 10
	windowTitle     = "ssh-farm 🌾"
)

// canvasSize is the outer size of the framed game window, clamped to the terminal.
func (g *Game) canvasSize() (w, h int) {
	skyH := 0
	if text, _ := g.seasonalSky(); text != "" {
		skyH = 1
	}
	return min(g.width, canvasMaxWidth), min(max(g.height-skyH, 1), canvasMaxHeight)
}

// contentWidth and contentHeight are the usable area inside the frame
// (canvas minus 2 border cells, and 4 cells of horizontal padding).
func (g *Game) contentWidth() int {
	w, _ := g.canvasSize()
	return w - 6
}

func (g *Game) contentHeight() int {
	_, h := g.canvasSize()
	return h - 2
}

// fullscreen is the single exit point for View: it centers the rendered
// content on the full terminal, takes over the alternate screen, and titles
// the window. Every View() return path must go through it.
func (g *Game) fullscreen(content string) tea.View {
	th := g.theme()

	// Three cooperating mechanisms paint the background, and all three are
	// needed:
	//
	//  1. WithWhitespaceStyle paints the letterbox Place adds around the
	//     canvas. Place emits exactly width x height cells, so painting what
	//     we draw paints the whole terminal.
	//  2. Paint re-asserts the palette after every SGR reset. lipgloss closes
	//     each styled span with a reset, which would otherwise drop the
	//     background for the rest of that line and render the canvas as
	//     stripes. This is the load-bearing one: it needs no terminal support.
	//  3. BackgroundColor/ForegroundColor set the terminal's own defaults via
	//     OSC 11/10, so anything we miss still falls back to dark rather than
	//     to the player's white. Terminal.app ignores these, which is exactly
	//     why (2) cannot be skipped.
	placed := lipgloss.Place(
		max(g.width, 1), max(g.height, 1),
		lipgloss.Center, lipgloss.Center, content,
		lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Background(th.Bg)),
	)

	v := tea.NewView(th.Paint(placed))
	v.BackgroundColor = th.Bg
	v.ForegroundColor = th.Fg
	v.AltScreen = true
	v.WindowTitle = windowTitle
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// composeCanvas assembles header, nav, body, notices, and footer into the
// framed canvas. The body region absorbs all leftover height so the window
// keeps the same size from screen to screen and tick to tick; the footer is
// pinned to the bottom under a thin rule. With centerBody the body floats in
// the middle of its region (overlay modals); otherwise it anchors top-left.
func (g *Game) composeCanvas(body string, centerBody bool) string {
	th := g.theme()
	cw, ch := g.contentWidth(), g.contentHeight()

	top := strings.Join([]string{
		g.viewHeader(),
		lipgloss.PlaceHorizontal(cw, lipgloss.Center, g.navRowContent()),
		"",
	}, "\n")

	bottomParts := []string{th.Rule.Render(strings.Repeat("─", max(cw, 1)))}
	if n := g.viewNotices(); n != "" {
		bottomParts = append(bottomParts, n)
	}
	bottomParts = append(bottomParts, g.viewFooter())
	bottom := strings.Join(bottomParts, "\n")

	bodyH := ch - lipgloss.Height(top) - lipgloss.Height(bottom)
	if bodyH < 1 {
		bodyH = 1
	}
	if centerBody {
		body = lipgloss.NewStyle().MaxWidth(cw - 4).MaxHeight(bodyH).Render(body)
		body = lipgloss.Place(cw, bodyH, lipgloss.Center, lipgloss.Center, body)
	} else if g.preserveBodyLines() {
		// Width() reflows pre-wrapped centered lines; MaxWidth keeps them.
		body = lipgloss.NewStyle().Height(bodyH).MaxWidth(cw).MaxHeight(bodyH).Render(body)
	} else {
		body = lipgloss.NewStyle().Width(cw).Height(bodyH).MaxWidth(cw).MaxHeight(bodyH).Render(body)
	}

	inner := lipgloss.NewStyle().MaxWidth(cw).MaxHeight(ch).Render(top + "\n" + body + "\n" + bottom)
	frame := th.Frame.Render(inner)
	if sky := g.viewSky(); sky != "" {
		return sky + "\n" + frame
	}
	return frame
}

// preserveBodyLines reports screens whose body text already has explicit
// line breaks and horizontal alignment that lipgloss.Width would reflow.
func (g *Game) preserveBodyLines() bool {
	return g.scr == scrStarShop && g.snap.State.ProgressionRebirths() < 1 && !g.snap.State.ContractsAvailable(g.content)
}
