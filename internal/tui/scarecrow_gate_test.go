package tui

import (
	"strings"
	"testing"

	"github.com/mynameis-nigel/ssh-farm/internal/sim"
)

// Scarecrow rebirth gates (gameplay/05, unlock_rebirths = [0, 0, 1, 2]): the
// first two are open to every run, the 3rd needs one progression rebirth and
// the 4th two.

func scarecrowMarketIdx(t *testing.T, g *Game) int {
	t.Helper()
	for i, it := range g.marketItems() {
		if it.kind == "scarecrow" {
			return i
		}
	}
	t.Fatal("the Market has no Scarecrow row")
	return -1
}

func hasNotice(g *Game, want string) bool {
	for _, n := range noticeTexts(g) {
		if strings.Contains(n, want) {
			return true
		}
	}
	return false
}

// Buys go through the real session, the way a player buys them. No test did
// this before, which is how a lock reason that was always set (so every buy
// said "disabled by Clockwork Denied", contract or not) went unnoticed.
func TestMarketBuysScarecrowsUpToTheRebirthGate(t *testing.T) {
	for _, tc := range []struct {
		rebirths int64
		canOwn   int
		refusal  string // notice for the first refused buy; "" at the cap
	}{
		{0, 2, "Scarecrow 3 needs 1 rebirth (you have 0)."},
		{1, 3, "Scarecrow 4 needs 2 rebirths (you have 1)."},
		{2, 4, ""},
	} {
		g, _ := savedFarm(t, func(st *sim.State) {
			st.Coins = 1_000_000
			st.Rebirths = tc.rebirths
		})
		g.scr = scrMarket
		g.marketIdx = scarecrowMarketIdx(t, g)
		for n := 1; n <= tc.canOwn; n++ {
			price, coins := g.snap.State.ScarecrowCost(g.content), g.snap.State.Coins
			g = press(t, g, "enter")
			st := g.snap.State
			if len(st.Scarecrows) != n || st.Coins != coins-price {
				t.Fatalf("%d rebirths, buy %d: owned %d, coins %d → %d (price %d); notices %q",
					tc.rebirths, n, len(st.Scarecrows), coins, st.Coins, price, noticeTexts(g))
			}
			if !hasNotice(g, "Scarecrow "+itoa(n)+" is standing guard") {
				t.Fatalf("%d rebirths, buy %d: no confirmation, notices %q", tc.rebirths, n, noticeTexts(g))
			}
		}
		if tc.refusal == "" {
			continue
		}
		coins := g.snap.State.Coins
		g = press(t, g, "enter")
		if len(g.snap.State.Scarecrows) != tc.canOwn || g.snap.State.Coins != coins {
			t.Fatalf("%d rebirths: the gated buy went through (owned %d)", tc.rebirths, len(g.snap.State.Scarecrows))
		}
		if !hasNotice(g, tc.refusal) {
			t.Fatalf("%d rebirths: notices %q, want %q", tc.rebirths, noticeTexts(g), tc.refusal)
		}
		for _, n := range noticeTexts(g) {
			if strings.Contains(n, "Clockwork") || strings.HasPrefix(n, "Hmm") {
				t.Fatalf("%d rebirths: misleading refusal %q", tc.rebirths, n)
			}
		}
	}
}

// Inside a contract the count is the contract's own, and the refusal says so:
// "you have 0" would be baffling to a player with nine lifetime rebirths.
func TestScarecrowGateNoticeInsideAContract(t *testing.T) {
	g := scarecrowFarm(t, canvasMaxWidth, canvasMaxHeight, 3, 0, 0)
	st := g.snap.State
	st.Coins, st.Rebirths, st.ActiveContract = 1_000_000, 9, sim.ContractBareHands
	g.scr = scrMarket
	g.marketIdx = scarecrowMarketIdx(t, g)
	g = press(t, g, "enter")
	if !hasNotice(g, "Scarecrow 3 needs 1 rebirth (this contract has 0).") {
		t.Fatalf("notices %q", noticeTexts(g))
	}
	if len(g.snap.State.Scarecrows) != 2 {
		t.Fatal("the gated buy went through")
	}
}

func TestMarketRefusesScarecrowsUnderClockworkDenied(t *testing.T) {
	g, now := contractSave(t, func(st *sim.State) {
		st.ContractsCompleted = 2
		st.ActiveContract = sim.ContractClockworkDenied
		st.Coins = 1_000_000
	})
	g, _ = tick(t, g, now+1)
	g.scr = scrMarket
	g.marketIdx = scarecrowMarketIdx(t, g)
	g = press(t, g, "enter")
	if len(g.snap.State.Scarecrows) != 0 || !hasNotice(g, "Scarecrow is disabled by Clockwork Denied.") {
		t.Fatalf("owned %d, notices %q", len(g.snap.State.Scarecrows), noticeTexts(g))
	}
}

