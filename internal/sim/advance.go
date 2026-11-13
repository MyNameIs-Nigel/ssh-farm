package sim

import (
	"github.com/mynameis-nigel/ssh-farm/internal/content"
)

// autoCycleCap bounds per-plot harvest/replant cycles simulated individually
// during one Advance.
const autoCycleCap = 50_000

// onlineTickThreshold: advances at or below this elapsed are "live" ticks.
const onlineTickThreshold = 2

// Events describes what happened during an Advance.
type Events struct {
	Elapsed        int64
	Matured        map[string]int
	AutoHarvested  map[string]int
	AutoCoins      int64
	Discoveries    int
	DiscoveryCoins int64
	Achievements   []string
	FailedHarvests map[string]int
	GoldenHarvests int
	GiftArrived    bool
	EventStarted   string
	EventEnded     string
	CritterVisits  []string
	// ScarecrowCoins is what the scarecrows dropped into their pouches. It is
	// not credited: the player collects pouches by hand (gameplay/05).
	ScarecrowCoins int64
	AwayVignettes  []string
	// ContractCompleted names the contract whose goal this Advance crossed,
	// so a live tick or the offline catch-up can announce it the way an
	// action does. No passive source credits coins inside a contract today
	// (scarecrow drops wait in a pouch), so this is a guard for future ones.
	ContractCompleted ContractID
}

// Empty reports whether nothing noteworthy happened.
func (e *Events) Empty() bool {
	return len(e.Matured) == 0 && len(e.AutoHarvested) == 0 &&
		e.Discoveries == 0 && len(e.Achievements) == 0 &&
		len(e.FailedHarvests) == 0 && e.GoldenHarvests == 0 &&
		!e.GiftArrived && e.EventStarted == "" && e.EventEnded == "" &&
		len(e.CritterVisits) == 0 && e.ScarecrowCoins == 0 &&
		e.ContractCompleted == ""
}

