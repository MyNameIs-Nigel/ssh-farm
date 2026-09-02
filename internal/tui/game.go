// Package tui is the player-facing terminal interface.
package tui

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-farm/internal/content"
	"github.com/mynameis-nigel/ssh-farm/internal/game"
	"github.com/mynameis-nigel/ssh-farm/internal/identity"
	"github.com/mynameis-nigel/ssh-farm/internal/leaderboard"
	"github.com/mynameis-nigel/ssh-farm/internal/sim"
	"github.com/mynameis-nigel/ssh-farm/internal/tui/hitbox"
	"github.com/mynameis-nigel/ssh-farm/internal/tui/theme"
)

// Model is the Bubble Tea model type used by the Wish middleware.
type Model = tea.Model

// ProgramOption is a Bubble Tea program option.
type ProgramOption = tea.ProgramOption

type errScreen struct {
	width, height int
}

func NewErrScreen() Model { return errScreen{} }

func (errScreen) Init() tea.Cmd { return nil }

func (e errScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		e.width, e.height = msg.Width, msg.Height
		return e, nil
	case tea.KeyPressMsg:
		return e, tea.Quit
	case tea.MouseClickMsg, tea.MouseMotionMsg, tea.MouseWheelMsg:
		return e, nil
	}
	return e, nil
}

func (e errScreen) View() tea.View {
	// The error screen paints itself the same way the game does. It has no
	// save to read a setting from, so it uses the pinned solid background —
	// this is the screen a player on a light terminal is most likely to hit.
	th := theme.New(theme.PhaseNight, true, "")
	msg := th.Value.Render("🌧 The farm could not be opened just now.") + "\n" +
		th.Hint.Render("Press any key to disconnect, then try again.")
	placed := lipgloss.Place(
		max(e.width, 1), max(e.height, 1), lipgloss.Center, lipgloss.Center, msg,
		lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Background(th.Bg)),
	)
	v := tea.NewView(th.Paint(placed))
	v.BackgroundColor = th.Bg
	v.ForegroundColor = th.Fg
	v.AltScreen = true
	v.WindowTitle = windowTitle
	return v
}

type screen int

const (
	scrFarm screen = iota
	scrMarket
	scrLand
	scrRebirth
	scrStarShop
	scrStats
	scrBoard
	scrHelp
)

var screenOrder = []screen{scrFarm, scrMarket, scrLand, scrRebirth, scrStarShop, scrStats, scrBoard, scrHelp}

// boardRefreshSeconds is how often the board screen re-calls Engine.Get
// while it's the active screen (gameplay/02's engine is itself TTL-cached,
// so this is just "don't even bother trying more often than this" — it
// does not bypass or shorten that cache).
const boardRefreshSeconds = 15

type overlay int

const (
	ovNone overlay = iota
	ovTutorial
	ovAway
	ovPicker
	ovUpgrade
	ovRebirthConfirm
	ovName
	ovConfig
	ovReplantWarn
	ovKicked
)

// tutorialPages is the number of pages in the new-player tutorial.
const tutorialPages = 4

type notice struct {
	text    string
	expires int64
}

type (
	tickMsg   time.Time
	kickedMsg string
)

// Game is the root model for one connected session.
type Game struct {
	sess    *game.Session
	content *content.Content
	id      identity.SessionIdentity

	snap   game.Snapshot
	now    int64
	width  int
	height int

	scr           screen
	overlay       overlay
	cursor        int
	pickerIdx     int
	pickerAutoSow bool // picker is choosing an auto-sow queue, not planting now
	marketIdx     int
	upgradeIdx    int
	progressIdx   int
	helpPage      int
	helpScroll    int
	nameInput     string

	board         *leaderboard.Engine
	lbBoard       leaderboard.Board
	lbErr         error
	lbNextRefresh int64
	lbScroll      int

	tutorialPage int
	tutorialSkip bool
	configIdx    int

	notices    []notice
	away       sim.Events
	kickReason string
	quitAt     int64

	idleTimeout int64
	lastInput   int64

	hits        *hitbox.Registry
	layout      frameLayout
	lastClickID string
	lastClickAt int64
}

// theme builds the palette for this frame: the day/night phase from the
// model's clock, the player's solid-background setting, and the active event
// (which recolours the frame and lifts the canvas).
func (g *Game) theme() theme.Theme {
	st := g.snap.State
	eventID := ""
	if st != nil && st.EventActive(g.now) {
		eventID = st.EventID
	}
	solid := st != nil && st.ThemeSolid
	return theme.New(theme.PhaseAt(g.now), solid, eventID)
}

func NewGame(id identity.SessionIdentity, res game.AttachResult, c *content.Content, board *leaderboard.Engine, width, height int, now int64, idleTimeout int64) *Game {
	g := &Game{
		sess:        res.Session,
		content:     c,
		id:          id,
		snap:        game.Snapshot{State: sim.New(c, 0, now), Now: now},
		now:         now,
		width:       max(width, 1),
		height:      max(height, 1),
		idleTimeout: idleTimeout,
		lastInput:   now,
		hits:        &hitbox.Registry{},
		board:       board,
	}
	switch {
	case res.Created:
		g.overlay = ovTutorial
	case !res.Away.Empty() || res.Away.Elapsed > 60:
		g.away = res.Away
		g.overlay = ovAway
	}
	return g
}

// Init starts exactly one tick chain: refresh delivers an immediate tickMsg,
// and every tickMsg handler schedules the next tick. Adding tickCmd here too
// would run a second, parallel 1Hz chain forever.
func (g *Game) Init() tea.Cmd {
	return tea.Batch(g.refresh(), g.waitKick())
}

