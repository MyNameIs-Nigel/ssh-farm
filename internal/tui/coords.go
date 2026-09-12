package tui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-farm/internal/tui/hitbox"
)

// frameLayout holds terminal coordinates for the framed canvas content region.
type frameLayout struct {
	ox, oy int // canvas top-left on terminal
	cx, cy int // content text origin (inside frame border + padding)
	cw     int // content width in cells
	bodyY  int // body region top on terminal
	bodyH  int
	navY   int
	navX   int // left edge of centered nav strip
	navW   int
}

const doubleClickMS = 400

func (g *Game) computeLayout() frameLayout {
	th := g.theme()
	cw, ch := g.canvasSize()
	skyH := 0
	if text, _ := g.seasonalSky(); text != "" {
		skyH = 1
	}
	ox := (g.width - cw) / 2
	oy := (g.height-(ch+skyH))/2 + skyH
	cxi := g.contentWidth()
	cy := oy + 1 // below top border

	top := strings.Join([]string{
		g.viewHeader(),
		lipgloss.PlaceHorizontal(cxi, lipgloss.Center, g.navRowContent()),
		"",
	}, "\n")
	topH := lipgloss.Height(top)

	bottomParts := []string{th.Rule.Render(strings.Repeat("─", max(cxi, 1)))}
	if n := g.viewNotices(); n != "" {
		bottomParts = append(bottomParts, n)
	}
	bottomParts = append(bottomParts, g.viewFooter())
	bottomH := lipgloss.Height(strings.Join(bottomParts, "\n"))

	bodyH := g.contentHeight() - topH - bottomH
	if bodyH < 1 {
		bodyH = 1
	}

	navLine := g.navRowContent()
	navW := lipgloss.Width(navLine)
	navX := ox + 3 + max((cxi-navW)/2, 0)
	navY := cy + lipgloss.Height(g.viewHeader())

	return frameLayout{
		ox: ox, oy: oy,
		cx: ox + 3, cy: cy,
		cw:    cxi,
		bodyY: cy + topH,
		bodyH: bodyH,
		navY:  navY,
		navX:  navX,
		navW:  navW,
	}
}

func (g *Game) addHit(x, y, w, h int, id string, data any) {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	g.hits.Add(hitbox.Box{X: x, Y: y, W: w, H: h, ID: id, Data: data})
}
