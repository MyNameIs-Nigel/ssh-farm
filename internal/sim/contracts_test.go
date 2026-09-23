package sim

import (
	"errors"
	"testing"

	"github.com/mynameis-nigel/ssh-farm/internal/content"
)

func contractReadyState(t *testing.T, c *content.Content) *State {
	t.Helper()
	s := New(c, 42, 1000)
	for _, u := range c.Upgrades {
		s.Upgrades[u.ID] = u.MaxLevel
	}
	for _, crop := range c.Crops {
		switch crop.Unlock.Kind {
		case "earnings":
			if s.LifetimeEarnings < crop.Unlock.Value {
				s.LifetimeEarnings = crop.Unlock.Value
			}
		case "prestige":
			if s.Rebirths < crop.Unlock.Value {
				s.Rebirths = crop.Unlock.Value
			}
		}
	}
	return s
}

func TestContractsUnlockOnlyAfterPermanentBaseGameCompletion(t *testing.T) {
	c := realContent(t)
	s := New(c, 1, 0)
	if s.ContractsAvailable(c) {
		t.Fatal("fresh save should not have contracts")
	}
	if err := StartContract(s, c, ContractBareHands, 10); !errors.Is(err, ErrContractsLocked) {
		t.Fatalf("start before completion = %v, want ErrContractsLocked", err)
	}

	s = contractReadyState(t, c)
	if !s.ContractsAvailable(c) {
		t.Fatal("permanently complete save should have contracts")
	}
	if err := StartContract(s, c, ContractBareHands, 10); err != nil {
		t.Fatalf("start first contract: %v", err)
	}
	if !s.ContractsUnlocked {
		t.Fatal("starting the campaign should permanently unlock it")
	}
}

func TestStartContractHardResetPreservesLifetimeIdentityAndSettings(t *testing.T) {
	c := realContent(t)
	s := contractReadyState(t, c)
	s.Coins = 999_999
	s.Plots = append(s.Plots, Plot{Crop: "turnip", AutoHarvest: true, AutoSow: true})
	s.PurchasedPlots = 1
	s.Zones["greenhouse"] = true
	s.Multipliers["fertilizer"] = 2
	s.SeedUpgrades["glimmercorn"] = 2
	s.Scarecrow = true
	s.GiftPending = true
	s.GiftArrivedAt = 900
	s.EventID = "market_day"
	s.EventStartedAt = 901
	s.EventEndsAt = 9999
	s.RunEarnings = 88_000
	s.PrestigeCurrency = 777
	s.LifetimeHarvests = 1234
	s.Achievements["first_sprout"] = 50
	s.FarmName = "NORTHFIELD"
	s.NewsEnabled = false
	s.CrittersEnabled = false
	s.ThemeSolid = true
	s.SeasonalOff = true
	s.ReplantWarned = true
	s.LastCrop = "turnip"

	wantLifetime := s.LifetimeEarnings
	wantRebirths := s.Rebirths
	if err := StartContract(s, c, ContractBareHands, 2000); err != nil {
		t.Fatal(err)
	}

	if s.ActiveContract != ContractBareHands || s.ContractsCompleted != 0 {
		t.Fatalf("contract state = %q/%d", s.ActiveContract, s.ContractsCompleted)
	}
	if s.Coins != c.Start.Coins || len(s.Plots) != c.Start.Plots || s.PurchasedPlots != 0 {
		t.Fatalf("farm not reset: coins=%d plots=%d purchased=%d", s.Coins, len(s.Plots), s.PurchasedPlots)
	}
	if s.PrestigeCurrency != 0 || len(s.Upgrades) != 0 || len(s.Zones) != 0 || len(s.Multipliers) != 0 || len(s.SeedUpgrades) != 0 {
		t.Fatalf("progression not reset: pp=%d upgrades=%v zones=%v multipliers=%v strains=%v",
			s.PrestigeCurrency, s.Upgrades, s.Zones, s.Multipliers, s.SeedUpgrades)
	}
	if s.Scarecrow || s.GiftPending || s.EventID != "" || s.RunEarnings != 0 || s.LastCrop != "" {
		t.Fatalf("transient state survived reset: %+v", s)
	}
	if s.LifetimeEarnings != wantLifetime || s.Rebirths != wantRebirths || s.LifetimeHarvests != 1234 {
		t.Fatal("lifetime statistics did not survive")
	}
	if s.FarmName != "NORTHFIELD" || s.Achievements["first_sprout"] != 50 {
		t.Fatal("identity or achievements did not survive")
	}
	if s.NewsEnabled || s.CrittersEnabled || !s.ThemeSolid || !s.SeasonalOff || !s.ReplantWarned {
		t.Fatal("player settings did not survive")
	}
	if s.UpdatedAt != 2000 {
		t.Fatalf("updated_at = %d, want 2000", s.UpdatedAt)
	}
}

