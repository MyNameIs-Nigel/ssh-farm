package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/mynameis-nigel/ssh-farm/internal/game"
	"github.com/mynameis-nigel/ssh-farm/internal/leaderboard"
	"github.com/mynameis-nigel/ssh-farm/internal/sim"
	"github.com/mynameis-nigel/ssh-farm/internal/tui/hitbox"
)

// contractSave stores a contract-era save shaped by mutate and attaches a
// game to it at the save's own timestamp, so no catch-up has run yet: the
// caller decides what the first tick simulates.
func contractSave(t *testing.T, mutate func(st *sim.State)) (*Game, int64) {
	t.Helper()
	f := newFixture(t)
	now := time.Now().Unix()
	seed := f.attach(t, now)
	seed.Session.Detach()
	st := sim.New(f.content, 1, now)
	st.ContractsUnlocked = true
	mutate(st)
	payload, err := st.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.PersistSaveWithContracts(context.Background(), f.id.Fingerprint, f.id.Slot, payload, st.Version, now, st.Coins, st.LifetimeEarnings, st.Rebirths, st.FarmName, st.ContractsCompleted, st.LeaderboardNameStyle); err != nil {
		t.Fatal(err)
	}
	return dismissIntro(t, f.newGame(t, now)), now
}

func contractGame(t *testing.T, completed int) *Game {
	t.Helper()
	g, now := contractSave(t, func(st *sim.State) { st.ContractsCompleted = completed })
	g, _ = tick(t, g, now+1)
	return g
}

func TestContractsLiveBehindStarShopWithoutANinthNavTab(t *testing.T) {
	g := contractGame(t, 0)
	g = press(t, g, `5`, `c`)
	if g.scr != scrContracts {
		t.Fatalf(`screen = %v, want contracts`, g.scr)
	}
	out := view(g)
	for _, want := range []string{`CONTRACTS`, `Bare Hands`, `Lean Season`, `Clockwork Denied`} {
		if !strings.Contains(out, want) {
			t.Fatalf(`contract screen missing %q:\n%s`, want, out)
		}
	}
	if strings.Contains(stripAnsi(g.viewNav()), `Contracts`) {
		t.Fatal(`contracts must not become a ninth navigation tab`)
	}
}

func TestContractConfirmationExplainsTermsAndStartsAttempt(t *testing.T) {
	g := contractGame(t, 0)
	g = press(t, g, `5`, `c`, `enter`)
	if g.overlay != ovContractConfirm {
		t.Fatalf(`overlay = %v, want contract confirmation`, g.overlay)
	}
	out := view(g)
	for _, want := range []string{`Earn 100,000 coins`, `Auto-harvest and auto-sow are locked`, `First contract seal`, `KEPT FOREVER`, `cannot be restored`} {
		if !strings.Contains(out, want) {
			t.Fatalf(`confirmation missing %q:\n%s`, want, out)
		}
	}
	g = press(t, g, `y`)
	if g.snap.State.ActiveContract != sim.ContractBareHands || g.overlay != ovNone {
		t.Fatalf(`contract start active=%q overlay=%v`, g.snap.State.ActiveContract, g.overlay)
	}
}

func TestBoardRendersSealsConditionalSuffixAndAnimatedName(t *testing.T) {
	g := &Game{width: 100, height: 35, snap: game.Snapshot{State: &sim.State{}}, boardAnimPhase: 0}
	r := leaderboard.Row{Rank: 1, DisplayName: `VIOLET FARM`, Suffix: `abcde`, ShowSuffix: false, ContractsCompleted: 3, NameStyle: sim.NameStylePurpleWave, Coins: 10, Rebirths: 2}
	first := g.boardRowLine(r, 80, 1)
	plain := stripAnsi(first)
	if !strings.Contains(plain, `◆◆◆ VIOLET FARM`) || strings.Contains(plain, `abcde`) {
		t.Fatalf(`unexpected board row: %q`, plain)
	}
	g.boardAnimPhase = 3
	second := g.boardRowLine(r, 80, 1)
	if first == second {
		t.Fatal(`purple wave should change as the animation phase advances`)
	}
	r.ShowSuffix = true
	if got := stripAnsi(g.boardRowLine(r, 80, 1)); !strings.Contains(got, `(abcde)`) {
		t.Fatalf(`duplicate suffix format missing: %q`, got)
	}
}

