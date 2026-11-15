package sim

import (
	"errors"

	"github.com/mynameis-nigel/ssh-farm/internal/content"
)

// Fertilizer (gameplay/06) is a permanent, one-shot item: using one ripens
// every growing crop at once. It survives rebirths and contracts.
const (
	// FertilizerStart is what every farm begins with. A save that has never
	// gained or spent fertilizer stores nothing and is read as holding this,
	// which is how existing saves get theirs without changing a byte.
	FertilizerStart = 1
	// FertilizerBaseMax is the cap before contracts; each completed contract
	// raises it by one (and refills to the new cap).
	FertilizerBaseMax = 3
)

var (
	ErrNoFertilizer   = errors.New("no fertilizer left")
	ErrNothingGrowing = errors.New("nothing is growing")
)

// fertilizerDropSalt separates the parcel's fertilizer roll from the save's
// RNG stream; see fertilizerDropped.
const fertilizerDropSalt uint64 = 0xF3A7_11E5_0D5E_ED00

// Fertilizer returns how many the player holds.
func (s *State) Fertilizer() int {
	if !s.FertilizerTracked {
		return FertilizerStart
	}
	return s.FertilizerHeld
}

// FertilizerMax is the most the player may hold: the base cap plus one per
// completed contract. It is derived, never stored.
func (s *State) FertilizerMax() int {
	return FertilizerBaseMax + max(s.ContractsCompleted, 0)
}

// setFertilizer stores an explicit count, clamped to [0, FertilizerMax].
func (s *State) setFertilizer(n int) {
	s.FertilizerHeld = min(max(n, 0), s.FertilizerMax())
	s.FertilizerTracked = true
}

// UseFertilizer spends one fertilizer to ripen every growing crop at now,
// returning how many plots it ripened. It refuses — without spending — when
// nothing is growing, so a stray keypress on a bare or fully ripe farm
// cannot waste one.
func UseFertilizer(s *State, c *content.Content, now int64) (int, error) {
	if s.Fertilizer() < 1 {
		return 0, ErrNoFertilizer
	}
	if s.GrowingPlots(c, now) == 0 {
		return 0, ErrNothingGrowing
	}
	s.setFertilizer(s.Fertilizer() - 1)
	return s.ripenAll(c, now), nil
}

// FertilizerForSale reports whether this balance sells it at all.
func FertilizerForSale(c *content.Content) bool { return c.Fertilizer.CostStarseeds > 0 }

// BuyFertilizer spends Starseeds on one fertilizer, up to the cap.
func BuyFertilizer(s *State, c *content.Content) error {
	if s.Fertilizer() >= s.FertilizerMax() {
		return ErrMaxed
	}
	cost := c.Fertilizer.CostStarseeds
	if cost < 1 {
		return ErrLocked // this balance does not sell it
	}
	if s.PrestigeCurrency < cost {
		return ErrCantAffordPP
	}
	s.PrestigeCurrency -= cost
	s.setFertilizer(s.Fertilizer() + 1)
	return nil
}

// GrowingPlots counts planted plots that are not yet ripe at now — the
// plots a fertilizer would ripen.
func (s *State) GrowingPlots(c *content.Content, now int64) int {
	n := 0
	for i := range s.Plots {
		p := &s.Plots[i]
		if crop := c.Crop(p.Crop); crop != nil && now < p.PlantedAt+s.GrowSeconds(c, crop) {
			n++
		}
	}
	return n
}

// ripenAll makes every growing crop ripe at exactly now and returns how many
// it moved. Ripe plots keep their PlantedAt, so a crop that was already
// waiting is not pushed later. An auto-harvest plot is gathered by the next
// tick, exactly as if it had ripened on its own.
func (s *State) ripenAll(c *content.Content, now int64) int {
	n := 0
	for i := range s.Plots {
		p := &s.Plots[i]
		crop := c.Crop(p.Crop)
		if crop == nil {
			continue
		}
		if grow := s.GrowSeconds(c, crop); now < p.PlantedAt+grow {
			p.PlantedAt = now - grow
			n++
		}
	}
	return n
}

// fertilizerDropped rolls whether the parcel being opened holds fertilizer.
// The roll is derived from the RNG without advancing it: drawing from the
// stream would shift every later roll, and the v1 parity harness replays a
// parcel against a byte-exact v1 golden. It is still seeded by the save, so
// it stays deterministic, and the stream moves on with every other roll, so
// successive parcels are independent.
func (s *State) fertilizerDropped(c *content.Content) bool {
	pct := c.Fertilizer.DropChancePct
	if pct <= 0 {
		return false
	}
	z := s.RNG ^ fertilizerDropSalt
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	z ^= z >> 31
	return int64(z%100) < pct
}
