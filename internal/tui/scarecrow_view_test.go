package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-farm/internal/sim"
)

const (
	plotTop   = "╭──────────────────╮"
	plotBot   = "╰──────────────────╯"
	scareTop  = "╭───────────╮"
	scareBot  = "╰───────────╯"
	scareName = "Scarecrow"
)

// scarecrowFarm is a deterministic render-only farm: plots planted with
// turnips at the golden clock and one scarecrow per pouch value. It touches
// only the UI snapshot, so it suits layout and navigation tests; anything
// that collects coins goes through savedFarm instead.
func scarecrowFarm(t *testing.T, w, h, plots int, pouches ...int64) *Game {
	t.Helper()
	g := goldenGame(t, w, h)
	st := g.snap.State
	st.Plots = make([]sim.Plot, plots)
	for i := range st.Plots {
		st.Plots[i] = sim.Plot{Crop: "turnip", PlantedAt: goldenBase}
	}
	for _, p := range pouches {
		st.Scarecrows = append(st.Scarecrows, sim.Scarecrow{Pouch: p})
	}
	g.cursor, g.scareSel = 0, -1
	return g
}

// savedFarm persists a save built by mutate and opens a real session on it,
// so collect actions run through the actor like they do in production.
func savedFarm(t *testing.T, mutate func(st *sim.State)) (*Game, int64) {
	t.Helper()
	g, now := contractSave(t, func(st *sim.State) {
		st.ContractsUnlocked = false
		mutate(st)
	})
	g, _ = tick(t, g, now)
	g.notices = nil
	return resize(t, g, canvasMaxWidth, canvasMaxHeight), now
}

func visibleLines(g *Game) []string {
	return strings.Split(stripAnsi(view(g)), "\n")
}

func assertNoLineOverflows(t *testing.T, g *Game, label string) {
	t.Helper()
	for i, l := range visibleLines(g) {
		if w := lipgloss.Width(l); w != g.width {
			t.Fatalf("%s: line %d is %d cells wide, terminal is %d:\n%s", label, i, w, g.width, l)
		}
	}
}

// --- Layout ------------------------------------------------------------------

// The reported screenshot: a full 16-plot farm leaves a column of whitespace on
// the right of the 100-wide canvas. Four scarecrows must fill exactly that
// column — one per plot row, fully drawn, nothing pushed past the frame.
func TestScarecrowColumnFillsTheWhitespaceBesideAFullFarm(t *testing.T) {
	g := scarecrowFarm(t, canvasMaxWidth, canvasMaxHeight, 16, 0, 12, 99_999, 123_456_789)
	assertNoLineOverflows(t, g, "100x38")
	lines := visibleLines(g)
	out := strings.Join(lines, "\n")

	if n := strings.Count(out, plotBot); n != 16 {
		t.Fatalf("%d plot cards fully drawn, want 16:\n%s", n, out)
	}
	if n := strings.Count(out, scareTop); n != 4 {
		t.Fatalf("%d scarecrow tops, want 4:\n%s", n, out)
	}
	if n := strings.Count(out, scareBot); n != 4 {
		t.Fatalf("%d scarecrow bottoms, want 4:\n%s", n, out)
	}

	// Each scarecrow shares its line with the first plot of a row, and they
	// all start in the same column, right of the fourth plot column.
	col := -1
	for _, first := range []string{"Plot 1 ", "Plot 5 ", "Plot 9 ", "Plot 13 "} {
		var row string
		for _, l := range lines {
			if strings.Contains(l, "│ "+first) {
				row = l
			}
		}
		if row == "" {
			t.Fatalf("no row for %q", first)
		}
		c := displayCol(row, scareName)
		if c < 0 {
			t.Fatalf("row with %q has no scarecrow beside it:\n%s", first, row)
		}
		if col == -1 {
			col = c
		} else if c != col {
			t.Fatalf("scarecrow column wanders: %d vs %d", c, col)
		}
		lastPlot := lipgloss.Width(row[:strings.LastIndex(row, "│ Plot ")])
		if c <= lastPlot+plotCardWidth-2 {
			t.Fatalf("scarecrow at col %d overlaps the fourth plot (starts %d)", c, lastPlot)
		}
	}

	// Pouches: small values in full, huge ones compacted so they fit the card.
	for _, want := range []string{"⛀ 0", "⛀ 12", "⛀ 99,999", "⛀ 123.4M"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing pouch %q", want)
		}
	}
}

