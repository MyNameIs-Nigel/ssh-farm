package sim

import (
	"errors"

	"github.com/mynameis-nigel/ssh-farm/internal/content"
)

// Scarecrows (gameplay/05): up to MaxCount per run, each dropping a random
// coin amount into its own pouch on a shared clock. Drops are never credited
// until the player collects them, so nothing here touches the wallet except
// the explicit Collect actions.

var (
	ErrUnknownScarecrow = errors.New("no such scarecrow")
	ErrPouchEmpty       = errors.New("that scarecrow's pouch is empty")
)

// scarecrowRollCap bounds the drops rolled one by one per scarecrow in a
// single Advance; a longer absence settles the rest at the expected value,
// the same way auto-plots batch long catch-ups.
const scarecrowRollCap = 2_000

// ScarecrowsAllowed reports whether the active contract permits scarecrows.
func (s *State) ScarecrowsAllowed() bool { return !s.contractBlocksScarecrow() }

// ScarecrowCost is the price of the next scarecrow, or -1 when none can be
// bought (the item is disabled or the run already owns the maximum).
func (s *State) ScarecrowCost(c *content.Content) int64 {
	sc := c.Scarecrow
	if sc.Cost <= 0 || int64(len(s.Scarecrows)) >= sc.MaxCount {
		return -1
	}
	cost := sc.Cost
	for range s.Scarecrows {
		cost = satMul(cost, sc.CostGrowthPct) / 100
	}
	return cost
}

// ScarecrowRebirthGate reports how many progression rebirths the next
// scarecrow needs (unlock_rebirths) and whether the run is still short of
// them. There is no gate once nothing more can be bought. The gate only ever
// refuses a purchase: scarecrows a run already owns are never taken back.
func (s *State) ScarecrowRebirthGate(c *content.Content) (need int64, locked bool) {
	if s.ScarecrowCost(c) < 0 {
		return 0, false
	}
	need = c.Scarecrow.RebirthsToBuy(len(s.Scarecrows))
	return need, s.ProgressionRebirths() < need
}

// BuyScarecrow purchases the next scarecrow at its escalating price.
func BuyScarecrow(s *State, c *content.Content) (int64, error) {
	if s.contractBlocksScarecrow() {
		return 0, ErrContractRestricted
	}
	if c.Scarecrow.Cost <= 0 {
		return 0, ErrUnknownItem
	}
	cost := s.ScarecrowCost(c)
	if cost < 0 {
		return 0, ErrMaxed
	}
	if _, locked := s.ScarecrowRebirthGate(c); locked {
		return 0, ErrLocked
	}
	if s.Coins < cost {
		return 0, ErrCantAfford
	}
	s.Coins -= cost
	s.Scarecrows = append(s.Scarecrows, Scarecrow{})
	return cost, nil
}

// scarecrowDrop scales one base roll by the synergy of owning several and by
// the run's rebirths. Every scarecrow gets the same multiplier.
func (s *State) scarecrowDrop(c *content.Content, base int64) int64 {
	n := int64(len(s.Scarecrows))
	if n < 1 {
		n = 1
	}
	synergy := 100 + c.Scarecrow.SynergyPct*(n-1)
	rebirth := 100 + c.Scarecrow.RebirthBonusPct*s.ProgressionRebirths()
	return satMul(satMul(base, synergy), rebirth) / 10_000
}

// ScarecrowDropRange is what one scarecrow adds to its pouch per drop right
// now (inclusive bounds), for the Market and Help copy.
func (s *State) ScarecrowDropRange(c *content.Content) (lo, hi int64) {
	return s.scarecrowDrop(c, c.Scarecrow.CoinsMin), s.scarecrowDrop(c, c.Scarecrow.CoinsMax)
}

