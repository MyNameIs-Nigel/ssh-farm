package content

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmbeddedContentLoadsAndValidates(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatalf("embedded content failed validation: %v", err)
	}
	if len(c.Crops) < 3 {
		t.Fatalf("want at least 3 crops, got %d", len(c.Crops))
	}
	seen := map[string]bool{}
	for _, cr := range c.Crops {
		seen[cr.Archetype] = true
	}
	for _, a := range []string{"fast", "slow", "risky"} {
		if !seen[a] {
			t.Fatalf("embedded crops missing archetype %q", a)
		}
	}
	if c.Crop("turnip") == nil {
		t.Fatal("lookup by id failed")
	}
	if len(c.Upgrades) == 0 || len(c.Multipliers) == 0 || len(c.Zones) == 0 || len(c.Achievements) == 0 {
		t.Fatal("embedded balance is missing progression content")
	}
	if c.StarseedLabel() != "Starseeds" {
		t.Fatalf("starseed label = %q", c.StarseedLabel())
	}
}

func TestOverrideDirIsUsed(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "crops.toml", minimalCrops)
	writeFixture(t, dir, "balance.toml", minimalBalance)

	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Start.Coins != 7 {
		t.Fatalf("override not honored: start coins = %d", c.Start.Coins)
	}
}

func TestRejectsMalformedAndContradictoryContent(t *testing.T) {
	cases := []struct {
		name    string
		crops   string
		balance string
	}{
		{"syntax error", "[[crop]\nid=", minimalBalance},
		{"missing archetype crop", `
[[crop]]
id = "a"
name = "A"
archetype = "fast"
seed_cost = 1
grow_seconds = 1
sell_value = 1
`, minimalBalance},
		{"duplicate crop id", minimalCrops + `
[[crop]]
id = "t"
name = "T2"
archetype = "fast"
seed_cost = 1
grow_seconds = 1
sell_value = 1
`, minimalBalance},
		{"risky fail chance invalid", minimalCrops + `
[[crop]]
id = "bad"
name = "Bad"
archetype = "risky"
seed_cost = 1
grow_seconds = 1
sell_value = 1
fail_chance_pct = 0
`, minimalBalance},
		{"unknown unlock kind", minimalCrops + `
[[crop]]
id = "bad"
name = "Bad"
archetype = "fast"
seed_cost = 1
grow_seconds = 1
sell_value = 1
unlock = { kind = "wishes" }
`, minimalBalance},
		{"zero grow time", minimalCrops + `
[[crop]]
id = "bad"
name = "Bad"
archetype = "fast"
seed_cost = 1
grow_seconds = 0
sell_value = 1
`, minimalBalance},
		{"upgrade reduction reaching 100%", minimalCrops, minimalBalance + `
[[upgrade]]
id = "toofast"
name = "Too Fast"
description = "x"
cost = 1
cost_growth_pct = 100
max_level = 10
effect = "grow_speed_pct"
effect_value = 10
`},
		{"achievement bad condition", minimalCrops, minimalBalance + `
[[achievement]]
id = "bad"
name = "Bad"
description = "x"
condition = { kind = "vibes", value = 1 }
`},
		{"season unlock with unknown season", minimalCrops + `
[[crop]]
id = "bad"
name = "Bad"
archetype = "fast"
seed_cost = 1
grow_seconds = 1
sell_value = 1
unlock = { kind = "season", season = "easter" }
`, minimalBalance},
		{"season unlock with a value", minimalCrops + `
[[crop]]
id = "bad"
name = "Bad"
archetype = "fast"
seed_cost = 1
grow_seconds = 1
sell_value = 1
unlock = { kind = "season", season = "halloween", value = 1 }
`, minimalBalance},
		{"season unlock with a zone", minimalCrops + `
[[crop]]
id = "bad"
name = "Bad"
archetype = "fast"
seed_cost = 1
grow_seconds = 1
sell_value = 1
unlock = { kind = "season", season = "christmas", zone = "greenhouse" }
`, minimalBalance},
		{"rebirths on a prestige unlock", minimalCrops + `
[[crop]]
id = "bad"
name = "Bad"
archetype = "fast"
seed_cost = 1
grow_seconds = 1
sell_value = 1
unlock = { kind = "prestige", value = 1, rebirths = 2 }
`, minimalBalance},
		{"rebirths on a season unlock", minimalCrops + `
[[crop]]
id = "bad"
name = "Bad"
archetype = "fast"
seed_cost = 1
grow_seconds = 1
sell_value = 1
unlock = { kind = "season", season = "halloween", rebirths = 1 }
`, minimalBalance},
		{"negative rebirths", minimalCrops + `
[[crop]]
id = "bad"
name = "Bad"
archetype = "fast"
seed_cost = 1
grow_seconds = 1
sell_value = 1
unlock = { kind = "earnings", value = 10, rebirths = -1 }
`, minimalBalance},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFixture(t, dir, "crops.toml", tc.crops)
			writeFixture(t, dir, "balance.toml", tc.balance)
			if _, err := Load(dir); err == nil {
				t.Fatalf("expected validation error for %s", tc.name)
			}
		})
	}
}