// A small farm with a tall scarecrow column still draws every card whole.
func TestScarecrowColumnTallerThanThePlotGrid(t *testing.T) {
	g := scarecrowFarm(t, canvasMaxWidth, canvasMaxHeight, 3, 1, 2, 3, 4)
	assertNoLineOverflows(t, g, "3 plots")
	out := stripAnsi(view(g))
	if strings.Count(out, scareBot) != 4 || strings.Count(out, plotBot) != 3 {
		t.Fatalf("cards clipped:\n%s", out)
	}
}

// Every width the grid can be drawn at: nothing overflows, and either the
// whole grid plus column is drawn or the farm falls back to the compact list.
func TestScarecrowLayoutAcrossWidths(t *testing.T) {
	for w := minWidth; w <= canvasMaxWidth; w++ {
		g := scarecrowFarm(t, w, canvasMaxHeight, 16, 5, 6, 7, 8)
		assertNoLineOverflows(t, g, "width "+itoa(w))
		out := stripAnsi(view(g))
		if g.compactFarm() {
			for i := 1; i <= 4; i++ {
				if !strings.Contains(out, "Scarecrow "+itoa(i)) {
					t.Fatalf("width %d compact list missing Scarecrow %d:\n%s", w, i, out)
				}
			}
			continue
		}
		if strings.Count(out, plotBot) != 16 || strings.Count(out, scareBot) != 4 {
			t.Fatalf("width %d: grid drawn but cards clipped:\n%s", w, out)
		}
	}
}

// Worst case for height: an event bar, the news banner and four notices. If
// the farm chooses the grid it must draw every card whole. (Before this
// change a 16-plot farm at 100x30 drew the grid and lost its bottom row
// whenever notices stacked up.)
func TestFarmGridNeverClipsAtShortHeights(t *testing.T) {
	for h := 24; h <= canvasMaxHeight; h++ {
		for _, scarecrows := range [][]int64{nil, {1, 2, 3, 4}} {
			g := scarecrowFarm(t, canvasMaxWidth, h, 16, scarecrows...)
			st := g.snap.State
			st.EventID = "market_day"
			st.EventStartedAt = goldenBase
			st.EventEndsAt = goldenBase + 120
			for i := 0; i < 4; i++ {
				g.addNotice("notice " + itoa(i))
			}
			if g.compactFarm() {
				continue
			}
			out := stripAnsi(view(g))
			if n := strings.Count(out, plotBot); n != 16 {
				t.Fatalf("height %d (%d scarecrows): grid chosen but only %d/16 plots whole:\n%s", h, len(scarecrows), n, out)
			}
			if n := strings.Count(out, scareBot); n != len(scarecrows) {
				t.Fatalf("height %d: %d/%d scarecrow cards whole:\n%s", h, n, len(scarecrows), out)
			}
		}
	}
}

func TestScarecrowCardShowsTheNextDrop(t *testing.T) {
	g := scarecrowFarm(t, canvasMaxWidth, canvasMaxHeight, 4, 7)
	g.snap.State.ScarecrowClock = 6 * 20 // six online seconds into a 15s drop
	out := stripAnsi(view(g))
	if !strings.Contains(out, "9s") {
		t.Fatalf("card should count down to the next drop (9s):\n%s", out)
	}
}

// --- The "for sale" card -----------------------------------------------------

func TestForSaleCardAdvertisesTheNextScarecrow(t *testing.T) {
	// Fresh farm: no clutter for a brand-new player.
	g := scarecrowFarm(t, canvasMaxWidth, canvasMaxHeight, 3)
	if strings.Contains(stripAnsi(view(g)), "for sale") {
		t.Fatal("a farm that has never earned the first price should not show a for-sale card")
	}

	g.snap.State.RunEarnings = g.content.Scarecrow.Cost
	out := stripAnsi(view(g))
	if !strings.Contains(out, "for sale") || !strings.Contains(out, money(g.content.Scarecrow.Cost)+"c") {
		t.Fatalf("once the run has earned the price, the next scarecrow is advertised:\n%s", out)
	}

	// One owned: the card shows the second price.
	g = scarecrowFarm(t, canvasMaxWidth, canvasMaxHeight, 3, 0)
	if !strings.Contains(stripAnsi(view(g)), money(g.snap.State.ScarecrowCost(g.content))+"c") {
		t.Fatal("for-sale card should quote the next scarecrow's price")
	}

	// All four owned, or forbidden by the contract: nothing for sale.
	g = scarecrowFarm(t, canvasMaxWidth, canvasMaxHeight, 3, 0, 0, 0, 0)
	if strings.Contains(stripAnsi(view(g)), "for sale") {
		t.Fatal("no for-sale card at the cap")
	}
	g = scarecrowFarm(t, canvasMaxWidth, canvasMaxHeight, 3)
	g.snap.State.RunEarnings = 1_000_000
	g.snap.State.ActiveContract = sim.ContractClockworkDenied
	if strings.Contains(stripAnsi(view(g)), "for sale") {
		t.Fatal("Clockwork Denied forbids scarecrows; don't advertise one")
	}
}

