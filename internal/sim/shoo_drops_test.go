package sim

import (
	"testing"

	"github.com/mynameis-nigel/ssh-farm/internal/content"
)

// shooContent is the test balance with the shoo drop chances pinned, in basis
// points (10000 = always), so a test can force or forbid a drop.
func shooContent(t *testing.T, starseedBp, fertilizerBp int64) *content.Content {
	t.Helper()
	c := testContent(t)
	c.Critters.StarseedChanceBp = starseedBp
	c.Critters.FertilizerChanceBp = fertilizerBp
	return c
}

func shooState(t *testing.T, c *content.Content, rebirths int64) *State {
	t.Helper()
	s := newTestState(t, c)
	s.Rebirths = rebirths
	s.Plots[0].Critter = "crow"
	return s
}

func TestShooCritterFertilizerDrop(t *testing.T) {
	t.Run("below cap banks one on top of the coins", func(t *testing.T) {
		c := shooContent(t, 0, 10000)
		s := shooState(t, c, 0)
		coins := s.Coins
		res, err := ShooCritter(s, c, 0, 1001)
		if err != nil {
			t.Fatal(err)
		}
		if !res.Fertilizer || res.FertilizerUsed {
			t.Fatalf("result %+v, want a banked fertilizer", res)
		}
		if res.Coins < c.Critters.ShooRewardMin || s.Coins != coins+res.Coins {
			t.Fatalf("the drop must not replace the coin reward: %+v, coins %d→%d", res, coins, s.Coins)
		}
		if got := s.Fertilizer(); got != FertilizerStart+1 {
			t.Fatalf("hold %d, want %d", got, FertilizerStart+1)
		}
		if s.Plots[0].Critter != "" {
			t.Fatal("the critter is still on the plot")
		}
	})

	t.Run("at the cap it is used on the spot", func(t *testing.T) {
		c := shooContent(t, 0, 10000)
		s := shooState(t, c, 0)
		s.setFertilizer(FertilizerBaseMax)
		if err := Plant(s, c, 1, "turnip", 1000); err != nil {
			t.Fatal(err)
		}
		res, err := ShooCritter(s, c, 0, 1001)
		if err != nil {
			t.Fatal(err)
		}
		if !res.Fertilizer || !res.FertilizerUsed || res.Ripened != 1 {
			t.Fatalf("result %+v, want fertilizer used on 1 plot", res)
		}
		if got := s.Fertilizer(); got != FertilizerBaseMax {
			t.Fatalf("hold %d, want to stay at %d", got, FertilizerBaseMax)
		}
	})
}

func TestShooCritterStarseedDrop(t *testing.T) {
	t.Run("pays a starseed after the first rebirth", func(t *testing.T) {
		c := shooContent(t, 10000, 0)
		s := shooState(t, c, 1)
		ss, fert := s.PrestigeCurrency, s.Fertilizer()
		res, err := ShooCritter(s, c, 0, 1001)
		if err != nil {
			t.Fatal(err)
		}
		if res.Starseeds != ShooStarseedReward || s.PrestigeCurrency != ss+ShooStarseedReward {
			t.Fatalf("result %+v, starseeds %d→%d, want +%d", res, ss, s.PrestigeCurrency, ShooStarseedReward)
		}
		if res.Fertilizer || s.Fertilizer() != fert {
			t.Fatalf("a starseed drop also gave fertilizer: %+v", res)
		}
	})

	t.Run("never before the first rebirth", func(t *testing.T) {
		// Parcels hold back Starseeds until the player has met them, and so does this.
		c := shooContent(t, 10000, 0)
		for i := 0; i < 50; i++ {
			s := shooState(t, c, 0)
			s.RNG = uint64(i) * 7919
			res, err := ShooCritter(s, c, 0, 1001)
			if err != nil {
				t.Fatal(err)
			}
			if res.Starseeds != 0 || s.PrestigeCurrency != 0 {
				t.Fatalf("starseeds before any rebirth: %+v", res)
			}
		}
	})

	t.Run("counts toward an active contract", func(t *testing.T) {
		c := shooContent(t, 10000, 0)
		s := shooState(t, c, 1)
		s.ActiveContract = ContractLeanSeason
		s.ContractRebirths = 1
		if _, err := ShooCritter(s, c, 0, 1001); err != nil {
			t.Fatal(err)
		}
		if s.ContractStarseeds != ShooStarseedReward {
			t.Fatalf("contract starseeds %d, want %d", s.ContractStarseeds, ShooStarseedReward)
		}
	})

	t.Run("a contract run starts its own progression at zero", func(t *testing.T) {
		// Rebirths is the historical count; inside a contract the gate reads
		// the contract's own rebirths, so a veteran gets no starseed on day one.
		c := shooContent(t, 10000, 0)
		s := shooState(t, c, 5)
		s.ActiveContract = ContractLeanSeason
		res, err := ShooCritter(s, c, 0, 1001)
		if err != nil || res.Starseeds != 0 || s.ContractStarseeds != 0 {
			t.Fatalf("contract day one: %+v, %v, contract starseeds %d", res, err, s.ContractStarseeds)
		}
	})
}

