package sim

import (
	"bytes"
	"errors"
	"testing"

	"github.com/mynameis-nigel/ssh-farm/internal/content"
)

// dropContent returns a fresh fixture load whose parcels always (or never)
// hold fertilizer. Each call loads its own copy, so mutating it cannot leak
// into another test.
func dropContent(t *testing.T, pct int64) *content.Content {
	t.Helper()
	c := testContent(t)
	c.Fertilizer.DropChancePct = pct
	return c
}

func TestFertilizerStarterNeedsNoSaveBytes(t *testing.T) {
	c := testContent(t)
	s := newTestState(t, c)
	if got := s.Fertilizer(); got != FertilizerStart {
		t.Fatalf("fresh save holds %d fertilizer, want the starter %d", got, FertilizerStart)
	}
	if got := s.FertilizerMax(); got != FertilizerBaseMax {
		t.Fatalf("fresh save max is %d, want %d", got, FertilizerBaseMax)
	}
	// The starter is implied by the zero value, so a save that never touched
	// fertilizer writes no new keys — v1 payloads stay byte-identical.
	b, err := s.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte(`"fertilizer`)) {
		t.Fatalf("untouched save wrote fertilizer keys: %s", b)
	}

	// An existing v1 save decodes holding the starter too.
	old, _ := loadGolden(t, "mid_rebirth.json")
	if got := old.Fertilizer(); got != FertilizerStart {
		t.Fatalf("v1 save holds %d fertilizer, want the starter %d", got, FertilizerStart)
	}
}

func TestFertilizerZeroIsStoredDistinctFromStarter(t *testing.T) {
	c := testContent(t)
	s := newTestState(t, c)
	if err := Plant(s, c, 0, "turnip", 1000); err != nil {
		t.Fatal(err)
	}
	if _, err := UseFertilizer(s, c, 1001); err != nil {
		t.Fatalf("use: %v", err)
	}
	b, err := s.Encode()
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodeState(b)
	if err != nil {
		t.Fatal(err)
	}
	if got := back.Fertilizer(); got != 0 {
		t.Fatalf("spent the only fertilizer, but the reloaded save holds %d", got)
	}
}

func TestUseFertilizerRipensOnlyGrowingPlots(t *testing.T) {
	c := testContent(t)
	s := newTestState(t, c)
	s.Coins = 1000
	s.Plots = make([]Plot, 4)
	now := int64(5000)
	mustPlant := func(i int, crop string, at int64) {
		t.Helper()
		if err := Plant(s, c, i, crop, at); err != nil {
			t.Fatalf("plant %d: %v", i, err)
		}
	}
	mustPlant(0, "turnip", now-10)   // growing (60s)
	mustPlant(1, "pumpkin", now-100) // growing (3600s)
	mustPlant(2, "turnip", now-500)  // already ripe
	// plot 3 stays empty
	ripeAt := s.Plots[2].PlantedAt

	n, err := UseFertilizer(s, c, now)
	if err != nil {
		t.Fatalf("use: %v", err)
	}
	if n != 2 {
		t.Fatalf("ripened %d plots, want 2", n)
	}
	for i := 0; i < 3; i++ {
		if !s.PlotReady(c, i, now) {
			t.Fatalf("plot %d not ready right after fertilizer", i)
		}
	}
	if s.Plots[2].PlantedAt != ripeAt {
		t.Fatal("fertilizer moved a plot that was already ripe")
	}
	if s.Plots[3].Crop != "" {
		t.Fatal("fertilizer planted an empty plot")
	}
	if got := s.Fertilizer(); got != 0 {
		t.Fatalf("fertilizer left %d, want 0", got)
	}
	// Ripe means harvestable at the same instant.
	if _, err := Harvest(s, c, 1, now); err != nil {
		t.Fatalf("harvest fertilized pumpkin: %v", err)
	}
}

func TestUseFertilizerRefusals(t *testing.T) {
	c := testContent(t)

	t.Run("nothing growing keeps the fertilizer", func(t *testing.T) {
		s := newTestState(t, c)
		if _, err := UseFertilizer(s, c, 1000); !errors.Is(err, ErrNothingGrowing) {
			t.Fatalf("empty farm: err %v, want ErrNothingGrowing", err)
		}
		if err := Plant(s, c, 0, "turnip", 1000); err != nil {
			t.Fatal(err)
		}
		if _, err := UseFertilizer(s, c, 1060); !errors.Is(err, ErrNothingGrowing) {
			t.Fatalf("only ripe crops: err %v, want ErrNothingGrowing", err)
		}
		if got := s.Fertilizer(); got != FertilizerStart {
			t.Fatalf("a refused use spent fertilizer: %d left", got)
		}
	})

	t.Run("none held", func(t *testing.T) {
		s := newTestState(t, c)
		s.setFertilizer(0)
		if err := Plant(s, c, 0, "turnip", 1000); err != nil {
			t.Fatal(err)
		}
		if _, err := UseFertilizer(s, c, 1001); !errors.Is(err, ErrNoFertilizer) {
			t.Fatalf("err %v, want ErrNoFertilizer", err)
		}
		if s.PlotReady(c, 0, 1001) {
			t.Fatal("refused use still ripened the plot")
		}
	})
}