func TestClickingForSaleOpensTheMarketOnTheScarecrow(t *testing.T) {
	g := scarecrowFarm(t, canvasMaxWidth, canvasMaxHeight, 4, 0)
	view(g)
	var box *struct{ x, y int }
	for _, b := range g.hits.Boxes() {
		if b.ID == "farm:scarecrow-shop" {
			box = &struct{ x, y int }{b.X + 1, b.Y + 1}
		}
	}
	if box == nil {
		t.Fatal("for-sale card has no hitbox")
	}
	m, _ := g.handleMouseClick(tea.Mouse(tea.MouseClickMsg{X: box.x, Y: box.y, Button: tea.MouseLeft}))
	g = m.(*Game)
	if g.scr != scrMarket || g.marketItems()[g.marketIdx].kind != "scarecrow" {
		t.Fatalf("screen %v, market row %d; want the Market on the Scarecrow row", g.scr, g.marketIdx)
	}
}

// --- Navigation --------------------------------------------------------------

func TestScarecrowCursorIsRowMajor(t *testing.T) {
	g := scarecrowFarm(t, canvasMaxWidth, canvasMaxHeight, 8, 0, 0, 0) // 2 plot rows, 3 scarecrows
	at := func(wantPlot, wantScare int, after string) {
		t.Helper()
		if g.scareSel != wantScare || (wantScare < 0 && g.cursor != wantPlot) {
			t.Fatalf("after %s: cursor %d scareSel %d; want plot %d scarecrow %d", after, g.cursor, g.scareSel, wantPlot, wantScare)
		}
	}
	g.cursor = 3
	g = press(t, g, "right")
	at(-1, 0, "right off the end of row 1")
	g = press(t, g, "right")
	at(4, -1, "right from scarecrow 1")
	g = press(t, g, "left")
	at(-1, 0, "left from the start of row 2")
	g = press(t, g, "left")
	at(3, -1, "left from scarecrow 1")

	g = press(t, g, "right", "down", "down")
	at(-1, 2, "down the column")
	g = press(t, g, "down")
	at(-1, 2, "down past the last scarecrow")
	g = press(t, g, "right")
	at(-1, 2, "right from a scarecrow with no plot row after it")
	g = press(t, g, "left")
	at(7, -1, "left from a scarecrow below the grid")
	g = press(t, g, "up")
	at(3, -1, "up within the plot grid is unchanged")

	// Without scarecrows, right still wraps to the next row as it always has.
	g = scarecrowFarm(t, canvasMaxWidth, canvasMaxHeight, 8)
	g.cursor = 3
	g = press(t, g, "right")
	if g.scareSel != -1 || g.cursor != 4 {
		t.Fatalf("no scarecrows: right from plot 4 went to cursor %d scareSel %d", g.cursor, g.scareSel)
	}
}

func TestOnlyOneCardIsHighlighted(t *testing.T) {
	g := scarecrowFarm(t, canvasMaxWidth, canvasMaxHeight, 4, 0)
	selectedPlot := g.plotCard(0)
	plainScare := g.scarecrowCard(0)
	g.cursor = 1
	plainPlot := g.plotCard(0)
	if selectedPlot == plainPlot {
		t.Fatal("precondition: the plot under the cursor is drawn differently")
	}
	g.cursor, g.scareSel = 0, 0
	if g.plotCard(0) != plainPlot {
		t.Fatal("with a scarecrow selected the plot under the remembered cursor still looks selected")
	}
	if g.scarecrowCard(0) == plainScare {
		t.Fatal("the selected scarecrow is not highlighted")
	}
}

