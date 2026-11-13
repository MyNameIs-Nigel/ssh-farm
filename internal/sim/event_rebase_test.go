package sim

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mynameis-nigel/ssh-farm/internal/content"
)

// warmFrontContent is the shared fixture plus a Warm Front (−50% grow time).
// The fixture itself stays event-light so seeded tests elsewhere don't shift.
func warmFrontContent(t *testing.T) *content.Content {
	t.Helper()
	dir := t.TempDir()
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join("testdata", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if e.Name() == "crops.toml" {
			b = append(b, []byte(`
[[crop]]
id = "fivemin"
name = "Fivemin"
archetype = "fast"
seed_cost = 5
grow_seconds = 300
sell_value = 9
unlock = { kind = "start" }
`)...)
		}
		if e.Name() == "balance.toml" {
			b = append(b, []byte(`
[[event]]
id = "warm_front"
name = "Warm Front"
description = "Crops grow twice as fast!"
effect = "grow_speed_pct"
effect_value = 50
`)...)
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c, err := content.Load(dir)
	if err != nil {
		t.Fatalf("load warm-front content: %v", err)
	}
	return c
}

// warmFrontState plants cropID in plot 0 at t=1000 and starts a Warm Front at
// the same moment, lasting `dur` seconds.
func warmFrontState(t *testing.T, c *content.Content, cropID string, dur int64) *State {
	t.Helper()
	s := New(c, 42, 1000)
	s.Coins = 100_000
	if err := Plant(s, c, 0, cropID, 1000); err != nil {
		t.Fatal(err)
	}
	s.EventID = "warm_front"
	s.EventStartedAt = 1000
	s.EventEndsAt = 1000 + dur
	return s
}

// The reported bug: a 300s crop with 3s left at event speed (147 of 150s) read
// 147 of 300s once the front ended and needed another 153s.
func TestWarmFrontEndKeepsNearlyGrownPlant(t *testing.T) {
	c := warmFrontContent(t)
	s := warmFrontState(t, c, "fivemin", 147) // 300s crop, 150s at event speed
	if got := s.GrowSeconds(c, c.Crop("fivemin")); got != 150 {
		t.Fatalf("event grow time = %d, want 150", got)
	}
	if got := s.PlotProgressPct(c, 0, 1147); got != 98 {
		t.Fatalf("progress under the front = %d, want 98", got)
	}

	Advance(s, c, 1147)
	if s.EventID != "" {
		t.Fatal("event still running after it ended")
	}
	if got := s.PlotProgressPct(c, 0, 1147); got != 98 {
		t.Fatalf("progress after the front ended = %d, want 98 (kept)", got)
	}
	if s.PlotReady(c, 0, 1152) {
		t.Fatal("plant ready before its remaining ~6s are up")
	}
	if !s.PlotReady(c, 0, 1153) {
		t.Fatalf("plant not ready 6s after the front ended; PlantedAt=%d", s.Plots[0].PlantedAt)
	}
}

// The fraction earned is what is kept — at every point through the growth.
func TestWarmFrontEndPreservesProgressFraction(t *testing.T) {
	c := warmFrontContent(t)
	for dur := int64(1); dur < 150; dur++ {
		s := warmFrontState(t, c, "fivemin", dur)
		before := s.PlotProgressPct(c, 0, 1000+dur)
		Advance(s, c, 1000+dur)
		after := s.PlotProgressPct(c, 0, 1000+dur)
		if after < before {
			t.Fatalf("dur=%d: progress went back %d%% → %d%%", dur, before, after)
		}
		if after > before+1 {
			t.Fatalf("dur=%d: progress jumped forward %d%% → %d%%", dur, before, after)
		}
		if s.PlotReady(c, 0, 1000+dur) {
			t.Fatalf("dur=%d: rounding made an unfinished plant ready", dur)
		}
	}
}

// After an offline gap the front is wound back to when it actually ended, not
// to the moment the player returned.
func TestWarmFrontEndRebasesAtEventEnd(t *testing.T) {
	c := warmFrontContent(t)
	s := warmFrontState(t, c, "fivemin", 100) // ends at 1100: 100/150 = 66%
	Advance(s, c, 1200)                       // player returns 100s after it ended

	// 200s of normal-speed progress banked at 1100, plus 100s since = 300s.
	if s.PlotReady(c, 0, 1199) {
		t.Fatal("plant ready early: event speed was applied past the event's end")
	}
	if !s.PlotReady(c, 0, 1200) {
		t.Fatalf("plant not ready at 1200; PlantedAt=%d", s.Plots[0].PlantedAt)
	}
}

// A plant that finished during the front and is waiting for the player must
// stay ripe, and must be announced exactly once.
func TestWarmFrontEndKeepsRipePlantRipe(t *testing.T) {
	c := warmFrontContent(t)
	s := warmFrontState(t, c, "fivemin", 180) // ripe at 1150, front ends 1180

	first := Advance(s, c, 1180)
	if !s.PlotReady(c, 0, 1180) {
		t.Fatal("ripe plant went unripe when the front ended")
	}
	if first.Matured["fivemin"] != 1 {
		t.Fatalf("matured = %d on the advance that crossed ripeness, want 1", first.Matured["fivemin"])
	}
	second := Advance(s, c, 1300)
	if second.Matured["fivemin"] != 0 {
		t.Fatal("ripe plant announced a second time")
	}
	if !s.PlotReady(c, 0, 1300) {
		t.Fatal("ripe plant unripe later")
	}
}

// An auto-harvest plot keeps the cycles it earned at event speed and the
// fraction of the cycle it was part way through.
func TestWarmFrontEndAutoHarvestKeepsCycles(t *testing.T) {
	c := warmFrontContent(t)
	s := warmFrontState(t, c, "turnip", 50) // 60s crop → 30s at event speed
	s.Plots[0].AutoHarvest = true
	s.Plots[0].AutoSow = true

	// 50s at event speed = one full cycle (30s) + 20s into the next (66%).
	ev := Advance(s, c, 1050)
	if got := ev.AutoHarvested["turnip"]; got != 1 {
		t.Fatalf("auto-harvested %d cycles, want 1", got)
	}
	if got := s.PlotProgressPct(c, 0, 1050); got != 66 {
		t.Fatalf("progress into second cycle = %d, want 66", got)
	}
}

// Clockwork Denied already ignores events, so clearing one must not move plots.
func TestBlockedEventClearLeavesPlotsAlone(t *testing.T) {
	c := warmFrontContent(t)
	s := warmFrontState(t, c, "turnip", 500)
	s.ActiveContract = ContractClockworkDenied
	planted := s.Plots[0].PlantedAt
	Advance(s, c, 1010)
	if s.EventID != "" {
		t.Fatal("event survived Clockwork Denied")
	}
	if s.Plots[0].PlantedAt != planted {
		t.Fatalf("PlantedAt moved %d → %d", planted, s.Plots[0].PlantedAt)
	}
}

func TestRebasePlantedAtNeverCrossesReadyLine(t *testing.T) {
	const fast, slow = 150, 300
	for elapsed := int64(1); elapsed < fast; elapsed++ {
		got := 1000 - rebasePlantedAt(1000-elapsed, 1000, fast, slow, false)
		if got >= slow {
			t.Fatalf("elapsed %d scaled to %d, at or past grow time %d", elapsed, got, slow)
		}
		if got*fast < elapsed*slow {
			t.Fatalf("elapsed %d scaled down to %d: rounding must not cost the player", elapsed, got)
		}
	}
}