func TestStatsCyclesOnlyUnlockedLeaderboardStyles(t *testing.T) {
	g := contractGame(t, 2)
	g.scr = scrStats
	g = press(t, g, `l`)
	if g.snap.State.LeaderboardNameStyle != sim.NameStyleLeaf {
		t.Fatalf(`style = %q, want leaf`, g.snap.State.LeaderboardNameStyle)
	}
}

func TestContractRestrictionsAreExplainedAtBlockedControls(t *testing.T) {
	g := contractGame(t, 0)
	g.snap.State.ActiveContract = sim.ContractBareHands
	if got := stripAnsi(g.viewUpgrade()); !strings.Contains(got, "Disabled by Bare Hands") {
		t.Fatalf("automation overlay did not explain contract lock: %s", got)
	}
	g.snap.State.ActiveContract = sim.ContractLeanSeason
	g.snap.State.Plots = make([]sim.Plot, 6)
	if got := stripAnsi(g.viewLand()); !strings.Contains(got, "caps this farm at six plots") {
		t.Fatalf("land screen did not explain contract cap: %s", got)
	}
	foundGreenhouse := false
	for _, item := range g.marketItems() {
		if item.id == "greenhouse" {
			foundGreenhouse = strings.Contains(item.lockReason, "Lean Season") && item.locked
		}
	}
	if !foundGreenhouse {
		t.Fatal("greenhouse was not visibly locked by Lean Season")
	}
}

func TestBoardAnimationStartsOnlyOneTickChain(t *testing.T) {
	g := &Game{scr: scrBoard, snap: game.Snapshot{State: &sim.State{}}, lbBoard: leaderboard.Board{Top: []leaderboard.Row{{NameStyle: sim.NameStylePurpleWave}}}}
	if cmd := g.startBoardAnimation(); cmd == nil {
		t.Fatal("first visible animated row should start a tick chain")
	}
	if cmd := g.startBoardAnimation(); cmd != nil {
		t.Fatal("a running board animation must not start a second tick chain")
	}
	g.scr = scrFarm
	m, cmd := g.Update(boardAnimMsg(time.Now()))
	g = m.(*Game)
	if cmd != nil || g.boardAnimRunning {
		t.Fatal("animation chain should stop when Board is no longer visible")
	}
	// The 1 Hz tick asks on every screen; off the Board the answer is no.
	if cmd := g.startBoardAnimation(); cmd != nil || g.boardAnimRunning {
		t.Fatal("a cached wave row must not start ticks while the Board is hidden")
	}
}

func TestLimitedColourHoldsTheWaveStill(t *testing.T) {
	g := &Game{scr: scrBoard, snap: game.Snapshot{State: &sim.State{}}, lbBoard: leaderboard.Board{Top: []leaderboard.Row{{NameStyle: sim.NameStylePurpleWave}}}}
	m, _ := g.Update(tea.ColorProfileMsg{Profile: colorprofile.ANSI})
	g = m.(*Game)
	if cmd := g.startBoardAnimation(); cmd != nil {
		t.Fatal("a 16-colour terminal should not run the wave's tick chain")
	}
	first := g.renderBoardName("VIOLET", sim.NameStylePurpleWave, g.theme().Value)
	g.boardAnimPhase = 5
	if again := g.renderBoardName("VIOLET", sim.NameStylePurpleWave, g.theme().Value); again != first {
		t.Fatal("the static fallback changed between phases")
	}
}