func (g *Game) refresh() tea.Cmd {
	return func() tea.Msg { return tickMsg(time.Now()) }
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (g *Game) waitKick() tea.Cmd {
	return func() tea.Msg {
		reason, ok := <-g.sess.Kicked()
		if !ok {
			return kickedMsg("")
		}
		return kickedMsg(reason)
	}
}

func (g *Game) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		g.width, g.height = msg.Width, msg.Height
		return g, nil

	case kickedMsg:
		if msg == "" {
			return g, tea.Quit
		}
		g.kickReason = string(msg)
		g.overlay = ovKicked
		g.quitAt = g.now + 3
		return g, nil

	case tickMsg:
		g.now = time.Time(msg).Unix()
		if g.quitAt != 0 && g.now >= g.quitAt {
			return g, tea.Quit
		}
		if g.idleTimeout > 0 && g.overlay != ovKicked && g.now-g.lastInput >= g.idleTimeout {
			g.kickReason = "You drifted off, so the farm tucked itself in. Reconnect whenever you like!"
			g.overlay = ovKicked
			g.quitAt = g.now + 3
			return g, tickCmd()
		}
		snap, ev, err := g.sess.Advance(g.now)
		if err != nil {
			return g, tea.Quit
		}
		g.snap = snap
		g.eventNotices(ev)
		g.pruneNotices()
		if g.scr == scrBoard && g.now >= g.lbNextRefresh {
			g.refreshBoard()
		}
		return g, tickCmd()

	case tea.KeyPressMsg:
		g.lastInput = g.now
		return g.handleKey(msg)

	case tea.MouseClickMsg:
		g.lastInput = g.now
		return g.handleMouseClick(tea.Mouse(msg))

	case tea.MouseWheelMsg:
		g.lastInput = g.now
		return g.handleMouseWheel(tea.Mouse(msg))
	}
	return g, nil
}

func (g *Game) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if key == "ctrl+c" {
		return g, tea.Quit
	}

	switch g.overlay {
	case ovKicked:
		return g, tea.Quit
	case ovTutorial:
		return g.handleTutorialKey(key)
	case ovAway:
		g.overlay = ovNone
		return g, nil
	case ovPicker:
		return g.handlePickerKey(key)
	case ovUpgrade:
		return g.handleUpgradeKey(key)
	case ovRebirthConfirm:
		return g.handleRebirthConfirmKey(key)
	case ovName:
		return g.handleNameKey(key, msg)
	case ovConfig:
		return g.handleConfigKey(key)
	case ovReplantWarn:
		return g.handleReplantWarnKey(key)
	}

	// Global keys.
	switch key {
	case "q":
		if g.scr == scrHelp {
			g.scr = scrFarm
			g.helpPage = 0
			g.helpScroll = 0
			return g, nil
		}
		return g, tea.Quit
	case "g":
		return g.redeemGift()
	case "1":
		g.scr = scrFarm
		return g, nil
	case "2":
		g.scr = scrMarket
		return g, nil
	case "3":
		g.scr = scrLand
		return g, nil
	case "4":
		g.scr = scrRebirth
		return g, nil
	case "5":
		g.scr = scrStarShop
		return g, nil
	case "6":
		g.scr = scrStats
		return g, nil
	case "7":
		g.scr = scrBoard
		g.refreshBoard()
		return g, nil
	case "?":
		g.scr = scrHelp
		g.helpPage = 0
		g.helpScroll = 0
		return g, nil
	case "tab":
		g.cycleScreen(true)
		return g, nil
	case "shift+tab":
		g.cycleScreen(false)
		return g, nil
	}

	switch g.scr {
	case scrFarm:
		return g.handleFarmKey(key)
	case scrMarket:
		return g.handleMarketKey(key)
	case scrLand:
		return g.handleLandKey(key)
	case scrRebirth:
		return g.handleRebirthKey(key)
	case scrStarShop:
		return g.handleStarShopKey(key)
	case scrStats:
		return g.handleStatsKey(key)
	case scrBoard:
		return g.handleBoardKey(key)
	case scrHelp:
		switch key {
		case "esc":
			g.scr = scrFarm
			g.helpPage = 0
			g.helpScroll = 0
		case "left", "h":
			if g.helpPage > 0 {
				g.helpPage--
				g.helpScroll = 0
			}
		case "right":
			if g.helpPage < 1 {
				g.helpPage++
				g.helpScroll = 0
			}
		case "up", "k":
			if g.helpScroll > 0 {
				g.helpScroll--
			}
		case "down", "j":
			g.helpScroll++
			g.clampHelpScroll()
		}
	}
	return g, nil
}

func (g *Game) cycleScreen(forward bool) {
	for i, s := range screenOrder {
		if s == g.scr {
			if forward {
				g.scr = screenOrder[(i+1)%len(screenOrder)]
			} else {
				g.scr = screenOrder[(i-1+len(screenOrder))%len(screenOrder)]
			}
			if g.scr == scrHelp {
				g.helpPage = 0
				g.helpScroll = 0
			}
			if g.scr == scrBoard {
				g.refreshBoard()
			}
			return
		}
	}
}