func TestFooterHintFollowsTheCursor(t *testing.T) {
	g := scarecrowFarm(t, canvasMaxWidth, canvasMaxHeight, 4, 0)
	if strings.Contains(stripAnsi(g.viewFooter()), "collect") {
		t.Fatal("plot footer should not advertise collecting")
	}
	g.scareSel = 0
	if !strings.Contains(stripAnsi(g.viewFooter()), "enter collect") {
		t.Fatalf("scarecrow footer = %q, want an enter-collect hint", stripAnsi(g.viewFooter()))
	}
}

func TestPlotKeysDoNothingOnAScarecrow(t *testing.T) {
	g := scarecrowFarm(t, canvasMaxWidth, canvasMaxHeight, 4, 0)
	g.scareSel = 0
	g = press(t, g, "u")
	if g.overlay == ovUpgrade {
		t.Fatal("u on a scarecrow opened the plot upgrade menu for a plot the player can't see selected")
	}
}

// --- Collecting --------------------------------------------------------------

func TestEnterOnAScarecrowCollectsItsPouch(t *testing.T) {
	g, _ := savedFarm(t, func(st *sim.State) {
		st.Scarecrows = []sim.Scarecrow{{Pouch: 40}, {Pouch: 0}}
	})
	coins := g.snap.State.Coins
	g = press(t, g, "right", "right", "right") // plot 3 is the end of the only row
	if g.scareSel != 0 {
		t.Fatalf("scareSel = %d, want 0", g.scareSel)
	}
	g = press(t, g, "enter")
	if g.snap.State.Coins != coins+40 || g.snap.State.Scarecrows[0].Pouch != 0 {
		t.Fatalf("coins %d→%d, pouch %d; want +40 and an empty pouch", coins, g.snap.State.Coins, g.snap.State.Scarecrows[0].Pouch)
	}
	if !strings.Contains(strings.Join(noticeTexts(g), "\n"), "40") {
		t.Fatalf("collect notice missing amount: %v", noticeTexts(g))
	}

	g.notices = nil
	g = press(t, g, "down", "enter") // the second pouch is empty
	for _, n := range noticeTexts(g) {
		if strings.Contains(n, "Hmm") {
			t.Fatalf("an empty pouch is not an error: %q", n)
		}
	}
	if len(noticeTexts(g)) == 0 {
		t.Fatal("an empty pouch should still say when the next drop lands")
	}
}

func TestHarvestAllAlsoEmptiesScarecrows(t *testing.T) {
	g, _ := savedFarm(t, func(st *sim.State) {
		st.Scarecrows = []sim.Scarecrow{{Pouch: 5}, {Pouch: 7}, {}}
	})
	coins := g.snap.State.Coins
	g = press(t, g, "a")
	if g.snap.State.Coins != coins+12 {
		t.Fatalf("coins %d→%d, want +12 from the pouches", coins, g.snap.State.Coins)
	}
	joined := strings.Join(noticeTexts(g), "\n")
	if strings.Contains(joined, "Nothing is ready") || !strings.Contains(strings.ToLower(joined), "scarecrow") {
		t.Fatalf("harvest-all notice should report the scarecrow coins: %v", noticeTexts(g))
	}

	g.notices = nil
	g = press(t, g, "a")
	if !strings.Contains(strings.Join(noticeTexts(g), "\n"), "Nothing is ready") {
		t.Fatalf("with nothing ready and empty pouches: %v", noticeTexts(g))
	}
}

func TestClickingAScarecrowSelectsThenCollects(t *testing.T) {
	g, _ := savedFarm(t, func(st *sim.State) {
		st.Scarecrows = []sim.Scarecrow{{Pouch: 9}}
	})
	coins := g.snap.State.Coins
	click := func() {
		t.Helper()
		lines := visibleLines(g)
		for y, l := range lines {
			if c := displayCol(l, scareName); c >= 0 {
				m, _ := g.handleMouseClick(tea.Mouse(tea.MouseClickMsg{X: c + 1, Y: y, Button: tea.MouseLeft}))
				g = m.(*Game)
				return
			}
		}
		t.Fatal("no scarecrow card on screen")
	}
	click()
	if g.scareSel != 0 || g.snap.State.Coins != coins {
		t.Fatalf("first click should select only: scareSel %d coins %d→%d", g.scareSel, coins, g.snap.State.Coins)
	}
	g.lastClickID = "" // a second click well outside the double-click window
	click()
	if g.snap.State.Coins != coins+9 {
		t.Fatalf("second click should collect: coins %d→%d", coins, g.snap.State.Coins)
	}
}