// A Warm Front shortens grow time; when it ends the plots are rebased to the
// slower speed. A fertilized crop must stay ripe through that rebase.
func TestFertilizedCropStaysRipeAfterGrowEventEnds(t *testing.T) {
	c := realContent(t)
	s := New(c, 3, 1000)
	if err := Plant(s, c, 0, "turnip", 1000); err != nil {
		t.Fatal(err)
	}
	s.EventID = "warm_front"
	s.EventStartedAt = 1000
	s.EventEndsAt = 1030
	if _, err := UseFertilizer(s, c, 1005); err != nil {
		t.Fatalf("use: %v", err)
	}
	Advance(s, c, 1100)
	if s.EventID != "" {
		t.Fatal("event should have ended")
	}
	if !s.PlotReady(c, 0, 1100) {
		t.Fatal("fertilized turnip turned unripe when the Warm Front ended")
	}
}

func TestBuyFertilizer(t *testing.T) {
	c := testContent(t)
	cost := c.Fertilizer.CostStarseeds
	s := newTestState(t, c)

	s.PrestigeCurrency = cost - 1
	if err := BuyFertilizer(s, c); !errors.Is(err, ErrCantAffordPP) {
		t.Fatalf("short of Starseeds: err %v, want ErrCantAffordPP", err)
	}

	s.PrestigeCurrency = cost*10 + 3
	for want := FertilizerStart + 1; want <= FertilizerBaseMax; want++ {
		if err := BuyFertilizer(s, c); err != nil {
			t.Fatalf("buy to %d: %v", want, err)
		}
		if got := s.Fertilizer(); got != want {
			t.Fatalf("after buying hold %d, want %d", got, want)
		}
	}
	spent := int64(FertilizerBaseMax-FertilizerStart) * cost
	if got := s.PrestigeCurrency; got != cost*10+3-spent {
		t.Fatalf("Starseeds %d, want %d", got, cost*10+3-spent)
	}
	before := s.PrestigeCurrency
	if err := BuyFertilizer(s, c); !errors.Is(err, ErrMaxed) {
		t.Fatalf("at cap: err %v, want ErrMaxed", err)
	}
	if s.PrestigeCurrency != before {
		t.Fatal("a refused purchase spent Starseeds")
	}
}

func TestFertilizerSurvivesRebirthAndContracts(t *testing.T) {
	c := testContent(t)
	s := newTestState(t, c)
	s.setFertilizer(2)
	s.FertilizerNoConfirm = true

	s.RunEarnings = c.Prestige.MinEarnings
	if _, err := Rebirth(s, c, 2000); err != nil {
		t.Fatalf("rebirth: %v", err)
	}
	if s.Fertilizer() != 2 || !s.FertilizerNoConfirm {
		t.Fatalf("rebirth reset fertilizer: %d held, no-confirm %v", s.Fertilizer(), s.FertilizerNoConfirm)
	}

	s.ContractsUnlocked = true
	if err := StartContract(s, c, ContractBareHands, 3000); err != nil {
		t.Fatalf("start contract: %v", err)
	}
	if s.Fertilizer() != 2 || !s.FertilizerNoConfirm {
		t.Fatalf("starting a contract reset fertilizer: %d held", s.Fertilizer())
	}
	if err := AbandonContract(s, c, 4000); err != nil {
		t.Fatalf("abandon: %v", err)
	}
	if s.Fertilizer() != 2 || !s.FertilizerNoConfirm {
		t.Fatalf("abandoning a contract reset fertilizer: %d held", s.Fertilizer())
	}
}

