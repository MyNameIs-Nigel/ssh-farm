package sim

import (
	"bytes"
	"errors"
	"testing"

	"github.com/mynameis-nigel/ssh-farm/internal/content"
)

// scarecrowState returns a fresh test save holding n scarecrows bought for
// free, so each test starts from a known pouch/clock of zero.
func scarecrowState(t *testing.T, n int) *State {
	t.Helper()
	c := testContent(t)
	s := newTestState(t, c)
	s.Scarecrows = make([]Scarecrow, n)
	return s
}

// tickOnline advances s in one-second steps, the way a connected session does.
func tickOnline(s *State, c *content.Content, seconds int64) {
	for i := int64(0); i < seconds; i++ {
		Advance(s, c, s.UpdatedAt+1)
	}
}

func TestBuyScarecrowEscalatesAndCapsAtMax(t *testing.T) {
	c := testContent(t) // cost 100, growth 300%, max 4
	s := newTestState(t, c)
	s.Coins = 1_000_000
	s.Rebirths = 2 // past every unlock_rebirths gate, so only price and cap apply

	want := []int64{100, 300, 900, 2700}
	for i, w := range want {
		if got := s.ScarecrowCost(c); got != w {
			t.Fatalf("price of scarecrow %d = %d, want %d", i+1, got, w)
		}
		before := s.Coins
		cost, err := BuyScarecrow(s, c)
		if err != nil || cost != w {
			t.Fatalf("buy %d: cost %d err %v, want %d", i+1, cost, err, w)
		}
		if s.Coins != before-w || len(s.Scarecrows) != i+1 {
			t.Fatalf("buy %d: coins %d (want %d), owned %d", i+1, s.Coins, before-w, len(s.Scarecrows))
		}
	}
	if got := s.ScarecrowCost(c); got != -1 {
		t.Fatalf("price past the cap = %d, want -1", got)
	}
	before := s.Coins
	if _, err := BuyScarecrow(s, c); !errors.Is(err, ErrMaxed) {
		t.Fatalf("fifth buy = %v, want ErrMaxed", err)
	}
	if s.Coins != before || len(s.Scarecrows) != 4 {
		t.Fatal("a refused buy must not charge or add a scarecrow")
	}
}

func TestBuyScarecrowCannotAffordChangesNothing(t *testing.T) {
	c := testContent(t)
	s := newTestState(t, c)
	s.Coins = 99
	if _, err := BuyScarecrow(s, c); !errors.Is(err, ErrCantAfford) {
		t.Fatalf("buy at 99 coins = %v, want ErrCantAfford", err)
	}
	if s.Coins != 99 || len(s.Scarecrows) != 0 {
		t.Fatal("unaffordable buy mutated the save")
	}
}

// unlock_rebirths = [0, 0, 1, 2]: the first two are open to every run, the
// 3rd needs one progression rebirth and the 4th two. A refused buy charges
// nothing and adds nothing.
func TestBuyScarecrowRebirthGates(t *testing.T) {
	c := testContent(t)
	for _, tc := range []struct {
		rebirths int64
		canOwn   int
	}{{0, 2}, {1, 3}, {2, 4}, {7, 4}} {
		s := newTestState(t, c)
		s.Rebirths = tc.rebirths
		s.Coins = 1_000_000
		for len(s.Scarecrows) < tc.canOwn {
			if _, err := BuyScarecrow(s, c); err != nil {
				t.Fatalf("%d rebirths: buying scarecrow %d failed: %v", tc.rebirths, len(s.Scarecrows)+1, err)
			}
		}
		if tc.canOwn == int(c.Scarecrow.MaxCount) {
			continue
		}
		need, locked := s.ScarecrowRebirthGate(c)
		if !locked || need != tc.rebirths+1 {
			t.Fatalf("%d rebirths owning %d: gate = (%d, %v), want (%d, true)", tc.rebirths, tc.canOwn, need, locked, tc.rebirths+1)
		}
		coins := s.Coins
		if _, err := BuyScarecrow(s, c); !errors.Is(err, ErrLocked) {
			t.Fatalf("%d rebirths: scarecrow %d = %v, want ErrLocked", tc.rebirths, tc.canOwn+1, err)
		}
		if s.Coins != coins || len(s.Scarecrows) != tc.canOwn {
			t.Fatalf("%d rebirths: a locked buy charged %d coins or added a scarecrow (owned %d)", tc.rebirths, coins-s.Coins, len(s.Scarecrows))
		}
	}
}

