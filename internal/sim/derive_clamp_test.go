package sim

import (
	"testing"

	"github.com/mynameis-nigel/ssh-farm/internal/content"
)

// The derive.go clamps below only run when stacked effects reach 100%, which
// production balance.toml never does. Each test pushes the fixture past the
// limit and pins the exact clamped value, so a balance edit that crosses the
// line (or a drifted constant) fails here instead of shipping a free-land or
// zero-second-crop exploit.

// growClampState returns a state whose grow-speed reduction is exactly pct:
// 50 from Deep Roots (5 x 10) and 24 from Rich Soil (3 x 8) plus whatever the
// active event supplies. The event is the only knob, so pct must be >= 74.
func growClampState(t *testing.T, c *content.Content, pct int64) *State {
	t.Helper()
	if pct < 74 {
		t.Fatalf("growClampState: pct %d below the 74%% permanent floor", pct)
	}
	ev := c.EventByID("market_day")
	ev.Effect = "grow_speed_pct"
	ev.EffectValue = pct - 74
	s := newTestState(t, c)
	s.Upgrades["growth"] = 5
	s.Multipliers["fertilizer"] = 3
	s.EventID = "market_day"
	return s
}

func TestGrowSecondsClampsStackedReductionAt99Percent(t *testing.T) {
	// 10000s makes 99% (100s), 98% (200s) and an unclamped 100% (0 -> floor 1)
	// all distinguishable.
	crop := &content.Crop{ID: "slowpoke", GrowSeconds: 10000}
	for _, tc := range []struct {
		name string
		pct  int64
		want int64
	}{
		{"below cap is untouched", 98, 200},
		{"at 99 is the cap itself", 99, 100},
		{"exactly 100 clamps to 99", 100, 100},
		{"well past 100 clamps to 99", 150, 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testContent(t)
			s := growClampState(t, c, tc.pct)
			if got := s.GrowSeconds(c, crop); got != tc.want {
				t.Fatalf("GrowSeconds at %d%% = %d, want %d", tc.pct, got, tc.want)
			}
		})
	}
}

func TestGrowSecondsNeverReachesZero(t *testing.T) {
	// 99% of a short crop rounds to 0 seconds; an unfloored crop matures
	// instantly and loops harvest inside a single Advance.
	for _, grow := range []int64{1, 60, 99} {
		for _, pct := range []int64{99, 100, 150} {
			c := testContent(t)
			s := growClampState(t, c, pct)
			crop := &content.Crop{ID: "quick", GrowSeconds: grow}
			if got := s.GrowSeconds(c, crop); got != 1 {
				t.Fatalf("GrowSeconds(grow=%d) at %d%% = %d, want the 1s floor", grow, pct, got)
			}
		}
	}
}

func TestSeedCostClampsDiscountAt99Percent(t *testing.T) {
	crop := &content.Crop{ID: "dear", SeedCost: 1000}
	for _, tc := range []struct {
		name string
		pct  int64
		want int64
	}{
		{"below cap is untouched", 98, 20},
		{"at 99 is the cap itself", 99, 10},
		{"exactly 100 clamps to 99", 100, 10},
		{"well past 100 clamps to 99", 150, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testContent(t)
			c.EventByID("market_day").EffectValue = tc.pct
			s := newTestState(t, c)
			s.EventID = "market_day"
			if got := s.SeedCost(c, crop); got != tc.want {
				t.Fatalf("SeedCost at %d%% = %d, want %d", tc.pct, got, tc.want)
			}
		})
	}
}

func TestSeedCostIsNeverFreeForAPricedCrop(t *testing.T) {
	// 5 * 1 / 100 truncates to 0: the 99% cap alone does not stop free seeds.
	for _, pct := range []int64{99, 100, 150} {
		c := testContent(t)
		c.EventByID("market_day").EffectValue = pct
		s := newTestState(t, c)
		s.EventID = "market_day"
		if got := s.SeedCost(c, c.Crop("turnip")); got != 1 {
			t.Fatalf("SeedCost(turnip) at %d%% = %d, want 1", pct, got)
		}
	}
	// A crop that is free to begin with stays free; the floor is not a tax.
	c := testContent(t)
	s := newTestState(t, c)
	s.EventID = "market_day"
	if got := s.SeedCost(c, &content.Crop{ID: "gift", SeedCost: 0}); got != 0 {
		t.Fatalf("SeedCost of a 0-cost crop = %d, want 0", got)
	}
}

func TestNextPlotCostClampsDiscountAt99Percent(t *testing.T) {
	// A 10000-coin base makes 99% (100), 98% (200) and 90% (1000) distinct.
	for _, tc := range []struct {
		name   string
		levels int // Surveying is 10% per level
		want   int64
	}{
		{"below cap is untouched", 9, 1000},
		{"exactly 100 clamps to 99", 10, 100},
		{"well past 100 clamps to 99", 15, 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testContent(t)
			c.Land.BasePlotCost = 10000
			s := newTestState(t, c)
			s.Upgrades["surveying"] = tc.levels
			if got := s.NextPlotCost(c); got != tc.want {
				t.Fatalf("NextPlotCost with %d levels = %d, want %d", tc.levels, got, tc.want)
			}
		})
	}
}

func TestNextPlotCostNeverFree(t *testing.T) {
	// Fixture base is 50: 99% off truncates to 0 without the 1-coin floor.
	for _, levels := range []int{10, 15} {
		c := testContent(t)
		s := newTestState(t, c)
		s.Upgrades["surveying"] = levels
		if got := s.NextPlotCost(c); got != 1 {
			t.Fatalf("NextPlotCost with %d levels = %d, want the 1-coin floor", levels, got)
		}
	}
}
