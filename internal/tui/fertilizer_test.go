package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-farm/internal/sim"
)

// fertFarm is a saved farm with a growing turnip on plot 0 (and a ripe one
// on plot 1), opened through a real session so F goes through the actor.
func fertFarm(t *testing.T, mutate func(st *sim.State)) (*Game, int64) {
	t.Helper()
	var planted int64
	g, now := savedFarm(t, func(st *sim.State) {
		planted = st.UpdatedAt
		st.Plots[0] = sim.Plot{Crop: "turnip", PlantedAt: planted}
		st.Plots[1] = sim.Plot{Crop: "turnip", PlantedAt: planted - 600}
		if mutate != nil {
			mutate(st)
		}
	})
	return g, now
}

// textAtHit returns the rendered text under a hitbox, so a test can prove
// the click target sits on the words it claims to.
func textAtHit(t *testing.T, g *Game, id string) string {
	t.Helper()
	lines := strings.Split(view(g), "\n")
	for _, b := range g.hits.Boxes() {
		if b.ID == id {
			if b.Y < 0 || b.Y >= len(lines) {
				t.Fatalf("hitbox %s row %d is off screen", id, b.Y)
			}
			return cutCells(stripAnsi(lines[b.Y]), b.X, b.X+b.W)
		}
	}
	t.Fatalf("no hitbox %s", id)
	return ""
}

// cutCells returns the display cells [from, to) of a plain line.
func cutCells(line string, from, to int) string {
	var b strings.Builder
	x := 0
	for _, r := range line {
		if x >= from && x < to {
			b.WriteRune(r)
		}
		x += lipgloss.Width(string(r))
	}
	return b.String()
}

func clickHit(t *testing.T, g *Game, id string) *Game {
	t.Helper()
	for _, b := range g.hits.Boxes() {
		if b.ID == id {
			m, _ := g.handleMouseClick(tea.Mouse(tea.MouseClickMsg{X: b.X, Y: b.Y, Button: tea.MouseLeft}))
			return m.(*Game)
		}
	}
	t.Fatalf("no hitbox %s", id)
	return g
}

func TestHeaderShowsFertilizerBesideStarseeds(t *testing.T) {
	g := goldenGame(t, canvasMaxWidth, canvasMaxHeight)
	header := stripAnsi(g.viewHeader())
	if !strings.Contains(header, "⛀ 25 coins  ϟ1/3") {
		t.Fatalf("a fresh farm's header should show its starter fertilizer:\n%s", header)
	}

	st := g.snap.State
	st.Rebirths, st.PrestigeCurrency = 1, 12
	st.ContractsCompleted = 2
	header = stripAnsi(g.viewHeader())
	if !strings.Contains(header, "✦ 12 Starseeds  ϟ1/5") {
		t.Fatalf("fertilizer should sit right of the Starseeds, capped at 3+contracts:\n%s", header)
	}
}

// Huge balances must not push the farm name (or the row) off the screen:
// the wallet drops its words, then abbreviates.
func TestHeaderWalletCompactsLongBalances(t *testing.T) {
	// At 80 columns dropping the words is enough; at 60 it must abbreviate.
	// (The 36-column minimum already clips the wallet on main: the slot and
	// clock run is fixed priority, so that is out of this row's hands.)
	for _, tc := range []struct {
		w          int
		want, gone string
	}{
		{80, "⛀ 9,123,456,789,012  ✦ 1,234,567  ϟ1/3", "coins"},
		{60, "⛀ 9.1T  ✦ 1.2M  ϟ1/3", "9,123"},
	} {
		g := goldenGame(t, tc.w, 24)
		st := g.snap.State
		st.FarmName = "Northfield"
		st.Coins = 9_123_456_789_012
		st.Rebirths, st.PrestigeCurrency = 40, 1_234_567
		first := strings.Split(g.viewHeader(), "\n")[0]
		plain := stripAnsi(first)
		if lw := lipgloss.Width(first); lw > g.contentWidth() {
			t.Errorf("width %d: header is %d wide, content is %d:\n%s", tc.w, lw, g.contentWidth(), plain)
		}
		if !strings.Contains(plain, tc.want) || strings.Contains(plain, tc.gone) {
			t.Errorf("width %d: want %q and no %q:\n%s", tc.w, tc.want, tc.gone, plain)
		}
		if !strings.Contains(plain, "Northfield") {
			t.Errorf("width %d: compaction should leave room for the farm name:\n%s", tc.w, plain)
		}
	}

	// Ordinary balances keep their words.
	g := goldenGame(t, 80, 24)
	if plain := stripAnsi(g.viewHeader()); !strings.Contains(plain, "⛀ 25 coins") {
		t.Fatalf("a short balance should not be compacted:\n%s", plain)
	}
}

