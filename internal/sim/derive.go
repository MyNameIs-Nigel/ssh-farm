package sim

import (
	"math"

	"github.com/mynameis-nigel/ssh-farm/internal/content"
	"github.com/mynameis-nigel/ssh-farm/internal/gameclock"
)

// VisibleCrops returns crops that should appear in pickers and catalogs.
// Prestige-locked crops with unlock value V are hidden until Rebirths >= V-1.
func VisibleCrops(s *State, c *content.Content) []content.Crop {
	var out []content.Crop
	for _, crop := range c.Crops {
		if crop.Unlock.Kind == "prestige" && crop.Unlock.Value > 1 {
			if s.Rebirths < crop.Unlock.Value-1 {
				continue
			}
		}
		out = append(out, crop)
	}
	return out
}

// CheapestUnlockedCrop returns the lowest seed-cost crop the player can plant.
func CheapestUnlockedCrop(s *State, c *content.Content) *content.Crop {
	var best *content.Crop
	for i := range c.Crops {
		crop := &c.Crops[i]
		if !s.Unlocked(crop.Unlock) {
			continue
		}
		if best == nil || crop.SeedCost < best.SeedCost {
			best = crop
		}
	}
	return best
}

// MercyPlantEligible reports whether planting cropID is free under mercy rules.
func (s *State) MercyPlantEligible(c *content.Content, cropID string) bool {
	if !s.allPlotsEmpty() {
		return false
	}
	cheapest := CheapestUnlockedCrop(s, c)
	if cheapest == nil || cropID != cheapest.ID {
		return false
	}
	return s.Coins < s.SeedCost(c, cheapest)
}

func (s *State) allPlotsEmpty() bool {
	for _, p := range s.Plots {
		if p.Crop != "" {
			return false
		}
	}
	return true
}

// SeedCost returns the effective seed cost after event discounts.
func (s *State) SeedCost(c *content.Content, crop *content.Crop) int64 {
	cost := crop.SeedCost
	if ev := s.activeEvent(c); ev != nil && ev.Effect == "seed_discount_pct" {
		discount := ev.EffectValue
		if discount >= 100 {
			discount = 99
		}
		cost = cost * (100 - discount) / 100
	}
	if cost < 0 {
		cost = 0
	}
	return cost
}

// SalvageValue returns the coins from a failed risky harvest at current strain level.
func (s *State) SalvageValue(c *content.Content, crop *content.Crop, now int64) int64 {
	num := SalvageNumerator(s.SeedUpgradeLevel(crop.ID))
	base := crop.SellValue * num / 8
	return s.sellMultiplied(c, base, crop.ID, now)
}

// GrowSeconds returns the crop's effective grow time for this save.
func (s *State) GrowSeconds(c *content.Content, crop *content.Crop) int64 {
	reduction := int64(0)
	for _, u := range c.Upgrades {
		if u.Effect == "grow_speed_pct" {
			reduction += u.EffectValue * int64(s.UpgradeLevel(u.ID))
		}
	}
	for _, m := range c.Multipliers {
		if m.Effect == "grow_speed_pct" {
			reduction += m.EffectValue * int64(s.MultiplierLevel(m.ID))
		}
	}
	if ev := s.activeEvent(c); ev != nil && ev.Effect == "grow_speed_pct" {
		reduction += ev.EffectValue
	}
	if reduction >= 100 {
		reduction = 99
	}
	g := crop.GrowSeconds * (100 - reduction) / 100
	if g < 1 {
		g = 1
	}
	return g
}

// sellMultiplied applies permanent and run-scoped sell bonuses to a base payout.
// now is the tick counter the payout is settled at; it drives the Moonberry
// night window.
func (s *State) sellMultiplied(c *content.Content, base int64, cropID string, now int64) int64 {
	bonus := int64(0)
	for _, u := range c.Upgrades {
		if u.Effect == "sell_bonus_pct" {
			bonus += u.EffectValue * int64(s.UpgradeLevel(u.ID))
		}
	}
	for _, m := range c.Multipliers {
		if m.Effect == "sell_bonus_pct" {
			bonus += m.EffectValue * int64(s.MultiplierLevel(m.ID))
		}
	}
	if ev := s.activeEvent(c); ev != nil && ev.Effect == "sell_bonus_pct" {
		bonus += ev.EffectValue
	}
	if isMoonberryHour(c, cropID, now) {
		bonus += c.Moon.FullMoonSellBonusPct
	}
	if bonus == 0 {
		return base
	}
	if base > math.MaxInt64/(100+bonus) {
		return math.MaxInt64
	}
	return base * (100 + bonus) / 100
}