func (g *Game) handleFarmKey(key string) (tea.Model, tea.Cmd) {
	st := g.snap.State
	cols := g.farmColumns()
	switch key {
	case "left":
		if g.cursor > 0 {
			g.cursor--
		}
	case "right":
		if g.cursor < len(st.Plots)-1 {
			g.cursor++
		}
	case "up":
		if g.cursor-cols >= 0 {
			g.cursor -= cols
		}
	case "down":
		if g.cursor+cols < len(st.Plots) {
			g.cursor += cols
		}
	case "u":
		g.upgradeIdx = g.cursor
		g.overlay = ovUpgrade
	case "x":
		if g.cursor < len(st.Plots) && st.Plots[g.cursor].Critter != "" {
			reward, snap, ach, err := g.sess.ShooCritter(g.now, g.cursor)
			g.applyAction(snap, ach, err)
			if err == nil {
				g.addNotice("Shooed the " + sanitizeText(st.Plots[g.cursor].Critter) + " (+" + money(reward) + " coins).")
			}
		}
	case "enter", "space", " ":
		if g.cursor >= len(st.Plots) {
			return g, nil
		}
		plot := st.Plots[g.cursor]
		switch {
		case plot.Crop == "":
			g.pickerAutoSow = false
			g.pickerIdx = g.pickerIndexFor(st.LastCrop)
			g.overlay = ovPicker
		case st.PlotReady(g.content, g.cursor, g.now):
			g.doHarvest(g.cursor)
		case plot.AutoSow:
			// A fully-automated plot is almost never harvestable by hand, so
			// reuse enter to change which crop it auto-replants.
			g.openAutoSowPicker(plot)
		default:
			crop := g.content.Crop(plot.Crop)
			if crop != nil {
				left := plot.PlantedAt + st.GrowSeconds(g.content, crop) - g.now
				g.addNotice("Still growing — ready in " + duration(left) + ".")
			}
		}
	case "a":
		g.harvestAllReady()
	case "r":
		return g.handleReplant()
	}
	return g, nil
}

// handleReplant gates the first replant behind a one-time warning, then runs it.
func (g *Game) handleReplant() (tea.Model, tea.Cmd) {
	if !g.snap.State.ReplantWarned {
		g.overlay = ovReplantWarn
		return g, nil
	}
	g.doReplantAll()
	return g, nil
}

// doReplantAll replants every empty plot with its remembered crop, spending
// most-expensive-first, and reports the outcome (including any shortfall).
func (g *Game) doReplantAll() {
	res, snap, ach, err := g.sess.ReplantAll(g.now)
	g.applyAction(snap, ach, err)
	if err != nil {
		return
	}
	if res.Planted == 0 {
		switch {
		case res.Short > 0:
			g.addNotice("Hmm: not enough coins to replant.")
		case res.Locked > 0:
			g.addNotice("Those remembered crops aren't unlocked yet.")
		default:
			g.addNotice("Nothing to replant — plots are busy or have no seed history.")
		}
		return
	}
	msg := "Replanted " + itoa(res.Planted) + " " + plural(res.Planted, "plot", "plots") +
		" (−" + money(res.Spent) + " coins)."
	if res.Short > 0 {
		msg += " Not enough coins for " + itoa(res.Short) + " more."
	}
	g.addNotice(msg)
}

// ackReplantWarning acknowledges the one-time replant warning and returns to
// the farm — triggered by any key press or any click while it's showing.
func (g *Game) ackReplantWarning() (tea.Model, tea.Cmd) {
	if snap, err := g.sess.AckReplantWarning(g.now); err == nil {
		g.snap = snap
	}
	g.overlay = ovNone
	g.addNotice("Replant ready — press r to replant every plot.")
	return g, nil
}

func (g *Game) handleReplantWarnKey(key string) (tea.Model, tea.Cmd) {
	return g.ackReplantWarning()
}

func (g *Game) handleTutorialKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "s":
		g.tutorialSkip = !g.tutorialSkip
	case "left", "h":
		if g.tutorialPage > 0 {
			g.tutorialPage--
		}
	case "enter", "space", " ", "right", "l":
		if g.tutorialSkip || g.tutorialPage >= tutorialPages-1 {
			g.overlay = ovNone
		} else {
			g.tutorialPage++
		}
	case "esc", "q":
		g.overlay = ovNone
	}
	return g, nil
}

func (g *Game) handleConfigKey(key string) (tea.Model, tea.Cmd) {
	settings := len(g.configRows())
	switch key {
	case "esc", "q", "c":
		g.overlay = ovNone
	case "up", "k":
		if g.configIdx > 0 {
			g.configIdx--
		}
	case "down", "j":
		if g.configIdx < settings-1 {
			g.configIdx++
		}
	case "enter", "space", " ":
		g.toggleConfig(g.configIdx)
	}
	return g, nil
}

// openAutoSowPicker opens the crop picker in queue-editing mode, starting the
// cursor on the crop the plot currently replants.
func (g *Game) openAutoSowPicker(plot sim.Plot) {
	g.pickerAutoSow = true
	g.pickerIdx = g.pickerIndexFor(plot.SowCrop())
	g.overlay = ovPicker
}

// pickerIndexFor returns the visible-crop index for cropID, falling back to
// the first crop when there is no remembered or currently visible crop.
func (g *Game) pickerIndexFor(cropID string) int {
	for i, crop := range g.visibleCrops() {
		if crop.ID == cropID {
			return i
		}
	}
	return 0
}

