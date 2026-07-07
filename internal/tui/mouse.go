package tui

import (
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/mynameis-nigel/ssh-farm/internal/tui/hitbox"
)

func (g *Game) handleMouseClick(mouse tea.Mouse) (tea.Model, tea.Cmd) {
	if g.width < minWidth || g.height < minHeight {
		return g, nil
	}
	if mouse.Button != tea.MouseLeft {
		return g, nil
	}

	if g.overlay != ovNone && g.overlay != ovKicked {
		if box, ok := g.hits.At(mouse.X, mouse.Y); ok && box.ID == "overlay:bg" {
			return g.dismissOverlay()
		}
	}

	box, ok := g.hits.At(mouse.X, mouse.Y)
	if !ok {
		return g, nil
	}

	nowMS := g.now * 1000
	isDouble := box.ID == g.lastClickID && nowMS-g.lastClickAt <= doubleClickMS
	g.lastClickID = box.ID
	g.lastClickAt = nowMS

	return g.dispatchHit(box, isDouble)
}

func (g *Game) handleMouseWheel(mouse tea.Mouse) (tea.Model, tea.Cmd) {
	if g.width < minWidth || g.height < minHeight {
		return g, nil
	}
	delta := 0
	switch mouse.Button {
	case tea.MouseWheelUp:
		delta = -1
	case tea.MouseWheelDown:
		delta = 1
	default:
		return g, nil
	}

	switch g.overlay {
	case ovPicker:
		g.pickerIdx = clamp(g.pickerIdx+delta, 0, len(g.visibleCrops())-1)
	case ovUpgrade:
		g.upgradeIdx = clamp(g.upgradeIdx+delta, 0, len(g.snap.State.Plots)-1)
	case ovConfig:
		g.configIdx = clamp(g.configIdx+delta, 0, 2)
	case ovNone:
		switch g.scr {
		case scrFarm:
			g.moveFarmCursor(delta)
		case scrMarket:
			g.marketIdx = clamp(g.marketIdx+delta, 0, len(g.marketItems())-1)
		case scrStarShop:
			if g.snap.State.Rebirths >= 1 {
				g.progressIdx = clamp(g.progressIdx+delta, 0, len(g.content.Upgrades)-1)
			}
		case scrHelp:
			g.helpScroll = max(g.helpScroll+delta, 0)
			g.clampHelpScroll()
		case scrBoard:
			g.lbScroll = max(g.lbScroll+delta, 0)
			g.clampBoardScroll()
		}
	}
	return g, nil
}

func (g *Game) dismissOverlay() (tea.Model, tea.Cmd) {
	switch g.overlay {
	case ovPicker:
		g.overlay = ovNone
		g.pickerAutoSow = false
	case ovReplantWarn:
		return g.ackReplantWarning()
	case ovRebirthConfirm, ovName, ovConfig, ovUpgrade, ovTutorial, ovAway:
		g.overlay = ovNone
	}
	return g, nil
}

func (g *Game) dispatchHit(box hitbox.Box, isDouble bool) (tea.Model, tea.Cmd) {
	id := box.ID
	switch {
	case id == "overlay:close":
		return g.dismissOverlay()
	case strings.HasPrefix(id, "nav:"):
		if scr, ok := box.Data.(screen); ok && g.overlay == ovNone {
			g.scr = scr
			if g.scr == scrHelp {
				g.helpPage = 0
				g.helpScroll = 0
			}
			if g.scr == scrBoard {
				g.refreshBoard()
			}
		}
	case strings.HasPrefix(id, "plot:"):
		if idx, ok := box.Data.(int); ok {
			if idx == g.cursor || isDouble {
				g.cursor = idx
				return g.pressKey("enter")
			}
			g.cursor = idx
		}
	case id == "farm:harvest-all":
		g.harvestAllReady()
	case id == "farm:replant":
		return g.handleReplant()
	case strings.HasPrefix(id, "picker:"):
		if idx, ok := box.Data.(int); ok {
			g.pickerIdx = idx
			if isDouble {
				return g.pressKey("enter")
			}
		}
	case id == "picker:cancel":
		g.overlay = ovNone
		g.pickerAutoSow = false
	case strings.HasPrefix(id, "market:"):
		if idx, ok := box.Data.(int); ok {
			g.marketIdx = idx
			if isDouble {
				return g.pressKey("b")
			}
		}
	case id == "land:buy":
		return g.pressKey("enter")
	case id == "rebirth:open":
		return g.pressKey("R")
	case id == "rebirth:confirm":
		if isDouble {
			return g.pressKey("y")
		}
	case id == "rebirth:cancel":
		g.overlay = ovNone
	case strings.HasPrefix(id, "shop:"):
		if idx, ok := box.Data.(int); ok {
			g.progressIdx = idx
			if isDouble {
				return g.pressKey("b")
			}
		}
	case id == "stats:rename":
		g.nameInput = g.snap.State.FarmName
		g.overlay = ovName
	case id == "board:yourow":
		g.openRename()
	case id == "board:refresh":
		g.refreshBoard()
	case id == "stats:config":
		g.configIdx = 0
		g.overlay = ovConfig
	case strings.HasPrefix(id, "config:"):
		if idx, ok := box.Data.(int); ok {
			g.configIdx = idx
			g.toggleConfig(idx)
		}
	case id == "name:save":
		return g.pressKey("enter")
	case id == "name:cancel":
		g.overlay = ovNone
	case strings.HasPrefix(id, "help:tab:"):
		if page, ok := box.Data.(int); ok {
			g.helpPage = page
			g.helpScroll = 0
		}
	case id == "tutorial:next":
		return g.pressKey("enter")
	case id == "tutorial:skip":
		return g.pressKey("s")
	case id == "away:dismiss":
		g.overlay = ovNone
	case id == "replant:dismiss":
		return g.ackReplantWarning()
	case id == "upgrade:auto-harvest":
		return g.pressKey("1")
	case id == "upgrade:auto-sow":
		return g.pressKey("2")
	}
	return g, nil
}

func (g *Game) pressKey(key string) (tea.Model, tea.Cmd) {
	return g.handleKey(keyPress(key))
}

func keyPress(s string) tea.KeyPressMsg {
	switch s {
	case "enter", " ":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc", "q":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "y":
		return tea.KeyPressMsg{Code: 'y', Text: "y"}
	case "R":
		return tea.KeyPressMsg{Code: 'R', Text: "R"}
	case "b":
		return tea.KeyPressMsg{Code: 'b', Text: "b"}
	case "s":
		return tea.KeyPressMsg{Code: 's', Text: "s"}
	case "1":
		return tea.KeyPressMsg{Code: '1', Text: "1"}
	case "2":
		return tea.KeyPressMsg{Code: '2', Text: "2"}
	default:
		r, _ := utf8.DecodeRuneInString(s)
		return tea.KeyPressMsg{Code: r, Text: s}
	}
}

func (g *Game) moveFarmCursor(delta int) {
	cols := g.farmColumns()
	st := g.snap.State
	if len(st.Plots) == 0 {
		return
	}
	if g.compactFarm() {
		g.cursor = clamp(g.cursor+delta, 0, len(st.Plots)-1)
		return
	}
	g.cursor = clamp(g.cursor+delta*cols, 0, len(st.Plots)-1)
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
