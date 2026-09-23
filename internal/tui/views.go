package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-farm/internal/content"
	"github.com/mynameis-nigel/ssh-farm/internal/sim"
	"github.com/mynameis-nigel/ssh-farm/internal/version"
)

// All styling now lives in internal/tui/theme: a per-frame Theme carries a
// background on every style, which the old package-level vars could not.

func (g *Game) View() tea.View {
	g.hits.Reset()
	if g.width < minWidth || g.height < minHeight {
		return g.fullscreen("This farm needs a bigger window\n(at least 36×10). Resize to play,\nor press q to leave.")
	}
	g.registerHitboxes()
	if g.overlay != ovNone {
		return g.fullscreen(g.composeCanvas(g.overlayBox(), true))
	}
	return g.fullscreen(g.composeCanvas(g.screenBody(), false))
}

func (g *Game) overlayBox() string {
	switch g.overlay {
	case ovTutorial:
		return g.viewTutorial()
	case ovAway:
		return g.viewAway()
	case ovPicker:
		return g.viewPicker()
	case ovUpgrade:
		return g.viewUpgrade()
	case ovRebirthConfirm:
		return g.viewRebirthConfirm()
	case ovName:
		return g.viewName()
	case ovConfig:
		return g.viewConfig()
	case ovReplantWarn:
		return g.viewReplantWarn()
	case ovContractConfirm:
		return g.viewContractConfirm()
	case ovContractReward:
		return g.viewContractReward()
	case ovKicked:
		return g.viewKicked()
	}
	return ""
}

func (g *Game) screenBody() string {
	switch g.scr {
	case scrFarm:
		return g.viewFarm()
	case scrMarket:
		return g.viewMarket()
	case scrLand:
		return g.viewLand()
	case scrRebirth:
		return g.viewRebirth()
	case scrStarShop:
		return g.viewStarShop()
	case scrStats:
		return g.viewStats()
	case scrBoard:
		return g.viewBoard()
	case scrHelp:
		return g.viewHelp()
	case scrContracts:
		return g.viewContracts()
	}
	return ""
}

func (g *Game) viewHeader() string {
	th := g.theme()
	st := g.snap.State

	// The wallet and the identity run (slot and clock) are fixed priority and
	// never shrink, so both are built and measured before the farm title, which
	// is the only part of this row allowed to give up columns.
	right := th.Value.Render("⛀ " + money(st.Coins) + " coins")
	if st.Rebirths > 0 || st.PrestigeCurrency > 0 {
		right += th.Header.Render("  ✦ " + money(st.PrestigeCurrency) + " " + g.starseedLabel())
	}
	tail := th.Header.Render("  ·  " + sanitizeText(g.id.Slot) + "  ·  " + formatClock(g.now))

	name := "ssh-farm"
	if st.FarmName != "" {
		name = sanitizeText(st.FarmName)
	}
	// titlePrefix is structural, so truncation eats into the name only and a
	// narrow row still opens with the farm glyph rather than a bare separator.
	// The trailing -1 reserves the one column the gap clamp below guarantees.
	// truncate counts runes while budget counts display columns; they agree for
	// the ASCII names this row is sized around, and a wide rune inside a name
	// can still cost one extra column.
	titlePrefix := g.titleGlyph()
	budget := g.contentWidth() - lipgloss.Width(right) - lipgloss.Width(tail) - lipgloss.Width(titlePrefix) - 1
	left := th.Title.Render(titlePrefix+truncate(name, budget)) + tail

	gap := g.contentWidth() - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	header := left + strings.Repeat(" ", gap) + right

	// Extra framed header rows, in priority order. The seasonal sky is the one
	// deliberate exception: composeCanvas renders it outside the top border
	// and coords.go reserves its row independently.
	for _, row := range []string{g.viewEventBar(), g.viewBanner()} {
		if row != "" {
			header += "\n" + row
		}
	}
	return header
}

func (g *Game) viewBanner() string {
	th := g.theme()
	st := g.snap.State
	var parts []string
	if st.GiftPending {
		parts = append(parts, th.Ready.Render("📦 A parcel waits at the gate — press g"))
	}
	if len(parts) == 0 {
		if !g.snap.State.NewsEnabled {
			return ""
		}
		return th.Banner.Render("📰 The Daily Furrow: " + g.dailyHeadline())
	}
	return strings.Join(parts, "  ")
}

func (g *Game) dailyHeadline() string {
	st := g.snap.State
	// No event branch here: the event bar sits directly above the headline
	// and already names the event, its effect and its countdown. This used to
	// be unreachable anyway — viewBanner only reached dailyHeadline when no
	// event was running — so restating it would have been new duplication.
	if st.GiftPending {
		return "PARCEL DELIVERY UP ACROSS THE COUNTY"
	}
	if seasonal := g.seasonalHeadline(); seasonal != "" {
		return seasonal
	}
	if len(g.content.Headlines) == 0 {
		return "ALL QUIET ON THE HOMESTEAD"
	}
	idx := int(g.now/3600) % len(g.content.Headlines)
	return strings.ToUpper(sanitizeText(g.content.Headlines[idx].Text))
}

// navRowContent is what renders on the nav strip's row: the screen tabs
// alone when no overlay is open, or the (dimmed, unclickable) tabs plus a
// clickable "[x] Close" affordance while one is — mouse users otherwise have
// no visible way to dismiss a menu without the keyboard. ovKicked keeps the
// plain tabs since the session is already ending.
func (g *Game) navRowContent() string {
	th := g.theme()
	tabs := g.viewNav()
	if g.overlay != ovNone && g.overlay != ovKicked {
		return tabs + "  " + th.NavOn.Render("[x] Close")
	}
	return tabs
}

// navLabel is one tab on the nav strip.
type navLabel struct {
	s    screen
	text string
	lock bool
	warn bool // render in the Warn style rather than the usual nav styles
}

// navLabels is the single source of truth for the nav strip: viewNav renders
// it and hitboxes.go:registerNavHits measures it. They used to hold separate
// copies of this list, which meant a conditional label — like the Help tab's
// size indicator — would drift the rendered row away from the hitboxes and
// move the click target of every tab after it.
func (g *Game) navLabels() []navLabel {
	return []navLabel{
		{s: scrFarm, text: "1 Farm"},
		{s: scrMarket, text: "2 Market"},
		{s: scrLand, text: "3 Land"},
		{s: scrRebirth, text: "4 Rebirth"},
		{s: scrStarShop, text: "5 StarShop", lock: g.snap.State.ProgressionRebirths() < 1 && !g.snap.State.ContractsAvailable(g.content)},
		{s: scrStats, text: "6 Stats"},
		{s: scrBoard, text: "7 Board"},
		{s: scrHelp, text: g.helpNavLabel(), warn: g.undersized()},
	}
}