// The terms modal is the only warning before an irreversible reset, so all
// of it must be on screen at the stock 80×24 — with a notice and an event
// bar each taking a row, the tightest the body gets.
func TestContractConfirmFitsStockTerminal(t *testing.T) {
	for completed, contract := range sim.Contracts() {
		for _, abandon := range []bool{false, true} {
			g := resize(t, contractGame(t, completed), 80, 24)
			g.snap.State.EventID = "market_day"
			g.snap.State.EventStartedAt = g.now
			g.snap.State.EventEndsAt = g.now + 120
			g.addNotice("A notice takes a footer row.")
			g.contractID, g.contractAbandon, g.overlay = contract.ID, abandon, ovContractConfirm

			label := contract.Name + map[bool]string{true: " abandon", false: " accept"}[abandon]
			assertFits(t, label, g.overlayBox(), g.contentWidth()-4)
			out := stripAnsi(view(g))
			wants := []string{contractAcceptLabel(abandon), contractCancel, `cannot be restored`, `RESET`, `KEPT FOREVER`}
			if !abandon {
				wants = append(wants, contract.Premise, contract.Goal, contract.Reward)
				wants = append(wants, contract.Restrictions...)
			}
			for _, want := range wants {
				if !strings.Contains(out, want) {
					t.Errorf("%s: modal lost %q at 80×24:\n%s", label, want, out)
				}
			}
		}
	}
}

// The accept button is a double-click like rebirth's, and both buttons have
// to sit exactly where they are drawn at any size.
func TestContractConfirmButtonsLandOnTheirLabels(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {canvasMaxWidth, canvasMaxHeight}} {
		g := resize(t, contractGame(t, 0), size[0], size[1])
		g = press(t, g, `5`, `c`, `enter`)
		lines := strings.Split(stripAnsi(view(g)), "\n")
		boxes := map[string]hitbox.Box{}
		for _, b := range g.hits.Boxes() {
			boxes[b.ID] = b
		}
		for id, label := range map[string]string{"contract:accept": contractAcceptLabel(false), "contract:cancel": contractCancel} {
			b, ok := boxes[id]
			if !ok {
				t.Fatalf("%v: no %s hitbox", size, id)
			}
			if got := string([]rune(lines[b.Y])[b.X:]); !strings.HasPrefix(got, label) {
				t.Fatalf("%v: %s hitbox at (%d,%d) covers %q, want %q", size, id, b.X, b.Y, got, label)
			}
		}

		click := func(id string) {
			b := boxes[id]
			m, _ := g.handleMouseClick(tea.Mouse(tea.MouseClickMsg{X: b.X, Y: b.Y, Button: tea.MouseLeft}))
			g = m.(*Game)
		}
		click("contract:accept")
		if g.snap.State.ActiveContract != "" || g.overlay != ovContractConfirm {
			t.Fatalf("%v: a single click must not accept (active=%q overlay=%v)", size, g.snap.State.ActiveContract, g.overlay)
		}
		click("contract:accept")
		if g.snap.State.ActiveContract != sim.ContractBareHands {
			t.Fatalf("%v: double-click did not accept", size)
		}
	}
}

func TestBoardRowKeepsItsStyleAfterAnEarnedName(t *testing.T) {
	g := &Game{width: 100, height: 35, snap: game.Snapshot{State: &sim.State{}}}
	you := g.theme().Selected.Render("X")
	youOpen := you[:strings.Index(you, "X")]
	for _, style := range []string{sim.NameStyleLeaf, sim.NameStylePurpleWave} {
		r := leaderboard.Row{Rank: 5, DisplayName: `LEAF`, Suffix: `abcde`, ShowSuffix: true, ContractsCompleted: 2, NameStyle: style, Coins: 10, Rebirths: 2, IsYou: true}
		out := g.boardRowLine(r, 60, 1)
		i := strings.Index(out, "(abcde)")
		last := strings.LastIndex(out[:i], "\x1b[")
		if i < 0 || !strings.HasPrefix(out[last:], youOpen) {
			t.Fatalf("%s: text after the name lost the YOU row style: %q", style, out)
		}
		if got := stripAnsi(out); lipgloss.Width(got) != 60 || !strings.Contains(got, "◆◆ LEAF (abcde) ↻ 2  ← YOU") {
			t.Fatalf("%s: row layout changed: %q", style, got)
		}
	}
}