func (g *Game) handlePickerKey(key string) (tea.Model, tea.Cmd) {
	crops := g.visibleCrops()
	switch key {
	case "esc", "q":
		g.overlay = ovNone
		g.pickerAutoSow = false
	case "up", "k":
		if g.pickerIdx > 0 {
			g.pickerIdx--
		}
	case "down", "j":
		if g.pickerIdx < len(crops)-1 {
			g.pickerIdx++
		}
	case "enter", "space", " ":
		if g.pickerIdx >= len(crops) {
			return g, nil
		}
		crop := crops[g.pickerIdx]
		if g.pickerAutoSow {
			snap, ach, err := g.sess.SetAutoSowCrop(g.now, g.cursor, crop.ID)
			g.applyAction(snap, ach, err)
			if err == nil {
				g.overlay = ovNone
				g.pickerAutoSow = false
				g.addNotice("Plot " + itoa(g.cursor+1) + " will auto-sow " + sanitizeText(crop.Name) + ".")
			}
			return g, nil
		}
		st := g.snap.State
		snap, ach, err := g.sess.Plant(g.now, g.cursor, crop.ID)
		g.applyAction(snap, ach, err)
		if err == nil {
			g.overlay = ovNone
			if st.MercyPlantEligible(g.content, crop.ID) {
				g.addNotice("Planted " + sanitizeText(crop.Name) + " — FREE, the land provides.")
			} else {
				g.addNotice("Planted " + sanitizeText(crop.Name) + ".")
			}
		}
	}
	return g, nil
}

func (g *Game) handleUpgradeKey(key string) (tea.Model, tea.Cmd) {
	st := g.snap.State
	switch key {
	case "esc", "q":
		g.overlay = ovNone
	case "up", "k":
		if g.upgradeIdx > 0 {
			g.upgradeIdx--
		}
	case "down", "j":
		if g.upgradeIdx < len(st.Plots)-1 {
			g.upgradeIdx++
		}
	case "1":
		snap, ach, err := g.sess.UpgradePlotAuto(g.now, g.upgradeIdx, "harvest")
		g.applyAction(snap, ach, err)
		if err == nil {
			g.addNotice("Plot " + itoa(g.upgradeIdx+1) + " now auto-harvests!")
		}
	case "2":
		snap, ach, err := g.sess.UpgradePlotAuto(g.now, g.upgradeIdx, "sow")
		g.applyAction(snap, ach, err)
		if err == nil {
			g.addNotice("Plot " + itoa(g.upgradeIdx+1) + " now auto-sows!")
		}
	}
	return g, nil
}

func (g *Game) handleMarketKey(key string) (tea.Model, tea.Cmd) {
	items := g.marketItems()
	switch key {
	case "up", "k":
		if g.marketIdx > 0 {
			g.marketIdx--
		}
	case "down", "j":
		if g.marketIdx < len(items)-1 {
			g.marketIdx++
		}
	case "enter", "space", " ", "b":
		if g.marketIdx >= len(items) {
			return g, nil
		}
		it := items[g.marketIdx]
		now := g.now
		var (
			snap game.Snapshot
			ach  []string
			err  error
		)
		switch it.kind {
		case "zone":
			snap, ach, err = g.sess.BuyZone(now, it.id)
		case "multiplier":
			snap, ach, err = g.sess.BuyMultiplier(now, it.id)
		case "strain":
			snap, ach, err = g.sess.BuySeedUpgrade(now, it.id)
		case "scarecrow":
			_, snap, ach, err = g.sess.BuyScarecrow(now)
		}
		g.applyAction(snap, ach, err)
		if err == nil {
			g.addNotice("Bought " + sanitizeText(it.name) + "!")
		}
	}
	return g, nil
}

func (g *Game) handleLandKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "enter", "space", " ", "b":
		cost, snap, ach, err := g.sess.BuyPlot(g.now)
		g.applyAction(snap, ach, err)
		if err == nil {
			g.addNotice("New plot tilled for " + money(cost) + " coins.")
		}
	}
	return g, nil
}

func (g *Game) handleRebirthKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "R":
		if g.snap.State.CanRebirth(g.content) {
			g.overlay = ovRebirthConfirm
		} else {
			need := g.content.Prestige.MinEarnings
			g.addNotice("Earn " + money(need) + " coins this run to rebirth (so far: " + money(g.snap.State.RunEarnings) + ").")
		}
	}
	return g, nil
}

func (g *Game) handleStarShopKey(key string) (tea.Model, tea.Cmd) {
	st := g.snap.State
	if st.Rebirths < 1 {
		return g, nil
	}
	ups := g.content.Upgrades
	switch key {
	case "up", "k":
		if g.progressIdx > 0 {
			g.progressIdx--
		}
	case "down", "j":
		if g.progressIdx < len(ups)-1 {
			g.progressIdx++
		}
	case "enter", "space", " ", "b":
		if g.progressIdx >= len(ups) {
			return g, nil
		}
		u := ups[g.progressIdx]
		snap, ach, err := g.sess.BuyUpgrade(g.now, u.ID)
		g.applyAction(snap, ach, err)
		if err == nil {
			g.addNotice(sanitizeText(u.Name) + " is now level " + itoa(g.snap.State.UpgradeLevel(u.ID)) + ".")
		}
	}
	return g, nil
}

func (g *Game) handleRebirthConfirmKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "y", "Y":
		gain, snap, ach, err := g.sess.Rebirth(g.now)
		g.applyAction(snap, ach, err)
		g.overlay = ovNone
		if err == nil {
			g.cursor = 0
			g.addNotice("Reborn! +" + money(gain) + " " + g.starseedLabel() + ". The land is fresh again.")
		}
	case "n", "N", "esc", "q":
		g.overlay = ovNone
	}
	return g, nil
}

func (g *Game) handleStatsKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "c":
		g.configIdx = 0
		g.overlay = ovConfig
	case "n":
		g.nameInput = g.snap.State.FarmName
		g.overlay = ovName
	}
	return g, nil
}