// Every plot card's hitbox must sit where the card is drawn. Plot cards are
// 20×5 with no gap; the hitboxes used to assume 22×5 plus a blank row, so
// clicks drifted onto the wrong plot from the second column and second row on.
func TestPlotHitboxesMatchTheDrawnCards(t *testing.T) {
	for _, scarecrows := range [][]int64{nil, {1, 2, 3, 4}} {
		g := scarecrowFarm(t, canvasMaxWidth, canvasMaxHeight, 16, scarecrows...)
		lines := visibleLines(g)
		for i := 0; i < 16; i++ {
			needle := "│ Plot " + itoa(i+1) + " "
			found := false
			for y, l := range lines {
				c := displayCol(l, needle)
				if c < 0 {
					continue
				}
				found = true
				for _, dx := range []int{0, 10, 19} { // left border, middle, right border
					box, ok := g.hits.At(c+dx, y)
					if !ok || box.ID != "plot:"+itoa(i) {
						t.Fatalf("%d scarecrows: click on Plot %d at (%d,%d) hit %q", len(scarecrows), i+1, c+dx, y, box.ID)
					}
				}
			}
			if !found {
				t.Fatalf("Plot %d not drawn", i+1)
			}
		}
	}
}

// --- Compact mode ------------------------------------------------------------

func TestCompactFarmListsScarecrowsAfterPlots(t *testing.T) {
	g := scarecrowFarm(t, 60, 30, 5, 40, 0)
	if !g.compactFarm() {
		t.Fatal("precondition: 60 wide is compact")
	}
	assertNoLineOverflows(t, g, "compact")
	out := stripAnsi(view(g))
	if !strings.Contains(out, "Scarecrow 1") || !strings.Contains(out, "Scarecrow 2") || !strings.Contains(out, "40") {
		t.Fatalf("compact list should show scarecrows and pouches:\n%s", out)
	}
	g.cursor = 4
	g = press(t, g, "down")
	if g.scareSel != 0 {
		t.Fatalf("down past the last plot should reach Scarecrow 1, got scareSel %d", g.scareSel)
	}
	g = press(t, g, "down", "down")
	if g.scareSel != 1 {
		t.Fatalf("down should stop at the last scarecrow, got %d", g.scareSel)
	}
	g = press(t, g, "up", "up")
	if g.scareSel != -1 || g.cursor != 4 {
		t.Fatalf("up from Scarecrow 1 should return to the last plot: cursor %d scareSel %d", g.cursor, g.scareSel)
	}

	// The compact hitboxes line up with the rows that mention each scarecrow.
	view(g)
	lines := visibleLines(g)
	for y, l := range lines {
		if strings.Contains(l, "Scarecrow 2") {
			box, ok := g.hits.At(g.layout.cx+2, y)
			if !ok || box.ID != "scarecrow:1" {
				t.Fatalf("compact Scarecrow 2 row hit %q", box.ID)
			}
		}
	}
}

// Compact plot rows used a different row budget for drawing and for hitboxes,
// so once the list scrolled, clicks landed on the wrong plot.
func TestCompactPlotHitboxesFollowScrolling(t *testing.T) {
	g := scarecrowFarm(t, 60, 24, 16)
	g.cursor = 15 // scrolled to the bottom
	lines := visibleLines(g)
	for y, l := range lines {
		for i := 0; i < 16; i++ {
			if strings.Contains(l, " "+itoa(i+1)+". ") {
				box, ok := g.hits.At(g.layout.cx+2, y)
				if !ok || box.ID != "plot:"+itoa(i) {
					t.Fatalf("row for plot %d at y=%d hit %q", i+1, y, box.ID)
				}
			}
		}
	}
}

// --- Copy --------------------------------------------------------------------

func TestMarketRowCountsScarecrows(t *testing.T) {
	g := scarecrowFarm(t, canvasMaxWidth, canvasMaxHeight, 3, 0)
	g.snap.State.Coins = 1_000_000
	g.scr = scrMarket
	out := stripAnsi(view(g))
	if !strings.Contains(out, "Scarecrow 1/4") || !strings.Contains(out, money(g.snap.State.ScarecrowCost(g.content))+"c") {
		t.Fatalf("market should show owned/max and the next price:\n%s", out)
	}
	g = scarecrowFarm(t, canvasMaxWidth, canvasMaxHeight, 3, 0, 0, 0, 0)
	g.scr = scrMarket
	if out := stripAnsi(view(g)); !strings.Contains(out, "Scarecrow 4/4") || !strings.Contains(out, "maxed") {
		t.Fatalf("market should show the cap:\n%s", out)
	}
}