// navStyle picks the style for one tab. It is shared with registerNavHits so
// the measured width always matches the rendered width — Bold can change it.
func (g *Game) navStyle(l navLabel) lipgloss.Style {
	th := g.theme()
	switch {
	case l.s == g.scr && g.overlay == ovNone:
		return th.NavOn
	case l.lock:
		return th.NavLock
	case l.warn:
		return th.NavWarn
	default:
		return th.NavOff
	}
}

func (g *Game) viewNav() string {
	labels := g.navLabels()
	parts := make([]string, 0, len(labels))
	for _, l := range labels {
		text := l.text
		if l.lock {
			text += " 🔒"
		}
		parts = append(parts, g.navStyle(l).Render(text))
	}
	return strings.Join(parts, "")
}

func (g *Game) viewNotices() string {
	th := g.theme()
	if len(g.notices) == 0 {
		return ""
	}
	lines := make([]string, 0, len(g.notices))
	for _, n := range g.notices {
		lines = append(lines, th.Notice.Render(truncate(n.text, g.contentWidth()-2)))
	}
	return strings.Join(lines, "\n")
}

func (g *Game) viewFooter() string {
	th := g.theme()
	var hints string
	switch {
	case g.overlay == ovPicker && g.pickerAutoSow:
		hints = "↑/↓ choose · enter queue auto-sow · esc/q close"
	case g.overlay == ovPicker:
		hints = "↑/↓ choose · enter plant · esc/q close"
	case g.overlay == ovUpgrade:
		hints = "↑/↓ plot · 1 auto-harvest · 2 auto-sow · esc/q close"
	case g.overlay == ovName:
		hints = "type name · enter save · esc/q close"
	case g.overlay == ovRebirthConfirm:
		hints = "y rebirth · n/esc/q keep farming"
	case g.overlay == ovConfig:
		hints = "↑/↓ select · enter toggle · esc/q close"
	case g.overlay == ovReplantWarn:
		hints = "press any key to continue"
	case g.overlay == ovContractConfirm:
		hints = "y confirm · n/esc/q cancel"
	case g.overlay == ovContractReward:
		hints = "press any key to continue"
	case g.overlay == ovTutorial:
		hints = "s toggle skip · enter continue · ←/→ pages"
	case g.overlay == ovAway:
		hints = "press any key to continue"
	case g.scr == scrFarm:
		hints = "←↑↓→ · enter plant/harvest · a harvest all · r replant all · u upgrades · g gift · q leave"
	case g.scr == scrMarket:
		hints = "↑/↓ select · enter buy · q quit"
	case g.scr == scrLand:
		hints = "enter buy plot · q quit"
	case g.scr == scrRebirth:
		hints = "R rebirth · q quit"
	case g.scr == scrStarShop:
		hints = "↑/↓ select · enter buy · q quit"
		if g.snap.State.ContractsAvailable(g.content) {
			hints = "↑/↓ select · enter buy · c contracts · q quit"
		}
	case g.scr == scrContracts:
		hints = "↑/↓ inspect · enter accept · a abandon active · esc back"
	case g.scr == scrStats && len(g.snap.State.AvailableLeaderboardNameStyles()) > 1:
		hints = "n name farm · l board style · c config · q quit"
	case g.scr == scrStats:
		hints = "n name farm · c config · q quit"
	case g.scr == scrBoard:
		hints = "↑/↓/wheel scroll · n rename farm · r refresh · esc back"
	default:
		hints = "1-6 screens · tab/shift+tab cycle · g gift · q leave"
	}
	return th.Hint.Render(truncate(hints, g.contentWidth()-1))
}

func (g *Game) farmColumns() int {
	if g.compactFarm() {
		return 1
	}
	cols := g.contentWidth() / 22
	if cols < 1 {
		cols = 1
	}
	if cols > 6 {
		cols = 6
	}
	return cols
}

func (g *Game) compactFarm() bool {
	cw, chh := g.contentWidth(), g.contentHeight()
	rowsNeeded := (len(g.snap.State.Plots) + cw/22 - 1) / max(cw/22, 1)
	return cw < 66 || chh < rowsNeeded*3+10
}

func (g *Game) viewFarm() string {
	st := g.snap.State
	if g.cursor >= len(st.Plots) {
		g.cursor = len(st.Plots) - 1
	}
	if g.compactFarm() {
		return g.viewFarmCompact()
	}

	cols := g.farmColumns()
	var rows []string
	for start := 0; start < len(st.Plots); start += cols {
		end := start + cols
		if end > len(st.Plots) {
			end = len(st.Plots)
		}
		cards := make([]string, 0, cols)
		for i := start; i < end; i++ {
			cards = append(cards, g.plotCard(i))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, cards...))
	}
	return strings.Join(rows, "\n")
}

// autoSowQueueLabel returns a styled "↻→ Name" hint when a plot's auto-sow
// queue differs from the crop currently growing there, so players can see at a
// glance that the rotation is about to switch. Empty when there's no override.
func (g *Game) autoSowQueueLabel(plot sim.Plot) string {
	th := g.theme()
	if !plot.AutoSow || plot.AutoSowCrop == "" || plot.AutoSowCrop == plot.Crop {
		return ""
	}
	name := plot.AutoSowCrop
	if crop := g.content.Crop(plot.AutoSowCrop); crop != nil {
		name = crop.Name
	}
	return th.Hint.Render(" ↻→" + sanitizeText(name))
}

func (g *Game) plotBadges(plot sim.Plot) string {
	var b strings.Builder
	if plot.AutoHarvest {
		b.WriteString("⚙")
	}
	if plot.AutoSow {
		b.WriteString("♻")
	}
	if plot.Critter != "" && plot.Crop == "" {
		b.WriteString("🐾")
	}
	return b.String()
}

func (g *Game) plotCard(i int) string {
	th := g.theme()
	st := g.snap.State
	plot := st.Plots[i]
	title := "Plot " + itoa(i+1)
	if badges := g.plotBadges(plot); badges != "" {
		title += " " + badges
	}
	var line1, line2 string
	switch {
	case plot.Crop == "":
		if plot.Critter != "" {
			line1 = th.Empty.Render("· " + sanitizeText(g.critterName(plot.Critter)) + " ·")
			line2 = th.Hint.Render("x to shoo")
		} else {
			line1 = th.Empty.Render("· empty ·")
			line2 = th.Empty.Render("enter to plant")
		}
	case st.PlotReady(g.content, i, g.now):
		line1 = g.cropName(plot.Crop)
		line2 = th.Ready.Render("✓ ready!")
	default:
		crop := g.content.Crop(plot.Crop)
		line1 = g.cropName(plot.Crop)
		if crop != nil {
			pct := st.PlotProgressPct(g.content, i, g.now)
			left := plot.PlantedAt + st.GrowSeconds(g.content, crop) - g.now
			line2 = th.Growing.Render(progressBar(pct, 8) + " " + duration(left))
		} else {
			line2 = th.Locked.Render("(unknown crop)")
		}
	}
	content := th.Selected.Render(title) + "\n" + line1 + "\n" + line2
	if i == g.cursor {
		return th.PlotSel.Render(content)
	}
	return th.PlotCard.Render(content)
}