func TestShooCritterNoDropByDefault(t *testing.T) {
	c := shooContent(t, 0, 0)
	for i := 0; i < 500; i++ {
		s := shooState(t, c, 3)
		s.RNG = uint64(i) * 0x9E3779B97F4A7C15
		fert, ss := s.Fertilizer(), s.PrestigeCurrency
		res, err := ShooCritter(s, c, 0, 1001)
		if err != nil {
			t.Fatal(err)
		}
		if res.Fertilizer || res.Starseeds != 0 || s.Fertilizer() != fert || s.PrestigeCurrency != ss {
			t.Fatalf("zero chances still dropped something: %+v", res)
		}
		if res.Coins <= 0 {
			t.Fatalf("no coins for shooing: %+v", res)
		}
	}
}

func TestShooCritterRefusalsDoNotRoll(t *testing.T) {
	c := shooContent(t, 10000, 0)
	s := shooState(t, c, 1)
	rng := s.RNG
	if _, err := ShooCritter(s, c, 1, 1001); err != ErrNoCritter {
		t.Fatalf("empty plot: %v, want ErrNoCritter", err)
	}
	if _, err := ShooCritter(s, c, 99, 1001); err != ErrUnknownPlot {
		t.Fatalf("bad plot: %v, want ErrUnknownPlot", err)
	}
	if s.RNG != rng || s.PrestigeCurrency != 0 {
		t.Fatal("a refused shoo rolled or paid")
	}
}

// Drops must be VERY rare, fertilizer rarer than a starseed, and the shipped
// balance must actually hit those rates: a stuck roll would pass every test
// above. Starseeds and fertilizer are one roll in disjoint bands, so neither
// can steal from the other.
func TestShooDropRatesInShippedBalance(t *testing.T) {
	c := realContent(t)
	star, fert := c.Critters.StarseedChanceBp, c.Critters.FertilizerChanceBp
	if fert <= 0 || star <= fert {
		t.Fatalf("want 0 < fertilizer (%dbp) < starseed (%dbp)", fert, star)
	}
	if star > 100 {
		t.Fatalf("starseed chance %dbp is above 1%% — not VERY rare", star)
	}
	const trials = 400_000
	var stars, ferts int
	for i := uint64(1); i <= trials; i++ {
		s := New(c, i*0x9E3779B97F4A7C15, 1000)
		s.Rebirths = 1
		s.Plots[0].Critter = "crow"
		res, err := ShooCritter(s, c, 0, 1001)
		if err != nil {
			t.Fatal(err)
		}
		if res.Starseeds > 0 {
			stars++
		}
		if res.Fertilizer {
			ferts++
		}
	}
	for _, tc := range []struct {
		name string
		got  int
		bp   int64
	}{{"starseed", stars, star}, {"fertilizer", ferts, fert}} {
		want := float64(trials) * float64(tc.bp) / 10000
		if g := float64(tc.got); g < want*0.7 || g > want*1.3 {
			t.Errorf("%s drops: %d in %d shoos, want about %.0f (%dbp)", tc.name, tc.got, trials, want, tc.bp)
		}
	}
}