func TestContractsAreStrictlyOrderedAndOneTime(t *testing.T) {
	c := realContent(t)
	s := contractReadyState(t, c)
	if err := StartContract(s, c, ContractLeanSeason, 10); !errors.Is(err, ErrContractSequence) {
		t.Fatalf("starting contract 2 first = %v, want ErrContractSequence", err)
	}
	if err := StartContract(s, c, ContractBareHands, 10); err != nil {
		t.Fatal(err)
	}
	if err := StartContract(s, c, ContractBareHands, 11); !errors.Is(err, ErrContractActive) {
		t.Fatalf("starting while active = %v, want ErrContractActive", err)
	}

	s.credit(contractBareHandsEarnings)
	if s.ContractsCompleted != 1 || s.ActiveContract != "" {
		t.Fatalf("bare hands completion = %d active=%q", s.ContractsCompleted, s.ActiveContract)
	}
	if !s.ContractsAvailable(c) {
		t.Fatal("campaign should stay available after the reset erased upgrades")
	}
	if err := StartContract(s, c, ContractBareHands, 20); !errors.Is(err, ErrContractSequence) {
		t.Fatalf("replaying contract 1 = %v, want ErrContractSequence", err)
	}
	if err := StartContract(s, c, ContractLeanSeason, 20); err != nil {
		t.Fatalf("start contract 2: %v", err)
	}
}

func TestContractProgressionUsesLocalRebirthsButKeepsHistoricalCount(t *testing.T) {
	c := realContent(t)
	s := contractReadyState(t, c)
	historical := s.Rebirths
	if err := StartContract(s, c, ContractBareHands, 10); err != nil {
		t.Fatal(err)
	}
	if got := s.ProgressionRebirths(); got != 0 {
		t.Fatalf("contract progression rebirths = %d, want 0", got)
	}
	if s.Unlocked(content.Unlock{Kind: "prestige", Value: 1}) {
		t.Fatal("historical rebirths leaked into contract crop gates")
	}

	s.RunEarnings = c.Prestige.MinEarnings
	if _, err := Rebirth(s, c, 20); err != nil {
		t.Fatal(err)
	}
	if s.Rebirths != historical+1 || s.ContractRebirths != 1 || s.ProgressionRebirths() != 1 {
		t.Fatalf("rebirth counts: historical=%d local=%d progression=%d",
			s.Rebirths, s.ContractRebirths, s.ProgressionRebirths())
	}
	if !s.Unlocked(content.Unlock{Kind: "prestige", Value: 1}) {
		t.Fatal("local rebirth should unlock prestige tier 1")
	}
}

func TestBareHandsBlocksAutomationAndCompletesAtContractEarnings(t *testing.T) {
	c := realContent(t)
	s := contractReadyState(t, c)
	if err := StartContract(s, c, ContractBareHands, 10); err != nil {
		t.Fatal(err)
	}
	s.Coins = 100_000
	if err := UpgradePlotAuto(s, c, 0, "harvest"); !errors.Is(err, ErrContractRestricted) {
		t.Fatalf("buy automation = %v, want ErrContractRestricted", err)
	}
	s.Plots[0].AutoSow = true // defensive path for imported/corrupt state
	if err := SetAutoSowCrop(s, c, 0, "turnip"); !errors.Is(err, ErrContractRestricted) {
		t.Fatalf("set auto-sow = %v, want ErrContractRestricted", err)
	}

	s.credit(contractBareHandsEarnings - 1)
	if s.ContractsCompleted != 0 {
		t.Fatal("completed one coin early")
	}
	s.credit(1)
	if s.ContractsCompleted != 1 || s.ActiveContract != "" {
		t.Fatalf("completion state = %d/%q", s.ContractsCompleted, s.ActiveContract)
	}
}