func (g *Game) viewFarmCompact() string {
	th := g.theme()
	st := g.snap.State
	maxRows := g.contentHeight() - 9
	if maxRows < 3 {
		maxRows = 3
	}
	start := 0
	if g.cursor >= maxRows {
		start = g.cursor - maxRows + 1
	}
	var b strings.Builder
	for i := start; i < len(st.Plots) && i < start+maxRows; i++ {
		plot := st.Plots[i]
		marker := "  "
		if i == g.cursor {
			marker = th.Selected.Render("▸ ")
		}
		badges := g.plotBadges(plot)
		var status string
		switch {
		case plot.Crop == "":
			if plot.Critter != "" {
				status = th.Empty.Render(sanitizeText(g.critterName(plot.Critter)))
			} else {
				status = th.Empty.Render("empty")
			}
		case st.PlotReady(g.content, i, g.now):
			status = g.cropName(plot.Crop) + " " + th.Ready.Render("✓ ready!")
		default:
			crop := g.content.Crop(plot.Crop)
			if crop != nil {
				left := plot.PlantedAt + st.GrowSeconds(g.content, crop) - g.now
				pct := st.PlotProgressPct(g.content, i, g.now)
				status = g.cropName(plot.Crop) + " " + th.Growing.Render(progressBar(pct, 6)+" "+duration(left))
			} else {
				status = th.Locked.Render("(unknown crop)")
			}
		}
		if plot.Crop != "" {
			status += g.autoSowQueueLabel(plot)
		}
		b.WriteString(marker + itoa(i+1) + badges + ". " + status + "\n")
	}
	if start+maxRows < len(st.Plots) {
		b.WriteString(th.Hint.Render("  …" + itoa(len(st.Plots)-start-maxRows) + " more below"))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (g *Game) cropName(id string) string {
	th := g.theme()
	if crop := g.content.Crop(id); crop != nil {
		return th.Value.Render(sanitizeText(crop.Name))
	}
	return th.Locked.Render(sanitizeText(id))
}

func (g *Game) viewPicker() string {
	th := g.theme()
	st := g.snap.State
	crops := g.visibleCrops()
	var queued string
	if g.pickerAutoSow && g.cursor < len(st.Plots) {
		queued = st.Plots[g.cursor].SowCrop()
	}
	var b strings.Builder
	if g.pickerAutoSow {
		b.WriteString(th.Section.Render("Auto-sow crop · plot "+itoa(g.cursor+1)) + "\n")
		b.WriteString(th.Hint.Render("Pick what this plot replants. Seed is paid when it sows.") + "\n\n")
	} else {
		b.WriteString(th.Section.Render("Plant on plot "+itoa(g.cursor+1)) + "\n\n")
	}
	for i, crop := range crops {
		marker := "  "
		if i == g.pickerIdx {
			marker = th.Selected.Render("▸ ")
		}
		name := sanitizeText(crop.Name)
		grow := duration(st.GrowSeconds(g.content, &crop))
		cost := st.SeedCost(g.content, &crop)
		info := name + "  " + money(cost) + "c · " + grow + " · sells " + money(crop.SellValue) + "c"
		if marker := seasonMarker(crop); marker != "" {
			info += "  " + marker
		}
		if crop.Archetype == "risky" {
			salvage := st.SalvageValue(g.content, &crop, g.now)
			info += " · fails to " + money(salvage) + "c (" + itoa(int(crop.FailChancePct)) + "%)"
		}
		switch {
		case !st.Unlocked(crop.Unlock):
			b.WriteString(marker + th.Locked.Render(info+"  🔒") + "\n")
		case g.pickerAutoSow:
			// Queueing doesn't spend coins now, so affordability is irrelevant
			// here — only flag the crop currently in the rotation.
			if crop.ID == queued {
				b.WriteString(marker + th.Ready.Render(info+"  ◀ current") + "\n")
			} else {
				b.WriteString(marker + th.Value.Render(info) + "\n")
			}
		case st.MercyPlantEligible(g.content, crop.ID) && sim.SeasonalPlantable(crop.Unlock, g.now):
			b.WriteString(marker + th.Ready.Render(info+"  FREE — the land provides") + "\n")
		case st.Coins < cost:
			b.WriteString(marker + th.Locked.Render(info+"  (can't afford)") + "\n")
		default:
			b.WriteString(marker + th.Value.Render(info) + "\n")
		}
	}
	return th.Box.Render(strings.TrimRight(b.String(), "\n"))
}

func (g *Game) viewUpgrade() string {
	th := g.theme()
	st := g.snap.State
	var b strings.Builder
	b.WriteString(th.Section.Render("Plot automation") + "\n\n")
	if st.ActiveContract == sim.ContractBareHands || st.ActiveContract == sim.ContractClockworkDenied {
		b.WriteString(th.Locked.Render("🔒 Disabled by "+contractName(st.ActiveContract)+".") + "\n")
		b.WriteString(th.Hint.Render("Manual planting and harvesting are part of this contract."))
		return th.Box.Render(b.String())
	}
	hCost := st.PlotAutoHarvestCost(g.content)
	sCost := g.content.PlotAutomation.AutoSowCost
	for i, plot := range st.Plots {
		marker := "  "
		if i == g.upgradeIdx {
			marker = th.Selected.Render("▸ ")
		}
		line := "Plot " + itoa(i+1)
		if plot.AutoHarvest {
			line += " ⚙"
		} else {
			line += " — harvest " + money(hCost) + "c (1)"
		}
		if plot.AutoSow {
			line += " ♻"
		} else if plot.AutoHarvest {
			line += " — sow " + money(sCost) + "c (2)"
		}
		b.WriteString(marker + th.Value.Render(line) + "\n")
	}
	b.WriteString("\n" + th.Hint.Render("Auto-sow needs harvester + "+money(g.content.PlotAutomation.AutoSowMinEarnings)+" lifetime coins."))
	return th.Box.Render(strings.TrimRight(b.String(), "\n"))
}

func (g *Game) viewName() string {
	th := g.theme()
	text := th.Section.Render("Name your farm") + "\n\n" +
		th.Value.Render("> "+sanitizeText(g.nameInput)+"_") + "\n\n" +
		th.Hint.Render("Enter to save · esc/q cancel")
	return th.Box.Render(text)
}

func (g *Game) gateText(u content.Unlock) string {
	switch u.Kind {
	case "earnings":
		return "earn " + money(u.Value) + " lifetime coins"
	case "prestige":
		return "rebirth " + money(u.Value) + "×"
	case "zone":
		if z := g.content.ZoneByID(u.Zone); z != nil {
			return "own the " + sanitizeText(z.Name)
		}
		return "own a zone"
	}
	return ""
}

func (g *Game) viewMarket() string {
	items := g.marketItems()
	if g.marketIdx >= len(items) && len(items) > 0 {
		g.marketIdx = len(items) - 1
	}
	parts := make([]string, len(g.marketLines()))
	for i, l := range g.marketLines() {
		parts[i] = l.text
	}
	return strings.TrimRight(strings.Join(parts, "\n"), "\n")
}

func (g *Game) viewLand() string {
	th := g.theme()
	st := g.snap.State
	var b strings.Builder
	b.WriteString(th.Section.Render("Your land") + "\n\n")
	b.WriteString("  Plots owned: " + th.Value.Render(itoa(len(st.Plots))) + "\n")
	cost := st.NextPlotCost(g.content)
	if cost < 0 {
		if st.ActiveContract == sim.ContractLeanSeason && len(st.Plots) >= 6 {
			b.WriteString("  " + th.Locked.Render("🔒 Lean Season caps this farm at six plots.") + "\n")
		} else {
			b.WriteString("  " + th.Hint.Render("The farm is as big as it can get — zones add more room.") + "\n")
		}
	} else {
		b.WriteString("  Next plot: " + th.Value.Render(money(cost)+" coins") + "\n")
		if st.Coins >= cost {
			b.WriteString("\n  " + th.Ready.Render("Press enter to till new ground.") + "\n")
		}
	}

	b.WriteString("\n" + th.Section.Render("Seed catalog (plant from Farm)") + "\n")
	for _, crop := range g.visibleCrops() {
		grow := duration(st.GrowSeconds(g.content, &crop))
		line := "  " + sanitizeText(crop.Name) + " — " + crop.Archetype + " · " +
			money(st.SeedCost(g.content, &crop)) + "c · " + grow +
			" · sells " + money(crop.SellValue) + "c"
		if marker := seasonMarker(crop); marker != "" {
			line += " · " + marker
		}
		if crop.Archetype == "risky" {
			line += " · fails to " + money(st.SalvageValue(g.content, &crop, g.now)) + "c"
		}
		if !st.Unlocked(crop.Unlock) {
			b.WriteString(th.Locked.Render(g.fitRow(line+"  🔒 "+g.gateText(crop.Unlock))) + "\n")
		} else {
			b.WriteString(th.Value.Render(line) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func (g *Game) viewRebirth() string {
	th := g.theme()
	st := g.snap.State
	gain := st.PrestigeGain(g.content)
	var b strings.Builder
	b.WriteString(th.Section.Render("Rebirth") + "\n\n")
	b.WriteString("  This run has earned " + th.Value.Render(money(st.RunEarnings)+" coins") + ".\n")
	if st.CanRebirth(g.content) {
		b.WriteString("  Rebirthing now grants " + th.Ready.Render("✦ "+money(gain)+" "+g.starseedLabel()) + ".\n")
	} else {
		b.WriteString("  " + th.Hint.Render("Earn "+money(g.content.Prestige.MinEarnings)+" coins in one run to unlock rebirth.") + "\n")
	}
	b.WriteString("\n" + th.Section.Render("  Kept: ") + th.Value.Render(g.starseedLabel()+", upgrades, achievements, crop unlocks") + "\n")
	b.WriteString(th.Section.Render("  Lost: ") + th.Value.Render("coins, plots, crops, multipliers, zones, plot automation") + "\n")

	b.WriteString("\n" + th.Section.Render("Next rebirth unlocks") + "\n")
	nextTier := st.ProgressionRebirths() + 1
	found := false
	for _, crop := range g.content.Crops {
		if crop.Unlock.Kind == "prestige" && crop.Unlock.Value == nextTier {
			b.WriteString("  " + th.Value.Render("🌱 "+sanitizeText(crop.Name)+" ("+crop.Archetype+")") + "\n")
			found = true
		}
	}
	if !found {
		b.WriteString("  " + th.Hint.Render("More secrets await deeper cycles…") + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (g *Game) viewStarShop() string {
	parts := make([]string, len(g.starShopLines()))
	for i, l := range g.starShopLines() {
		parts[i] = l.text
	}
	return strings.TrimRight(strings.Join(parts, "\n"), "\n")
}

// contractsIntro is everything above the Contracts screen's three rows.
// registerContractHits offsets the rows by its height, so the hint is
// wrapped here rather than reflowed later, where a narrow terminal would
// push the rows down without moving their hitboxes.
func (g *Game) contractsIntro() string {
	th := g.theme()
	hint := wrapIndent(g.contentWidth(), "", "Three one-time challenges. Each attempt resets the farm, not its legacy.")
	return th.Section.Render("CONTRACTS") + "\n" + th.Hint.Render(hint) + "\n\n"
}

func (g *Game) viewContracts() string {
	th := g.theme()
	st := g.snap.State
	contracts := sim.Contracts()
	var b strings.Builder
	b.WriteString(g.contractsIntro())
	for i, contract := range contracts {
		marker := "  "
		if i == g.contractIdx {
			marker = "▸ "
		}
		status := "LOCKED"
		switch {
		case i < st.ContractsCompleted:
			status = "✓ COMPLETE"
		case st.ActiveContract == contract.ID:
			status = "ACTIVE — " + g.contractProgress(contract.ID)
		case i == st.ContractsCompleted && st.ActiveContract == "":
			status = "READY"
		}
		line := fitWidth(marker+itoa(i+1)+". "+sanitizeText(contract.Name)+"  ["+status+"]", g.contentWidth())
		if i == g.contractIdx {
			b.WriteString(th.Selected.Render(line))
		} else if i < st.ContractsCompleted {
			b.WriteString(th.Ready.Render(line))
		} else {
			b.WriteString(th.Value.Render(line))
		}
		b.WriteString("\n")
	}
	selected := contracts[clamp(g.contractIdx, 0, len(contracts)-1)]
	b.WriteString("\n" + th.Value.Render(sanitizeText(selected.Premise)) + "\n")
	b.WriteString("  Goal: " + th.Ready.Render(sanitizeText(selected.Goal)) + "\n")
	b.WriteString("  Reward: " + th.Value.Render(sanitizeText(selected.Reward)) + "\n")
	if st.ActiveContract != "" {
		b.WriteString("\n" + th.Warn.Render("Press a to abandon the active attempt. Progress will be lost."))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (g *Game) contractProgress(id sim.ContractID) string {
	st := g.snap.State
	switch id {
	case sim.ContractBareHands:
		return money(st.ContractEarnings) + "/100,000 coins"
	case sim.ContractLeanSeason:
		return money(st.ContractStarseeds) + "/100 Starseeds"
	case sim.ContractClockworkDenied:
		return money(st.ContractRebirths) + "/5 rebirths"
	}
	return ""
}

// Contract modal button labels. registerContractConfirmHits measures these
// same strings, so a click lands exactly on what is drawn.
const (
	contractPromptGap = "    "
	contractCancel    = "n cancel"
)

// What a contract start or abandon resets and keeps (gameplay/04 "The hard
// reset"), compressed to fit one modal line each at 80 columns.
const (
	contractResetList = "coins, crops, land, Starseeds, upgrades, automation"
	contractKeptList  = "name, settings, achievements, seals, lifetime stats"
)

func contractAcceptLabel(abandon bool) string {
	if abandon {
		return "y abandon and reset"
	}
	return "y accept and reset"
}

// viewContractConfirm renders the accept/abandon modal. It has to fit a stock
// 80×24 terminal, whose body leaves about fourteen rows once a notice or an
// event bar takes its line, so the box drops the usual vertical padding and
// every term takes one labelled line, in gameplay/04's order: name and
// premise, goal, disabled systems, reward, what resets, what is kept, and
// the warning that the farm cannot be restored.
func (g *Game) viewContractConfirm() string {
	th := g.theme()
	contract, _ := sim.ContractByID(g.contractID)
	// Overlays are clipped to contentWidth-4; the box border and its
	// one-cell padding take four more.
	width := max(g.contentWidth()-8, 20)
	// Two label columns: the contract's own terms are short-labelled so a
	// long reward still fits on one line, and the reset summary aligns under
	// its widest label, KEPT FOREVER.
	const termW, summaryW = len("Reward  "), len("KEPT FOREVER  ")
	field := func(labelW int, label, value string) string {
		lines := strings.Split(wrapIndent(width-labelW, "", value), "\n")
		for i := range lines {
			pad := strings.Repeat(" ", labelW)
			if i == 0 {
				pad = th.Section.Render(label) + strings.Repeat(" ", labelW-len(label))
			}
			lines[i] = pad + th.Value.Render(lines[i])
		}
		return strings.Join(lines, "\n")
	}
	var b strings.Builder
	if g.contractAbandon {
		b.WriteString(th.Section.Render("Abandon "+sanitizeText(contract.Name)+"?") + "\n")
		b.WriteString(th.Value.Render(wrapIndent(width, "", "This attempt ends and the farm starts over as an ordinary one.")) + "\n\n")
		b.WriteString(field(summaryW, "RESET", contractResetList) + "\n")
		b.WriteString(field(summaryW, "KEPT FOREVER", contractKeptList) + "\n")
		b.WriteString(field(summaryW, "NEXT", sanitizeText(contract.Name)+" stays the next contract.") + "\n\n")
	} else {
		b.WriteString(th.Section.Render("Accept "+sanitizeText(contract.Name)+"?") + "\n")
		b.WriteString(th.Hint.Render(wrapIndent(width, "", sanitizeText(contract.Premise))) + "\n")
		b.WriteString(field(termW, "Goal", sanitizeText(contract.Goal)) + "\n")
		for i, rule := range contract.Restrictions {
			label := ""
			if i == 0 {
				label = "Rules"
			}
			b.WriteString(field(termW, label, sanitizeText(rule)) + "\n")
		}
		b.WriteString(field(termW, "Reward", sanitizeText(contract.Reward)) + "\n\n")
		b.WriteString(field(summaryW, "RESET", contractResetList) + "\n")
		b.WriteString(field(summaryW, "KEPT FOREVER", contractKeptList) + "\n")
	}
	b.WriteString(th.Warn.Render("⚠ Your current farm cannot be restored.") + "\n")
	b.WriteString(th.Ready.Render(contractAcceptLabel(g.contractAbandon)) + contractPromptGap + th.Hint.Render(contractCancel))
	return th.Box.Padding(0, 1).Render(b.String())
}

func (g *Game) viewContractReward() string {
	th := g.theme()
	contract, _ := sim.ContractByID(g.completedContract)
	return th.Box.Render(th.Section.Render("CONTRACT COMPLETE ◆") + "\n\n" +
		th.Ready.Render(sanitizeText(contract.Name)) + "\n" +
		th.Value.Render(sanitizeText(contract.Reward)) + "\n\n" +
		th.Hint.Render("Your new flair is ready on the leaderboard."))
}

func (g *Game) viewRebirthConfirm() string {
	th := g.theme()
	st := g.snap.State
	gain := st.PrestigeGain(g.content)
	text := th.Section.Render("Rebirth?") + "\n\n" +
		"You will gain  " + th.Ready.Render("✦ "+money(gain)+" "+g.starseedLabel()) + "\n" +
		"You will lose  " + th.Value.Render(money(st.Coins)+" coins, "+itoa(len(st.Plots))+" plots, and this run's progress") + "\n\n" +
		"Your " + g.starseedLabel() + ", upgrades, and achievements stay forever.\n\n" +
		th.Ready.Render("y") + " — yes, begin anew    " + th.Hint.Render("n — keep farming")
	return th.Box.Render(text)
}

func (g *Game) viewStats() string {
	th := g.theme()
	st := g.snap.State
	var b strings.Builder
	b.WriteString(th.Section.Render("This farm") + "\n")
	if st.FarmName != "" {
		b.WriteString("  Farm name: " + th.Value.Render(sanitizeText(st.FarmName)) + th.Hint.Render("  (n to rename)") + "\n")
	} else {
		b.WriteString("  Farm name: " + th.Hint.Render("(unnamed — press n)") + "\n")
	}
	b.WriteString("  Save slot: " + th.Value.Render(sanitizeText(g.id.Slot)) + "\n")
	b.WriteString("  Key: " + th.Hint.Render(shortFingerprint(g.id.Fingerprint)) + "\n")
	settings := make([]string, 0, 4)
	for _, r := range g.configRows() {
		settings = append(settings, strings.ToLower(r.label)+" "+onOff(r.on))
	}
	// Wrapped: four settings do not fit on one line at 80 columns.
	b.WriteString("  Settings:\n")
	b.WriteString(th.Value.Render(wrapIndent(g.contentWidth(), "    ", strings.Join(settings, " · "))) + "\n")
	b.WriteString("    " + th.Hint.Render("(c to configure)") + "\n")
	b.WriteString("\n" + th.Section.Render("Lifetime") + "\n")
	b.WriteString("  Earnings: " + th.Value.Render(money(st.LifetimeEarnings)+" coins") + "\n")
	b.WriteString("  Harvests: " + th.Value.Render(money(st.LifetimeHarvests)) + "\n")
	b.WriteString("  Rebirths: " + th.Value.Render(money(st.Rebirths)) + "  ·  " + g.starseedLabel() + ": " + th.Value.Render("✦ "+money(st.PrestigeCurrency)) + "\n")
	// The campaign stays out of sight until it can be entered, and "l" is
	// only offered once there is a second style to switch to.
	if st.ContractsAvailable(g.content) {
		line := "  Contracts: " + th.Value.Render(itoa(st.ContractsCompleted)+"/"+itoa(len(sim.Contracts()))) +
			"  ·  Board style: " + th.Value.Render(nameStyleLabel(st.LeaderboardNameStyle))
		if len(st.AvailableLeaderboardNameStyles()) > 1 {
			line += th.Hint.Render("  (l to change)")
		}
		b.WriteString(line + "\n")
	}

	b.WriteString("\n" + th.Section.Render("Achievements") + "\n")
	for _, a := range g.content.Achievements {
		if _, ok := st.Achievements[a.ID]; ok {
			b.WriteString("  " + th.Ready.Render("✓ "+sanitizeText(a.Name)) + th.Hint.Render(" — "+sanitizeText(a.Description)) + "\n")
		} else {
			b.WriteString("  " + th.Locked.Render("· "+sanitizeText(a.Name)+" — "+sanitizeText(a.Description)) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// viewBoard renders gameplay/02's Board: it computes nothing (rank, ties,
// window are all pre-computed on g.lbBoard by refreshBoard/Engine.Get) and
// masks nothing (gameplay/03 already re-checked every name). The header
// line is written before the scrolled region so it never scrolls away.
func (g *Game) viewBoard() string {
	th := g.theme()
	cw := g.contentWidth()
	header := g.boardHeaderLine(cw)
	if g.lbErr != nil {
		return header + "\n\n" + th.Locked.Render(truncate("LEADERBOARD UNAVAILABLE — TRY AGAIN SOON", cw))
	}

	lines := g.boardLines(cw)
	start, end := g.boardVisibleRange(len(lines))

	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n\n")
	for i := start; i < end; i++ {
		b.WriteString(lines[i].text)
		b.WriteString("\n")
	}
	if start > 0 || end < len(lines) {
		b.WriteString(th.Hint.Render("  ↑↓ scroll for more"))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(th.Hint.Render("updated " + duration(max(g.now-g.lbBoard.AsOf, 0)) + " ago"))
	return b.String()
}

func (g *Game) viewHelp() string {
	th := g.theme()
	tabs := g.helpTabs()
	body := g.viewHelpControls()
	if g.helpPage == 1 {
		body = g.viewHelpGameplay()
	}
	full := tabs + "\n\n" + body
	lines := strings.Split(full, "\n")
	visible := g.helpVisibleLines()
	if len(lines) <= visible {
		return full
	}
	start := g.helpScroll
	maxStart := len(lines) - visible
	if start > maxStart {
		start = maxStart
	}
	end := start + visible
	if end > len(lines) {
		end = len(lines)
	}
	out := strings.Join(lines[start:end], "\n")
	if start > 0 || end < len(lines) {
		out += "\n" + th.Hint.Render("  ↑↓ scroll · ← → pages")
	}
	return out
}

func (g *Game) helpVisibleLines() int {
	ch := g.contentHeight()
	// Reserve space for header, nav, tabs, footer, and frame padding.
	return max(ch-14, 6)
}

func (g *Game) clampHelpScroll() {
	body := g.viewHelpControls()
	if g.helpPage == 1 {
		body = g.viewHelpGameplay()
	}
	lines := strings.Split(g.helpTabs()+"\n\n"+body, "\n")
	maxStart := max(len(lines)-g.helpVisibleLines(), 0)
	if g.helpScroll > maxStart {
		g.helpScroll = maxStart
	}
}

func (g *Game) helpTabs() string {
	th := g.theme()
	controls, gameplay := "Controls", "Gameplay"
	if g.helpPage == 0 {
		controls = th.NavOn.Render(controls)
		gameplay = th.NavOff.Render(gameplay)
	} else {
		controls = th.NavOff.Render(controls)
		gameplay = th.NavOn.Render(gameplay)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, controls, " ", gameplay) +
		"\n" + th.Hint.Render("  ← → switch pages · esc/q back to farm · Idle Farmer v"+
		version.Version+" ("+version.Channel+")")
}

func (g *Game) viewHelpControls() string {
	th := g.theme()
	ss := g.starseedLabel()
	// Wrapped rather than hard-broken: the label is variable length and the
	// content width follows the terminal, so fixed breaks overflowed on any
	// window narrower than the design size.
	// The size notice sits first: the ⚠ on the nav tab is what sent the
	// player here, so the answer must be the first thing on the page.
	notice := ""
	if n := g.viewHelpSizeNotice(); n != "" {
		notice = n + "\n"
	}
	return notice +
		th.Section.Render("How it works") + "\n\n" +
		g.helpBody("Plant crops, go live your life, come back and harvest. Crops keep "+
			"growing while you're away. Earn coins, buy plots and upgrades, and "+
			"rebirth for "+ss+" — permanent bonuses that make every later run faster.") + "\n\n" +
		th.Section.Render("Keys") + "\n" +
		th.Value.Render("  1-6               switch screens\n"+
			"  ?                 help\n"+
			"  tab / shift+tab   cycle screens forward / back\n"+
			"  ←↑↓→              move around the farm\n"+
			"  enter / space     plant, harvest, or pick an auto-sow crop\n"+
			"  a                 harvest everything that's ready\n"+
			"  r                 replant every empty plot (last crop)\n"+
			"  u                 plot automation upgrades\n"+
			"  g                 redeem a gift parcel\n"+
			"  x                 shoo a critter off a plot\n"+
			"  n                 name your farm (stats screen)\n"+
			"  c                 settings / config (stats screen)\n"+
			"  R                 rebirth (rebirth screen, with confirmation)\n"+
			"  esc / q           close menus and overlays\n"+
			"  q / ctrl+c        leave the game (progress is saved automatically)") + "\n\n" +
		th.Hint.Render("  Your SSH key is your identity; the username picks the save slot.")
}

func (g *Game) helpTextWidth() int {
	return max(g.contentWidth()-2, 40)
}

func (g *Game) helpBody(text string) string {
	th := g.theme()
	return th.Value.Render(wrapIndent(g.helpTextWidth(), "  ", text))
}

func (g *Game) helpHint(text string) string {
	th := g.theme()
	return th.Hint.Render(wrapIndent(g.helpTextWidth(), "  ", text))
}

func (g *Game) helpSubHint(text string) string {
	th := g.theme()
	return th.Hint.Render(wrapIndent(g.helpTextWidth(), "    ", text))
}

func (g *Game) viewHelpGameplay() string {
	th := g.theme()
	c := g.content
	ss := g.starseedLabel()
	ec := c.EventsConfig
	gh := c.GoldenHarvest
	goldenMult := gh.Multiplier / 10
	if goldenMult < 1 {
		goldenMult = 1
	}

	var b strings.Builder
	b.WriteString(th.Section.Render("Random events") + "\n")
	b.WriteString(g.helpBody("While online, a banner announces a random event for "+
		duration(ec.MinDurationSec)+"–"+duration(ec.MaxDurationSec)+
		". Act before it expires!") + "\n")
	for _, ev := range c.Events {
		b.WriteString(th.Ready.Render(wrapIndent(g.helpTextWidth(), "  ",
			sanitizeText(ev.Name)+" — "+eventHelpEffect(ev))) + "\n")
		if action := eventHelpAction(ev); action != "" {
			b.WriteString(g.helpSubHint(action) + "\n")
		}
	}
	b.WriteString(g.helpHint("Stacks with Market upgrades (Merchant's Scale, Fertilizer).") + "\n")

	b.WriteString("\n" + th.Section.Render("Risky crops") + "\n")
	b.WriteString(g.helpBody("Glimmercorn, Moonberry, and Thunderpod can fail at harvest. "+
		"A failed harvest pays salvage instead of full sell value. "+
		"Hardier Strain upgrades (Market tab) raise the salvage floor: "+
		"Lv 0: 1/8 · Lv 1: 1/4 · Lv 2: 1/2 · Lv 3+: 3/4 of sell value.") + "\n")

	b.WriteString("\n" + th.Section.Render("Rebirth & "+ss) + "\n")
	b.WriteString(g.helpBody("Earn "+money(c.Prestige.MinEarnings)+
		"+ coins in one run, then rebirth for "+ss+". "+
		"Gain: isqrt(run earnings ÷ "+money(c.Prestige.Divisor)+
		") — e.g. "+money(c.Prestige.MinEarnings)+" run earnings → 10 "+ss+".") + "\n")
	b.WriteString(g.helpBody("Kept: "+ss+", permanent upgrades, achievements, crop unlocks. "+
		"Lost: coins, plots, crops, multipliers, zones, plot automation.") + "\n")

	b.WriteString("\n" + th.Section.Render("Automation") + "\n")
	b.WriteString(g.helpBody("Auto-harvest gathers ready crops on that plot each tick. "+
		"Auto-sow replants a crop after each harvest (per tick) if you can "+
		"afford the seed. Press enter on a growing auto-sow plot to change which "+
		"crop it replants (handy after unlocking a better crop). Needs auto-harvest "+
		"on that plot and "+money(c.PlotAutomation.AutoSowMinEarnings)+
		" lifetime earnings. Resets on rebirth. "+
		"Crops keep growing offline; auto plots simulate harvest cycles when you reconnect.") + "\n")

	b.WriteString("\n" + th.Section.Render("Critters & the Scarecrow") + "\n")
	b.WriteString(g.helpBody("Critters wander onto empty plots now and then (press x to shoo "+
		"one for a few coins). Buy the Scarecrow in the Market to shoo them "+
		"automatically and bank the coins. While you're offline it can't chase "+
		"critters, so it only trickles a small passive income — far less than "+
		"it earns at your side.") + "\n")

	b.WriteString("\n" + th.Section.Render("Replant all (r)") + "\n")
	b.WriteString(g.helpBody("On the Farm screen, r refills every empty plot with the crop it "+
		"last grew, spending coins most-expensive-first. If you can't afford "+
		"them all it plants what it can and tells you the rest fell short.") + "\n")

	b.WriteString("\n" + th.Section.Render("Gift parcels") + "\n")
	b.WriteString(g.helpBody("One parcel at a time; arrives about every "+
		duration(c.Gifts.OnlineIntervalSec)+" online or "+
		duration(c.Gifts.OfflineIntervalSec)+" away. Press g to redeem. "+
		"Usually coins ("+money(c.Gifts.CoinRewardFloor)+"–"+money(c.Gifts.CoinRewardCeiling)+
		", scaled by run earnings). After your first rebirth, "+
		money(c.Gifts.StarseedChancePct)+"% chance of "+ss+" instead.") + "\n")

	b.WriteString("\n" + th.Section.Render("Mercy plant") + "\n")
	b.WriteString(g.helpBody("Broke with empty plots? The cheapest unlocked seed plants "+
		"for FREE — \"the land provides\".") + "\n")

	b.WriteString("\n" + th.Section.Render("Golden harvest & night") + "\n")
	b.WriteString(g.helpBody("Any harvest has a "+money(gh.ChancePct)+
		"% chance to pay "+money(goldenMult)+"×. "+
		"Watch the clock beside your slot: any time from 20:00 to 03:59 gives "+
		"Moonberry +"+money(c.Moon.FullMoonSellBonusPct)+
		"% sell bonus. A full in-game day passes every 24 minutes, so the "+
		"night window comes round often.") + "\n")

	b.WriteString("\n" + th.Section.Render("Seasonal festivals") + "\n")
	b.WriteString(g.helpBody("Halloween runs Oct 1 through Oct 31 at 23:59 UTC "+
		"(bats by night, pumpkins by day — and a great moon all of the 31st). "+
		"Christmas runs Nov 25 through Dec 25 at 23:59 UTC "+
		"(stars by night only — and one great star all of Dec 25th, day and "+
		"night). Each festival brings limited seeds that out-earn normal crops: "+
		"plant them while the window lasts; anything already growing keeps its "+
		"full payout afterwards.") + "\n")
	b.WriteString(g.helpBody("Stats → c → Seasonal themes opts out of the look. "+
		"The seeds stay buyable in-season either way.") + "\n")

	return strings.TrimRight(b.String(), "\n")
}

func eventHelpEffect(ev content.Event) string {
	switch ev.Effect {
	case "seed_discount_pct":
		return "seeds −" + money(ev.EffectValue) + "%"
	case "sell_bonus_pct":
		return "sell value +" + money(ev.EffectValue) + "%"
	case "grow_speed_pct":
		return "grow time −" + money(ev.EffectValue) + "%"
	default:
		return sanitizeText(ev.Description)
	}
}

func eventHelpAction(ev content.Event) string {
	switch ev.Effect {
	case "seed_discount_pct":
		return "Good time to plant expensive slow crops."
	case "sell_bonus_pct":
		return "Harvest anything ready immediately."
	case "grow_speed_pct":
		return "Crops mature in half the time this cycle."
	default:
		return ""
	}
}

// tutorialContent returns the title and body for one tutorial step.
func (g *Game) tutorialContent(page int) (string, string) {
	ss := g.starseedLabel()
	switch page {
	case 0:
		return "Welcome to your farm! 🌱",
			"This is a cozy idle farm you tend over SSH. Plant a crop, then go\n" +
				"live your life — it keeps growing while you're disconnected.\n\n" +
				"Your SSH key is your identity, and the username you connect with\n" +
				"picks which farm (save slot) you open. No password, no account."
	case 1:
		return "Tending the farm 🌾",
			"• Move with the arrow keys; press enter on a plot to plant or harvest.\n" +
				"• a  harvests every ready plot at once.\n" +
				"• r  replants every empty plot with what it last grew.\n" +
				"• u  adds per-plot automation once you can afford it.\n" +
				"• x  shoos a critter off an empty plot for a few coins."
	case 2:
		return "Growing richer 💰",
			"• Sell crops for coins. Buy plots on the Land screen and run-scoped\n" +
				"  boosts — plus a critter-chasing Scarecrow — in the Market.\n" +
				"• Random events and gift parcels (press g) sweeten the day.\n" +
				"• Rebirth trades your run for " + ss + ": permanent bonuses that make\n" +
				"  every later run faster."
	default:
		return "Make it your own ⚙",
			"• Press ? any time for the full help screens.\n" +
				"• On the Stats screen, press c to open Settings — toggle lucky\n" +
				"  finds, the news ticker, critter visits, and seasonal themes\n" +
				"  whenever you like.\n\n" +
				"That's everything. Have fun, and check back whenever you like!"
	}
}

func (g *Game) viewTutorial() string {
	th := g.theme()
	if g.tutorialPage < 0 {
		g.tutorialPage = 0
	}
	if g.tutorialPage > tutorialPages-1 {
		g.tutorialPage = tutorialPages - 1
	}
	title, body := g.tutorialContent(g.tutorialPage)

	box := th.Section.Render("Tutorial · " + itoa(g.tutorialPage+1) + "/" + itoa(tutorialPages))
	check := "[ ]"
	if g.tutorialSkip {
		check = th.Ready.Render("[x]")
	}
	cont := "Continue ▸"
	if g.tutorialSkip || g.tutorialPage == tutorialPages-1 {
		cont = "Continue to farm ▸"
	}
	controls := check + " Skip" + "      " + th.Ready.Render(cont)

	text := th.Title.Render(title) + "\n\n" +
		th.Value.Render(rewrap(body, g.overlayTextWidth())) + "\n\n" +
		box + "  " + th.Hint.Render("s to skip · enter to continue") + "\n" +
		controls
	return th.Box.Render(text)
}

func (g *Game) viewConfig() string {
	th := g.theme()
	rows := g.configRows()
	// Overlays are clipped to contentWidth-4 before centring, and the box
	// itself costs 2 border + 4 padding cells.
	limit := g.contentWidth() - 10
	var b strings.Builder
	b.WriteString(th.Section.Render("Settings") + "\n\n")
	for i, r := range rows {
		marker := "  "
		if i == g.configIdx {
			marker = th.Selected.Render("▸ ")
		}
		box := "[ ]"
		if r.on {
			box = th.Ready.Render("[x]")
		}
		line := marker + box + " " + th.Value.Render(r.label) +
			th.Hint.Render("  — "+r.hint)
		if w := lipgloss.Width(line); w > limit {
			// Drop the hint rather than let the frame clip the label.
			line = marker + box + " " + th.Value.Render(truncate(r.label, limit-6))
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n" + th.Hint.Render("Settings are saved per farm. Lucky finds also live here."))
	return th.Box.Render(strings.TrimRight(b.String(), "\n"))
}

func (g *Game) viewReplantWarn() string {
	th := g.theme()
	text := th.Section.Render("About “r — replant all” 🌱") + "\n\n" +
		th.Value.Render(rewrap("Replant fills every empty plot with the crop it last grew (or your "+
			"last-planted crop for fresh plots). It spends coins automatically, "+
			"buying the most expensive seeds first.\n\n"+
			"If you can't afford every plot, it plants as many as it can and "+
			"tells you how many it skipped — just like a normal short purchase.\n\n"+
			"Plots that are still growing are left untouched.", g.overlayTextWidth())) + "\n\n" +
		th.Ready.Render("Press any key to close.")
	return th.Box.Render(text)
}

func (g *Game) viewAway() string {
	th := g.theme()
	ev := g.away
	var b strings.Builder
	b.WriteString(th.Title.Render("Welcome back! 🌾") + "\n\n")
	b.WriteString(th.Value.Render("You were away "+duration(ev.Elapsed)+".") + "\n")
	for _, v := range ev.AwayVignettes {
		b.WriteString(th.Hint.Render("  "+sanitizeText(v)) + "\n")
	}
	wrote := len(ev.AwayVignettes) > 0
	for id, n := range ev.Matured {
		name := id
		if crop := g.content.Crop(id); crop != nil {
			name = crop.Name
		}
		b.WriteString(th.Ready.Render("  🌱 "+itoa(n)+"× "+sanitizeText(name)+" matured and await harvest") + "\n")
		wrote = true
	}
	if total := totalCount(ev.AutoHarvested); total > 0 {
		b.WriteString(th.Ready.Render("  ⚙ Auto-plots gathered "+itoa(total)+" crops (+"+money(ev.AutoCoins)+" coins)") + "\n")
		wrote = true
	}
	if ev.GoldenHarvests > 0 {
		b.WriteString(th.Ready.Render("  ✨ "+itoa(ev.GoldenHarvests)+" golden harvest(s)!") + "\n")
		wrote = true
	}
	for id, n := range ev.FailedHarvests {
		name := id
		if crop := g.content.Crop(id); crop != nil {
			name = crop.Name
		}
		b.WriteString(th.Ready.Render("  💥 "+itoa(n)+"× "+sanitizeText(name)+" failed in the field") + "\n")
		wrote = true
	}
	if ev.Discoveries > 0 {
		b.WriteString(th.Ready.Render("  ✨ "+itoa(ev.Discoveries)+" lucky finds (+"+money(ev.DiscoveryCoins)+" coins)") + "\n")
		wrote = true
	}
	if ev.GiftArrived {
		b.WriteString(th.Ready.Render("  📦 A parcel waits at the gate — press g") + "\n")
		wrote = true
	}
	if !wrote {
		b.WriteString(th.Hint.Render("  The fields rested quietly. A fine time to plant something.") + "\n")
	}
	return th.Box.Render(strings.TrimRight(b.String(), "\n"))
}

func (g *Game) viewKicked() string {
	th := g.theme()
	return th.Box.Render(
		th.Section.Render("Until next time 🌙") + "\n\n" +
			th.Value.Render(rewrap(sanitizeText(g.kickReason), g.overlayTextWidth())) + "\n\n" +
			th.Hint.Render("Your progress is saved. Disconnecting…"))
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func totalCount(m map[string]int) int {
	t := 0
	for _, n := range m {
		t += n
	}
	return t
}

func nameStyleLabel(style string) string {
	if style == "" {
		return "traditional"
	}
	return strings.ReplaceAll(style, "_", " ")
}