func TestHelpDescribesTheNewScarecrow(t *testing.T) {
	g := scarecrowFarm(t, canvasMaxWidth, canvasMaxHeight, 3)
	gameplay := stripAnsi(g.viewHelpGameplay())
	for _, want := range []string{"Scarecrow", "collect", "every " + duration(g.content.Scarecrow.DropIntervalSec), "4"} {
		if !strings.Contains(gameplay, want) {
			t.Errorf("gameplay help missing %q", want)
		}
	}
	for _, stale := range []string{"shoo them automatically", "bank the coins", "trickles"} {
		if strings.Contains(gameplay, stale) {
			t.Errorf("gameplay help still describes the old scarecrow: %q", stale)
		}
	}
	if !strings.Contains(gameplay, "scarecrows") {
		t.Error("rebirth help should list scarecrows among what a rebirth loses")
	}
	for p := 0; p < tutorialPages; p++ {
		_, body := g.tutorialContent(p)
		if strings.Contains(body, "critter-chasing") {
			t.Errorf("tutorial page %d still sells a critter-chasing scarecrow", p+1)
		}
	}
}

func TestWelcomeBackReportsScarecrowGains(t *testing.T) {
	g := scarecrowFarm(t, canvasMaxWidth, canvasMaxHeight, 3, 120)
	g.away = sim.Events{Elapsed: 3600, ScarecrowCoins: 120}
	out := stripAnsi(g.viewAway())
	if !strings.Contains(out, "120") || !strings.Contains(strings.ToLower(out), "scarecrow") {
		t.Fatalf("welcome back should report what the scarecrows gathered:\n%s", out)
	}
}

// A drop every 15 seconds must not become a notice every 15 seconds.
func TestOnlineDropsDoNotSpamNotices(t *testing.T) {
	g, now := savedFarm(t, func(st *sim.State) {
		st.Scarecrows = make([]sim.Scarecrow, 4)
	})
	for s := int64(1); s <= 31; s++ {
		g, _ = tick(t, g, now+s)
	}
	if g.snap.State.ScarecrowPouchTotal() == 0 {
		t.Fatal("precondition: 31 online seconds should have dropped coins")
	}
	for _, n := range noticeTexts(g) {
		if strings.Contains(strings.ToLower(n), "scarecrow") {
			t.Fatalf("online drop produced a notice: %q", n)
		}
	}
}

func TestRebirthScreenCountsPouches(t *testing.T) {
	g := scarecrowFarm(t, canvasMaxWidth, canvasMaxHeight, 3, 300, 200)
	g.snap.State.RunEarnings = 20_000
	g.scr = scrRebirth
	out := stripAnsi(view(g))
	if !strings.Contains(out, "500 in scarecrow pouches") {
		t.Fatalf("rebirth screen should say the pouches count toward the payout:\n%s", out)
	}
	if !strings.Contains(out, "scarecrows") || !strings.Contains(out, money(g.snap.State.PrestigeGain(g.content))) {
		t.Fatalf("rebirth screen should list scarecrows as lost and preview the pouch-inclusive gain:\n%s", out)
	}
}

// The clipping fix must not over-correct: the recommended 100x30 terminal
// keeps the card grid for a full farm and its scarecrows, and a burst of
// notices shows the newest ones that fit instead of eating the bottom row.
func TestFullFarmKeepsTheGridAt100x30(t *testing.T) {
	g := scarecrowFarm(t, 100, 30, 16, 1, 2, 3, 4)
	for i := 0; i < 4; i++ {
		g.addNotice("notice " + itoa(i))
	}
	if g.compactFarm() {
		t.Fatal("a 16-plot farm with four scarecrows should stay a card grid at 100x30")
	}
	out := stripAnsi(view(g))
	if strings.Count(out, plotBot) != 16 || strings.Count(out, scareBot) != 4 {
		t.Fatalf("cards clipped at 100x30:\n%s", out)
	}
	if !strings.Contains(out, "notice 3") {
		t.Fatalf("the newest notice must stay visible:\n%s", out)
	}
	assertNoLineOverflows(t, g, "100x30")
}
