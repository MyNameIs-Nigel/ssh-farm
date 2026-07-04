// Package content loads and validates the data-driven game definitions
// (crops, balance, upgrades, tools, zones, achievements, flavor). Content is
// configuration, not code: it is validated strictly on load so the server
// fails fast on a bad file instead of running with a broken economy.
package content

import (
	"fmt"
	"io/fs"
	"os"

	"github.com/BurntSushi/toml"

	"github.com/mynameis-nigel/ssh-farm/data"
)

// Unlock gates a crop, tool, or zone behind a progression condition.
type Unlock struct {
	Kind  string `toml:"kind"`  // "start", "earnings", "prestige", "zone"
	Value int64  `toml:"value"` // threshold for earnings/prestige kinds
	Zone  string `toml:"zone"`  // zone id for the zone kind
}

// Crop is one plantable crop definition.
type Crop struct {
	ID            string `toml:"id"`
	Name          string `toml:"name"`
	Archetype     string `toml:"archetype"` // "fast", "slow", "risky"
	SeedCost      int64  `toml:"seed_cost"`
	GrowSeconds   int64  `toml:"grow_seconds"`
	SellValue     int64  `toml:"sell_value"`
	FailChancePct int64  `toml:"fail_chance_pct"` // risky only: chance of salvage vs full sell
	Unlock        Unlock `toml:"unlock"`
}

// Upgrade is a permanent Starseed-bought bonus.
type Upgrade struct {
	ID            string `toml:"id"`
	Name          string `toml:"name"`
	Description   string `toml:"description"`
	Cost          int64  `toml:"cost"`
	CostGrowthPct int64  `toml:"cost_growth_pct"`
	MaxLevel      int    `toml:"max_level"`
	Effect        string `toml:"effect"`
	EffectValue   int64  `toml:"effect_value"`
}

// Multiplier is a run-scoped coin-purchased bonus (Market tab).
type Multiplier struct {
	ID            string `toml:"id"`
	Name          string `toml:"name"`
	Description   string `toml:"description"`
	Cost          int64  `toml:"cost"`
	CostGrowthPct int64  `toml:"cost_growth_pct"`
	MaxLevel      int    `toml:"max_level"`
	Effect        string `toml:"effect"` // grow_speed_pct, sell_bonus_pct
	EffectValue   int64  `toml:"effect_value"`
}

// SeedUpgrade is a per-risky-crop Hardier Strain upgrade (Market tab).
type SeedUpgrade struct {
	ID            string `toml:"id"`
	CropID        string `toml:"crop_id"`
	Name          string `toml:"name"`
	Description   string `toml:"description"`
	Cost          int64  `toml:"cost"`
	CostGrowthPct int64  `toml:"cost_growth_pct"`
	MaxLevel      int    `toml:"max_level"` // 3 levels → salvage 1/4, 1/2, 3/4
}

// Zone is a run-scoped farm expansion.
type Zone struct {
	ID          string `toml:"id"`
	Name        string `toml:"name"`
	Description string `toml:"description"`
	Cost        int64  `toml:"cost"`
	ExtraPlots  int    `toml:"extra_plots"`
	Unlock      Unlock `toml:"unlock"`
}

// Event is a random online event type.
type Event struct {
	ID          string `toml:"id"`
	Name        string `toml:"name"`
	Description string `toml:"description"`
	Effect      string `toml:"effect"` // seed_discount_pct, sell_bonus_pct, grow_speed_pct
	EffectValue int64  `toml:"effect_value"`
}

// EventsConfig tunes random event frequency and duration.
type EventsConfig struct {
	MinIntervalSec int64 `toml:"min_interval_sec"`
	MaxIntervalSec int64 `toml:"max_interval_sec"`
	MinDurationSec int64 `toml:"min_duration_sec"`
	MaxDurationSec int64 `toml:"max_duration_sec"`
}

// PlotAutomation configures per-plot automation purchase costs.
type PlotAutomation struct {
	AutoHarvestBaseCost  int64 `toml:"auto_harvest_base_cost"`
	AutoHarvestGrowthPct int64 `toml:"auto_harvest_growth_pct"`
	AutoSowCost          int64 `toml:"auto_sow_cost"`
	AutoSowMinEarnings   int64 `toml:"auto_sow_min_earnings"`
}