// handleBoardKey drives the leaderboard screen (gameplay/02's Board,
// gameplay/03's per-row display rules). It computes nothing itself — every
// number on screen comes straight from the last Engine.Get result.
func (g *Game) handleBoardKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		g.scr = scrFarm
	case "up", "k":
		if g.lbScroll > 0 {
			g.lbScroll--
		}
	case "down", "j":
		g.lbScroll++
		g.clampBoardScroll()
	case "n":
		g.openRename()
	case "r", "R":
		g.refreshBoard()
	}
	return g, nil
}

// openRename primes the rename overlay with the farm's current name — the
// same shortcut the Stats screen's "n" key and the board's your-row click
// both offer.
func (g *Game) openRename() {
	g.nameInput = g.snap.State.FarmName
	g.overlay = ovName
}

// refreshBoard calls the leaderboard engine for "you" (which is cheap: the
// engine holds its own TTL-bounded cache, see internal/leaderboard) and
// schedules the next automatic refresh boardRefreshSeconds out. Called on
// every path that enters the board screen, on the tick loop while it's
// open, and on an explicit r/click refresh.
func (g *Game) refreshBoard() {
	you := leaderboard.SaveRef{Fingerprint: g.id.Fingerprint, Slot: g.id.Slot}
	board, err := g.board.Get(context.Background(), you)
	g.lbErr = err
	if err == nil {
		g.lbBoard = board
	}
	g.lbNextRefresh = g.now + boardRefreshSeconds
	g.clampBoardScroll()
}

// boardLine is one rendered line of the board's scrollable region, paired
// with its source Row (nil for the divider/status lines) so hitboxes can
// be attached without re-deriving the same line layout twice.
type boardLine struct {
	text string
	row  *leaderboard.Row
}

// boardLines lays out gameplay/02's Top (up to 10) then, only when it
// exists, a divider and the ±3 Window around you — gameplay/02 already
// dedupes the window against Top, so this never repeats a row.
func (g *Game) boardLines(cw int) []boardLine {
	th := g.theme()
	rankWidth := 0
	for _, r := range g.lbBoard.Top {
		rankWidth = max(rankWidth, len(itoa(r.Rank)))
	}
	for _, r := range g.lbBoard.Window {
		rankWidth = max(rankWidth, len(itoa(r.Rank)))
	}

	var lines []boardLine
	for i := range g.lbBoard.Top {
		lines = append(lines, boardLine{text: g.boardRowLine(g.lbBoard.Top[i], cw, rankWidth), row: &g.lbBoard.Top[i]})
	}
	if len(g.lbBoard.Window) > 0 {
		lines = append(lines, boardLine{text: th.Rule.Render(strings.Repeat("─", max(cw, 1)))})
		for i := range g.lbBoard.Window {
			lines = append(lines, boardLine{text: g.boardRowLine(g.lbBoard.Window[i], cw, rankWidth), row: &g.lbBoard.Window[i]})
		}
	}
	if len(lines) == 0 {
		lines = append(lines, boardLine{text: th.Hint.Render("No farms on the board yet — be the first!")})
	}
	return lines
}

// boardRowLine renders one Row: a "▸"+highlight+"← YOU" marker on your own
// row (rendered highlighted exactly once — Row.IsYou is only ever true for
// the one row matching your fingerprint+slot, even amid coin ties), the
// suffix always shown next to the name (gameplay/03's anti-impersonation
// signal), the rebirth count ("↻ N"), and a tasteful gold/cyan/violet accent
// on the top 3 ranks. The right-aligned amount is lifetime coin earnings,
// not the save's current spendable balance. rankWidth right-aligns the rank
// number to the widest rank in the current Top+Window so "#1" and "#10"
// don't stagger the name column.
func (g *Game) boardRowLine(r leaderboard.Row, cw, rankWidth int) string {
	th := g.theme()
	name := r.DisplayName
	if name == "" {
		name = "FARM" // never-renamed farm; gameplay/02's documented UI fallback
	}
	marker := "  "
	if r.IsYou {
		marker = "▸ "
	}
	rank := itoa(r.Rank)
	if pad := rankWidth - len(rank); pad > 0 {
		rank = strings.Repeat(" ", pad) + rank
	}
	prefix := marker + "#" + rank + "  "
	suffix := " ·" + r.Suffix
	rebirth := " ↻ " + money(r.Rebirths)
	youMarker := ""
	if r.IsYou {
		youMarker = "  ← YOU"
	}
	right := "◈ " + money(r.Coins)
	fixedWidth := lipgloss.Width(prefix) + lipgloss.Width(suffix) + lipgloss.Width(rebirth) +
		lipgloss.Width(youMarker) + lipgloss.Width(right) + 1
	nameWidth := cw - fixedWidth
	if nameWidth < 1 {
		nameWidth = 1
	}
	left := prefix + truncate(sanitizeText(name), nameWidth) + suffix + rebirth + youMarker
	line := alignSides(left, right, cw)
	switch {
	case r.IsYou:
		return th.Selected.Render(line)
	case r.Rank == 1:
		return th.BoardGold.Render(line)
	case r.Rank == 2:
		return th.BoardSilver.Render(line)
	case r.Rank == 3:
		return th.BoardBronze.Render(line)
	default:
		return th.Value.Render(line)
	}
}

