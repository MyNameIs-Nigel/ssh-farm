package tui

import (
	"errors"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-farm/internal/sim"
)

// Farm grid geometry. Plot cards are drawn 20×5 (border included) with no gap
// between them; the Scarecrow column (gameplay/05) sits one cell to the right
// of the grid in 13-wide cards, so four plot columns plus the column fill the
// standard 94-cell body exactly. Hitboxes and layout both read these, so the
// clickable area can never drift from what is drawn again.
const (
	plotCardWidth  = 20
	plotCardHeight = 5
	scareCardWidth = 13
	scareColumnGap = 1
	// scareCardText is the text width inside a scarecrow card (border and
	// one cell of padding each side removed).
	scareCardText = scareCardWidth - 4
	// maxNoticeLines mirrors addNotice's cap.
	maxNoticeLines = 4
)

func (g *Game) scareCount() int { return len(g.snap.State.Scarecrows) }

// scareForSale reports whether the column ends with a "for sale" card. It
// appears once the run has earned the first price (so a brand-new farm isn't
// cluttered) or a scarecrow is already owned, and never past the cap or under
// a contract that forbids scarecrows.
func (g *Game) scareForSale() bool {
	st := g.snap.State
	if !st.ScarecrowsAllowed() || st.ScarecrowCost(g.content) < 0 {
		return false
	}
	return len(st.Scarecrows) > 0 || st.RunEarnings >= g.content.Scarecrow.Cost
}

// scareColumnCards is the number of cards stacked in the scarecrow column.
func (g *Game) scareColumnCards() int {
	n := g.scareCount()
	if g.scareForSale() {
		n++
	}
	return n
}

// scareColumnWidth is the horizontal space the column takes, gap included.
func (g *Game) scareColumnWidth() int {
	if g.scareColumnCards() == 0 {
		return 0
	}
	return scareColumnGap + scareCardWidth
}

// gridColumns is the plot-column count for the card grid. The scarecrow
// column gets its room first: the grid gives up a plot column rather than
// let the column spill past the frame.
func (g *Game) gridColumns() int {
	cw := g.contentWidth()
	cols := min(max(cw/22, 1), 6)
	if w := g.scareColumnWidth(); w > 0 {
		cols = min(cols, max((cw-w)/plotCardWidth, 1))
	}
	return cols
}

// gridRows is the height of the card grid in card rows: the taller of the
// plot grid and the scarecrow column.
func (g *Game) gridRows() int {
	cols := g.gridColumns()
	plotRows := (len(g.snap.State.Plots) + cols - 1) / cols
	return max(plotRows, g.scareColumnCards())
}

// farmChromeLines is every framed line that is not farm body, apart from
// notices: the header block, nav and its spacer, the rule, and the footer.
func (g *Game) farmChromeLines() int {
	return lipgloss.Height(g.viewHeader()) + 2 + 1 + 1
}

// noticeBudget is how many notice lines the screen can show. On the farm's
// card grid it is whatever the grid leaves free (at least one, or the farm
// goes compact), so a burst of notices shows the newest few instead of
// pushing the bottom row of cards out of the frame.
func (g *Game) noticeBudget() int {
	if g.scr != scrFarm || g.overlay != ovNone || g.compactFarm() {
		return maxNoticeLines
	}
	free := g.contentHeight() - g.gridRows()*plotCardHeight - g.farmChromeLines()
	return min(max(free, 1), maxNoticeLines)
}

// clampFarmCursor keeps the scarecrow selection valid after a rebirth or
// contract reset took the scarecrows away.
func (g *Game) clampFarmCursor() {
	if g.scareSel >= g.scareCount() {
		g.scareSel = g.scareCount() - 1
	}
	if n := len(g.snap.State.Plots); g.cursor >= n {
		g.cursor = max(n-1, 0)
	}
}

// shortMoney fits a coin amount into a scarecrow card: in full below
// 100,000, then one truncated decimal (123,456,789 → 123.4M) so it never
// overstates the pouch.
func shortMoney(n int64) string {
	if n < 100_000 {
		return money(n)
	}
	units := []string{"K", "M", "B", "T", "Qa", "Qi"}
	div := int64(1_000)
	for i, u := range units {
		if n/div < 1_000 || i == len(units)-1 {
			tenths := n / (div / 10)
			return fmt.Sprintf("%d.%d%s", tenths/10, tenths%10, u)
		}
		div *= 1_000
	}
	return money(n)
}