func (s *State) activeEvent(c *content.Content) *content.Event {
	if s.EventID == "" {
		return nil
	}
	return c.EventByID(s.EventID)
}

// EventActive reports whether an event is running at now.
func (s *State) EventActive(now int64) bool {
	return s.EventID != "" && now < s.EventEndsAt
}

// Moonberry's sell bonus runs through the in-game night: 20:00 inclusive to
// 03:59 inclusive. The window wraps midnight, so the check is a disjunction
// rather than a single range.
const (
	moonberryStartMinute = 20 * 60
	moonberryEndMinute   = 4 * 60
)

// isMoonberryHour reports whether cropID gets the night sell bonus at now.
//
// It reads the same gameclock minute-of-day the header clock displays, so the
// bonus is active exactly when the player can see a qualifying time on screen.
// It deliberately does not consult MoonPhase: the moon cycle is derived from
// persisted wall-clock days and would not line up with the accelerated clock.
func isMoonberryHour(c *content.Content, cropID string, now int64) bool {
	if cropID != c.Moon.MoonberryCropID {
		return false
	}
	m := gameclock.MinuteOfDay(now)
	return m >= moonberryStartMinute || m < moonberryEndMinute
}

// MoonPhase returns the day index (0..cycle-1) in the current moon cycle.
func (s *State) MoonPhase() int64 {
	cycle := int64(28)
	if s.MoonEpoch == 0 {
		return 0
	}
	day := s.UpdatedAt / 86400
	if cycle < 1 {
		return 0
	}
	return (day - s.MoonEpoch) % cycle
}

// MoonPhaseName returns a cosmetic label for the current moon phase.
func (s *State) MoonPhaseName(c *content.Content) string {
	cycle := c.Moon.CycleDays
	if cycle < 1 {
		cycle = 28
	}
	phase := s.MoonPhase()
	switch {
	case phase == cycle/2:
		return "Full Moon"
	case phase == 0:
		return "New Moon"
	case phase < cycle/4:
		return "Waxing Crescent"
	case phase < cycle/2:
		return "Waxing Gibbous"
	case phase < 3*cycle/4:
		return "Waning Gibbous"
	default:
		return "Waning Crescent"
	}
}

// startCoins returns the wallet a new run begins with, including the
// permanent start-coins upgrades.
func startCoins(c *content.Content, upgrades map[string]int) int64 {
	coins := c.Start.Coins
	for _, u := range c.Upgrades {
		if u.Effect == "start_coins" {
			coins = satAdd(coins, u.EffectValue*int64(upgrades[u.ID]))
		}
	}
	return coins
}

// NextPlotCost returns the price of the next plot, or -1 when the farm is at
// the purchasable cap.
func (s *State) NextPlotCost(c *content.Content) int64 {
	if c.Start.Plots+s.PurchasedPlots >= c.Land.MaxPlots {
		return -1
	}
	cost := c.Land.BasePlotCost
	for i := 0; i < s.PurchasedPlots; i++ {
		if cost > math.MaxInt64/c.Land.GrowthPct {
			cost = math.MaxInt64 / 100
			break
		}
		cost = cost * c.Land.GrowthPct / 100
	}
	discount := int64(0)
	for _, u := range c.Upgrades {
		if u.Effect == "plot_discount_pct" {
			discount += u.EffectValue * int64(s.UpgradeLevel(u.ID))
		}
	}
	if discount >= 100 {
		discount = 99
	}
	cost = cost * (100 - discount) / 100
	if cost < 1 {
		cost = 1
	}
	return cost
}

// PlotAutoHarvestCost returns the cost to add auto-harvest to a plot.
func (s *State) PlotAutoHarvestCost(c *content.Content) int64 {
	n := int64(0)
	for _, p := range s.Plots {
		if p.AutoHarvest {
			n++
		}
	}
	cost := c.PlotAutomation.AutoHarvestBaseCost
	for i := int64(0); i < n; i++ {
		if cost > math.MaxInt64/c.PlotAutomation.AutoHarvestGrowthPct {
			cost = math.MaxInt64 / 100
			break
		}
		cost = cost * c.PlotAutomation.AutoHarvestGrowthPct / 100
	}
	if cost < 1 {
		cost = 1
	}
	return cost
}