// boardHeaderLine is the one line that must never scroll away: the title
// plus "YOU: #rank/total" (or the unranked callout for a farm still below
// gameplay/02's coin floor).
func (g *Game) boardHeaderLine(cw int) string {
	th := g.theme()
	title := th.Section.Render("LEADERBOARD — RICHEST FARMS")
	var rank string
	switch {
	case g.lbBoard.You == nil:
		rank = "YOU: UNRANKED — EARN YOUR FIRST COIN"
	default:
		rank = "YOU: #" + itoa(g.lbBoard.You.Rank) + "/" + itoa(g.lbBoard.Total)
	}
	return alignSides(title, th.Value.Render(rank), cw)
}

// boardVisibleRows is how many of boardLines' rows fit under the pinned
// header, above the scroll hint + staleness line.
func (g *Game) boardVisibleRows() int {
	return max(g.contentHeight()-11, 3)
}

// boardVisibleRange clamps g.lbScroll and returns the [start,end) slice of
// boardLines currently on screen — shared by the renderer and the hitbox
// registration so the two can never disagree about which line is where.
func (g *Game) boardVisibleRange(total int) (start, end int) {
	visible := g.boardVisibleRows()
	maxStart := max(total-visible, 0)
	start = g.lbScroll
	if start > maxStart {
		start = maxStart
	}
	if start < 0 {
		start = 0
	}
	end = start + visible
	if end > total {
		end = total
	}
	return start, end
}

func (g *Game) clampBoardScroll() {
	total := len(g.boardLines(g.contentWidth()))
	maxStart := max(total-g.boardVisibleRows(), 0)
	if g.lbScroll > maxStart {
		g.lbScroll = maxStart
	}
	if g.lbScroll < 0 {
		g.lbScroll = 0
	}
}

func (g *Game) handleNameKey(key string, msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "q":
		g.overlay = ovNone
	case "enter":
		snap, err := g.sess.RenameFarm(context.Background(), g.now, g.nameInput)
		if err != nil {
			switch {
			case errors.Is(err, game.ErrNameRateLimited):
				g.addNotice("Slow down — try again in a bit.")
			case errors.Is(err, game.ErrNameLocked):
				g.addNotice("This farm's name has been locked.")
			default:
				g.addNotice("That name isn't allowed.")
			}
			return g, nil
		}
		g.snap = snap
		g.overlay = ovNone
		g.addNotice("Your farm is now called " + sanitizeText(g.snap.State.FarmName) + ".")
	case "backspace":
		runes := []rune(g.nameInput)
		if len(runes) > 0 {
			g.nameInput = string(runes[:len(runes)-1])
		}
	default:
		if key == "space" {
			key = " "
		}
		// Accept any single printable rune (not just ASCII), capped at the
		// sim's 24-rune limit so the overlay can't grow unbounded from
		// held-down keys before Enter validates it.
		r := []rune(key)
		if len(r) == 1 && unicode.IsPrint(r[0]) && len([]rune(g.nameInput)) < 24 {
			g.nameInput += string(r)
		}
	}
	return g, nil
}

func (g *Game) redeemGift() (tea.Model, tea.Cmd) {
	res, snap, ach, err := g.sess.RedeemGift(g.now)
	g.applyAction(snap, ach, err)
	if err != nil {
		return g, nil
	}
	if res.Starseeds > 0 {
		g.addNotice("📦 Parcel opened! +" + money(res.Starseeds) + " " + g.starseedLabel() + "!")
	} else {
		g.addNotice("📦 Parcel opened! +" + money(res.Coins) + " coins!")
	}
	return g, nil
}

func (g *Game) doHarvest(i int) {
	res, snap, ach, err := g.sess.Harvest(g.now, i)
	g.applyAction(snap, ach, err)
	if err != nil {
		return
	}
	crop := g.content.Crop(res.CropID)
	name := res.CropID
	if crop != nil {
		name = crop.Name
	}
	if res.Golden {
		g.addNotice("✨ GOLDEN HARVEST! " + sanitizeText(name) + " (+" + money(res.Payout) + ")")
		return
	}
	if res.Failed && crop != nil {
		g.addNotice("💥 " + sanitizeText(name) + " failed — salvaged " + money(res.Payout) + " coins (" + salvageFractionLabel(g.snap.State.SeedUpgradeLevel(crop.ID)) + " of normal).")
		return
	}
	text := "Harvested " + sanitizeText(name) + " (+" + money(res.Payout) + ")"
	if res.Discovery > 0 {
		text += " and found " + money(res.Discovery) + " coins in the soil!"
	}
	g.addNotice(text)
}

func salvageFractionLabel(strainLevel int) string {
	switch strainLevel {
	case 0:
		return "1/8"
	case 1:
		return "1/4"
	case 2:
		return "1/2"
	default:
		return "3/4"
	}
}

func (g *Game) harvestAllReady() {
	st := g.snap.State
	total, count := int64(0), 0
	now := g.now
	for i := range st.Plots {
		if !st.PlotReady(g.content, i, g.now) {
			continue
		}
		res, snap, ach, err := g.sess.Harvest(now, i)
		if err != nil {
			continue
		}
		g.snap = snap
		g.achievementNotices(ach)
		total += res.Payout + res.Discovery
		count++
	}
	if count > 0 {
		g.addNotice("Harvested " + itoa(count) + " plots (+" + money(total) + " coins).")
	} else {
		g.addNotice("Nothing is ready yet.")
	}
}

func (g *Game) applyAction(snap game.Snapshot, ach []string, err error) {
	if err != nil {
		if err == game.ErrSessionClosed {
			return
		}
		g.addNotice("Hmm: " + err.Error() + ".")
		if snap.State != nil {
			g.snap = snap
		}
		return
	}
	g.snap = snap
	g.achievementNotices(ach)
}

