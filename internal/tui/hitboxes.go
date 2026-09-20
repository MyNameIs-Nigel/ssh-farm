package tui

import (
	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-farm/internal/sim"
)

func (g *Game) registerHitboxes() {
	if g.width < minWidth || g.height < minHeight {
		return
	}
	g.layout = g.computeLayout()
	// Background must register before nav so the "[x] Close" hit (added by
	// registerNavHits while an overlay is open) isn't shadowed by the
	// full-canvas background box — last-added wins on overlap.
	g.registerOverlayBG()
	g.registerNavHits()

	switch g.overlay {
	case ovNone:
		switch g.scr {
		case scrFarm:
			g.registerFarmHits()
		case scrMarket:
			g.registerMarketHits()
		case scrLand:
			g.registerLandHits()
		case scrRebirth:
			g.registerRebirthHits()
		case scrStarShop:
			g.registerStarShopHits()
		case scrContracts:
			g.registerContractHits()
		case scrStats:
			g.registerStatsHits()
		case scrBoard:
			g.registerBoardHits()
		case scrHelp:
			g.registerHelpHits()
		}
	case ovPicker:
		g.registerPickerHits()
	case ovUpgrade:
		g.registerUpgradeHits()
	case ovRebirthConfirm:
		g.registerRebirthConfirmHits()
	case ovName:
		g.registerNameHits()
	case ovConfig:
		g.registerConfigHits()
	case ovTutorial:
		g.registerTutorialHits()
	case ovAway:
		g.registerAwayHits()
	case ovReplantWarn:
		g.registerReplantWarnHits()
	}
}

func (g *Game) registerOverlayBG() {
	if g.overlay == ovNone || g.overlay == ovKicked {
		return
	}
	// Full canvas capture for background dismiss.
	cw, ch := g.canvasSize()
	g.addHit(g.layout.ox, g.layout.oy, cw, ch, "overlay:bg", nil)
}

func (g *Game) registerNavHits() {
	th := g.theme()
	if g.overlay == ovKicked {
		return
	}
	if g.overlay != ovNone {
		tabsW := lipgloss.Width(g.viewNav())
		closeText := th.NavOn.Render("[x] Close")
		closeW := lipgloss.Width(closeText)
		g.addHit(g.layout.navX+tabsW+2, g.layout.navY, closeW, 1, "overlay:close", nil)
		return
	}
	// navLabels/navStyle are shared with viewNav so the measured widths are
	// the rendered widths; keeping a second copy of the list here is what
	// used to let the two drift apart.
	x := g.layout.navX
	for _, l := range g.navLabels() {
		text := l.text
		if l.lock {
			text += " 🔒"
		}
		w := lipgloss.Width(g.navStyle(l).Render(text))
		if !l.lock {
			g.addHit(x, g.layout.navY, w, 1, "nav:"+itoa(int(l.s)), l.s)
		}
		x += w
	}
}

func (g *Game) registerFarmHits() {
	st := g.snap.State
	ly := g.layout
	y := ly.bodyY
	if g.compactFarm() {
		maxRows := ly.bodyH
		start := 0
		if g.cursor >= maxRows {
			start = g.cursor - maxRows + 1
		}
		for i := start; i < len(st.Plots) && i < start+maxRows; i++ {
			g.addHit(ly.cx, y, ly.cw, 1, "plot:"+itoa(i), i)
			y++
		}
		return
	}
	cols := g.farmColumns()
	cardW, cardH := 22, 5
	for start := 0; start < len(st.Plots); start += cols {
		end := start + cols
		if end > len(st.Plots) {
			end = len(st.Plots)
		}
		x := ly.cx
		for i := start; i < end; i++ {
			g.addHit(x, y, cardW, cardH, "plot:"+itoa(i), i)
			x += cardW
		}
		y += cardH + 1
	}
}

func (g *Game) registerPickerHits() {
	crops := g.visibleCrops()
	ly := g.layout
	// Modal centered in body — approximate box within canvas center.
	boxW := minInt(ly.cw-4, 60)
	boxH := minInt(len(crops)+6, ly.bodyH-2)
	boxX := ly.cx + (ly.cw-boxW)/2
	boxY := ly.bodyY + (ly.bodyH-boxH)/2
	rowY := boxY + 4
	for i := range crops {
		g.addHit(boxX+2, rowY, boxW-4, 1, "picker:"+itoa(i), i)
		rowY++
	}
	g.addHit(boxX+boxW-12, boxY+boxH-2, 10, 1, "picker:cancel", nil)
}

// registerMarketHits mirrors viewMarket's line layout exactly (section
// headers, blank separators, one row per item) so a click always lands on
// what's actually drawn there.
func (g *Game) registerMarketHits() {
	y := g.layout.bodyY
	for _, l := range g.marketLines() {
		if l.idx >= 0 {
			g.addHit(g.layout.cx, y, g.layout.cw, 1, "market:"+itoa(l.idx), l.idx)
		}
		y++
	}
}

func (g *Game) registerLandHits() {
	g.addHit(g.layout.cx, g.layout.bodyY+2, g.layout.cw, 3, "land:buy", nil)
}

func (g *Game) registerRebirthHits() {
	g.addHit(g.layout.cx, g.layout.bodyY+4, 20, 1, "rebirth:open", nil)
}