func TestMarketRowShowsTheRebirthGate(t *testing.T) {
	g := scarecrowFarm(t, canvasMaxWidth, canvasMaxHeight, 3, 0, 0)
	g.snap.State.Coins = 1_000_000
	g.scr = scrMarket
	out := stripAnsi(view(g))
	if !strings.Contains(out, "Scarecrow 2/4") || !strings.Contains(out, "🔒 rebirth 1×") {
		t.Fatalf("the 3rd should show its rebirth gate:\n%s", out)
	}
	if strings.Contains(out, money(g.snap.State.ScarecrowCost(g.content))+"c") {
		t.Fatalf("a gated scarecrow should not quote a price it can't be bought at:\n%s", out)
	}

	g.snap.State.Rebirths = 1
	if out := stripAnsi(view(g)); strings.Contains(out, "🔒 rebirth") || !strings.Contains(out, money(g.snap.State.ScarecrowCost(g.content))+"c") {
		t.Fatalf("one rebirth opens the 3rd at its price:\n%s", out)
	}

	// The gate is the part of the row that never gives way.
	g.snap.State.Rebirths = 0
	g = resize(t, g, 80, 24)
	if out := stripAnsi(view(g)); !strings.Contains(out, "🔒 rebirth 1×") {
		t.Fatalf("gate cut off at 80x24:\n%s", out)
	}
	assertNoLineOverflows(t, g, "market 80x24")
}

// With the next one gated, the column's last card says how many more
// rebirths it takes instead of advertising a price.
func TestLockedScarecrowCardCountsTheRebirthsLeft(t *testing.T) {
	for _, tc := range []struct {
		name           string
		owned          int
		rebirths       int64
		contract       sim.ContractID
		contractRebs   int64
		line1, line2   string
		wantLockedCard bool
	}{
		{"two owned, no rebirths", 2, 0, "", 0, "1 more", "rebirth", true},
		{"three owned before the gates", 3, 0, "", 0, "2 more", "rebirths", true},
		{"three owned, one rebirth", 3, 1, "", 0, "1 more", "rebirth", true},
		{"contract counts its own", 2, 9, sim.ContractBareHands, 0, "1 more", "rebirth", true},
		{"one rebirth opens the 3rd", 2, 1, "", 0, "", "", false},
	} {
		pouches := make([]int64, tc.owned)
		g := scarecrowFarm(t, canvasMaxWidth, canvasMaxHeight, 8, pouches...)
		st := g.snap.State
		st.Rebirths, st.ActiveContract, st.ContractRebirths = tc.rebirths, tc.contract, tc.contractRebs
		out := stripAnsi(view(g))
		if !tc.wantLockedCard {
			if !strings.Contains(out, "for sale") || strings.Contains(out, "more") {
				t.Fatalf("%s: want the ordinary for-sale card:\n%s", tc.name, out)
			}
			continue
		}
		pad := func(s string) string { return "│ " + s + strings.Repeat(" ", scareCardText-len(s)) + " │" }
		if !strings.Contains(out, pad(tc.line1)) || !strings.Contains(out, pad(tc.line2)) {
			t.Fatalf("%s: want a card reading %q / %q:\n%s", tc.name, tc.line1, tc.line2, out)
		}
		if strings.Contains(out, "for sale") {
			t.Fatalf("%s: a gated scarecrow is not for sale:\n%s", tc.name, out)
		}
		if got := strings.Count(out, scareBot); got != tc.owned+1 {
			t.Fatalf("%s: %d scarecrow cards, want %d owned + the locked one", tc.name, got, tc.owned)
		}
		found := false
		for _, b := range g.hits.Boxes() {
			found = found || b.ID == "farm:scarecrow-shop"
		}
		if !found {
			t.Fatalf("%s: the locked card should still open the Market when clicked", tc.name)
		}
		assertNoLineOverflows(t, g, tc.name)
	}
}

func TestHelpNamesTheScarecrowRebirthGates(t *testing.T) {
	g := scarecrowFarm(t, canvasMaxWidth, canvasMaxHeight, 3)
	help := strings.Join(strings.Fields(stripAnsi(g.viewHelpGameplay())), " ")
	if !strings.Contains(help, "The 3rd needs 1 rebirth and the 4th needs 2 rebirths.") {
		t.Fatalf("gameplay help should spell out the gates:\n%s", help)
	}
}

// The Rebirth screen's "Next rebirth unlocks" list is where the game tells a
// player what rebirthing buys them; a Scarecrow gate belongs there with the
// crop tiers.
func TestRebirthScreenPreviewsTheNextScarecrow(t *testing.T) {
	for _, tc := range []struct {
		rebirths int64
		want     string
	}{
		{0, "Scarecrow 3"},
		{1, "Scarecrow 4"},
		{2, ""},
	} {
		g := scarecrowFarm(t, canvasMaxWidth, canvasMaxHeight, 3)
		g.snap.State.Rebirths = tc.rebirths
		g.scr = scrRebirth
		out := stripAnsi(view(g))
		_, next, _ := strings.Cut(out, "Next rebirth unlocks")
		if tc.want == "" {
			if strings.Contains(next, "Scarecrow") {
				t.Fatalf("%d rebirths: every scarecrow is already open:\n%s", tc.rebirths, next)
			}
			continue
		}
		if !strings.Contains(next, "⛀ "+tc.want+" in the Market") {
			t.Fatalf("%d rebirths: want %q previewed:\n%s", tc.rebirths, tc.want, next)
		}
	}
}