// The lock is the first thing the player needs to hear about: telling a run
// that cannot buy the 3rd at any price to save up would be misleading.
func TestScarecrowRebirthGateOutranksThePrice(t *testing.T) {
	c := testContent(t)
	s := scarecrowState(t, 2)
	s.Coins = 0
	if _, err := BuyScarecrow(s, c); !errors.Is(err, ErrLocked) {
		t.Fatalf("broke and short a rebirth: %v, want ErrLocked", err)
	}
}

// Contracts restart progression at zero, so their gates count the contract's
// own rebirths, as every other gameplay gate does (ProgressionRebirths).
func TestScarecrowRebirthGatesCountContractRebirths(t *testing.T) {
	c := testContent(t)
	s := scarecrowState(t, 2)
	s.Coins = 1_000_000
	s.Rebirths = 9
	s.ActiveContract = ContractBareHands
	if _, err := BuyScarecrow(s, c); !errors.Is(err, ErrLocked) {
		t.Fatalf("9 lifetime but 0 contract rebirths: %v, want ErrLocked", err)
	}
	s.ContractRebirths = 1
	if _, err := BuyScarecrow(s, c); err != nil {
		t.Fatalf("1 contract rebirth should open the 3rd: %v", err)
	}
}

// Scarecrows bought before the gates existed are the player's: the gate only
// ever refuses a purchase, it never takes one back, and they keep dropping.
func TestScarecrowsBoughtBeforeTheGatesKeepWorking(t *testing.T) {
	c := testContent(t)
	s := scarecrowState(t, 4) // four owned at zero rebirths
	payload, err := s.Encode()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := DecodeState(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Scarecrows) != 4 {
		t.Fatalf("reloaded save holds %d scarecrows, want 4", len(loaded.Scarecrows))
	}
	tickOnline(loaded, c, c.Scarecrow.DropIntervalSec)
	for i, sc := range loaded.Scarecrows {
		if sc.Pouch <= 0 {
			t.Fatalf("scarecrow %d stopped dropping: pouch %d", i+1, sc.Pouch)
		}
	}
	if need, locked := loaded.ScarecrowRebirthGate(c); need != 0 || locked {
		t.Fatalf("a full column has nothing left to gate, got (%d, %v)", need, locked)
	}

	three := scarecrowState(t, 3)
	if need, locked := three.ScarecrowRebirthGate(c); need != 2 || !locked {
		t.Fatalf("three owned at zero rebirths: gate = (%d, %v), want the 4th's (2, true)", need, locked)
	}
}

func TestScarecrowRebirthGateIsEmptyWhenDisabled(t *testing.T) {
	c := testContent(t)
	c.Scarecrow.Cost = 0
	s := scarecrowState(t, 2)
	if need, locked := s.ScarecrowRebirthGate(c); need != 0 || locked {
		t.Fatalf("disabled item: gate = (%d, %v), want none", need, locked)
	}
}

func TestBuyScarecrowDisabledWhenUnconfigured(t *testing.T) {
	c := testContent(t)
	c.Scarecrow.Cost = 0
	s := newTestState(t, c)
	s.Coins = 1_000_000
	if _, err := BuyScarecrow(s, c); !errors.Is(err, ErrUnknownItem) {
		t.Fatalf("buy with cost 0 = %v, want ErrUnknownItem", err)
	}
}