func TestFarmFooterHints(t *testing.T) {
	g := goldenGame(t, canvasMaxWidth, canvasMaxHeight)
	footer := stripAnsi(g.viewFooter())
	if !strings.HasSuffix(footer, "F fertilizer · q leave") {
		t.Fatalf("F fertilizer should sit just before q leave: %q", footer)
	}
	for _, gone := range []string{"plant/harvest", "g gift"} {
		if strings.Contains(footer, gone) {
			t.Fatalf("farm footer still lists %q: %q", gone, footer)
		}
	}
	g.scareSel = 0
	g.snap.State.Scarecrows = []sim.Scarecrow{{}}
	if footer := stripAnsi(g.viewFooter()); strings.Contains(footer, "g gift") || !strings.Contains(footer, "F fertilizer · q leave") {
		t.Fatalf("scarecrow footer: %q", footer)
	}
	// At a stock 80 columns the whole hint still fits, ending in q leave.
	g = resize(t, g, 80, 24)
	g.scareSel = -1
	if footer := stripAnsi(g.viewFooter()); !strings.HasSuffix(footer, "F fertilizer · q leave") {
		t.Fatalf("80-column farm footer is cut short: %q", footer)
	}
	// Enter is no longer in the footer, so the first-run tutorial must teach it.
	_, body := g.tutorialContent(1)
	if !strings.Contains(body, "enter on a plot plants or harvests") || !strings.Contains(body, "F  spreads fertilizer") {
		t.Fatalf("tutorial page 2 should teach enter and F:\n%s", body)
	}
}

func TestFertilizerPromptThenUse(t *testing.T) {
	g, now := fertFarm(t, nil)
	ripeAt := g.snap.State.Plots[1].PlantedAt

	g = press(t, g, "F")
	if g.overlay != ovFertilizerConfirm {
		t.Fatalf("F should ask first, overlay = %v", g.overlay)
	}
	out := stripAnsi(view(g))
	for _, want := range []string{"Use fertilizer?", "ϟ1/3", "1 plot is growing", "leaving 0", "[ ] Don't ask again (d)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("prompt missing %q:\n%s", want, out)
		}
	}
	g = press(t, g, "enter")
	if g.overlay != ovFertilizerConfirm || g.snap.State.Fertilizer() != 1 {
		t.Fatal("enter must not confirm: it is the farm's harvest key")
	}
	g = press(t, g, "n")
	if g.overlay != ovNone || g.snap.State.Fertilizer() != 1 || g.snap.State.PlotReady(g.content, 0, now) {
		t.Fatal("cancelling the prompt still used the fertilizer")
	}

	g = press(t, g, "F", "y")
	st := g.snap.State
	if g.overlay != ovNone {
		t.Fatalf("prompt stayed open: %v", g.overlay)
	}
	if st.Fertilizer() != 0 || !st.PlotReady(g.content, 0, now) {
		t.Fatalf("y should ripen plot 0 and spend the fertilizer (left %d)", st.Fertilizer())
	}
	if st.Plots[1].PlantedAt != ripeAt {
		t.Fatal("an already-ripe plot was moved")
	}
	if st.FertilizerNoConfirm {
		t.Fatal("confirming without the box ticked turned the prompt off")
	}
	if n := noticeTexts(g); len(n) == 0 || !strings.Contains(n[len(n)-1], "1 crop ripened. ϟ0/3 left.") {
		t.Fatalf("notices %q", n)
	}
	if !strings.Contains(stripAnsi(g.viewHeader()), "ϟ0/3") {
		t.Fatal("header did not update to ϟ0/3")
	}
}