// GiftTuning configures gift package arrival and rewards.
type GiftTuning struct {
	OnlineIntervalSec  int64 `toml:"online_interval_sec"`
	OfflineIntervalSec int64 `toml:"offline_interval_sec"`
	StarseedChancePct  int64 `toml:"starseed_chance_pct"`
	CoinRewardFloor    int64 `toml:"coin_reward_floor"`
	CoinRewardCeiling  int64 `toml:"coin_reward_ceiling"`
}

// GoldenHarvest tunes the rare golden harvest bonus.
type GoldenHarvest struct {
	ChancePct  int64 `toml:"chance_pct"`
	Multiplier int64 `toml:"multiplier"` // payout × multiplier / 10 (10× at 100)
}

// MoonConfig tunes the cosmetic moon cycle and Moonberry bonus.
type MoonConfig struct {
	CycleDays            int64  `toml:"cycle_days"`
	FullMoonSellBonusPct int64  `toml:"full_moon_sell_bonus_pct"`
	MoonberryCropID      string `toml:"moonberry_crop_id"`
}

// CritterConfig tunes cosmetic critter visits on empty plots.
type CritterConfig struct {
	VisitChancePct int64    `toml:"visit_chance_pct"`
	MaxActive      int64    `toml:"max_active"` // cap on simultaneous critters; <=0 means unlimited
	ShooRewardMin  int64    `toml:"shoo_reward_min"`
	ShooRewardMax  int64    `toml:"shoo_reward_max"`
	Kinds          []string `toml:"kind"`
}

// ScarecrowConfig tunes the run-scoped scarecrow market purchase.
type ScarecrowConfig struct {
	Cost                int64 `toml:"cost"`                   // coin price (<=0 disables the item)
	OfflineCoinsPerHour int64 `toml:"offline_coins_per_hour"` // passive trickle while away
}

// Headline is a one-line flavor line for the Daily Furrow banner.
type Headline struct {
	Text string `toml:"text"`
}

// Labels holds player-facing names for currencies and similar.
type Labels struct {
	StarseedName string `toml:"starseed_name"`
	StarseedDesc string `toml:"starseed_desc"`
}

// Condition triggers an achievement.
type Condition struct {
	Kind  string `toml:"kind"` // harvests, earnings, plots, rebirths, balance
	Value int64  `toml:"value"`
}

// Achievement is a positive-only milestone.
type Achievement struct {
	ID          string    `toml:"id"`
	Name        string    `toml:"name"`
	Description string    `toml:"description"`
	Condition   Condition `toml:"condition"`
}

// Flavor configures the optional ambient discoveries.
type Flavor struct {
	Enabled            bool  `toml:"enabled"`
	DiscoveryChancePct int64 `toml:"discovery_chance_pct"`
	DiscoveryMin       int64 `toml:"discovery_min"`
	DiscoveryMax       int64 `toml:"discovery_max"`
}

// Start configures a fresh run.
type Start struct {
	Coins int64 `toml:"coins"`
	Plots int   `toml:"plots"`
}

// Land configures plot expansion costs.
type Land struct {
	BasePlotCost int64 `toml:"base_plot_cost"`
	GrowthPct    int64 `toml:"growth_pct"`
	MaxPlots     int   `toml:"max_plots"`
}

// Prestige configures the rebirth formula.
type Prestige struct {
	Divisor     int64 `toml:"divisor"`
	MinEarnings int64 `toml:"min_earnings"`
}

type cropsFile struct {
	Crops []Crop `toml:"crop"`
}

type balanceFile struct {
	Start          Start           `toml:"start"`
	Land           Land            `toml:"land"`
	Prestige       Prestige        `toml:"prestige"`
	Labels         Labels          `toml:"labels"`
	Upgrades       []Upgrade       `toml:"upgrade"`
	Multipliers    []Multiplier    `toml:"multiplier"`
	SeedUpgrades   []SeedUpgrade   `toml:"seed_upgrade"`
	Zones          []Zone          `toml:"zone"`
	Events         []Event         `toml:"event"`
	EventsConfig   EventsConfig    `toml:"events"`
	PlotAutomation PlotAutomation  `toml:"plot_automation"`
	Gifts          GiftTuning      `toml:"gifts"`
	GoldenHarvest  GoldenHarvest   `toml:"golden_harvest"`
	Moon           MoonConfig      `toml:"moon"`
	Critters       CritterConfig   `toml:"critters"`
	Scarecrow      ScarecrowConfig `toml:"scarecrow"`
	Headlines      []Headline      `toml:"headline"`
	Achievements   []Achievement   `toml:"achievement"`
	Flavor         Flavor          `toml:"flavor"`
}