// The heart of the feature: drops land every drop_interval_sec of online play,
// go into the pouch, and are never credited until collected.
func TestScarecrowOnlineDropsEveryIntervalIntoThePouch(t *testing.T) {
	c := testContent(t) // 15s interval, 2–8 coins
	s := scarecrowState(t, 1)
	coins, earned := s.Coins, s.RunEarnings

	tickOnline(s, c, 14)
	if s.Scarecrows[0].Pouch != 0 {
		t.Fatalf("pouch after 14s = %d, want 0 (no drop before 15s)", s.Scarecrows[0].Pouch)
	}
	tickOnline(s, c, 1)
	if p := s.Scarecrows[0].Pouch; p < 2 || p > 8 {
		t.Fatalf("pouch after one drop = %d, want 2–8", p)
	}
	tickOnline(s, c, 15*9)
	if p := s.Scarecrows[0].Pouch; p < 2*10 || p > 8*10 {
		t.Fatalf("pouch after ten drops = %d, want 20–80", p)
	}
	if s.Coins != coins || s.RunEarnings != earned {
		t.Fatalf("drops were auto-collected: coins %d→%d, run earnings %d→%d", coins, s.Coins, earned, s.RunEarnings)
	}
}

// Ten drops of a uniform 2–8 roll that all came out identical would mean the
// roll isn't random at all.
func TestScarecrowDropsAreRandom(t *testing.T) {
	c := testContent(t)
	s := scarecrowState(t, 1)
	seen := map[int64]bool{}
	for i := 0; i < 20; i++ {
		before := s.Scarecrows[0].Pouch
		tickOnline(s, c, 15)
		seen[s.Scarecrows[0].Pouch-before] = true
	}
	if len(seen) < 3 {
		t.Fatalf("20 drops produced only %d distinct amounts: %v", len(seen), seen)
	}
}

func TestEachScarecrowRollsItsOwnDrop(t *testing.T) {
	c := testContent(t)
	s := scarecrowState(t, 4)
	tickOnline(s, c, 15*20)
	first := s.Scarecrows[0].Pouch
	same := true
	for _, sc := range s.Scarecrows[1:] {
		if sc.Pouch != first {
			same = false
		}
	}
	if same {
		t.Fatalf("four scarecrows filled identical pouches (%d) over 20 drops; rolls are shared", first)
	}
}

// Offline the same clock runs offline_slowdown times slower: an hour away is
// 12 drops, not 240. The bounds are disjoint, so the comparison is exact.
func TestScarecrowOfflineIsMuchSlower(t *testing.T) {
	c := testContent(t)
	off := scarecrowState(t, 1)
	ev := Advance(off, c, off.UpdatedAt+3600)
	if p := off.Scarecrows[0].Pouch; p < 2*12 || p > 8*12 {
		t.Fatalf("offline hour pouch = %d, want 12 drops (24–96)", p)
	}
	if ev.ScarecrowCoins != off.Scarecrows[0].Pouch {
		t.Fatalf("event reports %d, pouch holds %d", ev.ScarecrowCoins, off.Scarecrows[0].Pouch)
	}

	on := scarecrowState(t, 1)
	tickOnline(on, c, 3600)
	if p := on.Scarecrows[0].Pouch; p < 2*240 {
		t.Fatalf("online hour pouch = %d, want 240 drops (>= 480)", p)
	}
}

// A partial offline interval is carried, not discarded: two 150s absences
// add up to one 300s offline drop.
func TestScarecrowOfflineRemainderCarries(t *testing.T) {
	c := testContent(t)
	s := scarecrowState(t, 1)
	Advance(s, c, s.UpdatedAt+150)
	if s.Scarecrows[0].Pouch != 0 {
		t.Fatal("150s offline is half a drop")
	}
	Advance(s, c, s.UpdatedAt+150)
	if s.Scarecrows[0].Pouch == 0 {
		t.Fatal("the second 150s should complete the offline drop")
	}
}

// More scarecrows pay more *each*, and rebirths raise them further.
func TestScarecrowSynergyAndRebirthScaling(t *testing.T) {
	c := testContent(t)
	c.Scarecrow.CoinsMin, c.Scarecrow.CoinsMax = 4, 4 // deterministic

	for _, tc := range []struct {
		owned    int
		rebirths int64
		perDrop  int64
	}{
		{1, 0, 4},  // ×1.00
		{2, 0, 5},  // ×1.25
		{4, 0, 7},  // ×1.75
		{1, 5, 6},  // ×1.50 from five rebirths
		{4, 5, 10}, // 4 × 1.75 × 1.5 = 10.5, floored
	} {
		s := scarecrowState(t, tc.owned)
		s.Rebirths = tc.rebirths
		lo, hi := s.ScarecrowDropRange(c)
		if lo != tc.perDrop || hi != tc.perDrop {
			t.Errorf("%d owned, %d rebirths: range %d–%d, want %d", tc.owned, tc.rebirths, lo, hi, tc.perDrop)
		}
		tickOnline(s, c, 15)
		for i, sc := range s.Scarecrows {
			if sc.Pouch != tc.perDrop {
				t.Errorf("%d owned, %d rebirths: scarecrow %d pouch %d, want %d", tc.owned, tc.rebirths, i, sc.Pouch, tc.perDrop)
			}
		}
	}
}