// Advance simulates the state from its UpdatedAt to the given timestamp.
func Advance(s *State, c *content.Content, to int64) Events {
	ev := Events{
		Matured:        map[string]int{},
		AutoHarvested:  map[string]int{},
		FailedHarvests: map[string]int{},
	}
	if to <= s.UpdatedAt {
		return ev
	}
	from := s.UpdatedAt
	elapsed := to - from
	ev.Elapsed = elapsed
	activeContract, contractsDone := s.ActiveContract, s.ContractsCompleted

	online := elapsed <= onlineTickThreshold

	// Clockwork Denied removes event effects even if a defensive/corrupt save
	// somehow enters the contract with one already running.
	if s.contractBlocksEvents() && s.EventID != "" {
		s.endEvent(c, from)
	}

	// Expire finished events. The plots are rebased at the moment the event
	// actually ended — not at `to`, which after an offline gap can be hours
	// later — so growth earned at event speed is kept at its true fraction.
	if s.EventID != "" && to >= s.EventEndsAt {
		ev.EventEnded = s.EventID
		s.endEvent(c, min(max(s.EventEndsAt, from), to))
	}

	// Roll gifts and events before plot simulation.
	if online && !s.contractBlocksEvents() {
		s.rollOnlineEvent(c, elapsed, to, &ev)
	}
	if !s.contractBlocksGifts() {
		s.rollGiftArrival(c, elapsed, online, to, &ev)
	}

	s.advanceScarecrows(c, elapsed, online, &ev)

	for i := range s.Plots {
		plot := &s.Plots[i]
		if plot.Crop == "" {
			if online {
				s.maybeCritterVisit(c, plot, &ev)
			}
			continue
		}
		crop := c.Crop(plot.Crop)
		if crop == nil {
			continue
		}
		grow := s.GrowSeconds(c, crop)
		matureAt := plot.PlantedAt + grow

		if !plot.AutoHarvest || s.contractBlocksAutomation() {
			if matureAt > from && matureAt <= to {
				ev.Matured[crop.ID]++
			}
			continue
		}

		cycles := 0
		for matureAt <= to {
			if cycles >= autoCycleCap {
				// A seasonal crop whose window has closed must not be
				// batch-estimated past the season: fall through to the
				// per-cycle path, which harvests once and leaves the plot
				// empty via the replant check below.
				if plot.AutoSow && s.Coins >= s.SeedCost(c, crop) && SeasonalPlantable(crop.Unlock, to) {
					gross := expectedPayout(s, c, crop, matureAt)
					seedCost := s.SeedCost(c, crop)
					if gross > seedCost {
						remaining := (to - matureAt) / grow
						n := remaining + 1
						totalGross := satMul(gross, n)
						totalSeeds := satMul(seedCost, n)
						s.credit(totalGross)
						if totalSeeds > s.Coins {
							totalSeeds = s.Coins
						}
						s.Coins -= totalSeeds
						ev.AutoCoins = satAdd(ev.AutoCoins, totalGross-totalSeeds)
						ev.AutoHarvested[crop.ID] += int(n)
						s.LifetimeHarvests = satAdd(s.LifetimeHarvests, n)
						plot.PlantedAt = matureAt + remaining*grow
						matureAt = plot.PlantedAt + grow
						continue
					}
				}
				// Batching doesn't apply (unprofitable crop or empty wallet).
				// Stop here so one Advance stays bounded; PlantedAt is the
				// loop's cursor, so later Advances settle the remainder.
				break
			}
			payout, discovery, failed, golden := s.harvestPayout(c, crop, matureAt)
			s.credit(payout)
			ev.AutoCoins = satAdd(ev.AutoCoins, payout)
			if failed {
				ev.FailedHarvests[crop.ID]++
			}
			if golden {
				ev.GoldenHarvests++
			}
			if discovery > 0 {
				s.credit(discovery)
				ev.Discoveries++
				ev.DiscoveryCoins += discovery
			}
			s.LifetimeHarvests = satAdd(s.LifetimeHarvests, 1)
			ev.AutoHarvested[crop.ID]++

			// Replant the queued crop (AutoSowCrop) if one is set, else the crop
			// just harvested. Switching crops re-derives grow time so subsequent
			// cycles in this Advance use the new crop's timing.
			if plot.AutoSow {
				sow := crop
				if next := plot.SowCrop(); next != crop.ID {
					if nc := c.Crop(next); nc != nil {
						sow = nc
					}
				}
				if s.Coins >= s.SeedCost(c, sow) && s.Unlocked(sow.Unlock) && SeasonalPlantable(sow.Unlock, to) {
					s.Coins -= s.SeedCost(c, sow)
					ev.AutoCoins -= s.SeedCost(c, sow)
					if sow.ID != crop.ID {
						crop = sow
						grow = s.GrowSeconds(c, crop)
						plot.Crop = crop.ID
					}
					plot.PlantedAt = matureAt
					matureAt += grow
					cycles++
					continue
				}
			}
			plot.Crop = ""
			plot.PlantedAt = 0
			break
		}
	}

	s.UpdatedAt = to
	ev.Achievements = s.CheckAchievements(c, to)
	if s.ContractsCompleted > contractsDone {
		ev.ContractCompleted = activeContract
	}

	if !online && elapsed > 60 {
		ev.AwayVignettes = awayVignettes(s, c, &ev)
	}
	return ev
}

// endEvent clears the running event and rebases every growing plot so the
// growth it has earned survives the change in grow time.
//
// Progress is (now − PlantedAt) / GrowSeconds, so merely dropping the event
// doubles the denominator of a Warm Front and every plant slides backwards:
// a 300s crop with 3s left at event speed (147s of 150s) would suddenly read
// 147s of 300s and need another 153s. Instead PlantedAt is moved so the same
// fraction of the (now longer) grow time is already behind it, then the plant
// carries on at normal speed from there. at is when the event really ended.
func (s *State) endEvent(c *content.Content, at int64) {
	if s.EventID == "" {
		s.EventStartedAt, s.EventEndsAt = 0, 0
		return
	}
	fast := make([]int64, len(s.Plots))
	for i := range s.Plots {
		if crop := c.Crop(s.Plots[i].Crop); crop != nil {
			fast[i] = s.GrowSeconds(c, crop)
		}
	}
	s.EventID, s.EventStartedAt, s.EventEndsAt = "", 0, 0
	for i := range s.Plots {
		p := &s.Plots[i]
		crop := c.Crop(p.Crop)
		if crop == nil || fast[i] == 0 {
			continue
		}
		cycling := p.AutoHarvest && !s.contractBlocksAutomation()
		p.PlantedAt = rebasePlantedAt(p.PlantedAt, at, fast[i], s.GrowSeconds(c, crop), cycling)
	}
}

