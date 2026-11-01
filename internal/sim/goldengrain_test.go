package sim

import (
	"errors"
	"testing"

	"github.com/mynameis-nigel/ssh-farm/internal/content"
)

// Goldengrain is gated twice: the run must own the Greenhouse AND the
// progression must have rebirthed twice. Either alone stays locked.
func TestGoldengrainNeedsGreenhouseAndTwoRebirths(t *testing.T) {
	c, err := content.Load("")
	if err != nil {
		t.Fatal(err)
	}
	gz := c.ZoneByID("greenhouse")
	if gz == nil {
		t.Fatal("embedded content has no greenhouse zone")
	}

	for _, tc := range []struct {
		name       string
		rebirths   int64
		greenhouse bool
		want       error
	}{
		{"neither", 0, false, ErrLocked},
		{"greenhouse, one rebirth", 1, true, ErrLocked},
		{"two rebirths, no greenhouse", 2, false, ErrLocked},
		{"greenhouse and two rebirths", 2, true, nil},
		{"greenhouse and more rebirths", 4, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := New(c, 1, 0)
			s.Rebirths = tc.rebirths
			s.Coins = gz.Cost + 1_000
			s.LifetimeEarnings = 1_000_000
			if tc.greenhouse {
				if err := BuyZone(s, c, "greenhouse"); err != nil {
					t.Fatalf("buy greenhouse: %v", err)
				}
			}
			before := s.Coins
			if err := Plant(s, c, 0, "goldengrain", 0); !errors.Is(err, tc.want) {
				t.Fatalf("plant goldengrain = %v, want %v", err, tc.want)
			}
			if tc.want == nil && before-s.Coins != s.SeedCost(c, c.Crop("goldengrain")) {
				t.Fatalf("planting charged %d, want the seed cost", before-s.Coins)
			}
		})
	}
}

// During a contract the rebirth requirement reads the contract's own count,
// exactly as prestige-tier crops do.
func TestGoldengrainUsesProgressionRebirths(t *testing.T) {
	c, err := content.Load("")
	if err != nil {
		t.Fatal(err)
	}
	gate := c.Crop("goldengrain").Unlock
	s := New(c, 1, 0)
	s.Zones["greenhouse"] = true
	s.Rebirths = 5
	s.ActiveContract = ContractBareHands
	s.ContractRebirths = 1
	if s.Unlocked(gate) {
		t.Fatal("contract run with 1 contract rebirth should not unlock goldengrain")
	}
	s.ContractRebirths = 2
	if !s.Unlocked(gate) {
		t.Fatal("contract run with 2 contract rebirths and a greenhouse should unlock goldengrain")
	}
}

// Like a tier-2 prestige crop, Goldengrain is hidden on a fresh save and
// previewed (locked) from rebirth 1.
func TestGoldengrainVisibility(t *testing.T) {
	c, err := content.Load("")
	if err != nil {
		t.Fatal(err)
	}
	visible := func(s *State) bool {
		for _, crop := range VisibleCrops(s, c) {
			if crop.ID == "goldengrain" {
				return true
			}
		}
		return false
	}
	s := New(c, 1, 0)
	if visible(s) {
		t.Fatal("rebirth-0 save should not see goldengrain")
	}
	s.Rebirths = 1
	if !visible(s) {
		t.Fatal("rebirth-1 save should preview goldengrain")
	}
}

// Goldengrain is meant to be the best-paying permanent fast crop by profit
// rate. Seasonal seeds are excluded: they pay above the permanent curve by
// design and are balanced by their event window.
func TestGoldengrainIsTheBestPermanentFastCrop(t *testing.T) {
	c, err := content.Load("")
	if err != nil {
		t.Fatal(err)
	}
	rate := func(cr *content.Crop) float64 {
		return float64(cr.SellValue-cr.SeedCost) / float64(cr.GrowSeconds)
	}
	gg := c.Crop("goldengrain")
	for i := range c.Crops {
		cr := &c.Crops[i]
		if cr.ID == gg.ID || cr.Archetype != "fast" || cr.Unlock.Kind == "season" {
			continue
		}
		if rate(cr) >= rate(gg) {
			t.Errorf("%s profits %.2fc/min, at or above goldengrain's %.2fc/min",
				cr.ID, rate(cr)*60, rate(gg)*60)
		}
	}
}

// A crop's extra rebirth requirement counts toward "base game complete": a
// save short of it cannot enter contracts even with every other gate met.
func TestContractsNeedExtraRebirthRequirements(t *testing.T) {
	c := realContent(t)
	s := contractReadyState(t, c)
	if !s.ContractsAvailable(c) {
		t.Fatal("test setup: ready state should have contracts")
	}
	c.Crops = append(c.Crops, content.Crop{
		ID: "deepgrain", Name: "Deepgrain", Archetype: "fast", SeedCost: 1, GrowSeconds: 1, SellValue: 2,
		Unlock: content.Unlock{Kind: "zone", Zone: "greenhouse", Rebirths: s.Rebirths + 1},
	})
	if s.ContractsAvailable(c) {
		t.Fatal("a crop needing one more rebirth should keep contracts locked")
	}
	s.Rebirths++
	if !s.ContractsAvailable(c) {
		t.Fatal("meeting the extra rebirth requirement should open contracts")
	}
}