func TestContractCompletionRefillsAndRaisesFertilizerCap(t *testing.T) {
	c := testContent(t)
	s := newTestState(t, c)
	s.ContractsUnlocked = true

	wantMax := FertilizerBaseMax
	for i, id := range []ContractID{ContractBareHands, ContractLeanSeason, ContractClockworkDenied} {
		s.setFertilizer(0)
		if err := StartContract(s, c, id, int64(10_000*(i+1))); err != nil {
			t.Fatalf("start %s: %v", id, err)
		}
		switch id {
		case ContractBareHands:
			s.credit(contractBareHandsEarnings)
		case ContractLeanSeason:
			s.creditStarseeds(contractLeanSeasonStarseeds)
		case ContractClockworkDenied:
			s.ContractRebirths = contractClockworkRebirths - 1
			s.RunEarnings = c.Prestige.MinEarnings
			if _, err := Rebirth(s, c, int64(10_000*(i+1))+5); err != nil {
				t.Fatalf("final rebirth: %v", err)
			}
		}
		wantMax++
		if s.ContractsCompleted != i+1 {
			t.Fatalf("%s did not complete", id)
		}
		if got := s.FertilizerMax(); got != wantMax {
			t.Fatalf("after %s max is %d, want %d", id, got, wantMax)
		}
		if got := s.Fertilizer(); got != wantMax {
			t.Fatalf("after %s hold %d, want a full %d", id, got, wantMax)
		}
	}
}

func TestParcelFertilizerDrop(t *testing.T) {
	t.Run("below cap adds one and nothing else", func(t *testing.T) {
		c := dropContent(t, 100)
		s := newTestState(t, c)
		s.GiftPending = true
		rng, coins, ss := s.RNG, s.Coins, s.PrestigeCurrency
		res, err := RedeemGift(s, c, 1000)
		if err != nil {
			t.Fatal(err)
		}
		if !res.Fertilizer || res.FertilizerUsed {
			t.Fatalf("result %+v, want a banked fertilizer", res)
		}
		if res.Coins != 0 || res.Starseeds != 0 || s.Coins != coins || s.PrestigeCurrency != ss {
			t.Fatalf("a fertilizer parcel also paid out: %+v", res)
		}
		if got := s.Fertilizer(); got != FertilizerStart+1 {
			t.Fatalf("hold %d, want %d", got, FertilizerStart+1)
		}
		if s.RNG != rng {
			t.Fatal("the fertilizer roll advanced the save's RNG stream")
		}
		if s.GiftPending {
			t.Fatal("parcel still pending after redeem")
		}
	})

	t.Run("at cap is used at once", func(t *testing.T) {
		c := dropContent(t, 100)
		s := newTestState(t, c)
		s.setFertilizer(FertilizerBaseMax)
		if err := Plant(s, c, 0, "turnip", 1000); err != nil {
			t.Fatal(err)
		}
		s.GiftPending = true
		res, err := RedeemGift(s, c, 1001)
		if err != nil {
			t.Fatal(err)
		}
		if !res.Fertilizer || !res.FertilizerUsed || res.Ripened != 1 {
			t.Fatalf("result %+v, want fertilizer used on 1 plot", res)
		}
		if got := s.Fertilizer(); got != FertilizerBaseMax {
			t.Fatalf("hold %d, want to stay at %d", got, FertilizerBaseMax)
		}
		if !s.PlotReady(c, 0, 1001) {
			t.Fatal("the at-cap parcel did not ripen the turnip")
		}
	})

	t.Run("chance zero never drops", func(t *testing.T) {
		c := dropContent(t, 0)
		s := newTestState(t, c)
		for i := 0; i < 200; i++ {
			s.GiftPending = true
			res, err := RedeemGift(s, c, 1000)
			if err != nil {
				t.Fatal(err)
			}
			if res.Fertilizer {
				t.Fatal("dropped fertilizer at 0% chance")
			}
		}
	})
}

// The drop roll is derived from the RNG without advancing it, so check it is
// still a fair roll: across many independent saves it should land near the
// configured chance, and below the Starseed chance it is meant to undercut.
func TestParcelFertilizerDropRate(t *testing.T) {
	c := realContent(t)
	pct := c.Fertilizer.DropChancePct
	if pct <= 0 || pct >= c.Gifts.StarseedChancePct {
		t.Fatalf("fertilizer drop %d%% must be positive and rarer than Starseeds (%d%%)", pct, c.Gifts.StarseedChancePct)
	}
	const trials = 20_000
	hits := 0
	for seed := uint64(1); seed <= trials; seed++ {
		s := New(c, seed*0x9E3779B97F4A7C15, 1000)
		if s.fertilizerDropped(c) {
			hits++
		}
	}
	got := float64(hits) * 100 / trials
	if lo, hi := float64(pct)*0.8, float64(pct)*1.2; got < lo || got > hi {
		t.Fatalf("drop rate %.2f%%, want within 20%% of %d%%", got, pct)
	}
}