func TestTickThatCompletesAContractOpensTheReward(t *testing.T) {
	g, now := contractSave(t, func(st *sim.State) {
		st.ActiveContract = sim.ContractBareHands
		st.ContractEarnings = 99_999
		st.Scarecrow = true
		st.Plots[0] = sim.Plot{Critter: "crow"}
	})
	g, _ = tick(t, g, now+1) // the scarecrow's bounty crosses the goal
	if g.overlay != ovContractReward || g.completedContract != sim.ContractBareHands {
		t.Fatalf("overlay = %v, completed = %q; want the Bare Hands reward", g.overlay, g.completedContract)
	}
	if !strings.Contains(stripAnsi(view(g)), "CONTRACT COMPLETE") {
		t.Fatal("reward modal not rendered")
	}

	// Seals already earned are not news: connecting must not re-announce them.
	if g := contractGame(t, 2); g.overlay != ovNone {
		t.Fatalf("connecting with two seals opened overlay %v", g.overlay)
	}
}

func TestOfflineCompletionFollowsTheWelcomeBack(t *testing.T) {
	f := newFixture(t)
	now := time.Now().Unix()
	res := f.attach(t, now)
	t.Cleanup(res.Session.Detach)
	res.Created = false
	res.Away = sim.Events{Elapsed: 3600, ContractCompleted: sim.ContractBareHands}
	g := NewGame(f.id, res, f.content, f.board, 100, 35, now, 0)
	if g.overlay != ovAway {
		t.Fatalf("overlay = %v, want the welcome-back summary first", g.overlay)
	}
	g = press(t, g, `enter`)
	if g.overlay != ovContractReward || g.completedContract != sim.ContractBareHands {
		t.Fatalf("after the summary: overlay = %v, completed = %q", g.overlay, g.completedContract)
	}
	g = press(t, g, `enter`)
	if g.overlay != ovNone {
		t.Fatalf("reward did not close: %v", g.overlay)
	}
}

func TestStarShopContractsRowOpensContractsByKeyAndMouse(t *testing.T) {
	g := contractGame(t, 0)
	g = press(t, g, `5`)
	for range g.content.Upgrades {
		g = press(t, g, `down`)
	}
	if g.progressIdx != contractsRowIdx(g.content) {
		t.Fatalf("cursor stopped at %d, want the Contracts row %d", g.progressIdx, contractsRowIdx(g.content))
	}
	if g = press(t, g, `down`); g.progressIdx != contractsRowIdx(g.content) {
		t.Fatal("cursor ran past the Contracts row")
	}
	if !strings.Contains(stripAnsi(view(g)), "◆ Contracts — 0/3 complete") {
		t.Fatalf("StarShop has no Contracts row:\n%s", stripAnsi(view(g)))
	}
	if g = press(t, g, `enter`); g.scr != scrContracts {
		t.Fatalf("enter on the Contracts row: screen = %v", g.scr)
	}

	g = press(t, g, `esc`)
	view(g)
	id := "shop:" + itoa(contractsRowIdx(g.content))
	for _, b := range g.hits.Boxes() {
		if b.ID == id {
			for range 2 {
				m, _ := g.handleMouseClick(tea.Mouse(tea.MouseClickMsg{X: b.X, Y: b.Y, Button: tea.MouseLeft}))
				g = m.(*Game)
			}
		}
	}
	if g.scr != scrContracts {
		t.Fatalf("double-clicking %s: screen = %v", id, g.scr)
	}
}

// The intro hint wraps on a narrow terminal; the rows' hitboxes must follow.
func TestContractRowHitboxesFollowTheWrappedIntro(t *testing.T) {
	for _, width := range []int{60, 80, canvasMaxWidth} {
		g := resize(t, contractGame(t, 0), width, 30)
		g = press(t, g, `5`, `c`)
		lines := strings.Split(stripAnsi(view(g)), "\n")
		for _, b := range g.hits.Boxes() {
			if idx, ok := b.Data.(int); ok && strings.HasPrefix(b.ID, "contract:") {
				if name := sim.Contracts()[idx].Name; !strings.Contains(lines[b.Y], name) {
					t.Errorf("width %d: %s hitbox row reads %q, want %q", width, b.ID, lines[b.Y], name)
				}
			}
		}
	}
}