// rebasePlantedAt returns the PlantedAt that keeps a plot's progress fraction
// at `at` when its grow time changes from fast to slow seconds.
//
//   - A plot still growing keeps its fraction. Rounding favours the player but
//     never carries a plot across the ready line, so it cannot mature early.
//   - A mature plot that is waiting to be harvested keeps the moment it became
//     ready; it must not turn unripe again because the event ended.
//   - An auto-harvest plot keeps its whole (possibly multi-cycle) overshoot,
//     scaled, because PlantedAt is the cursor the catch-up loop settles cycles
//     from and the cycles already earned at event speed must still be paid.
func rebasePlantedAt(planted, at, fast, slow int64, cycling bool) int64 {
	elapsed := at - planted
	if elapsed <= 0 || fast == slow {
		return planted
	}
	if elapsed >= fast && !cycling {
		return planted + fast - slow
	}
	scaled := (satMul(elapsed, slow) + fast - 1) / fast
	if elapsed < fast && scaled >= slow {
		scaled = slow - 1
	}
	return at - scaled
}

func awayVignettes(s *State, c *content.Content, ev *Events) []string {
	var lines []string
	if totalCount(ev.Matured) > 0 {
		lines = append(lines, "The fields kept growing while you were gone.")
	}
	if totalCount(ev.AutoHarvested) > 0 {
		lines = append(lines, "Your automated plots worked without rest.")
	}
	for id, n := range ev.FailedHarvests {
		name := id
		if crop := c.Crop(id); crop != nil {
			name = crop.Name
		}
		if n == 1 {
			lines = append(lines, "A "+name+" crop failed in the night.")
		} else {
			lines = append(lines, "Several "+name+" crops failed in the night.")
		}
	}
	if ev.GiftArrived {
		lines = append(lines, "A parcel arrived at the gate while you were away.")
	}
	if ev.ScarecrowCoins > 0 {
		lines = append(lines, "Your scarecrows kept watch; their pouches are waiting.")
	}
	if len(lines) == 0 {
		vignettes := []string{
			"A fox watched your turnips. It did not help.",
			"The scarecrow held its post admirably.",
			"Dew settled on empty furrows like silver coins.",
			"A distant owl hooted approval at your patience.",
		}
		idx := int(s.UpdatedAt % int64(len(vignettes)))
		lines = append(lines, vignettes[idx])
	}
	return lines
}

func totalCount(m map[string]int) int {
	t := 0
	for _, n := range m {
		t += n
	}
	return t
}

func (s *State) rollGiftArrival(c *content.Content, elapsed int64, online bool, to int64, ev *Events) {
	if s.GiftPending {
		return
	}
	interval := c.Gifts.OfflineIntervalSec
	if online {
		interval = c.Gifts.OnlineIntervalSec
		bonus := s.upgradeEffectSum(c, "gift_rate_pct")
		if bonus > 0 {
			interval = interval * 100 / (100 + bonus)
		}
	}
	if interval < 1 {
		return
	}
	// Basis points (1/100 %): percent resolution would floor to zero for any
	// interval over 100× the elapsed span, silencing online gifts entirely.
	chance := elapsed * 10000 / interval
	if chance > 10000 {
		chance = 10000
	}
	if chance > 0 && s.rollBp() < chance {
		s.GiftPending = true
		s.GiftArrivedAt = to
		ev.GiftArrived = true
	}
}

func (s *State) rollOnlineEvent(c *content.Content, elapsed, to int64, ev *Events) {
	if len(c.Events) == 0 {
		return
	}
	if s.EventActive(to) {
		return
	}
	ec := c.EventsConfig
	minI, maxI := ec.MinIntervalSec, ec.MaxIntervalSec
	bonus := s.upgradeEffectSum(c, "event_rate_pct")
	if bonus > 0 {
		minI = minI * 100 / (100 + bonus)
		maxI = maxI * 100 / (100 + bonus)
	}
	if minI < 1 {
		minI = 1
	}
	if maxI < minI {
		maxI = minI
	}
	span := maxI - minI
	interval := minI
	if span > 0 {
		interval += s.rollRange(0, span)
	}
	// Per-tick chance in basis points for a ~interval-second mean wait.
	// Percent resolution would floor to 0 here and a 1% floor made events
	// fire every ~100s instead of the configured 15–25 minutes.
	chance := elapsed * 10000 / interval
	if chance < 1 {
		chance = 1
	}
	if s.rollBp() >= chance {
		return
	}
	idx := int(s.rollRange(0, int64(len(c.Events)-1)))
	evDef := c.Events[idx]
	s.EventID = evDef.ID
	durMin, durMax := ec.MinDurationSec, ec.MaxDurationSec
	durBonus := s.upgradeEffectSum(c, "event_duration_pct") + int64(s.UpgradeLevel("stargazer"))*10
	if durBonus > 0 {
		durMin = durMin * (100 + durBonus) / 100
		durMax = durMax * (100 + durBonus) / 100
	}
	dur := durMin
	if durMax > durMin {
		dur += s.rollRange(0, durMax-durMin)
	}
	s.EventStartedAt = to
	s.EventEndsAt = to + dur
	ev.EventStarted = evDef.ID
}