func TestFertilizerDontAskAgain(t *testing.T) {
	g, _ := fertFarm(t, func(st *sim.State) { st.FertilizerHeld, st.FertilizerTracked = 3, true })

	// Ticking the box and then cancelling saves nothing.
	g = press(t, g, "F", "d")
	if !strings.Contains(stripAnsi(view(g)), "[x] Don't ask again") {
		t.Fatal("d did not tick the box")
	}
	g = press(t, g, "esc")
	if g.snap.State.FertilizerNoConfirm || g.snap.State.Fertilizer() != 3 {
		t.Fatal("cancelling with the box ticked still changed the save")
	}

	// The box resets each time the prompt opens; tick it and confirm.
	g = press(t, g, "F")
	if g.fertDontAsk {
		t.Fatal("the box should start unticked every time")
	}
	g = press(t, g, "d", "y")
	if !g.snap.State.FertilizerNoConfirm || g.snap.State.Fertilizer() != 2 {
		t.Fatalf("confirm with the box ticked: no-confirm %v, held %d", g.snap.State.FertilizerNoConfirm, g.snap.State.Fertilizer())
	}

	// Next time F acts at once — once something is growing again.
	g = press(t, g, "1")
	g.cursor = 0
	g = press(t, g, "enter") // harvest the ripe turnip
	g = press(t, g, "enter") // open the picker on the empty plot
	g = press(t, g, "enter") // plant
	if g.snap.State.GrowingPlots(g.content, g.now) == 0 {
		t.Fatalf("setup: nothing replanted; notices %q", noticeTexts(g))
	}
	g = press(t, g, "F")
	if g.overlay != ovNone || g.snap.State.Fertilizer() != 1 {
		t.Fatalf("with the prompt off F should act at once: overlay %v, held %d", g.overlay, g.snap.State.Fertilizer())
	}

	// Settings brings the prompt back.
	g.scr = scrStats
	g = press(t, g, "c")
	rows := g.configRows()
	idx := len(rows) - 1
	if rows[idx].label != "Fertilizer prompt" || rows[idx].on {
		t.Fatalf("last settings row = %q on=%v, want the fertilizer prompt, off", rows[idx].label, rows[idx].on)
	}
	g.configIdx = idx
	g = press(t, g, "enter")
	if g.snap.State.FertilizerNoConfirm {
		t.Fatal("the settings row did not turn the prompt back on")
	}
}

func TestFertilizerKeyExplainsWhenItCannotWork(t *testing.T) {
	g, _ := savedFarm(t, func(st *sim.State) {})
	g = press(t, g, "F")
	if g.overlay != ovNone {
		t.Fatal("F on a bare farm should not open a prompt")
	}
	if n := noticeTexts(g); len(n) == 0 || !strings.Contains(n[len(n)-1], "Nothing is growing") {
		t.Fatalf("notices %q", n)
	}

	g, _ = fertFarm(t, func(st *sim.State) { st.FertilizerHeld, st.FertilizerTracked = 0, true })
	g = press(t, g, "F")
	if g.overlay != ovNone {
		t.Fatal("F with none held should not open a prompt")
	}
	if n := noticeTexts(g); len(n) == 0 || !strings.Contains(n[len(n)-1], "No fertilizer left. The StarShop sells it for ✦ 5.") {
		t.Fatalf("notices %q", n)
	}
}