// Content is the validated, immutable game configuration. Lookup maps are
// built once at load; treat the whole struct as read-only after Load.
type Content struct {
	Crops          []Crop
	Upgrades       []Upgrade
	Multipliers    []Multiplier
	SeedUpgrades   []SeedUpgrade
	Zones          []Zone
	Events         []Event
	Headlines      []Headline
	Achievements   []Achievement
	Start          Start
	Land           Land
	Prestige       Prestige
	Labels         Labels
	EventsConfig   EventsConfig
	PlotAutomation PlotAutomation
	Gifts          GiftTuning
	GoldenHarvest  GoldenHarvest
	Moon           MoonConfig
	Critters       CritterConfig
	Scarecrow      ScarecrowConfig
	Flavor         Flavor

	cropByID          map[string]*Crop
	upgradeByID       map[string]*Upgrade
	multiplierByID    map[string]*Multiplier
	seedUpgradeByID   map[string]*SeedUpgrade
	seedUpgradeByCrop map[string]*SeedUpgrade
	zoneByID          map[string]*Zone
	eventByID         map[string]*Event
}

// Load reads content from overrideDir when set, otherwise from the embedded
// files, and validates everything.
func Load(overrideDir string) (*Content, error) {
	var fsys fs.FS = data.FS
	if overrideDir != "" {
		fsys = os.DirFS(overrideDir)
	}

	var cf cropsFile
	if err := decodeTOML(fsys, "crops.toml", &cf); err != nil {
		return nil, err
	}
	var bf balanceFile
	if err := decodeTOML(fsys, "balance.toml", &bf); err != nil {
		return nil, err
	}

	c := &Content{
		Crops:          cf.Crops,
		Upgrades:       bf.Upgrades,
		Multipliers:    bf.Multipliers,
		SeedUpgrades:   bf.SeedUpgrades,
		Zones:          bf.Zones,
		Events:         bf.Events,
		Headlines:      bf.Headlines,
		Achievements:   bf.Achievements,
		Start:          bf.Start,
		Land:           bf.Land,
		Prestige:       bf.Prestige,
		Labels:         bf.Labels,
		EventsConfig:   bf.EventsConfig,
		PlotAutomation: bf.PlotAutomation,
		Gifts:          bf.Gifts,
		GoldenHarvest:  bf.GoldenHarvest,
		Moon:           bf.Moon,
		Critters:       bf.Critters,
		Scarecrow:      bf.Scarecrow,
		Flavor:         bf.Flavor,
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	c.buildIndexes()
	return c, nil
}

func decodeTOML(fsys fs.FS, name string, v any) error {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		return fmt.Errorf("content: read %s: %w", name, err)
	}
	if err := toml.Unmarshal(b, v); err != nil {
		return fmt.Errorf("content: parse %s: %w", name, err)
	}
	return nil
}

func (c *Content) buildIndexes() {
	c.cropByID = make(map[string]*Crop, len(c.Crops))
	for i := range c.Crops {
		c.cropByID[c.Crops[i].ID] = &c.Crops[i]
	}
	c.upgradeByID = make(map[string]*Upgrade, len(c.Upgrades))
	for i := range c.Upgrades {
		c.upgradeByID[c.Upgrades[i].ID] = &c.Upgrades[i]
	}
	c.multiplierByID = make(map[string]*Multiplier, len(c.Multipliers))
	for i := range c.Multipliers {
		c.multiplierByID[c.Multipliers[i].ID] = &c.Multipliers[i]
	}
	c.seedUpgradeByID = make(map[string]*SeedUpgrade, len(c.SeedUpgrades))
	c.seedUpgradeByCrop = make(map[string]*SeedUpgrade, len(c.SeedUpgrades))
	for i := range c.SeedUpgrades {
		c.seedUpgradeByID[c.SeedUpgrades[i].ID] = &c.SeedUpgrades[i]
		c.seedUpgradeByCrop[c.SeedUpgrades[i].CropID] = &c.SeedUpgrades[i]
	}
	c.zoneByID = make(map[string]*Zone, len(c.Zones))
	for i := range c.Zones {
		c.zoneByID[c.Zones[i].ID] = &c.Zones[i]
	}
	c.eventByID = make(map[string]*Event, len(c.Events))
	for i := range c.Events {
		c.eventByID[c.Events[i].ID] = &c.Events[i]
	}
}