// scareDropLine is the "time to next drop" row: a 4-cell bar and the
// countdown, falling back to the countdown alone if an operator configured
// an interval too long for the card.
func (g *Game) scareDropLine(barWidth int) string {
	pct, left := g.snap.State.ScarecrowNextDrop(g.content)
	line := progressBar(pct, barWidth) + " " + duration(left)
	if lipgloss.Width(line) > scareCardText {
		line = duration(left)
	}
	return line
}

func (g *Game) scarePouchLine(pouch int64) string {
	th := g.theme()
	style := th.Empty
	if pouch > 0 {
		style = th.Ready
	}
	return style.Render("⛀ " + shortMoney(pouch))
}

// scarecrowCard draws scarecrow i for the grid's column.
func (g *Game) scarecrowCard(i int) string {
	th := g.theme()
	pouch := g.snap.State.Scarecrows[i].Pouch
	content := th.ScareTitle.Render("Scarecrow") + "\n" +
		g.scarePouchLine(pouch) + "\n" +
		th.Growing.Render(g.scareDropLine(4))
	if i == g.scareSel {
		return th.ScareSel.Render(content)
	}
	return th.ScareCard.Render(content)
}

// scareShopCard is the dim placeholder advertising the next scarecrow.
func (g *Game) scareShopCard() string {
	th := g.theme()
	st := g.snap.State
	cost := st.ScarecrowCost(g.content)
	price := th.Locked.Render(shortMoney(cost) + "c")
	if st.Coins >= cost {
		price = th.Value.Render(shortMoney(cost) + "c")
	}
	return th.ScareShop.Render(th.Empty.Render("Scarecrow") + "\n" +
		th.Hint.Render("for sale") + "\n" + price)
}

// scareColumn stacks the owned scarecrows and the for-sale card.
func (g *Game) scareColumn() string {
	cards := make([]string, 0, g.scareColumnCards())
	for i := range g.snap.State.Scarecrows {
		cards = append(cards, g.scarecrowCard(i))
	}
	if g.scareForSale() {
		cards = append(cards, g.scareShopCard())
	}
	return strings.Join(cards, "\n")
}

// withScareColumn places the column one gap to the right of a plot grid that
// is cols cards wide, padding a short grid so the column stays put.
func (g *Game) withScareColumn(grid string, cols int) string {
	if g.scareColumnCards() == 0 {
		return grid
	}
	grid = lipgloss.NewStyle().Width(cols * plotCardWidth).Render(grid)
	return lipgloss.JoinHorizontal(lipgloss.Top, grid, strings.Repeat(" ", scareColumnGap), g.scareColumn())
}

// --- Compact list ------------------------------------------------------------

// compactRowBudget is how many entries the compact list shows at once. The
// renderer and the hitboxes both use it (they used to disagree, so a
// scrolled list's clicks landed on the wrong plot).
func (g *Game) compactRowBudget() int {
	return max(g.contentHeight()-9, 3)
}

// compactPos is the cursor's index in the compact list: plots, then scarecrows.
func (g *Game) compactPos() int {
	if g.scareSel >= 0 {
		return len(g.snap.State.Plots) + g.scareSel
	}
	return g.cursor
}

func (g *Game) setCompactPos(pos int) {
	n := len(g.snap.State.Plots)
	total := n + g.scareCount()
	pos = min(max(pos, 0), max(total-1, 0))
	if pos >= n {
		g.scareSel = pos - n
		return
	}
	g.cursor, g.scareSel = pos, -1
}

// compactStart is the first list entry shown, scrolled so the cursor is visible.
func (g *Game) compactStart() int {
	if pos, budget := g.compactPos(), g.compactRowBudget(); pos >= budget {
		return pos - budget + 1
	}
	return 0
}

func (g *Game) compactScarecrowRow(i int) string {
	th := g.theme()
	marker := "  "
	if i == g.scareSel {
		marker = th.Selected.Render("▸ ")
	}
	// Pad every pouch to the widest one so the drop bars line up in a column.
	width := 0
	for _, sc := range g.snap.State.Scarecrows {
		width = max(width, lipgloss.Width("⛀ "+shortMoney(sc.Pouch)))
	}
	pouch := g.scarePouchLine(g.snap.State.Scarecrows[i].Pouch)
	pouch += strings.Repeat(" ", max(width-lipgloss.Width(pouch), 0))
	return marker + th.ScareTitle.Render("Scarecrow "+itoa(i+1)) + "  " + pouch + "  " +
		th.Growing.Render(g.scareDropLine(4))
}