// scarecrowClockTarget is the clock reading at which a drop lands. The clock
// counts offline seconds, and an online second is worth OfflineSlowdown.
func scarecrowClockTarget(c *content.Content) int64 {
	return satMul(c.Scarecrow.DropIntervalSec, c.Scarecrow.OfflineSlowdown)
}

// ScarecrowNextDrop reports progress toward the next drop and how many
// online seconds remain until it lands.
func (s *State) ScarecrowNextDrop(c *content.Content) (pct int, onlineSecsLeft int64) {
	target := scarecrowClockTarget(c)
	slow := c.Scarecrow.OfflineSlowdown
	if target < 1 || slow < 1 {
		return 0, 0
	}
	clock := min(max(s.ScarecrowClock, 0), target)
	return int(clock * 100 / target), (target - clock + slow - 1) / slow
}

// advanceScarecrows runs the shared drop clock for elapsed seconds and rolls
// each scarecrow's drops into its pouch. Called by Advance only.
func (s *State) advanceScarecrows(c *content.Content, elapsed int64, online bool, ev *Events) {
	sc := c.Scarecrow
	if len(s.Scarecrows) == 0 || sc.Cost <= 0 || s.contractBlocksScarecrow() {
		return
	}
	target := scarecrowClockTarget(c)
	if target < 1 {
		return
	}
	rate := int64(1)
	if online {
		rate = sc.OfflineSlowdown
	}
	s.ScarecrowClock = satAdd(max(s.ScarecrowClock, 0), satMul(elapsed, rate))
	drops := s.ScarecrowClock / target
	s.ScarecrowClock %= target
	if drops == 0 {
		return
	}

	rolled := min(drops, scarecrowRollCap)
	for i := range s.Scarecrows {
		var gained int64
		for k := int64(0); k < rolled; k++ {
			gained = satAdd(gained, s.scarecrowDrop(c, s.rollRange(sc.CoinsMin, sc.CoinsMax)))
		}
		if rest := drops - rolled; rest > 0 {
			mean := s.scarecrowDrop(c, (sc.CoinsMin+sc.CoinsMax)/2)
			gained = satAdd(gained, satMul(mean, rest))
		}
		s.Scarecrows[i].Pouch = satAdd(s.Scarecrows[i].Pouch, gained)
		ev.ScarecrowCoins = satAdd(ev.ScarecrowCoins, gained)
	}
}

// ScarecrowPouchTotal is every uncollected coin across the run's scarecrows.
func (s *State) ScarecrowPouchTotal() int64 {
	var total int64
	for _, sc := range s.Scarecrows {
		total = satAdd(total, sc.Pouch)
	}
	return total
}

// CollectScarecrow empties scarecrow i's pouch into the wallet.
func CollectScarecrow(s *State, i int) (int64, error) {
	if i < 0 || i >= len(s.Scarecrows) {
		return 0, ErrUnknownScarecrow
	}
	pouch := s.Scarecrows[i].Pouch
	if pouch <= 0 {
		return 0, ErrPouchEmpty
	}
	s.Scarecrows[i].Pouch = 0
	s.credit(pouch)
	return pouch, nil
}

// CollectScarecrows empties every pouch, returning the coins and how many
// scarecrows had something to give.
func CollectScarecrows(s *State) (total int64, collected int) {
	for i := range s.Scarecrows {
		if got, err := CollectScarecrow(s, i); err == nil {
			total = satAdd(total, got)
			collected++
		}
	}
	return total, collected
}

// ShooRewardRange is the coin range one hand-shooed critter pays now: the
// base range grown by RebirthBonusPct per progression rebirth.
func (s *State) ShooRewardRange(c *content.Content) (lo, hi int64) {
	return s.shooScaled(c, c.Critters.ShooRewardMin), s.shooScaled(c, c.Critters.ShooRewardMax)
}

func (s *State) shooScaled(c *content.Content, base int64) int64 {
	return satMul(base, 100+c.Critters.RebirthBonusPct*s.ProgressionRebirths()) / 100
}