func (g *Game) achievementNotices(ids []string) {
	for _, id := range ids {
		for _, a := range g.content.Achievements {
			if a.ID == id {
				g.addNotice("🏆 Achievement: " + sanitizeText(a.Name) + "!")
			}
		}
	}
}

func (g *Game) eventNotices(ev sim.Events) {
	for id, n := range ev.Matured {
		name := id
		if crop := g.content.Crop(id); crop != nil {
			name = crop.Name
		}
		if n == 1 {
			g.addNotice("🌱 Your " + sanitizeText(name) + " is ready to harvest!")
		} else {
			g.addNotice("🌱 " + itoa(n) + "× " + sanitizeText(name) + " are ready to harvest!")
		}
	}
	if ev.AutoCoins > 0 {
		g.addNotice("⚙ Auto-plots gathered crops (+" + money(ev.AutoCoins) + ").")
	}
	if ev.ScarecrowCoins > 0 {
		g.addNotice("🐦 Your scarecrow shooed critters (+" + money(ev.ScarecrowCoins) + " coins).")
	}
	if ev.GoldenHarvests > 0 {
		g.addNotice("✨ " + itoa(ev.GoldenHarvests) + " golden harvest(s)!")
	}
	for id, n := range ev.FailedHarvests {
		name := id
		if crop := g.content.Crop(id); crop != nil {
			name = crop.Name
		}
		g.addNotice("💥 " + itoa(n) + "× " + sanitizeText(name) + " failed while you were away.")
	}
	if ev.GiftArrived {
		g.addNotice("📦 A parcel waits at the gate — press g to open it.")
	}
	if ev.EventEnded != "" {
		if e := g.content.EventByID(ev.EventEnded); e != nil {
			g.addNotice("⚡ " + sanitizeText(e.Name) + " has passed.")
		}
	}
	if ev.EventStarted != "" {
		if e := g.content.EventByID(ev.EventStarted); e != nil {
			g.addNotice("📰 " + sanitizeText(e.Name) + ": " + sanitizeText(e.Description))
		}
	}
	for _, c := range ev.CritterVisits {
		g.addNotice("A " + sanitizeText(c) + " visited an empty plot.")
	}
	g.achievementNotices(ev.Achievements)
}

func (g *Game) addNotice(text string) {
	g.notices = append(g.notices, notice{text: text, expires: g.now + 6})
	if len(g.notices) > 4 {
		g.notices = g.notices[len(g.notices)-4:]
	}
}

func (g *Game) pruneNotices() {
	kept := g.notices[:0]
	for _, n := range g.notices {
		if n.expires > g.now {
			kept = append(kept, n)
		}
	}
	g.notices = kept
}

func (g *Game) starseedLabel() string { return g.content.StarseedLabel() }

func (g *Game) visibleCrops() []content.Crop {
	return sim.VisibleCrops(g.snap.State, g.content)
}

type marketItem struct {
	id, name, desc string
	cost           int64
	kind           string // multiplier, strain, zone
	owned          bool
	locked         bool
	gate           content.Unlock
	level, maxLvl  int
}

func (g *Game) marketItems() []marketItem {
	st := g.snap.State
	var items []marketItem
	for i := range g.content.Multipliers {
		m := &g.content.Multipliers[i]
		lvl := st.MultiplierLevel(m.ID)
		items = append(items, marketItem{
			id: m.ID, name: m.Name, desc: m.Description,
			cost: st.MultiplierCost(m), kind: "multiplier",
			locked: false, level: lvl, maxLvl: m.MaxLevel,
		})
	}
	for i := range g.content.SeedUpgrades {
		su := &g.content.SeedUpgrades[i]
		crop := g.content.Crop(su.CropID)
		locked := crop != nil && !st.Unlocked(crop.Unlock)
		items = append(items, marketItem{
			id: su.ID, name: su.Name, desc: su.Description,
			cost: st.SeedUpgradeCost(su), kind: "strain",
			locked: locked, level: st.SeedUpgradeLevel(su.CropID), maxLvl: su.MaxLevel,
		})
	}
	if g.content.Scarecrow.Cost > 0 {
		items = append(items, marketItem{
			id: "scarecrow", name: "Scarecrow",
			desc: "Auto-shoos critters for coins; trickles a little while you're away.",
			cost: g.content.Scarecrow.Cost, kind: "scarecrow", owned: st.Scarecrow,
		})
	}
	for _, z := range g.content.Zones {
		items = append(items, marketItem{
			id: z.ID, name: z.Name, desc: z.Description, cost: z.Cost, kind: "zone",
			owned: st.Zones[z.ID], locked: !st.Unlocked(z.Unlock), gate: z.Unlock,
		})
	}
	return items
}

// marketLine is one rendered line of the Market screen, paired with its source
// item index (when selectable) so hitboxes can mirror the renderer exactly.
type marketLine struct {
	text string
	idx  int // marketItems index; -1 for section headers and blank lines
}

const noMarketItem = -1