// --- Navigation --------------------------------------------------------------

// navigateFarm applies an arrow key. The grid is row-major with each
// scarecrow at the end of its row: → off a row's last plot lands on that
// row's scarecrow and → again continues to the next row; ← reverses that;
// ↑/↓ walk the scarecrow column. The compact list is one linear sequence.
func (g *Game) navigateFarm(key string) {
	g.clampFarmCursor()
	if g.compactFarm() {
		switch key {
		case "left", "up":
			g.setCompactPos(g.compactPos() - 1)
		case "right", "down":
			g.setCompactPos(g.compactPos() + 1)
		}
		return
	}
	n, k, cols := len(g.snap.State.Plots), g.scareCount(), g.farmColumns()
	if g.scareSel >= 0 {
		r := g.scareSel
		switch key {
		case "up":
			g.scareSel = max(r-1, 0)
		case "down":
			g.scareSel = min(r+1, k-1)
		case "right":
			if next := (r + 1) * cols; next < n {
				g.cursor, g.scareSel = next, -1
			}
		case "left":
			if r*cols < n {
				g.cursor = min((r+1)*cols, n) - 1
			} else {
				g.cursor = n - 1 // a scarecrow below the plot grid
			}
			g.scareSel = -1
		}
		return
	}
	row := g.cursor / cols
	switch key {
	case "right":
		endOfRow := g.cursor%cols == cols-1 || g.cursor == n-1
		switch {
		case endOfRow && row < k:
			g.scareSel = row
		case g.cursor < n-1:
			g.cursor++
		}
	case "left":
		switch {
		case g.cursor%cols == 0 && row > 0 && row-1 < k:
			g.scareSel = row - 1
		case g.cursor > 0:
			g.cursor--
		}
	case "up":
		if g.cursor-cols >= 0 {
			g.cursor -= cols
		}
	case "down":
		if g.cursor+cols < n {
			g.cursor += cols
		}
	}
}

// --- Collecting --------------------------------------------------------------

func (g *Game) collectScarecrow(i int) {
	got, snap, ach, err := g.sess.CollectScarecrow(g.now, i)
	if errors.Is(err, sim.ErrPouchEmpty) {
		if snap.State != nil {
			g.snap = snap
		}
		_, left := g.snap.State.ScarecrowNextDrop(g.content)
		g.addNotice("Scarecrow " + itoa(i+1) + "'s pouch is empty — next drop in " + duration(left) + ".")
		return
	}
	g.applyAction(snap, ach, err)
	if err == nil {
		g.addNotice("Collected " + money(got) + " coins from Scarecrow " + itoa(i+1) + ".")
	}
}

// collectAllScarecrows empties every pouch for harvest-all. It returns the
// coins and how many scarecrows paid out; it says nothing itself so the
// caller can fold the result into one notice.
func (g *Game) collectAllScarecrows() (int64, int) {
	if g.snap.State.ScarecrowPouchTotal() == 0 {
		return 0, 0
	}
	total, n, snap, ach, err := g.sess.CollectScarecrows(g.now)
	g.applyAction(snap, ach, err)
	if err != nil {
		return 0, 0
	}
	return total, n
}

// scarecrowHelp is the Help → Gameplay paragraph, built from live tuning so
// it can't drift from balance.toml.
func (g *Game) scarecrowHelp() string {
	sc := g.content.Scarecrow
	if sc.Cost <= 0 {
		return "Scarecrows are not available on this server."
	}
	away := duration(sc.DropIntervalSec * sc.OfflineSlowdown)
	return "Buy up to " + money(sc.MaxCount) + " Scarecrows in the Market (from " +
		money(sc.Cost) + " coins, each dearer than the last). While you play, every " +
		"Scarecrow drops " + money(sc.CoinsMin) + "–" + money(sc.CoinsMax) + " coins into its pouch every " +
		duration(sc.DropIntervalSec) + "; while you're away, only once every " + away + ". " +
		"Pouches are never collected for you: move onto a Scarecrow beside your plots and " +
		"press enter (or click it), or press a to collect them all with your harvest. " +
		"Each Scarecrow beyond the first adds " + money(sc.SynergyPct) + "% to every drop, " +
		"and each rebirth adds " + money(sc.RebirthBonusPct) + "% more. Scarecrows reset on rebirth."
}