// Crop returns the crop definition for id, or nil.
func (c *Content) Crop(id string) *Crop { return c.cropByID[id] }

// UpgradeByID returns the upgrade definition for id, or nil.
func (c *Content) UpgradeByID(id string) *Upgrade { return c.upgradeByID[id] }

// MultiplierByID returns the multiplier definition for id, or nil.
func (c *Content) MultiplierByID(id string) *Multiplier { return c.multiplierByID[id] }

// SeedUpgradeByID returns the seed upgrade definition for id, or nil.
func (c *Content) SeedUpgradeByID(id string) *SeedUpgrade { return c.seedUpgradeByID[id] }

// SeedUpgradeForCrop returns the Hardier Strain upgrade for a crop, or nil.
func (c *Content) SeedUpgradeForCrop(cropID string) *SeedUpgrade { return c.seedUpgradeByCrop[cropID] }

// ZoneByID returns the zone definition for id, or nil.
func (c *Content) ZoneByID(id string) *Zone { return c.zoneByID[id] }

// EventByID returns the event definition for id, or nil.
func (c *Content) EventByID(id string) *Event { return c.eventByID[id] }

// StarseedLabel returns the player-facing name for prestige currency.
func (c *Content) StarseedLabel() string {
	if c.Labels.StarseedName != "" {
		return c.Labels.StarseedName
	}
	return "Starseeds"
}