// Contract runs count their own rebirths, like every other gate.
func TestScarecrowRebirthScalingUsesProgressionRebirths(t *testing.T) {
	c := testContent(t)
	c.Scarecrow.CoinsMin, c.Scarecrow.CoinsMax = 4, 4
	s := scarecrowState(t, 1)
	s.Rebirths = 50
	s.ActiveContract = ContractBareHands
	s.ContractRebirths = 0
	if lo, _ := s.ScarecrowDropRange(c); lo != 4 {
		t.Fatalf("contract run with 0 contract rebirths drops %d, want the unboosted 4", lo)
	}
}

func TestScarecrowsNoLongerShooCritters(t *testing.T) {
	c := testContent(t)
	c.Critters.VisitChancePct = 100
	s := scarecrowState(t, 4)
	coins := s.Coins
	ev := Advance(s, c, s.UpdatedAt+1)
	if len(ev.CritterVisits) == 0 {
		t.Fatal("critters should still visit empty plots with scarecrows standing")
	}
	Advance(s, c, s.UpdatedAt+1)
	if s.activeCritterCount() == 0 {
		t.Fatal("a scarecrow shooed a critter; shooing is manual now")
	}
	if s.Coins != coins {
		t.Fatalf("coins moved %d→%d without any action", coins, s.Coins)
	}
}

func TestCritterCapAppliesWithScarecrows(t *testing.T) {
	c := testContent(t)
	c.Critters.VisitChancePct = 100
	c.Critters.MaxActive = 2
	s := scarecrowState(t, 1)
	s.Plots = make([]Plot, 5)
	Advance(s, c, s.UpdatedAt+1)
	if got := s.activeCritterCount(); got != 2 {
		t.Fatalf("active critters = %d, want the cap of 2 (scarecrows no longer bypass it)", got)
	}
}

func TestShooRewardScalesWithRebirths(t *testing.T) {
	c := testContent(t) // rebirth_bonus_pct = 50
	c.Critters.ShooRewardMin, c.Critters.ShooRewardMax = 10, 10
	for _, tc := range []struct {
		rebirths int64
		want     int64
	}{{0, 10}, {1, 15}, {2, 20}, {10, 60}} {
		s := newTestState(t, c)
		s.Rebirths = tc.rebirths
		s.Plots[0].Critter = "crow"
		got, err := ShooCritter(s, c, 0)
		if err != nil || got != tc.want {
			t.Errorf("%d rebirths: shoo = %d (%v), want %d", tc.rebirths, got, err, tc.want)
		}
		if lo, hi := s.ShooRewardRange(c); lo != tc.want || hi != tc.want {
			t.Errorf("%d rebirths: displayed range %d–%d, want %d", tc.rebirths, lo, hi, tc.want)
		}
	}
}

func TestCollectScarecrowCreditsOnlyThatPouch(t *testing.T) {
	s := scarecrowState(t, 2)
	s.Scarecrows[0].Pouch = 40
	s.Scarecrows[1].Pouch = 7
	coins, run, life := s.Coins, s.RunEarnings, s.LifetimeEarnings

	got, err := CollectScarecrow(s, 0)
	if err != nil || got != 40 {
		t.Fatalf("collect = %d, %v; want 40", got, err)
	}
	if s.Coins != coins+40 || s.RunEarnings != run+40 || s.LifetimeEarnings != life+40 {
		t.Fatal("collected coins must count as coins, run earnings and lifetime earnings")
	}
	if s.Scarecrows[0].Pouch != 0 || s.Scarecrows[1].Pouch != 7 {
		t.Fatalf("pouches after collecting #0 = %d/%d, want 0/7", s.Scarecrows[0].Pouch, s.Scarecrows[1].Pouch)
	}
	if _, err := CollectScarecrow(s, 0); !errors.Is(err, ErrPouchEmpty) {
		t.Fatalf("collecting an empty pouch = %v, want ErrPouchEmpty", err)
	}
	for _, bad := range []int{-1, 2} {
		if _, err := CollectScarecrow(s, bad); !errors.Is(err, ErrUnknownScarecrow) {
			t.Fatalf("collect(%d) = %v, want ErrUnknownScarecrow", bad, err)
		}
	}
}