func TestFertilizerPromptMouse(t *testing.T) {
	g, now := fertFarm(t, nil)
	g = press(t, g, "F")
	view(g)
	if got := textAtHit(t, g, "fertilizer:dontask"); got != "[ ] Don't ask again (d)" {
		t.Fatalf("checkbox hitbox covers %q", got)
	}
	if got := textAtHit(t, g, "fertilizer:use"); got != fertilizerUseLabel {
		t.Fatalf("use hitbox covers %q", got)
	}
	if got := textAtHit(t, g, "fertilizer:cancel"); got != fertilizerCancelLabel {
		t.Fatalf("cancel hitbox covers %q", got)
	}

	g = clickHit(t, g, "fertilizer:dontask")
	if !g.fertDontAsk {
		t.Fatal("clicking the checkbox did not tick it")
	}
	view(g)
	g = clickHit(t, g, "fertilizer:cancel")
	if g.overlay != ovNone || g.snap.State.Fertilizer() != 1 {
		t.Fatal("cancel click used the fertilizer")
	}

	g = press(t, g, "F")
	view(g)
	g = clickHit(t, g, "fertilizer:use")
	if g.overlay != ovNone || g.snap.State.Fertilizer() != 0 || !g.snap.State.PlotReady(g.content, 0, now) {
		t.Fatal("use click did not spread the fertilizer")
	}
}

func TestStarShopSellsFertilizer(t *testing.T) {
	g, _ := savedFarm(t, func(st *sim.State) {
		st.Rebirths = 1
		st.PrestigeCurrency = 11
	})
	g = press(t, g, "5")
	idx := fertilizerRowIdx(g.content)
	if idx != len(g.content.Upgrades) {
		t.Fatalf("fertilizer row %d, want it right after the upgrades", idx)
	}
	for range idx {
		g = press(t, g, "down")
	}
	if g.progressIdx != idx {
		t.Fatalf("cursor at %d, want the fertilizer row %d", g.progressIdx, idx)
	}
	if out := stripAnsi(view(g)); !strings.Contains(out, "ϟ Fertilizer (1/3) — Ripens every growing crop at once (F).") {
		t.Fatalf("StarShop has no fertilizer row:\n%s", out)
	}

	g = press(t, g, "enter")
	if st := g.snap.State; st.Fertilizer() != 2 || st.PrestigeCurrency != 6 {
		t.Fatalf("after buying: held %d, Starseeds %d", st.Fertilizer(), st.PrestigeCurrency)
	}

	// Mouse: the row's hitbox sits on the row, and a double-click buys.
	view(g)
	if got := textAtHit(t, g, "shop:"+itoa(idx)); !strings.Contains(got, "Fertilizer (2/3)") {
		t.Fatalf("shop hitbox %d covers %q", idx, got)
	}
	g = clickHit(t, g, "shop:"+itoa(idx))
	view(g)
	g = clickHit(t, g, "shop:"+itoa(idx))
	if st := g.snap.State; st.Fertilizer() != 3 || st.PrestigeCurrency != 1 {
		t.Fatalf("after double-click: held %d, Starseeds %d", st.Fertilizer(), st.PrestigeCurrency)
	}

	g.snap.State.PrestigeCurrency = 50
	g = press(t, g, "enter")
	if g.snap.State.Fertilizer() != 3 {
		t.Fatal("bought past the cap")
	}
	if n := noticeTexts(g); len(n) == 0 || !strings.Contains(n[len(n)-1], "sack is full (ϟ3/3)") {
		t.Fatalf("notices %q", n)
	}
	if out := stripAnsi(view(g)); !strings.Contains(out, "✓ ϟ Fertilizer (3/3)") || !strings.Contains(out, "full") {
		t.Fatalf("a full sack should read as full:\n%s", out)
	}
}

