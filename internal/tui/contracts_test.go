package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mynameis-nigel/ssh-farm/internal/game"
	"github.com/mynameis-nigel/ssh-farm/internal/leaderboard"
	"github.com/mynameis-nigel/ssh-farm/internal/sim"
)

func contractGame(t *testing.T, completed int) *Game {
	t.Helper()
	f := newFixture(t)
	now := time.Now().Unix()
	seed := f.attach(t, now)
	seed.Session.Detach()
	st := sim.New(f.content, 1, now)
	st.ContractsUnlocked = true
	st.ContractsCompleted = completed
	payload, err := st.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.PersistSaveWithContracts(context.Background(), f.id.Fingerprint, f.id.Slot, payload, st.Version, now, st.Coins, st.LifetimeEarnings, st.Rebirths, st.FarmName, st.ContractsCompleted, st.LeaderboardNameStyle); err != nil {
		t.Fatal(err)
	}
	g := dismissIntro(t, f.newGame(t, now+1))
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
	for _, want := range []string{`Earn 100,000 coins`, `Auto-harvest and auto-sow are locked`, `First contract seal`, `lifetime earnings`} {
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
}