func TestLeanSeasonCapsLandAndCountsEarnedStarseeds(t *testing.T) {
	c := realContent(t)
	s := contractReadyState(t, c)
	s.ContractsUnlocked = true
	s.ContractsCompleted = 1
	if err := StartContract(s, c, ContractLeanSeason, 10); err != nil {
		t.Fatal(err)
	}
	s.Coins = 1_000_000
	for len(s.Plots) < contractLeanSeasonMaxPlots {
		if _, err := BuyPlot(s, c); err != nil {
			t.Fatalf("buy plot %d: %v", len(s.Plots)+1, err)
		}
	}
	if _, err := BuyPlot(s, c); !errors.Is(err, ErrContractRestricted) {
		t.Fatalf("buy plot seven = %v, want ErrContractRestricted", err)
	}
	if err := BuyZone(s, c, "greenhouse"); !errors.Is(err, ErrContractRestricted) {
		t.Fatalf("buy greenhouse = %v, want ErrContractRestricted", err)
	}
	if err := UpgradePlotAuto(s, c, 0, "harvest"); err != nil {
		t.Fatalf("automation should be available in Lean Season: %v", err)
	}

	s.creditStarseeds(contractLeanSeasonStarseeds - 1)
	s.PrestigeCurrency = 1_000_000
	if err := BuyUpgrade(s, c, c.Upgrades[0].ID); err != nil {
		t.Fatal(err)
	}
	if s.ContractsCompleted != 1 {
		t.Fatal("spending should not complete or erase cumulative progress")
	}
	s.creditStarseeds(1)
	if s.ContractsCompleted != 2 || s.ActiveContract != "" {
		t.Fatalf("completion state = %d/%q", s.ContractsCompleted, s.ActiveContract)
	}
}

func TestClockworkDeniedBlocksPassiveSystemsAndCompletesAtFiveRebirths(t *testing.T) {
	c := realContent(t)
	c.EventsConfig.MinIntervalSec = 1
	c.EventsConfig.MaxIntervalSec = 1
	c.EventsConfig.MinDurationSec = 60
	c.EventsConfig.MaxDurationSec = 60
	c.Gifts.OnlineIntervalSec = 1

	s := contractReadyState(t, c)
	s.ContractsUnlocked = true
	s.ContractsCompleted = 2
	if err := StartContract(s, c, ContractClockworkDenied, 10); err != nil {
		t.Fatal(err)
	}
	s.Coins = 1_000_000
	if err := UpgradePlotAuto(s, c, 0, "harvest"); !errors.Is(err, ErrContractRestricted) {
		t.Fatalf("buy automation = %v, want ErrContractRestricted", err)
	}
	if _, err := BuyScarecrow(s, c); !errors.Is(err, ErrContractRestricted) {
		t.Fatalf("buy scarecrow = %v, want ErrContractRestricted", err)
	}
	s.GiftPending = true
	if _, err := RedeemGift(s, c); !errors.Is(err, ErrContractRestricted) {
		t.Fatalf("redeem gift = %v, want ErrContractRestricted", err)
	}
	s.GiftPending = false
	ev := Advance(s, c, 11)
	if ev.EventStarted != "" || ev.GiftArrived || s.EventID != "" || s.GiftPending {
		t.Fatalf("passive system fired under contract: event=%q gift=%v", ev.EventStarted, ev.GiftArrived)
	}

	for i := 0; i < contractClockworkRebirths; i++ {
		s.RunEarnings = c.Prestige.MinEarnings
		if _, err := Rebirth(s, c, int64(20+i)); err != nil {
			t.Fatalf("rebirth %d: %v", i+1, err)
		}
		if i < contractClockworkRebirths-1 && s.ContractsCompleted != 2 {
			t.Fatalf("completed after only %d rebirths", i+1)
		}
	}
	if s.ContractsCompleted != 3 || s.ActiveContract != "" {
		t.Fatalf("completion state = %d/%q", s.ContractsCompleted, s.ActiveContract)
	}
}