func (g *Game) registerRebirthConfirmHits() {
	cx := g.layout.cx + g.layout.cw/2 - 15
	cy := g.layout.bodyY + g.layout.bodyH/2
	g.addHit(cx, cy+4, 14, 1, "rebirth:confirm", nil)
	g.addHit(cx+16, cy+4, 10, 1, "rebirth:cancel", nil)
}

func (g *Game) registerStarShopHits() {
	if g.snap.State.ProgressionRebirths() < 1 && !g.snap.State.ContractsAvailable(g.content) {
		return
	}
	y := g.layout.bodyY
	for _, l := range g.starShopLines() {
		if l.idx >= 0 {
			g.addHit(g.layout.cx, y, g.layout.cw, 1, "shop:"+itoa(l.idx), l.idx)
		}
		y++
	}
}

func (g *Game) registerContractHits() {
	for i := range sim.Contracts() {
		g.addHit(g.layout.cx, g.layout.bodyY+3+i, g.layout.cw, 1, "contract:"+itoa(i), i)
	}
}

func (g *Game) registerStatsHits() {
	y := g.layout.bodyY + 6
	g.addHit(g.layout.cx+2, y, 16, 1, "stats:rename", nil)
	g.addHit(g.layout.cx+2, y+2, 14, 1, "stats:config", nil)
}

// registerBoardHits mirrors viewBoard's line layout exactly (header, blank,
// visible rows, optional scroll hint, blank, "updated…") so a click always
// lands on what's actually drawn there.
func (g *Game) registerBoardHits() {
	if g.lbErr != nil {
		return
	}
	ly := g.layout
	cw := ly.cw
	lines := g.boardLines(cw)
	start, end := g.boardVisibleRange(len(lines))

	y := ly.bodyY + 2
	for i := start; i < end; i++ {
		if l := lines[i]; l.row != nil && l.row.IsYou {
			g.addHit(ly.cx, y, cw, 1, "board:yourow", nil)
		}
		y++
	}
	if start > 0 || end < len(lines) {
		y++ // scroll hint line
	}
	y++ // blank line before "updated…"
	g.addHit(ly.cx, y, cw, 1, "board:refresh", nil)
}

func (g *Game) registerHelpHits() {
	tabY := g.layout.bodyY
	g.addHit(g.layout.cx, tabY, 8, 1, "help:tab:0", 0)
	g.addHit(g.layout.cx+10, tabY, 8, 1, "help:tab:1", 1)
}

// registerConfigHits places one hitbox per settings row. The overlay is
// centred in the body region, so the rows' position is measured from the
// rendered box rather than assumed: the previous fixed bodyY+4+i*2 offset
// put every hitbox on the wrong line (rows render one per line, not two, and
// centring moves the box as the body height changes).
func (g *Game) registerConfigHits() {
	box := g.viewConfig()
	boxW, boxH := lipgloss.Width(box), lipgloss.Height(box)
	top := g.layout.bodyY + max((g.layout.bodyH-boxH)/2, 0)
	left := g.layout.cx + max((g.layout.cw-boxW)/2, 0)

	// Inside the box: 1 border + 1 padding + "Settings" + 1 blank line.
	firstRow := top + 4
	for i := range g.configRows() {
		g.addHit(left+1, firstRow+i, max(boxW-2, 1), 1, "config:"+itoa(i), i)
	}
}

func (g *Game) registerUpgradeHits() {
	for i := 0; i < len(g.snap.State.Plots); i++ {
		g.addHit(g.layout.cx+2, g.layout.bodyY+3+i, g.layout.cw-4, 1, "upgrade:plot:"+itoa(i), i)
	}
	g.addHit(g.layout.cx+2, g.layout.bodyY+g.layout.bodyH-4, 16, 1, "upgrade:auto-harvest", nil)
	g.addHit(g.layout.cx+20, g.layout.bodyY+g.layout.bodyH-4, 14, 1, "upgrade:auto-sow", nil)
}

func (g *Game) registerNameHits() {
	cy := g.layout.bodyY + g.layout.bodyH/2
	g.addHit(g.layout.cx+g.layout.cw/2-8, cy+4, 8, 1, "name:save", nil)
	g.addHit(g.layout.cx+g.layout.cw/2+2, cy+4, 8, 1, "name:cancel", nil)
}

func (g *Game) registerTutorialHits() {
	cy := g.layout.bodyY + g.layout.bodyH/2
	g.addHit(g.layout.cx+g.layout.cw/2-12, cy+6, 10, 1, "tutorial:skip", nil)
	g.addHit(g.layout.cx+g.layout.cw/2+2, cy+6, 12, 1, "tutorial:next", nil)
}

func (g *Game) registerAwayHits() {
	g.addHit(g.layout.cx, g.layout.bodyY+g.layout.bodyH-3, g.layout.cw, 2, "away:dismiss", nil)
}

func (g *Game) registerReplantWarnHits() {
	g.addHit(g.layout.cx, g.layout.bodyY+g.layout.bodyH-3, g.layout.cw, 2, "replant:dismiss", nil)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// suppress unused import if style vars only used indirectly
var _ = lipgloss.Width