// UpgradeCost returns the Starseed price of the next level of an upgrade, or
// -1 when it is already at max level.
func (s *State) UpgradeCost(u *content.Upgrade) int64 {
	level := s.UpgradeLevel(u.ID)
	if level >= u.MaxLevel {
		return -1
	}
	cost := u.Cost
	for i := 0; i < level; i++ {
		if cost > math.MaxInt64/u.CostGrowthPct {
			return math.MaxInt64 / 100
		}
		cost = cost * u.CostGrowthPct / 100
	}
	return cost
}

// MultiplierCost returns the coin price of the next level of a multiplier.
func (s *State) MultiplierCost(m *content.Multiplier) int64 {
	level := s.MultiplierLevel(m.ID)
	if level >= m.MaxLevel {
		return -1
	}
	cost := m.Cost
	for i := 0; i < level; i++ {
		if cost > math.MaxInt64/m.CostGrowthPct {
			return math.MaxInt64 / 100
		}
		cost = cost * m.CostGrowthPct / 100
	}
	return cost
}

// SeedUpgradeCost returns the coin price of the next Hardier Strain level.
func (s *State) SeedUpgradeCost(su *content.SeedUpgrade) int64 {
	level := s.SeedUpgradeLevel(su.CropID)
	if level >= su.MaxLevel {
		return -1
	}
	cost := su.Cost
	for i := 0; i < level; i++ {
		if cost > math.MaxInt64/su.CostGrowthPct {
			return math.MaxInt64 / 100
		}
		cost = cost * su.CostGrowthPct / 100
	}
	return cost
}

// PrestigeGain returns the Starseeds a rebirth would award: isqrt(run earnings / divisor).
func (s *State) PrestigeGain(c *content.Content) int64 {
	return isqrt(s.RunEarnings / c.Prestige.Divisor)
}

// CanRebirth reports whether the run has earned enough to rebirth.
func (s *State) CanRebirth(c *content.Content) bool {
	return s.RunEarnings >= c.Prestige.MinEarnings && s.PrestigeGain(c) >= 1
}

// isqrt is the integer square root (floor) for non-negative n.
func isqrt(n int64) int64 {
	if n <= 0 {
		return 0
	}
	r := int64(math.Sqrt(float64(n)))
	for r > 0 && r > n/r {
		r--
	}
	for r+1 <= n/(r+1) {
		r++
	}
	return r
}

// Unlocked reports whether an unlock gate is satisfied for this save.
func (s *State) Unlocked(u content.Unlock) bool {
	switch u.Kind {
	case "", "start":
		return true
	case "earnings":
		return s.LifetimeEarnings >= u.Value
	case "prestige":
		return s.Rebirths >= u.Value
	case "zone":
		return s.Zones[u.Zone]
	default:
		return false
	}
}

// PlotReady reports whether plot i is planted and mature at now.
func (s *State) PlotReady(c *content.Content, i int, now int64) bool {
	if i < 0 || i >= len(s.Plots) || s.Plots[i].Crop == "" {
		return false
	}
	crop := c.Crop(s.Plots[i].Crop)
	if crop == nil {
		return false
	}
	return now >= s.Plots[i].PlantedAt+s.GrowSeconds(c, crop)
}

// PlotProgressPct returns growth progress 0–100 for a planted plot.
func (s *State) PlotProgressPct(c *content.Content, i int, now int64) int {
	if i < 0 || i >= len(s.Plots) || s.Plots[i].Crop == "" {
		return 0
	}
	crop := c.Crop(s.Plots[i].Crop)
	if crop == nil {
		return 0
	}
	grow := s.GrowSeconds(c, crop)
	elapsed := now - s.Plots[i].PlantedAt
	if elapsed <= 0 {
		return 0
	}
	if elapsed >= grow {
		return 100
	}
	return int(elapsed * 100 / grow)
}

// upgradeEffectSum sums effect values from permanent upgrades with a given effect.
func (s *State) upgradeEffectSum(c *content.Content, effect string) int64 {
	var sum int64
	for _, u := range c.Upgrades {
		if u.Effect == effect {
			sum += u.EffectValue * int64(s.UpgradeLevel(u.ID))
		}
	}
	return sum
}
