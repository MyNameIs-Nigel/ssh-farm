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
	"github.com/charmbracelet/colorprofile"

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
	scrContracts // reached from StarShop; deliberately absent from screenOrder/nav
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
	ovContractConfirm
	ovContractReward
	ovKicked
	ovGuestQuit
	ovDev
	ovFertilizerConfirm
)

// tutorialPages is the number of pages in the new-player tutorial.
const tutorialPages = 4

type notice struct {
	text    string
	expires int64
}

type (
	tickMsg      time.Time
	boardAnimMsg time.Time
	kickedMsg    string
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

	scr               screen
	overlay           overlay
	cursor            int
	scareSel          int // scarecrow under the cursor, or -1 on a plot; cursor keeps its plot meanwhile
	pickerIdx         int
	pickerAutoSow     bool // picker is choosing an auto-sow queue, not planting now
	marketIdx         int
	upgradeIdx        int
	progressIdx       int
	helpPage          int
	helpScroll        int
	nameInput         string
	contractIdx       int
	contractID        sim.ContractID
	contractAbandon   bool
	completedContract sim.ContractID
	// pendingReward is a contract completion waiting for the screen to be
	// free of other modals; see showPendingReward.
	pendingReward sim.ContractID

	board            *leaderboard.Engine
	lbBoard          leaderboard.Board
	lbErr            error
	lbNextRefresh    int64
	lbScroll         int
	boardAnimPhase   int
	boardAnimRunning bool
	// limitedColor is set from the program's ColorProfileMsg when the
	// terminal has fewer than 256 colours; the name wave then holds still.
	// The server currently forces TrueColor for every session, so this is
	// the fallback for any host that stops doing so.
	limitedColor bool

	tutorialPage int
	tutorialSkip bool
	// fertDontAsk is the fertilizer prompt's "don't ask again" box. It is
	// only saved when the player confirms with it ticked.
	fertDontAsk bool
	devSel      int
	devInput    string
	configIdx   int

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
// model's clock, the player's solid-background setting, the active event
// (which recolours the frame and lifts the canvas), and the festival skin.
func (g *Game) theme() theme.Theme {
	st := g.snap.State
	eventID := ""
	if st != nil && st.EventActive(g.now) {
		eventID = st.EventID
	}
	solid := st != nil && st.ThemeSolid
	return theme.NewWithSeason(theme.PhaseAt(g.now), solid, eventID, g.season())
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
		scareSel:    -1,
	}
	switch {
	case res.Created:
		g.overlay = ovTutorial
	case !res.Away.Empty() || res.Away.Elapsed > 60:
		g.away = res.Away
		g.overlay = ovAway
	}
	g.announceContract(res.Away.ContractCompleted)
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

func boardAnimCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg { return boardAnimMsg(t) })
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
	m, cmd := g.update(msg)
	g.showPendingReward()
	return m, cmd
}

func (g *Game) update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		g.announceContract(snap.ContractCompleted)
		g.pruneNotices()
		if g.scr == scrBoard && g.now >= g.lbNextRefresh {
			g.refreshBoard()
		}
		return g, tea.Batch(tickCmd(), g.startBoardAnimation())

	case tea.ColorProfileMsg:
		g.limitedColor = msg.Profile < colorprofile.ANSI256
		return g, nil

	case boardAnimMsg:
		if !g.boardAnimates() {
			g.boardAnimRunning = false
			return g, nil
		}
		g.boardAnimPhase++
		return g, boardAnimCmd()

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
		return g.requestQuit()
	}

	if key == "ctrl+d" && g.sess.DevMode() && g.overlay != ovKicked {
		if g.overlay == ovDev {
			g.overlay = ovNone
		} else {
			g.overlay = ovDev
			g.devInput = ""
		}
		return g, nil
	}

	switch g.overlay {
	case ovDev:
		return g.handleDevKey(key)
	case ovGuestQuit:
		switch key {
		case "enter", "y":
			return g, tea.Quit
		case "esc", "n", "q":
			g.overlay = ovNone
		}
		return g, nil
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
	case ovContractConfirm:
		return g.handleContractConfirmKey(key)
	case ovFertilizerConfirm:
		return g.handleFertilizerConfirmKey(key)
	case ovContractReward:
		g.overlay = ovNone
		return g, nil
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
		return g.requestQuit()
	case "g":
		return g.redeemGift()
	case "F":
		return g.requestFertilizer()
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
		return g, g.startBoardAnimation()
	case "?":
		g.scr = scrHelp
		g.helpPage = 0
		g.helpScroll = 0
		return g, nil
	case "tab":
		return g, g.cycleScreen(true)
	case "shift+tab":
		return g, g.cycleScreen(false)
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
	if g.scr == scrContracts {
		return g.handleContractsKey(key)
	}
	return g, nil
}

