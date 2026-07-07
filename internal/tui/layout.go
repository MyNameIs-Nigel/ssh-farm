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

var (
	styleFrame = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("65")).Padding(0, 2)
	styleRule  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

// canvasSize is the outer size of the framed game window, clamped to the terminal.
func (g *Game) canvasSize() (w, h int) {
	return min(g.width, canvasMaxWidth), min(g.height, canvasMaxHeight)
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
	v := tea.NewView(lipgloss.Place(max(g.width, 1), max(g.height, 1), lipgloss.Center, lipgloss.Center, content))
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
	cw, ch := g.contentWidth(), g.contentHeight()

	top := strings.Join([]string{
		g.viewHeader(),
		lipgloss.PlaceHorizontal(cw, lipgloss.Center, g.navRowContent()),
		"",
	}, "\n")

	bottomParts := []string{styleRule.Render(strings.Repeat("─", max(cw, 1)))}
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
	return styleFrame.Render(inner)
}

// preserveBodyLines reports screens whose body text already has explicit
// line breaks and horizontal alignment that lipgloss.Width would reflow.
func (g *Game) preserveBodyLines() bool {
	return g.scr == scrStarShop && g.snap.State.Rebirths < 1
}