func TestAbandonContractStartsFreshWithoutRestoringProgress(t *testing.T) {
	c := realContent(t)
	s := contractReadyState(t, c)
	if err := StartContract(s, c, ContractBareHands, 10); err != nil {
		t.Fatal(err)
	}
	s.Coins = 9999
	s.ContractEarnings = 9000
	if err := AbandonContract(s, c, 20); err != nil {
		t.Fatal(err)
	}
	if s.ActiveContract != "" || s.Coins != c.Start.Coins || s.ContractEarnings != 0 {
		t.Fatalf("abandon did not start fresh: active=%q coins=%d earnings=%d",
			s.ActiveContract, s.Coins, s.ContractEarnings)
	}
	if !s.ContractsUnlocked || s.ContractsCompleted != 0 {
		t.Fatal("abandon relocked or advanced the campaign")
	}
	if err := AbandonContract(s, c, 30); !errors.Is(err, ErrNoActiveContract) {
		t.Fatalf("second abandon = %v, want ErrNoActiveContract", err)
	}
}

func TestContractNameStylesUnlockByCompletion(t *testing.T) {
	c := realContent(t)
	s := New(c, 1, 0)
	if err := SetLeaderboardNameStyle(s, NameStyleLeaf); !errors.Is(err, ErrNameStyleLocked) {
		t.Fatalf("style before contract 2 = %v, want ErrNameStyleLocked", err)
	}
	s.ContractsCompleted = 2
	if err := SetLeaderboardNameStyle(s, NameStyleLeaf); err != nil {
		t.Fatalf("static style after contract 2: %v", err)
	}
	if err := SetLeaderboardNameStyle(s, NameStylePurpleWave); !errors.Is(err, ErrNameStyleLocked) {
		t.Fatalf("wave before contract 3 = %v, want ErrNameStyleLocked", err)
	}
	s.ContractsCompleted = 3
	if err := SetLeaderboardNameStyle(s, NameStylePurpleWave); err != nil {
		t.Fatalf("wave after contract 3: %v", err)
	}
	if err := SetLeaderboardNameStyle(s, "rainbow-chaos"); !errors.Is(err, ErrUnknownNameStyle) {
		t.Fatalf("unknown style = %v, want ErrUnknownNameStyle", err)
	}
}

func TestPayloadUpgradeV4InitializesContractsWithoutInferringCompletion(t *testing.T) {
	b := []byte("{\"version\":4,\"rng\":1,\"updated_at\":100,\"coins\":25,\"plots\":[{},{},{}]," +
		"\"zones\":{},\"upgrades\":{\"growth\":5},\"achievements\":{},\"rebirths\":999," +
		"\"lifetime_earnings\":999999999,\"lifetime_harvests\":10}")
	s, err := DecodeState(b)
	if err != nil {
		t.Fatal(err)
	}
	if s.Version != StateVersion {
		t.Fatalf("version = %d, want %d", s.Version, StateVersion)
	}
	if s.ContractsUnlocked || s.ContractsCompleted != 0 || s.ActiveContract != "" || s.LeaderboardNameStyle != "" {
		t.Fatalf("migration inferred contract state: %+v", s)
	}
}

// Bare Hands allows the Scarecrow, so its bounties (online) and its trickle
// (offline) can cross the goal inside Advance rather than in an action. The
// caller has no other way to learn the contract finished there.
func TestAdvanceReportsContractCompletedDuringCatchUp(t *testing.T) {
	c := realContent(t)
	for _, tc := range []struct {
		name    string
		elapsed int64
		setup   func(s *State)
	}{
		{"online scarecrow bounty", 1, func(s *State) { s.Plots[0].Critter = "crow" }},
		{"offline scarecrow trickle", 3600, func(*State) {}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := contractReadyState(t, c)
			if err := StartContract(s, c, ContractBareHands, 10); err != nil {
				t.Fatal(err)
			}
			s.Scarecrow = true
			s.ContractEarnings = contractBareHandsEarnings - 1
			tc.setup(s)

			ev := Advance(s, c, 10+tc.elapsed)
			if ev.ContractCompleted != ContractBareHands || s.ContractsCompleted != 1 {
				t.Fatalf("ContractCompleted = %q, completed = %d", ev.ContractCompleted, s.ContractsCompleted)
			}
			if ev.Empty() {
				t.Fatal("a completed contract must make the away summary non-empty")
			}
			if again := Advance(s, c, 20+tc.elapsed); again.ContractCompleted != "" {
				t.Fatalf("second Advance re-reported %q", again.ContractCompleted)
			}
		})
	}
}