func (g *Game) cycleScreen(forward bool) tea.Cmd {
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
				return g.startBoardAnimation()
			}
			return nil
		}
	}
	return nil
}

func (g *Game) handleFarmKey(key string) (tea.Model, tea.Cmd) {
	st := g.snap.State
	g.clampFarmCursor()
	onScare := g.scareSel >= 0
	switch key {
	case "left", "right", "up", "down":
		g.navigateFarm(key)
	case "u":
		if onScare {
			g.addNotice("Scarecrows don't take plot upgrades — buy more in the Market.")
			return g, nil
		}
		g.upgradeIdx = g.cursor
		g.overlay = ovUpgrade
	case "x":
		if !onScare && g.cursor < len(st.Plots) && st.Plots[g.cursor].Critter != "" {
			reward, snap, ach, err := g.sess.ShooCritter(g.now, g.cursor)
			g.applyAction(snap, ach, err)
			if err == nil {
				g.addNotice("Shooed the " + sanitizeText(g.critterName(st.Plots[g.cursor].Critter)) + " (+" + money(reward) + " coins).")
			}
		}
	case "enter", "space", " ":
		if onScare {
			g.collectScarecrow(g.scareSel)
			return g, nil
		}
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
			if st.MercyPlantEligible(g.content, crop.ID) && sim.SeasonalPlantable(crop.Unlock, g.now) {
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
	if st.ActiveContract == sim.ContractBareHands || st.ActiveContract == sim.ContractClockworkDenied {
		if key == "esc" || key == "q" {
			g.overlay = ovNone
		}
		return g, nil
	}
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
		if it.lockReason != "" {
			g.addNotice(sanitizeText(it.name) + " is " + it.lockReason + ".")
			return g, nil
		}
		if it.kind == "scarecrow" && it.locked {
			g.addNotice(g.scarecrowGateNotice())
			return g, nil
		}
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
		if err == nil && it.kind == "scarecrow" {
			g.addNotice("Scarecrow " + itoa(len(g.snap.State.Scarecrows)) + " is standing guard beside your plots.")
		} else if err == nil {
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
	if st.ProgressionRebirths() < 1 && !st.ContractsAvailable(g.content) {
		return g, nil
	}
	ups := g.content.Upgrades
	g.progressIdx = clamp(g.progressIdx, 0, g.starShopRowCount()-1)
	switch key {
	case "c":
		g.openContracts()
	case "up", "k":
		if g.progressIdx > 0 {
			g.progressIdx--
		}
	case "down", "j":
		if g.progressIdx < g.starShopRowCount()-1 {
			g.progressIdx++
		}
	case "enter", "space", " ", "b":
		if g.progressIdx == contractsRowIdx(g.content) {
			g.openContracts()
			return g, nil
		}
		if g.progressIdx == fertilizerRowIdx(g.content) {
			g.buyFertilizer()
			return g, nil
		}
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

// fertilizerRowIdx is the StarShop row index of the Fertilizer purchase,
// directly after the lifetime upgrades, or -1 when this balance does not
// sell it (gameplay/06).
func fertilizerRowIdx(c *content.Content) int {
	if !sim.FertilizerForSale(c) {
		return -1
	}
	return len(c.Upgrades)
}

// contractsRowIdx is the StarShop row index of the Contracts entry, which
// sits after the lifetime upgrades and the Fertilizer row (gameplay/04: no
// ninth nav tab).
func contractsRowIdx(c *content.Content) int {
	if fertilizerRowIdx(c) >= 0 {
		return len(c.Upgrades) + 1
	}
	return len(c.Upgrades)
}

// starShopRowCount is how many StarShop rows the cursor can land on.
func (g *Game) starShopRowCount() int {
	if g.snap.State.ContractsAvailable(g.content) {
		return contractsRowIdx(g.content) + 1
	}
	return contractsRowIdx(g.content)
}

// openContracts moves to the Contracts screen with the next contract (or
// the last one, once the campaign is done) under the cursor.
func (g *Game) openContracts() {
	st := g.snap.State
	if !st.ContractsAvailable(g.content) {
		return
	}
	g.scr = scrContracts
	g.contractIdx = clamp(st.ContractsCompleted, 0, len(sim.Contracts())-1)
}

func (g *Game) handleContractsKey(key string) (tea.Model, tea.Cmd) {
	contracts := sim.Contracts()
	switch key {
	case "esc", "q":
		g.scr = scrStarShop
	case "up", "k":
		g.contractIdx = max(g.contractIdx-1, 0)
	case "down", "j":
		g.contractIdx = min(g.contractIdx+1, len(contracts)-1)
	case "enter", "space", " ":
		if g.snap.State.ActiveContract != "" {
			return g, nil
		}
		if g.contractIdx == g.snap.State.ContractsCompleted && g.contractIdx < len(contracts) {
			g.contractID = contracts[g.contractIdx].ID
			g.contractAbandon = false
			g.overlay = ovContractConfirm
		}
	case "a", "A":
		if g.snap.State.ActiveContract != "" {
			g.contractID = g.snap.State.ActiveContract
			g.contractAbandon = true
			g.overlay = ovContractConfirm
		}
	}
	return g, nil
}

func (g *Game) handleContractConfirmKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "y", "Y":
		if g.contractAbandon {
			snap, ach, err := g.sess.AbandonContract(g.now)
			// The catch-up before the abandon can cross the goal, leaving
			// nothing to abandon: the reward is the news, not a refusal.
			won := errors.Is(err, sim.ErrNoActiveContract) && snap.ContractCompleted != ""
			if won {
				err = nil
			}
			g.applyAction(snap, ach, err)
			if err == nil && !won {
				g.addNotice("Contract abandoned. The farm begins fresh.")
			}
		} else {
			snap, ach, err := g.sess.StartContract(g.now, g.contractID)
			g.applyAction(snap, ach, err)
			if err == nil {
				g.scr = scrFarm
				g.cursor = 0
				g.addNotice("Contract accepted. The farm begins fresh.")
			}
		}
		g.overlay = ovNone
	case "n", "N", "esc", "q":
		g.overlay = ovNone
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
		g.openRename()
	case "l":
		if g.id.IsGuest() {
			g.guestLocked("change leaderboard identity")
			return g, nil
		}
		if len(g.snap.State.AvailableLeaderboardNameStyles()) > 1 {
			snap, err := g.sess.SetLeaderboardNameStyle(g.now, g.snap.State.NextLeaderboardNameStyle())
			g.applyAction(snap, nil, err)
		}
	case "L":
		// Only advertised once Contract 3 has earned the wave; before that
		// the key is inert rather than an error for a control nobody saw.
		if g.snap.State.ContractsCompleted < 3 {
			return g, nil
		}
		if g.id.IsGuest() {
			g.guestLocked("change leaderboard identity")
			return g, nil
		}
		snap, err := g.sess.ToggleLeaderboardNameWave(g.now)
		g.applyAction(snap, nil, err)
		// Traditional has nothing to animate, so the Stats line cannot
		// show the change; say what the next colour will do instead.
		if err == nil && g.snap.State.LeaderboardNameStyle == sim.NameStyleTraditional {
			if g.snap.State.LeaderboardWaveOff {
				g.addNotice("Name wave off: colors will hold still.")
			} else {
				g.addNotice("Name wave on: colors will animate.")
			}
		}
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
	case "pgup":
		g.lbScroll -= g.boardVisibleRows()
		g.clampBoardScroll()
	case "pgdown":
		g.lbScroll += g.boardVisibleRows()
		g.clampBoardScroll()
	case "home":
		g.lbScroll = 0
	case "end":
		g.lbScroll = len(g.lbBoard.Rows)
		g.clampBoardScroll()
	case "n":
		g.openRename()
	case "r", "R":
		g.refreshBoardNow()
		return g, g.startBoardAnimation()
	}
	return g, nil
}

// openRename primes the rename overlay with the farm's current name — the
// same shortcut the Stats screen's "n" key and the board's your-row click
// both offer.
func (g *Game) openRename() {
	if g.id.IsGuest() {
		g.guestLocked("rename your farm")
		return
	}
	g.nameInput = g.snap.State.FarmName
	g.overlay = ovName
}

// refreshBoard calls the leaderboard engine for "you" (which is cheap: the
// engine holds its own TTL-bounded cache, see internal/leaderboard) and
// schedules the next automatic refresh boardRefreshSeconds out. Called on
// every path that enters the board screen, on the tick loop while it's
// open, and on an explicit r/click refresh.
func (g *Game) refreshBoard() { g.loadBoard(false) }

// refreshBoardNow is the explicit r/click refresh. Unlike refreshBoard it
// skips the engine's TTL cache — otherwise the key would re-read the same
// snapshot it already had and visibly do nothing for up to a TTL.
func (g *Game) refreshBoardNow() { g.loadBoard(true) }

func (g *Game) loadBoard(force bool) {
	you := leaderboard.SaveRef{}
	if !g.id.IsGuest() {
		you = leaderboard.SaveRef{Fingerprint: g.id.Fingerprint, Slot: g.id.Slot}
	}
	get := g.board.Get
	if force {
		get = g.board.Refresh
	}
	board, err := get(context.Background(), you)
	g.lbErr = err
	if err == nil {
		g.lbBoard = board
	}
	g.lbNextRefresh = g.now + boardRefreshSeconds
	g.clampBoardScroll()
}

// boardAnimates reports whether the wave tick should run right now.
func (g *Game) boardAnimates() bool {
	return g.scr == scrBoard && !g.limitedColor && g.boardHasAnimatedName()
}

// boardHasAnimatedName reports whether a wave-styled farm is actually on
// screen — rows scrolled out of view don't need the 120 ms tick.
func (g *Game) boardHasAnimatedName() bool {
	found := false
	g.boardPlan().each(func(i int) {
		if sim.IsWaveNameStyle(g.lbBoard.Rows[i].NameStyle) {
			found = true
		}
	})
	return found
}

// startBoardAnimation starts the wave's tick chain, at most one at a time,
// and only while the Board is on screen: the 1 Hz tick calls this on every
// screen, and the cached board may still hold a wave row after you leave.
func (g *Game) startBoardAnimation() tea.Cmd {
	if g.boardAnimRunning || !g.boardAnimates() {
		return nil
	}
	g.boardAnimRunning = true
	return boardAnimCmd()
}

// boardLine is one rendered line of the board's list region, paired with its
// source Row (nil for the divider/status lines) so hitboxes can be attached
// without re-deriving the same line layout twice.
type boardLine struct {
	text string
	row  *leaderboard.Row
}

// boardYouIndex is your position in the ranked list, or -1 when unranked.
func (g *Game) boardYouIndex() int {
	if g.lbBoard.You == nil {
		return -1
	}
	return g.lbBoard.YouIndex
}

// boardPlan lays the full ranked list into the viewport (see planBoard).
func (g *Game) boardPlan() boardPlan {
	return planBoard(len(g.lbBoard.Rows), g.boardYouIndex(), g.lbScroll, g.boardVisibleRows())
}

// boardRankWidth is the width of the largest rank on the board, so "#1" and
// "#100" line up and the name column doesn't shift while scrolling.
func (g *Game) boardRankWidth() int {
	if n := len(g.lbBoard.Rows); n > 0 {
		return len(itoa(g.lbBoard.Rows[n-1].Rank))
	}
	return 0
}

// boardFrame renders only the lines the plan draws: the scrolled list and,
// when you are ranked but below it, a divider and your ±3 neighbours. Cost is
// the viewport, not the board, because this runs on every frame and tick.
func (g *Game) boardFrame(cw int) []boardLine {
	th := g.theme()
	if len(g.lbBoard.Rows) == 0 {
		return []boardLine{{text: th.Hint.Render("No farms on the board yet — be the first!")}}
	}
	plan := g.boardPlan()
	rankWidth := g.boardRankWidth()
	lines := make([]boardLine, 0, plan.lines())
	add := func(i int) {
		r := &g.lbBoard.Rows[i]
		lines = append(lines, boardLine{text: g.boardRowLine(*r, cw, rankWidth), row: r})
	}
	for i := plan.start; i < plan.end; i++ {
		add(i)
	}
	if plan.pinned {
		lines = append(lines, boardLine{text: th.Rule.Render(strings.Repeat("─", max(cw, 1)))})
		for i := plan.pinLo; i <= plan.pinHi; i++ {
			add(i)
		}
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
	seals := strings.Repeat("◆", clamp(r.ContractsCompleted, 0, 3))
	if seals != "" {
		seals += " "
	}
	suffix := ""
	if r.ShowSuffix {
		suffix = " (" + r.Suffix + ")"
	}
	rebirth := " ↻ " + money(r.Rebirths)
	youMarker := ""
	if r.IsYou {
		youMarker = "  ← YOU"
	}
	right := "◈ " + money(r.Coins)
	fixedWidth := lipgloss.Width(prefix) + lipgloss.Width(seals) + lipgloss.Width(suffix) + lipgloss.Width(rebirth) +
		lipgloss.Width(youMarker) + lipgloss.Width(right) + 1
	nameWidth := cw - fixedWidth
	if nameWidth < 1 {
		nameWidth = 1
	}
	name = truncate(sanitizeText(name), nameWidth)
	// Laid out unstyled first: alignSides may truncate, and truncate cuts by
	// rune, which would split an escape sequence.
	line := alignSides(prefix+seals+name+suffix+rebirth+youMarker, right, cw)

	rowStyle := th.Value
	switch {
	case r.IsYou:
		rowStyle = th.Selected
	case r.Rank == 1:
		rowStyle = th.BoardGold
	case r.Rank == 2:
		rowStyle = th.BoardSilver
	case r.Rank == 3:
		rowStyle = th.BoardBronze
	}
	// An earned name style colours the name alone. Its span has to close the
	// row's style and reopen it afterwards: a styled span nested inside one
	// Render ends in a reset, and everything after it (suffix, rebirths, the
	// YOU marker, the coins) would fall back to the canvas default.
	start := len(prefix) + len(seals)
	end := start + len(name)
	if r.NameStyle == sim.NameStyleTraditional || end > len(line) || line[start:end] != name {
		return rowStyle.Render(line)
	}
	return rowStyle.Render(line[:start]) + g.renderBoardName(name, r.NameStyle, rowStyle) + rowStyle.Render(line[end:])
}

// renderBoardName applies a contract-earned name style. The result is
// exactly as wide as name, so alignment and hitboxes never move between
// animation frames. Unknown styles fall back to the row's own treatment.
func (g *Game) renderBoardName(name, style string, rowStyle lipgloss.Style) string {
	th := g.theme()
	if s, ok := th.LeaderboardName(style); ok {
		return s.Render(name)
	}
	colour := sim.NameStyleBase(style)
	if _, ok := th.NameWave(colour, 0); !sim.IsWaveNameStyle(style) || !ok {
		return rowStyle.Render(name)
	}
	if g.limitedColor {
		// A 16-colour terminal would quantise the ramp into a flicker
		// between two unrelated colours; the wave's own static colour,
		// which every ramp ends on, reads better.
		if s, ok := th.LeaderboardName(colour); ok {
			return s.Render(name)
		}
		return rowStyle.Render(name)
	}
	var b strings.Builder
	for i, r := range []rune(name) {
		s, _ := th.NameWave(colour, i+g.boardAnimPhase)
		b.WriteString(s.Render(string(r)))
	}
	return b.String()
}

// boardHeaderLine is the one line that must never scroll away: the title
// plus "YOU: #rank/total" (or the unranked callout for a farm still below
// gameplay/02's coin floor).
func (g *Game) boardHeaderLine(cw int) string {
	th := g.theme()
	title := th.Section.Render("LEADERBOARD — RICHEST FARMS")
	var rank string
	switch {
	case g.id.IsGuest():
		rank = "Guests are not ranked"
	case g.lbBoard.You == nil:
		rank = "YOU: UNRANKED — EARN YOUR FIRST COIN"
	default:
		rank = "YOU: #" + itoa(g.lbBoard.You.Rank) + "/" + itoa(g.lbBoard.Total)
	}
	return alignSides(title, th.Value.Render(rank), cw)
}

// boardVisibleRows is how many list lines fit under the pinned header, above
// the scroll hint + staleness line.
func (g *Game) boardVisibleRows() int {
	return max(g.contentHeight()-11, 3)
}

// boardScrollable reports whether the board has more rows than fit on
// screen, i.e. whether the arrow keys have anything to do.
func (g *Game) boardScrollable() bool {
	return len(g.lbBoard.Rows) > g.boardVisibleRows()
}

func (g *Game) clampBoardScroll() {
	maxStart := max(len(g.lbBoard.Rows)-g.boardVisibleRows(), 0)
	g.lbScroll = min(max(g.lbScroll, 0), maxStart)
}

func (g *Game) handleNameKey(key string, msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "q":
		g.overlay = ovNone
	case "enter":
		snap, err := g.sess.RenameFarm(context.Background(), g.now, g.nameInput)
		if err != nil {
			switch {
			case errors.Is(err, game.ErrGuestRequiresKey):
				g.guestLocked("rename your farm")
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
	st := g.snap.State
	switch {
	case res.FertilizerUsed && res.Ripened > 0:
		g.addNotice("📦 Parcel opened — Fertilizer! Your sack was full, so it went straight on: " +
			itoa(res.Ripened) + " " + plural(res.Ripened, "crop", "crops") + " ripened.")
	case res.FertilizerUsed:
		g.addNotice("📦 Parcel opened — Fertilizer! Your sack was full and nothing was growing, so it soaked away.")
	case res.Fertilizer:
		g.addNotice("📦 Parcel opened — Fertilizer! " + fertilizerCount(st) + " (F to use)")
	case res.Starseeds > 0:
		g.addNotice("📦 Parcel opened! +" + money(res.Starseeds) + " " + g.starseedLabel() + "!")
	default:
		g.addNotice("📦 Parcel opened! +" + money(res.Coins) + " coins!")
	}
	return g, nil
}

// fertilizerGlyph marks fertilizer everywhere it is counted. It is the Greek
// koppa, the classic text lightning bolt: one cell wide like ✦ and ⛀,
// where ⚡ is an emoji that terminals draw two cells wide (and that already
// stands for random events in the notices).
const fertilizerGlyph = "ϟ"

// fertilizerCount renders the held/max count, e.g. "ϟ2/3".
func fertilizerCount(st *sim.State) string {
	return fertilizerGlyph + itoa(st.Fertilizer()) + "/" + itoa(st.FertilizerMax())
}

// requestFertilizer is the F key. It explains a use that cannot happen
// rather than open a prompt for it, and skips the prompt once the player has
// ticked "don't ask again".
func (g *Game) requestFertilizer() (tea.Model, tea.Cmd) {
	st := g.snap.State
	switch {
	case st.Fertilizer() < 1:
		msg := "No fertilizer left."
		if sim.FertilizerForSale(g.content) {
			msg += " The StarShop sells it for ✦ " + money(g.content.Fertilizer.CostStarseeds) + "."
		}
		g.addNotice(msg)
		return g, nil
	case st.GrowingPlots(g.content, g.now) == 0:
		g.addNotice("Nothing is growing — plant something before using fertilizer.")
		return g, nil
	}
	if st.FertilizerNoConfirm {
		g.useFertilizer()
		return g, nil
	}
	g.fertDontAsk = false
	g.overlay = ovFertilizerConfirm
	return g, nil
}

func (g *Game) handleFertilizerConfirmKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	// Only y confirms, like the rebirth and contract prompts: enter is the
	// farm's most-pressed key, and a harvest reflex must not spend a rare item.
	case "y", "Y":
		g.overlay = ovNone
		if g.fertDontAsk {
			if snap, err := g.sess.SetFertilizerConfirm(g.now, false); err == nil {
				g.snap = snap
			}
		}
		g.useFertilizer()
	case "d", "D", "space", " ":
		g.fertDontAsk = !g.fertDontAsk
	case "n", "N", "esc", "q":
		g.overlay = ovNone
	}
	return g, nil
}

func (g *Game) useFertilizer() {
	n, snap, ach, err := g.sess.UseFertilizer(g.now)
	g.applyAction(snap, ach, err)
	if err != nil {
		return
	}
	g.addNotice(fertilizerGlyph + " Fertilizer spread: " + itoa(n) + " " + plural(n, "crop", "crops") +
		" ripened. " + fertilizerCount(g.snap.State) + " left.")
}

func (g *Game) buyFertilizer() {
	snap, ach, err := g.sess.BuyFertilizer(g.now)
	if errors.Is(err, sim.ErrMaxed) {
		// The generic "already at maximum" says nothing about how to raise it.
		g.addNotice("Your fertilizer sack is full (" + fertilizerCount(g.snap.State) + "). Each contract you complete adds room for one more.")
		if snap.State != nil {
			g.snap = snap
		}
		return
	}
	g.applyAction(snap, ach, err)
	if err == nil {
		g.addNotice("Bought fertilizer. " + fertilizerCount(g.snap.State) + " — press F to use.")
	}
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
	scareCoins, scareN := g.collectAllScarecrows()
	switch {
	case count > 0 && scareN > 0:
		g.addNotice("Harvested " + itoa(count) + " " + plural(count, "plot", "plots") + " and emptied " +
			itoa(scareN) + " " + plural(scareN, "scarecrow", "scarecrows") + " (+" + money(total+scareCoins) + " coins).")
	case count > 0:
		g.addNotice("Harvested " + itoa(count) + " plots (+" + money(total) + " coins).")
	case scareN > 0:
		g.addNotice("Emptied " + itoa(scareN) + " " + plural(scareN, "scarecrow", "scarecrows") + " (+" + money(scareCoins) + " coins).")
	default:
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
			// The catch-up before a refused action can still cross a goal
			// (abandoning just as the last coins land, say).
			g.announceContract(snap.ContractCompleted)
		}
		return
	}
	g.snap = snap
	g.announceContract(snap.ContractCompleted)
	g.achievementNotices(ach)
}

// announceContract queues the reward for a contract the latest call
// completed. It is shown by showPendingReward rather than here: the handler
// that made the call often closes its own modal afterwards (rebirth's
// confirm, the only way Clockwork Denied can finish), and would close the
// reward along with it.
func (g *Game) announceContract(id sim.ContractID) {
	if id != "" {
		g.pendingReward = id
	}
}

// showPendingReward opens a queued contract reward once nothing else is on
// screen, so it follows the welcome-back summary or whatever modal the
// winning action came from. It runs after every Update.
func (g *Game) showPendingReward() {
	if g.pendingReward == "" || g.overlay != ovNone {
		return
	}
	g.completedContract = g.pendingReward
	g.pendingReward = ""
	g.overlay = ovContractReward
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
		g.addNotice("A " + sanitizeText(g.critterName(c)) + " visited an empty plot.")
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
	return sim.VisibleCropsAt(g.snap.State, g.content, g.now)
}

type marketItem struct {
	id, name, desc string
	cost           int64
	kind           string // multiplier, strain, zone
	owned          bool
	locked         bool
	gate           content.Unlock
	level, maxLvl  int
	lockReason     string
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
	if sc := g.content.Scarecrow; sc.Cost > 0 {
		blocked := !st.ScarecrowsAllowed()
		lo, hi := st.ScarecrowDropRange(g.content)
		it := marketItem{
			id: "scarecrow", name: "Scarecrow",
			desc: "drops " + money(lo) + "–" + money(hi) + "c every " + duration(sc.DropIntervalSec) +
				" online, " + money(sc.OfflineSlowdown) + "× slower away; collect on the farm",
			cost: st.ScarecrowCost(g.content), kind: "scarecrow",
			level: len(st.Scarecrows), maxLvl: int(sc.MaxCount),
			locked: blocked, lockReason: map[bool]string{true: "disabled by Clockwork Denied"}[blocked],
		}
		if need, gated := st.ScarecrowRebirthGate(g.content); gated && !blocked {
			it.locked, it.gate = true, content.Unlock{Kind: "prestige", Value: need}
		}
		items = append(items, it)
	}
	for _, z := range g.content.Zones {
		blocked := st.ActiveContract == sim.ContractLeanSeason && z.ID == "greenhouse"
		items = append(items, marketItem{
			id: z.ID, name: z.Name, desc: z.Description, cost: z.Cost, kind: "zone",
			owned: st.Zones[z.ID], locked: blocked || !st.Unlocked(z.Unlock), gate: z.Unlock,
			lockReason: map[bool]string{true: "disabled by Lean Season"}[blocked],
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
		// The description carries live numbers, so it is the part that gives
		// way on a narrow row; the price or status at the end never does.
		var prefix, tail string
		style := th.Value
		switch {
		case it.level >= it.maxLvl:
			prefix, tail, style = "✓ ", "  maxed", th.Ready
		case it.locked:
			reason := it.lockReason
			if reason == "" {
				reason = g.gateText(it.gate)
			}
			tail, style = "  🔒 "+reason, th.Locked
		case st.Coins < it.cost:
			tail, style = "  "+money(it.cost)+"c", th.Locked
		default:
			tail = "  " + money(it.cost) + "c"
		}
		head := prefix + sanitizeText(it.name) + " " + itoa(it.level) + "/" + itoa(it.maxLvl)
		room := g.contentWidth() - lipgloss.Width(marker) - lipgloss.Width(head) - lipgloss.Width(tail)
		desc := ""
		if room > 4 {
			desc = fitWidth(" — "+sanitizeText(it.desc), room)
		}
		return marker + style.Render(head+desc+tail)
	case "zone":
		line := sanitizeText(it.name) + " — " + money(it.cost) + "c · " + sanitizeText(it.desc)
		switch {
		case it.owned:
			return marker + th.Ready.Render("✓ "+line)
		case it.locked:
			// The gate reason is the first thing to go on a narrow terminal:
			// the item name matters more than why it is locked.
			reason := it.lockReason
			if reason == "" {
				reason = g.gateText(it.gate)
			}
			return marker + th.Locked.Render(fitWidth(line+"  🔒 "+reason,
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
	if st.ProgressionRebirths() < 1 && !st.ContractsAvailable(g.content) {
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
	lines = append(lines, starShopLine{text: "  Rebirths this run: " + th.Value.Render(money(st.ProgressionRebirths())), idx: noShopItem})
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
	if idx := fertilizerRowIdx(g.content); idx >= 0 {
		marker := "  "
		if g.progressIdx == idx {
			marker = th.Selected.Render("▸ ")
		}
		cost := g.content.Fertilizer.CostStarseeds
		line := fertilizerGlyph + " Fertilizer (" + itoa(st.Fertilizer()) + "/" + itoa(st.FertilizerMax()) +
			") — Ripens every growing crop at once (F)."
		var row string
		switch {
		case st.Fertilizer() >= st.FertilizerMax():
			row = marker + th.Ready.Render(alignSides("✓ "+line, "full", lineWidth))
		case st.PrestigeCurrency < cost:
			row = marker + th.Locked.Render(alignSides(line, "✦ "+money(cost), lineWidth))
		default:
			row = marker + th.Value.Render(alignSides(line, "✦ "+money(cost), lineWidth))
		}
		lines = append(lines, starShopLine{idx: noShopItem})
		lines = append(lines, starShopLine{text: th.Section.Render("Supplies"), idx: noShopItem})
		lines = append(lines, starShopLine{text: row, idx: idx})
	}
	if st.ContractsAvailable(g.content) {
		marker := "  "
		idx := contractsRowIdx(g.content)
		if g.progressIdx == idx {
			marker = th.Selected.Render("▸ ")
		}
		lines = append(lines, starShopLine{idx: noShopItem})
		lines = append(lines, starShopLine{
			text: marker + th.Ready.Render(alignSides("◆ Contracts — "+g.contractsSummary(), "open ›", lineWidth)),
			idx:  idx,
		})
	}
	return lines
}

// contractsSummary is the StarShop Contracts row's one-line status.
func (g *Game) contractsSummary() string {
	st := g.snap.State
	switch {
	case st.ActiveContract != "":
		return contractName(st.ActiveContract) + " " + g.contractProgress(st.ActiveContract)
	case st.ContractsCompleted >= len(sim.Contracts()):
		return "campaign complete"
	default:
		return itoa(st.ContractsCompleted) + "/" + itoa(len(sim.Contracts())) + " complete"
	}
}

func itoa(n int) string {
	return money(int64(n))
}

func contractName(id sim.ContractID) string {
	if contract, ok := sim.ContractByID(id); ok {
		return contract.Name
	}
	return "this contract"
}