func writeFixture(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

const minimalCrops = `
[[crop]]
id = "t"
name = "T"
archetype = "fast"
seed_cost = 1
grow_seconds = 10
sell_value = 2

[[crop]]
id = "s"
name = "S"
archetype = "slow"
seed_cost = 5
grow_seconds = 100
sell_value = 20

[[crop]]
id = "r"
name = "R"
archetype = "risky"
seed_cost = 3
grow_seconds = 30
sell_value = 10
fail_chance_pct = 20
`

const minimalBalance = `
[start]
coins = 7
plots = 2

[land]
base_plot_cost = 10
growth_pct = 150
max_plots = 5

[prestige]
divisor = 100
min_earnings = 1000

[events]
min_interval_sec = 60
max_interval_sec = 120
min_duration_sec = 30
max_duration_sec = 60

[plot_automation]
auto_harvest_base_cost = 10
auto_harvest_growth_pct = 150
auto_sow_cost = 20
auto_sow_min_earnings = 100

[gifts]
online_interval_sec = 100
offline_interval_sec = 1000
starseed_chance_pct = 10
coin_reward_floor = 1
coin_reward_ceiling = 100

[golden_harvest]
chance_pct = 0
multiplier = 100

[moon]
cycle_days = 28
full_moon_sell_bonus_pct = 10
moonberry_crop_id = "t"

[critters]
visit_chance_pct = 0
shoo_reward_min = 1
shoo_reward_max = 2
kind = ["crow"]

[flavor]
enabled = false
`

func TestSeasonUnlockLoads(t *testing.T) {
	crops := minimalCrops + `
[[crop]]
id = "spook"
name = "Spook"
archetype = "fast"
seed_cost = 1
grow_seconds = 1
sell_value = 2
unlock = { kind = "season", season = "halloween" }
`
	dir := t.TempDir()
	writeFixture(t, dir, "crops.toml", crops)
	writeFixture(t, dir, "balance.toml", minimalBalance)
	c, err := Load(dir)
	if err != nil {
		t.Fatalf("season unlock should validate: %v", err)
	}
	crop := c.Crop("spook")
	if crop == nil || crop.Unlock.Kind != "season" || crop.Unlock.Season != "halloween" {
		t.Fatalf("season unlock did not survive load: %+v", crop)
	}
}

func TestEmbeddedSeasonalCropsExist(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{
		"candycorn":  "halloween",
		"witchhazel": "halloween",
		"peppermint": "christmas",
		"tinseltree": "christmas",
	} {
		crop := c.Crop(id)
		if crop == nil {
			t.Fatalf("embedded crops missing seasonal %q", id)
		}
		if crop.Unlock.Kind != "season" || crop.Unlock.Season != want {
			t.Fatalf("%q unlock = %+v, want season %q", id, crop.Unlock, want)
		}
	}
}

// scarecrowBlock is a valid [scarecrow] table; each rejection case below
// breaks exactly one key of it.
const scarecrowBlock = `
[scarecrow]
max_count = 4
cost = 10
cost_growth_pct = 300
drop_interval_sec = 15
offline_slowdown = 20
coins_min = 2
coins_max = 8
synergy_pct = 25
rebirth_bonus_pct = 10
unlock_rebirths = [0, 0, 1, 2]
`

func TestScarecrowConfigValidation(t *testing.T) {
	load := func(balance string) error {
		dir := t.TempDir()
		writeFixture(t, dir, "crops.toml", minimalCrops)
		writeFixture(t, dir, "balance.toml", balance)
		_, err := Load(dir)
		return err
	}
	if err := load(minimalBalance); err != nil {
		t.Fatalf("a balance with no [scarecrow] table must load (scarecrows disabled): %v", err)
	}
	if err := load(minimalBalance + scarecrowBlock); err != nil {
		t.Fatalf("valid scarecrow block rejected: %v", err)
	}
	if err := load(minimalBalance + strings.Replace(scarecrowBlock, "unlock_rebirths = [0, 0, 1, 2]\n", "", 1)); err != nil {
		t.Fatalf("a scarecrow block without unlock_rebirths (no rebirth gates) must load: %v", err)
	}
	for _, tc := range []struct{ name, from, to string }{
		{"negative cost", "cost = 10", "cost = -1"},
		{"zero max", "max_count = 4", "max_count = 0"},
		{"zero interval", "drop_interval_sec = 15", "drop_interval_sec = 0"},
		{"zero slowdown", "offline_slowdown = 20", "offline_slowdown = 0"},
		{"min above max", "coins_min = 2", "coins_min = 9"},
		{"zero min", "coins_min = 2", "coins_min = 0"},
		{"growth below 100", "cost_growth_pct = 300", "cost_growth_pct = 99"},
		{"negative synergy", "synergy_pct = 25", "synergy_pct = -1"},
		{"negative rebirth bonus", "rebirth_bonus_pct = 10", "rebirth_bonus_pct = -1"},
		{"gate list shorter than max_count", "unlock_rebirths = [0, 0, 1, 2]", "unlock_rebirths = [0, 1]"},
		{"gate list longer than max_count", "unlock_rebirths = [0, 0, 1, 2]", "unlock_rebirths = [0, 0, 1, 2, 3]"},
		{"negative gate", "unlock_rebirths = [0, 0, 1, 2]", "unlock_rebirths = [-1, 0, 1, 2]"},
		// Scarecrows are bought in order, so a later one needing fewer
		// rebirths than an earlier one is a typo, not a design.
		{"decreasing gates", "unlock_rebirths = [0, 0, 1, 2]", "unlock_rebirths = [0, 2, 1, 2]"},
	} {
		bad := strings.Replace(scarecrowBlock, tc.from, tc.to, 1)
		if bad == scarecrowBlock {
			t.Fatalf("%s: replacement did not apply", tc.name)
		}
		if err := load(minimalBalance + bad); err == nil {
			t.Errorf("%s: invalid scarecrow config accepted", tc.name)
		}
	}
	if err := load(strings.Replace(minimalBalance, "kind = [\"crow\"]", "rebirth_bonus_pct = -5\nkind = [\"crow\"]", 1)); err == nil {
		t.Error("negative critter rebirth_bonus_pct accepted")
	}
}

// The shipped tuning is the one the design doc (gameplay/05) promises.
func TestEmbeddedScarecrowTuning(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	sc := c.Scarecrow
	if sc.MaxCount != 4 || sc.DropIntervalSec != 15 || sc.OfflineSlowdown < 10 {
		t.Fatalf("embedded scarecrow tuning drifted from gameplay/05: %+v", sc)
	}
	if sc.SynergyPct <= 0 || c.Critters.RebirthBonusPct <= 0 {
		t.Fatalf("scarecrow synergy and critter rebirth bonus must be positive: %+v / %+v", sc, c.Critters)
	}
	// The first two are open to every run; the 3rd needs one rebirth and the
	// 4th two.
	for owned, want := range []int64{0, 0, 1, 2} {
		if got := sc.RebirthsToBuy(owned); got != want {
			t.Errorf("rebirths to buy scarecrow %d = %d, want %d", owned+1, got, want)
		}
	}
}

func TestRebirthsToBuyWithoutGates(t *testing.T) {
	sc := ScarecrowConfig{MaxCount: 4}
	for owned := range 6 {
		if got := sc.RebirthsToBuy(owned); got != 0 {
			t.Fatalf("no unlock_rebirths: scarecrow %d needs %d rebirths, want 0", owned+1, got)
		}
	}
	sc.UnlockRebirths = []int64{0, 0, 1, 2}
	if got := sc.RebirthsToBuy(-1); got != 0 {
		t.Fatalf("RebirthsToBuy(-1) = %d, want 0", got)
	}
	if got := sc.RebirthsToBuy(4); got != 0 {
		t.Fatalf("RebirthsToBuy past the list = %d, want 0 (the cap refuses it, not a gate)", got)
	}
}

func TestEmbeddedGoldengrain(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	gg, turnip := c.Crop("goldengrain"), c.Crop("turnip")
	if gg == nil || turnip == nil {
		t.Fatal("embedded crops missing goldengrain or turnip")
	}
	if gg.Archetype != "fast" || gg.SeedCost != 12 {
		t.Fatalf("goldengrain = %+v, want a fast crop costing 12", gg)
	}
	if gg.GrowSeconds*4 != turnip.GrowSeconds*3 {
		t.Fatalf("goldengrain grows in %ds, want 3/4 of turnip's %ds", gg.GrowSeconds, turnip.GrowSeconds)
	}
	want := Unlock{Kind: "zone", Zone: "greenhouse", Rebirths: 2}
	if gg.Unlock != want {
		t.Fatalf("goldengrain unlock = %+v, want %+v", gg.Unlock, want)
	}
	if got := gg.Unlock.RequiredRebirths(); got != 2 {
		t.Fatalf("RequiredRebirths = %d, want 2", got)
	}
}

func TestRequiredRebirths(t *testing.T) {
	for _, tc := range []struct {
		u    Unlock
		want int64
	}{
		{Unlock{Kind: "start"}, 0},
		{Unlock{Kind: "prestige", Value: 3}, 3},
		{Unlock{Kind: "earnings", Value: 500}, 0},
		{Unlock{Kind: "zone", Zone: "greenhouse", Rebirths: 2}, 2},
	} {
		if got := tc.u.RequiredRebirths(); got != tc.want {
			t.Errorf("%+v.RequiredRebirths() = %d, want %d", tc.u, got, tc.want)
		}
	}
}