func TestStatsShowsContractsOnlyOnceTheyMatter(t *testing.T) {
	f := newFixture(t)
	fresh := dismissIntro(t, f.newGame(t, time.Now().Unix()))
	fresh.scr = scrStats
	if strings.Contains(stripAnsi(view(fresh)), "Contracts:") {
		t.Fatal("a new farm should not see the contract campaign in Stats")
	}
	for completed, wantHint := range map[int]bool{0: false, 2: true} {
		g := contractGame(t, completed)
		g.scr = scrStats
		out := stripAnsi(view(g))
		if !strings.Contains(out, "Contracts: "+itoa(completed)+"/3") {
			t.Fatalf("completed=%d: Stats lacks the contract line:\n%s", completed, out)
		}
		if got := strings.Contains(out, "(l to change)"); got != wantHint {
			t.Fatalf("completed=%d: style hint shown = %v, want %v", completed, got, wantHint)
		}
	}
}

func TestEveryStaticNameStyleHasAThemeColour(t *testing.T) {
	th := (&Game{snap: game.Snapshot{State: &sim.State{}}}).theme()
	for _, style := range (&sim.State{ContractsCompleted: 3}).AvailableLeaderboardNameStyles() {
		if style == sim.NameStyleTraditional || style == sim.NameStylePurpleWave {
			continue
		}
		if _, ok := th.LeaderboardName(style); !ok {
			t.Errorf("style %q has no theme colour and would render as traditional", style)
		}
	}
}

// Clockwork Denied can only finish on a rebirth, and a rebirth is taken
// from its confirm modal: the handler closing that modal must not also
// close the reward the rebirth just opened.
func TestFinalClockworkRebirthShowsTheReward(t *testing.T) {
	g, now := contractSave(t, func(st *sim.State) {
		st.ContractsCompleted = 2
		st.ActiveContract = sim.ContractClockworkDenied
		st.ContractRebirths = 4
		st.RunEarnings = 1_000_000
	})
	g, _ = tick(t, g, now+1)
	if !g.snap.State.CanRebirth(g.content) {
		t.Fatal("fixture cannot rebirth")
	}
	g = press(t, g, `4`, `R`, `y`)
	if g.snap.State.ContractsCompleted != 3 {
		t.Fatalf("completed = %d, want the third contract recorded", g.snap.State.ContractsCompleted)
	}
	if g.overlay != ovContractReward || g.completedContract != sim.ContractClockworkDenied {
		t.Fatalf("overlay = %v, completed = %q; want the Clockwork Denied reward", g.overlay, g.completedContract)
	}
}

// Pressing y to abandon just as the goal lands: the catch-up before the
// abandon completes the contract, so there is nothing left to abandon.
// The player should see the reward, not a refusal.
func TestAbandonThatLosesTheRaceToTheGoalShowsTheReward(t *testing.T) {
	g, now := contractSave(t, func(st *sim.State) {
		st.ActiveContract = sim.ContractBareHands
		st.ContractEarnings = 99_999
		st.Scarecrow = true
		st.Plots[0] = sim.Plot{Critter: "crow"}
	})
	g, _ = tick(t, g, now) // load the save; no time passes, so nothing is shooed yet
	g.scr = scrContracts
	g = press(t, g, `a`)
	if g.overlay != ovContractConfirm || !g.contractAbandon {
		t.Fatalf("abandon prompt not open: overlay = %v", g.overlay)
	}
	g.now = now + 1 // the scarecrow's bounty lands in the abandon's catch-up
	g = press(t, g, `y`)
	if g.overlay != ovContractReward || g.completedContract != sim.ContractBareHands {
		t.Fatalf("overlay = %v, completed = %q; want the Bare Hands reward", g.overlay, g.completedContract)
	}
	for _, n := range g.notices {
		if strings.Contains(n.text, "Hmm") || strings.Contains(n.text, "abandoned") {
			t.Fatalf("a won contract should not read as a failed or completed abandon: %q", n.text)
		}
	}
}