func (s *State) maybeCritterVisit(c *content.Content, plot *Plot, ev *Events) {
	if !s.CrittersEnabled {
		return
	}
	if plot.Critter != "" || len(c.Critters.Kinds) == 0 {
		return
	}
	if c.Critters.VisitChancePct <= 0 {
		return
	}
	// Honor the active-critter cap (<=0 means unlimited).
	if limit := c.Critters.MaxActive; limit > 0 && s.activeCritterCount() >= limit {
		return
	}
	if s.roll100() >= c.Critters.VisitChancePct {
		return
	}
	idx := int(s.rollRange(0, int64(len(c.Critters.Kinds)-1)))
	plot.Critter = c.Critters.Kinds[idx]
	ev.CritterVisits = append(ev.CritterVisits, plot.Critter)
}

// activeCritterCount counts plots currently hosting a critter.
func (s *State) activeCritterCount() int64 {
	var n int64
	for i := range s.Plots {
		if s.Plots[i].Critter != "" {
			n++
		}
	}
	return n
}

// harvestPayout rolls one harvest of crop.
func (s *State) harvestPayout(c *content.Content, crop *content.Crop, now int64) (payout, discovery int64, failed, golden bool) {
	base := crop.SellValue
	if crop.Archetype == "risky" {
		if s.roll100() < crop.FailChancePct {
			base = crop.SellValue * SalvageNumerator(s.SeedUpgradeLevel(crop.ID)) / 8
			failed = true
		}
	}
	payout = s.sellMultiplied(c, base, crop.ID, now)

	if c.GoldenHarvest.ChancePct > 0 && s.roll100() < c.GoldenHarvest.ChancePct {
		mult := c.GoldenHarvest.Multiplier
		if mult < 10 {
			mult = 10
		}
		payout = payout * mult / 10
		golden = true
	}

	if c.Flavor.Enabled && s.FlavorEnabled && c.Flavor.DiscoveryChancePct > 0 {
		if s.roll100() < c.Flavor.DiscoveryChancePct {
			discovery = s.rollRange(c.Flavor.DiscoveryMin, c.Flavor.DiscoveryMax)
		}
	}
	return payout, discovery, failed, golden
}

// expectedPayout is the integer expected value of one harvest for batched catch-up.
func expectedPayout(s *State, c *content.Content, crop *content.Crop, now int64) int64 {
	if crop.Archetype != "risky" {
		return s.sellMultiplied(c, crop.SellValue, crop.ID, now)
	}
	failPct := crop.FailChancePct
	salvage := crop.SellValue * SalvageNumerator(s.SeedUpgradeLevel(crop.ID)) / 8
	successPct := 100 - failPct
	base := (failPct*salvage + successPct*crop.SellValue) / 100
	return s.sellMultiplied(c, base, crop.ID, now)
}

// CheckAchievements records any newly satisfied achievement conditions.
func (s *State) CheckAchievements(c *content.Content, now int64) []string {
	var newly []string
	for _, a := range c.Achievements {
		if _, earned := s.Achievements[a.ID]; earned {
			continue
		}
		var met bool
		switch a.Condition.Kind {
		case "harvests":
			met = s.LifetimeHarvests >= a.Condition.Value
		case "earnings":
			met = s.LifetimeEarnings >= a.Condition.Value
		case "plots":
			met = int64(len(s.Plots)) >= a.Condition.Value
		case "rebirths":
			met = s.Rebirths >= a.Condition.Value
		case "balance":
			met = s.Coins >= a.Condition.Value
		}
		if met {
			s.Achievements[a.ID] = now
			newly = append(newly, a.ID)
		}
	}
	return newly
}