func TestCollectScarecrowsEmptiesEveryPouch(t *testing.T) {
	s := scarecrowState(t, 3)
	s.Scarecrows[0].Pouch = 5
	s.Scarecrows[2].Pouch = 11
	coins := s.Coins
	total, n := CollectScarecrows(s)
	if total != 16 || n != 2 {
		t.Fatalf("collect all = %d from %d, want 16 from 2 (the empty one doesn't count)", total, n)
	}
	if s.Coins != coins+16 || s.ScarecrowPouchTotal() != 0 {
		t.Fatal("collect-all must credit everything and leave every pouch empty")
	}
}

// Rebirth throws coins away anyway, so an uncollected pouch would vanish
// silently. It is banked first, so it counts toward the Starseed payout, and
// the preview the rebirth screen shows agrees with what the rebirth pays.
func TestRebirthBanksPouchesAndResetsScarecrows(t *testing.T) {
	c := testContent(t)
	s := scarecrowState(t, 2)
	s.RunEarnings = 10_000
	s.LifetimeEarnings = 10_000
	s.Scarecrows[0].Pouch = 20_000
	s.Scarecrows[1].Pouch = 10_000
	s.ScarecrowClock = 123

	preview := s.PrestigeGain(c)
	if want := isqrt(40_000 / c.Prestige.Divisor); preview != want {
		t.Fatalf("preview = %d, want %d (run earnings + pouches)", preview, want)
	}
	gain, err := Rebirth(s, c, 2000)
	if err != nil || gain != preview {
		t.Fatalf("rebirth gain = %d (%v), want the previewed %d", gain, err, preview)
	}
	if s.LifetimeEarnings != 40_000 {
		t.Fatalf("lifetime = %d, want the banked 40,000", s.LifetimeEarnings)
	}
	if len(s.Scarecrows) != 0 || s.ScarecrowClock != 0 {
		t.Fatal("scarecrows are run-scoped and must reset on rebirth")
	}
}

// A pouch that would push the run over the rebirth threshold makes the run
// eligible: it's the player's money either way.
func TestPouchesCountTowardRebirthEligibility(t *testing.T) {
	c := testContent(t)
	s := scarecrowState(t, 1)
	s.RunEarnings = c.Prestige.MinEarnings - 1
	if s.CanRebirth(c) {
		t.Fatal("precondition: run should be one coin short")
	}
	s.Scarecrows[0].Pouch = 1
	if !s.CanRebirth(c) {
		t.Fatal("an uncollected pouch should count toward rebirth eligibility")
	}
}

func TestContractStartWipesScarecrows(t *testing.T) {
	c := realContent(t)
	s := contractReadyState(t, c)
	s.Scarecrows = []Scarecrow{{Pouch: 9}, {Pouch: 3}}
	s.ScarecrowClock = 40
	if err := StartContract(s, c, ContractBareHands, 2000); err != nil {
		t.Fatal(err)
	}
	if len(s.Scarecrows) != 0 || s.ScarecrowClock != 0 {
		t.Fatal("starting a contract is a hard reset; scarecrows must go")
	}
}

