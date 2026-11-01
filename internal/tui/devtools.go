package tui

import (
	"math"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var devRows = []struct {
	action, label string
	step          int64
}{
	{"coins", "Coins", 1000},
	{"starseeds", "Starseeds", 10},
	{"earnings", "Run earnings", 10000},
	{"rebirths", "Lifetime rebirths", 1},
	{"mature", "Mature planted crops", 0},
	{"gift", "Spawn parcel", 0},
	{"event", "Event", 0},
	{"contracts", "Unlock contracts", 0},
}

func (g *Game) devValue(index int) int64 {
	st := g.snap.State
	switch index {
	case 0:
		return st.Coins
	case 1:
		return st.PrestigeCurrency
	case 2:
		return st.RunEarnings
	case 3:
		return st.Rebirths
	}
	return 0
}

func (g *Game) handleDevKey(key string) (tea.Model, tea.Cmd) {
	if !g.sess.DevMode() {
		return g, nil
	}
	row := devRows[g.devSel]
	switch key {
	case "esc", "q":
		g.overlay = ovNone
		g.devInput = ""
	case "up", "k":
		g.devSel = (g.devSel + len(devRows) - 1) % len(devRows)
		g.devInput = ""
	case "down", "j":
		g.devSel = (g.devSel + 1) % len(devRows)
		g.devInput = ""
	case "backspace":
		if len(g.devInput) > 0 {
			g.devInput = g.devInput[:len(g.devInput)-1]
		}
	case "enter", "space", " ", "left", "right":
		value := g.devValue(g.devSel)
		if row.step > 0 {
			if g.devInput != "" {
				var err error
				value, err = strconv.ParseInt(g.devInput, 10, 64)
				if err != nil {
					g.addNotice("Use a whole number from 0 to 9223372036854775807.")
					return g, nil
				}
			} else if key == "left" {
				value = max(0, value-row.step)
			} else if key == "right" {
				value = min(value, math.MaxInt64-row.step) + row.step
			} else {
				return g, nil
			}
		} else if row.action == "event" {
			index := 0
			for i, event := range g.content.Events {
				if event.ID == g.snap.State.EventID {
					index = i + 1
					break
				}
			}
			delta := 1
			if key == "left" {
				delta = -1
			}
			value = int64((index + delta + len(g.content.Events) + 1) % (len(g.content.Events) + 1))
		} else if key == "left" {
			return g, nil
		}
		snap, ach, err := g.sess.DevApply(g.now, row.action, value)
		g.applyAction(snap, ach, err)
		if err == nil {
			g.devInput = ""
			g.addNotice("DEV: " + row.label + " applied.")
		}
	default:
		if row.step > 0 && len(key) > 0 && len(g.devInput)+len(key) <= 19 && strings.Trim(key, "0123456789") == "" {
			g.devInput += key
		}
	}
	return g, nil
}

// Keep the selected row visible even when notices reduce the available height.
func (g *Game) devRange() (int, int) {
	if g.computeLayout().bodyH < 7 {
		return g.devSel, g.devSel + 1
	}
	visible := max(g.computeLayout().bodyH-6, 1)
	start := max(0, g.devSel-visible+1)
	return start, min(start+visible, len(devRows))
}

func (g *Game) viewDev() string {
	th := g.theme()
	start, end := g.devRange()
	lines := []string{th.Section.Render("DEV TOOLS · saved"), ""}
	for i := start; i < end; i++ {
		row := devRows[i]
		marker, value := "  ", "[enter]"
		if i == g.devSel {
			marker = "▸ "
		}
		if row.step > 0 {
			value = strconv.FormatInt(g.devValue(i), 10)
			if i == g.devSel && g.devInput != "" {
				value = g.devInput + "_"
			}
		} else if row.action == "event" {
			value = "off"
			if event := g.content.EventByID(g.snap.State.EventID); event != nil {
				value = event.Name
			}
		}
		line := fitWidth(marker+row.label+": "+value, max(g.contentWidth()-10, 1))
		if i == g.devSel {
			lines = append(lines, th.Selected.Render(line))
		} else {
			lines = append(lines, th.Value.Render(line))
		}
	}
	if g.computeLayout().bodyH < 7 {
		return lines[2]
	}
	return th.Box.Render(strings.Join(lines, "\n"))
}

func (g *Game) registerDevHits() {
	box := g.viewDev()
	w, h := lipgloss.Width(box), lipgloss.Height(box)
	top := g.layout.bodyY + max((g.layout.bodyH-h)/2, 0)
	left := g.layout.cx + max((g.layout.cw-w)/2, 0)
	start, end := g.devRange()
	if g.layout.bodyH < 7 {
		g.addHit(left, top, w, 1, "dev:"+itoa(start), start)
		return
	}
	for i := start; i < end; i++ {
		g.addHit(left+1, top+4+i-start, max(w-2, 1), 1, "dev:"+itoa(i), i)
	}
}