func (c *Content) validate() error {
	if len(c.Crops) == 0 {
		return fmt.Errorf("content: no crops defined")
	}
	if c.Start.Coins < 0 || c.Start.Plots < 1 {
		return fmt.Errorf("content: start must have coins >= 0 and plots >= 1")
	}
	if c.Land.BasePlotCost < 1 || c.Land.GrowthPct < 100 {
		return fmt.Errorf("content: land base_plot_cost must be >= 1 and growth_pct >= 100")
	}
	if c.Land.MaxPlots < c.Start.Plots {
		return fmt.Errorf("content: land max_plots (%d) below start plots (%d)", c.Land.MaxPlots, c.Start.Plots)
	}
	if c.Prestige.Divisor < 1 {
		return fmt.Errorf("content: prestige divisor must be >= 1")
	}
	if c.Prestige.MinEarnings < 1 {
		return fmt.Errorf("content: prestige min_earnings must be >= 1")
	}

	zoneIDs := map[string]bool{}
	for i, z := range c.Zones {
		if err := requireID("zone", i, z.ID, z.Name); err != nil {
			return err
		}
		if zoneIDs[z.ID] {
			return fmt.Errorf("content: duplicate zone id %q", z.ID)
		}
		zoneIDs[z.ID] = true
	}
	for _, z := range c.Zones {
		if z.Unlock.Kind == "zone" && z.Unlock.Zone == z.ID {
			return fmt.Errorf("content: zone %q cannot be unlocked by itself", z.ID)
		}
		if z.Cost < 0 || z.ExtraPlots < 1 {
			return fmt.Errorf("content: zone %q needs cost >= 0 and extra_plots >= 1", z.ID)
		}
		if err := validateUnlock("zone", z.ID, z.Unlock, zoneIDs); err != nil {
			return err
		}
	}

	archetypes := map[string]bool{"fast": false, "slow": false, "risky": false}
	cropIDs := map[string]bool{}
	for i, cr := range c.Crops {
		if err := requireID("crop", i, cr.ID, cr.Name); err != nil {
			return err
		}
		if cropIDs[cr.ID] {
			return fmt.Errorf("content: duplicate crop id %q", cr.ID)
		}
		cropIDs[cr.ID] = true
		if _, ok := archetypes[cr.Archetype]; !ok {
			return fmt.Errorf("content: crop %q has unknown archetype %q", cr.ID, cr.Archetype)
		}
		archetypes[cr.Archetype] = true
		if cr.SeedCost < 0 || cr.SellValue < 0 || cr.GrowSeconds < 1 {
			return fmt.Errorf("content: crop %q needs seed_cost >= 0, sell_value >= 0, grow_seconds >= 1", cr.ID)
		}
		if cr.Archetype == "risky" {
			if cr.FailChancePct < 1 || cr.FailChancePct > 99 {
				return fmt.Errorf("content: crop %q fail_chance_pct must be 1-99", cr.ID)
			}
		} else if cr.FailChancePct != 0 {
			return fmt.Errorf("content: crop %q sets fail_chance_pct but is not risky", cr.ID)
		}
		if err := validateUnlock("crop", cr.ID, cr.Unlock, zoneIDs); err != nil {
			return err
		}
	}
	for a, present := range archetypes {
		if !present {
			return fmt.Errorf("content: no crop with archetype %q (all three are required)", a)
		}
	}

	upgradeIDs := map[string]bool{}
	validUpgradeEffects := map[string]bool{
		"grow_speed_pct": true, "sell_bonus_pct": true,
		"start_coins": true, "plot_discount_pct": true,
		"gift_rate_pct": true, "event_rate_pct": true, "event_duration_pct": true,
	}
	for i, u := range c.Upgrades {
		if err := requireID("upgrade", i, u.ID, u.Name); err != nil {
			return err
		}
		if upgradeIDs[u.ID] {
			return fmt.Errorf("content: duplicate upgrade id %q", u.ID)
		}
		upgradeIDs[u.ID] = true
		if !validUpgradeEffects[u.Effect] {
			return fmt.Errorf("content: upgrade %q has unknown effect %q", u.ID, u.Effect)
		}
		if u.Cost < 1 || u.CostGrowthPct < 100 || u.MaxLevel < 1 || u.EffectValue < 1 {
			return fmt.Errorf("content: upgrade %q needs cost >= 1, cost_growth_pct >= 100, max_level >= 1, effect_value >= 1", u.ID)
		}
		if u.Effect == "grow_speed_pct" || u.Effect == "plot_discount_pct" {
			if u.EffectValue*int64(u.MaxLevel) >= 100 {
				return fmt.Errorf("content: upgrade %q would reach a %d%% reduction at max level; must stay below 100%%", u.ID, u.EffectValue*int64(u.MaxLevel))
			}
		}
	}

	multIDs := map[string]bool{}
	validMultEffects := map[string]bool{"grow_speed_pct": true, "sell_bonus_pct": true}
	for i, m := range c.Multipliers {
		if err := requireID("multiplier", i, m.ID, m.Name); err != nil {
			return err
		}
		if multIDs[m.ID] {
			return fmt.Errorf("content: duplicate multiplier id %q", m.ID)
		}
		multIDs[m.ID] = true
		if !validMultEffects[m.Effect] {
			return fmt.Errorf("content: multiplier %q has unknown effect %q", m.ID, m.Effect)
		}
		if m.Cost < 1 || m.CostGrowthPct < 100 || m.MaxLevel < 1 || m.EffectValue < 1 {
			return fmt.Errorf("content: multiplier %q needs cost >= 1, cost_growth_pct >= 100, max_level >= 1, effect_value >= 1", m.ID)
		}
		if m.Effect == "grow_speed_pct" && m.EffectValue*int64(m.MaxLevel) >= 100 {
			return fmt.Errorf("content: multiplier %q grow reduction would reach 100%% at max level", m.ID)
		}
	}

	seedUpIDs := map[string]bool{}
	for i, su := range c.SeedUpgrades {
		if err := requireID("seed_upgrade", i, su.ID, su.Name); err != nil {
			return err
		}
		if seedUpIDs[su.ID] {
			return fmt.Errorf("content: duplicate seed_upgrade id %q", su.ID)
		}
		seedUpIDs[su.ID] = true
		if su.CropID == "" {
			return fmt.Errorf("content: seed_upgrade %q needs crop_id", su.ID)
		}
		var crop *Crop
		for i := range c.Crops {
			if c.Crops[i].ID == su.CropID {
				crop = &c.Crops[i]
				break
			}
		}
		if crop == nil {
			return fmt.Errorf("content: seed_upgrade %q references unknown crop %q", su.ID, su.CropID)
		}
		if crop.Archetype != "risky" {
			return fmt.Errorf("content: seed_upgrade %q must target a risky crop", su.ID)
		}
		if su.MaxLevel != 3 {
			return fmt.Errorf("content: seed_upgrade %q max_level must be 3", su.ID)
		}
		if su.Cost < 1 || su.CostGrowthPct < 100 {
			return fmt.Errorf("content: seed_upgrade %q needs cost >= 1 and cost_growth_pct >= 100", su.ID)
		}
	}

	eventIDs := map[string]bool{}
	validEventEffects := map[string]bool{
		"seed_discount_pct": true, "sell_bonus_pct": true, "grow_speed_pct": true,
	}
	for i, ev := range c.Events {
		if err := requireID("event", i, ev.ID, ev.Name); err != nil {
			return err
		}
		if eventIDs[ev.ID] {
			return fmt.Errorf("content: duplicate event id %q", ev.ID)
		}
		eventIDs[ev.ID] = true
		if !validEventEffects[ev.Effect] {
			return fmt.Errorf("content: event %q has unknown effect %q", ev.ID, ev.Effect)
		}
		if ev.EffectValue < 1 {
			return fmt.Errorf("content: event %q effect_value must be >= 1", ev.ID)
		}
	}

	ec := c.EventsConfig
	if ec.MinIntervalSec < 1 || ec.MaxIntervalSec < ec.MinIntervalSec {
		return fmt.Errorf("content: events interval config invalid")
	}
	if ec.MinDurationSec < 1 || ec.MaxDurationSec < ec.MinDurationSec {
		return fmt.Errorf("content: events duration config invalid")
	}

	pa := c.PlotAutomation
	if pa.AutoHarvestBaseCost < 1 || pa.AutoHarvestGrowthPct < 100 {
		return fmt.Errorf("content: plot_automation harvest costs invalid")
	}
	if pa.AutoSowCost < 1 || pa.AutoSowMinEarnings < 1 {
		return fmt.Errorf("content: plot_automation sow costs invalid")
	}

	gt := c.Gifts
	if gt.OnlineIntervalSec < 1 || gt.OfflineIntervalSec < 1 {
		return fmt.Errorf("content: gifts interval config invalid")
	}
	if gt.StarseedChancePct < 0 || gt.StarseedChancePct > 100 {
		return fmt.Errorf("content: gifts starseed_chance_pct must be 0-100")
	}

	gh := c.GoldenHarvest
	if gh.ChancePct < 0 || gh.ChancePct > 100 || gh.Multiplier < 10 {
		return fmt.Errorf("content: golden_harvest config invalid")
	}

	if c.Critters.MaxActive < 0 {
		return fmt.Errorf("content: critters max_active must be >= 0")
	}
	if c.Scarecrow.Cost < 0 || c.Scarecrow.OfflineCoinsPerHour < 0 {
		return fmt.Errorf("content: scarecrow cost and offline_coins_per_hour must be >= 0")
	}

	achIDs := map[string]bool{}
	validConds := map[string]bool{
		"harvests": true, "earnings": true, "plots": true, "rebirths": true, "balance": true,
	}
	for i, a := range c.Achievements {
		if err := requireID("achievement", i, a.ID, a.Name); err != nil {
			return err
		}
		if achIDs[a.ID] {
			return fmt.Errorf("content: duplicate achievement id %q", a.ID)
		}
		achIDs[a.ID] = true
		if !validConds[a.Condition.Kind] {
			return fmt.Errorf("content: achievement %q has unknown condition kind %q", a.ID, a.Condition.Kind)
		}
		if a.Condition.Value < 1 {
			return fmt.Errorf("content: achievement %q condition value must be >= 1", a.ID)
		}
	}

	f := c.Flavor
	if f.Enabled {
		if f.DiscoveryChancePct < 0 || f.DiscoveryChancePct > 100 {
			return fmt.Errorf("content: flavor discovery_chance_pct must be 0-100")
		}
		if f.DiscoveryMin < 0 || f.DiscoveryMax < f.DiscoveryMin {
			return fmt.Errorf("content: flavor discovery range invalid (min %d, max %d)", f.DiscoveryMin, f.DiscoveryMax)
		}
	}

	return nil
}

func requireID(kind string, idx int, id, name string) error {
	if id == "" {
		return fmt.Errorf("content: %s #%d has no id", kind, idx+1)
	}
	if name == "" {
		return fmt.Errorf("content: %s %q has no name", kind, id)
	}
	return nil
}

func validateUnlock(kind, id string, u Unlock, zoneIDs map[string]bool) error {
	switch u.Kind {
	case "", "start":
		return nil
	case "earnings", "prestige":
		if u.Value < 1 {
			return fmt.Errorf("content: %s %q unlock value must be >= 1", kind, id)
		}
		return nil
	case "zone":
		if !zoneIDs[u.Zone] {
			return fmt.Errorf("content: %s %q unlock references unknown zone %q", kind, id, u.Zone)
		}
		return nil
	default:
		return fmt.Errorf("content: %s %q has unknown unlock kind %q", kind, id, u.Kind)
	}
}