// Drops are not credits, so a tick can no longer finish Bare Hands; only
// collecting the pouch can.
func TestScarecrowDropsCompleteAContractOnlyWhenCollected(t *testing.T) {
	c := realContent(t)
	s := contractReadyState(t, c)
	if err := StartContract(s, c, ContractBareHands, 10); err != nil {
		t.Fatal(err)
	}
	s.Scarecrows = make([]Scarecrow, 1)
	s.ContractEarnings = contractBareHandsEarnings - 1

	ev := Advance(s, c, 10+3600)
	if ev.ContractCompleted != "" || s.ContractsCompleted != 0 {
		t.Fatal("a pouch filling up completed the contract without being collected")
	}
	if s.Scarecrows[0].Pouch == 0 {
		t.Fatal("precondition: an hour offline should have dropped coins")
	}
	if _, err := CollectScarecrow(s, 0); err != nil {
		t.Fatal(err)
	}
	if s.ContractsCompleted != 1 || s.ActiveContract != "" {
		t.Fatalf("collecting should cross the goal: completed %d, active %q", s.ContractsCompleted, s.ActiveContract)
	}
}

func TestClockworkDeniedFreezesScarecrows(t *testing.T) {
	c := testContent(t)
	s := scarecrowState(t, 1) // a corrupt save: scarecrows inside the contract
	s.ActiveContract = ContractClockworkDenied
	Advance(s, c, s.UpdatedAt+3600)
	if s.Scarecrows[0].Pouch != 0 {
		t.Fatal("Clockwork Denied forbids scarecrows; they must not drop coins")
	}
}

// Without scarecrows nothing scarecrow-shaped may appear in the save, so
// payloads that never bought one stay byte-identical (the parity promise).
func TestNoScarecrowsLeavesPayloadUntouched(t *testing.T) {
	c := testContent(t)
	s := newTestState(t, c)
	tickOnline(s, c, 60)
	Advance(s, c, s.UpdatedAt+7200)
	b, err := s.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte("scarecrow")) {
		t.Fatalf("a save with no scarecrows serialized scarecrow state: %s", b)
	}
}

func TestLegacyScarecrowSaveMigratesToOne(t *testing.T) {
	old := []byte(`{"version":4,"rng":1,"updated_at":100,"coins":50,"plots":[{}],"scarecrow":true,"news_enabled":true,"critters_enabled":true}`)
	s, err := DecodeState(old)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Scarecrows) != 1 || s.Scarecrows[0].Pouch != 0 {
		t.Fatalf("legacy scarecrow migrated to %+v, want one empty scarecrow", s.Scarecrows)
	}
	b, _ := s.Encode()
	if bytes.Contains(b, []byte(`"scarecrow":`)) {
		t.Fatalf("legacy flag survived re-encode: %s", b)
	}
	again, err := DecodeState(b)
	if err != nil || len(again.Scarecrows) != 1 {
		t.Fatalf("migration is not idempotent: %+v, %v", again.Scarecrows, err)
	}
}

func TestCloneDoesNotShareScarecrows(t *testing.T) {
	s := scarecrowState(t, 2)
	cl := s.Clone()
	cl.Scarecrows[0].Pouch = 999
	if s.Scarecrows[0].Pouch != 0 {
		t.Fatal("Clone shares the Scarecrows backing array; UI snapshots could mutate the save")
	}
}

// A year away must finish promptly and still land within the possible range.
func TestLongOfflineCatchUpIsBounded(t *testing.T) {
	c := testContent(t)
	s := scarecrowState(t, 4)
	const year = 365 * 86400
	Advance(s, c, s.UpdatedAt+year)
	drops := int64(year / (15 * 20))
	lo, hi := s.ScarecrowDropRange(c)
	for i, sc := range s.Scarecrows {
		if sc.Pouch < drops*lo || sc.Pouch > drops*hi {
			t.Fatalf("scarecrow %d pouch %d outside %d–%d after a year", i, sc.Pouch, drops*lo, drops*hi)
		}
	}
}

func TestScarecrowNextDropCountsDownOnline(t *testing.T) {
	c := testContent(t)
	s := scarecrowState(t, 1)
	if pct, left := s.ScarecrowNextDrop(c); pct != 0 || left != 15 {
		t.Fatalf("fresh clock: %d%%, %ds left; want 0%%, 15s", pct, left)
	}
	tickOnline(s, c, 6)
	if pct, left := s.ScarecrowNextDrop(c); pct != 40 || left != 9 {
		t.Fatalf("after 6s: %d%%, %ds left; want 40%%, 9s", pct, left)
	}
}