func TestParcelFertilizerNotices(t *testing.T) {
	g, now := fertFarm(t, func(st *sim.State) { st.GiftPending = true })
	g.content.Fertilizer.DropChancePct = 100
	g = press(t, g, "g")
	if n := noticeTexts(g); len(n) == 0 || !strings.Contains(n[len(n)-1], "Fertilizer! ϟ2/3 (F to use)") {
		t.Fatalf("notices %q", n)
	}

	g, now = fertFarm(t, func(st *sim.State) {
		st.GiftPending = true
		st.FertilizerHeld, st.FertilizerTracked = 3, true
	})
	g.content.Fertilizer.DropChancePct = 100
	g = press(t, g, "g")
	if n := noticeTexts(g); len(n) == 0 || !strings.Contains(n[len(n)-1], "sack was full, so it went straight on: 1 crop ripened.") {
		t.Fatalf("notices %q", n)
	}
	if !g.snap.State.PlotReady(g.content, 0, now) || g.snap.State.Fertilizer() != 3 {
		t.Fatal("an at-cap parcel should ripen the farm and leave the count alone")
	}
}

func TestContractRewardShowsFertilizerRefill(t *testing.T) {
	g := contractGame(t, 0)
	g.snap.State.ContractsCompleted = 1
	g.snap.State.FertilizerHeld, g.snap.State.FertilizerTracked = 4, true
	g.completedContract = sim.ContractBareHands
	g.overlay = ovContractReward
	if out := stripAnsi(view(g)); !strings.Contains(out, "Fertilizer sack grown and refilled: ϟ4/4") {
		t.Fatalf("reward modal:\n%s", out)
	}
}

// The tutorial is where enter is now taught, so every page must render its
// bullets one per line, fit a stock 80x24 terminal unclipped, and keep its
// Skip/Continue click targets on the words.
func TestTutorialPagesRenderBulletsAndClickTargets(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {canvasMaxWidth, canvasMaxHeight}} {
		for page := 0; page < tutorialPages; page++ {
			g := goldenGame(t, sz[0], sz[1])
			g.overlay, g.tutorialPage = ovTutorial, page
			box := g.viewTutorial()
			if h := lipgloss.Height(box); h > g.computeLayout().bodyH {
				t.Errorf("%dx%d page %d: tutorial is %d rows, body has %d", sz[0], sz[1], page+1, h, g.computeLayout().bodyH)
			}
			out := stripAnsi(view(g))
			_, body := g.tutorialContent(page)
			for _, line := range strings.Split(body, "\n") {
				if b, ok := strings.CutPrefix(line, "• "); ok {
					first := strings.Fields(b)
					if !strings.Contains(out, "• "+strings.Join(first[:min(3, len(first))], " ")) {
						t.Errorf("%dx%d page %d: bullet %q does not start its own line:\n%s", sz[0], sz[1], page+1, line, out)
					}
				}
			}
			if got := textAtHit(t, g, "tutorial:skip"); !strings.HasSuffix(got, "Skip") {
				t.Errorf("%dx%d page %d: skip hitbox covers %q", sz[0], sz[1], page+1, got)
			}
			if got := textAtHit(t, g, "tutorial:next"); !strings.HasPrefix(got, "Continue") {
				t.Errorf("%dx%d page %d: continue hitbox covers %q", sz[0], sz[1], page+1, got)
			}
		}
	}
}

func TestTutorialClicks(t *testing.T) {
	g := goldenGame(t, 80, 24)
	g.overlay, g.tutorialPage, g.tutorialSkip = ovTutorial, 0, false
	view(g)
	g = clickHit(t, g, "tutorial:next")
	if g.overlay != ovTutorial || g.tutorialPage != 1 {
		t.Fatalf("Continue click: overlay %v page %d", g.overlay, g.tutorialPage)
	}
	view(g)
	g = clickHit(t, g, "tutorial:skip")
	if !g.tutorialSkip {
		t.Fatal("Skip click did not tick skip")
	}
}
