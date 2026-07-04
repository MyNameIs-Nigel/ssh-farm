package content

import (
	"os"
	"path/filepath"
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