// marketLines lays out the Market screen body in the same order viewMarket
// draws it — section headers, blank separators, and one row per item.
func (g *Game) marketLines() []marketLine {
	th := g.theme()
	st := g.snap.State
	items := g.marketItems()
	var lines []marketLine

	lines = append(lines, marketLine{text: th.Section.Render("Multipliers (this run)"), idx: noMarketItem})
	for i, it := range items {
		if it.kind != "multiplier" {
			continue
		}
		lines = append(lines, marketLine{text: g.marketItemRow(i, it, st), idx: i})
	}

	lines = append(lines, marketLine{idx: noMarketItem})
	lines = append(lines, marketLine{text: th.Section.Render("Hardier Strains"), idx: noMarketItem})
	for i, it := range items {
		if it.kind != "strain" {
			continue
		}
		lines = append(lines, marketLine{text: g.marketItemRow(i, it, st), idx: i})
	}

	if g.content.Scarecrow.Cost > 0 {
		lines = append(lines, marketLine{idx: noMarketItem})
		lines = append(lines, marketLine{text: th.Section.Render("Helpers (this run)"), idx: noMarketItem})
		for i, it := range items {
			if it.kind != "scarecrow" {
				continue
			}
			lines = append(lines, marketLine{text: g.marketItemRow(i, it, st), idx: i})
		}
	}

	lines = append(lines, marketLine{idx: noMarketItem})
	lines = append(lines, marketLine{text: th.Section.Render("Zones"), idx: noMarketItem})
	for i, it := range items {
		if it.kind != "zone" {
			continue
		}
		lines = append(lines, marketLine{text: g.marketItemRow(i, it, st), idx: i})
	}

	return lines
}

func (g *Game) marketItemRow(i int, it marketItem, st *sim.State) string {
	th := g.theme()
	marker := "  "
	if i == g.marketIdx {
		marker = th.Selected.Render("▸ ")
	}
	switch it.kind {
	case "multiplier", "strain":
		line := sanitizeText(it.name) + " Lv" + itoa(it.level) + "/" + itoa(it.maxLvl) + " — " + sanitizeText(it.desc)
		switch {
		case it.level >= it.maxLvl:
			return marker + th.Ready.Render("✓ "+line+"  maxed")
		case it.kind == "strain" && it.locked:
			return marker + th.Locked.Render(line+"  🔒")
		case st.Coins < it.cost:
			return marker + th.Locked.Render(line+"  "+money(it.cost)+"c")
		default:
			return marker + th.Value.Render(line+"  "+money(it.cost)+"c")
		}
	case "scarecrow":
		line := sanitizeText(it.name) + " — " + sanitizeText(it.desc)
		switch {
		case it.owned:
			return marker + th.Ready.Render("✓ "+line+"  owned")
		case st.Coins < it.cost:
			return marker + th.Locked.Render(line+"  "+money(it.cost)+"c")
		default:
			return marker + th.Value.Render(line+"  "+money(it.cost)+"c")
		}
	case "zone":
		line := sanitizeText(it.name) + " — " + money(it.cost) + "c · " + sanitizeText(it.desc)
		switch {
		case it.owned:
			return marker + th.Ready.Render("✓ "+line)
		case it.locked:
			// The gate reason is the first thing to go on a narrow terminal:
			// the item name matters more than why it is locked.
			return marker + th.Locked.Render(fitWidth(line+"  🔒 "+g.gateText(it.gate),
				g.contentWidth()-lipgloss.Width(marker)))
		case st.Coins < it.cost:
			return marker + th.Locked.Render(line+"  (can't afford)")
		default:
			return marker + th.Value.Render(line)
		}
	}
	return ""
}

// starShopLine is one rendered line of the Star Shop screen, paired with its
// upgrade index when the row is selectable.
type starShopLine struct {
	text string
	idx  int // content.Upgrades index; -1 for headers, stats, and blank lines
}

const noShopItem = -1

func (g *Game) starShopLines() []starShopLine {
	th := g.theme()
	st := g.snap.State
	if st.Rebirths < 1 {
		hint := centerWrap(g.contentWidth(), "The cosmos keeps its deeper rewards for those who begin anew.")
		return []starShopLine{
			{text: th.Section.Render("StarShop"), idx: noShopItem},
			{idx: noShopItem},
			{text: th.Locked.Render("  🔒 Rebirth once to discover what lies beyond."), idx: noShopItem},
			{idx: noShopItem},
			{text: th.Hint.Render(hint), idx: noShopItem},
		}
	}

	var lines []starShopLine
	lines = append(lines, starShopLine{text: th.Section.Render("StarShop — " + g.starseedLabel()), idx: noShopItem})
	lines = append(lines, starShopLine{idx: noShopItem})
	lines = append(lines, starShopLine{text: "  Balance: " + th.Value.Render("✦ "+money(st.PrestigeCurrency)), idx: noShopItem})
	lines = append(lines, starShopLine{text: "  Rebirths: " + th.Value.Render(money(st.Rebirths)), idx: noShopItem})
	lines = append(lines, starShopLine{idx: noShopItem})
	lines = append(lines, starShopLine{text: th.Section.Render("Lifetime upgrades"), idx: noShopItem})

	lineWidth := g.contentWidth() - 2
	for i, u := range g.content.Upgrades {
		marker := "  "
		if i == g.progressIdx {
			marker = th.Selected.Render("▸ ")
		}
		level := st.UpgradeLevel(u.ID)
		cost := st.UpgradeCost(&g.content.Upgrades[i])
		line := sanitizeText(u.Name) + " (Lv " + itoa(level) + "/" + itoa(u.MaxLevel) + ") — " + sanitizeText(u.Description)
		var row string
		switch {
		case cost < 0:
			row = marker + th.Ready.Render(alignSides("✓ "+line, "maxed", lineWidth))
		case st.PrestigeCurrency < cost:
			row = marker + th.Locked.Render(alignSides(line, "✦ "+money(cost), lineWidth))
		default:
			row = marker + th.Value.Render(alignSides(line, "✦ "+money(cost), lineWidth))
		}
		lines = append(lines, starShopLine{text: row, idx: i})
	}
	return lines
}

func itoa(n int) string {
	return money(int64(n))
}
