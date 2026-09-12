package sim

import (
	"strings"
	"testing"
	"time"
)

// Festival timestamps (wall-clock unix) for the seasonal tests. The sim
// fixtures use small tick counters that fall in January (no season), so
// seasonal cases build real calendar dates instead.
func halloweenNoon() int64 {
	return time.Date(2024, time.October, 15, 12, 0, 0, 0, time.UTC).Unix()
}

func christmasNoon() int64 {
	return time.Date(2024, time.December, 10, 12, 0, 0, 0, time.UTC).Unix()
}

func plainNovember() int64 {
	return time.Date(2024, time.November, 14, 12, 0, 0, 0, time.UTC).Unix()
}

func TestSeasonalPlantableFollowsTheCalendar(t *testing.T) {
	c := testContent(t)
	lantern := c.Crop("lanternberry")
	snow := c.Crop("snowbell")
	turnip := c.Crop("turnip")
	if lantern == nil || snow == nil || turnip == nil {
		t.Fatal("fixture is missing seasonal crops")
	}
	if !SeasonalPlantable(turnip.Unlock, plainNovember()) {
		t.Fatal("ordinary crops are always plantable")
	}
	if !SeasonalPlantable(lantern.Unlock, halloweenNoon()) {
		t.Fatal("halloween seed should be plantable in october")
	}
	if SeasonalPlantable(lantern.Unlock, christmasNoon()) {
		t.Fatal("halloween seed should not be plantable in december")
	}
	if SeasonalPlantable(lantern.Unlock, plainNovember()) {
		t.Fatal("halloween seed should not be plantable in november")
	}
	if !SeasonalPlantable(snow.Unlock, christmasNoon()) {
		t.Fatal("christmas seed should be plantable in december")
	}
	if SeasonalPlantable(snow.Unlock, halloweenNoon()) {
		t.Fatal("christmas seed should not be plantable in october")
	}
}

func TestPlantRefusesOutOfSeasonSeeds(t *testing.T) {
	c := testContent(t)
	s := newTestState(t, c)
	s.Coins = 10_000

	if err := Plant(s, c, 0, "lanternberry", plainNovember()); err != ErrLocked {
		t.Fatalf("out-of-season plant: got %v, want ErrLocked", err)
	}
	if s.Plots[0].Crop != "" {
		t.Fatal("refused plant must leave the plot empty")
	}
	if err := Plant(s, c, 0, "lanternberry", halloweenNoon()); err != nil {
		t.Fatalf("in-season plant: %v", err)
	}
	if err := Plant(s, c, 1, "snowbell", christmasNoon()); err != nil {
		t.Fatalf("in-season christmas plant: %v", err)
	}
	if err := Plant(s, c, 2, "snowbell", halloweenNoon()); err != ErrLocked {
		t.Fatalf("christmas seed in october: got %v, want ErrLocked", err)
	}
}

func TestPlantedSeasonalCropsSurviveTheWindow(t *testing.T) {
	c := testContent(t)
	s := newTestState(t, c)
	s.Coins = 10_000

	planted := halloweenNoon()
	if err := Plant(s, c, 0, "lanternberry", planted); err != nil {
		t.Fatal(err)
	}
	// After the window closes the crop keeps growing and harvests full value.
	after := time.Date(2024, time.November, 5, 12, 0, 0, 0, time.UTC).Unix()
	res, err := Harvest(s, c, 0, after)
	if err != nil {
		t.Fatalf("post-season harvest: %v", err)
	}
	if res.CropID != "lanternberry" || res.Payout != 150 {
		t.Fatalf("post-season harvest = %+v, want full 150c payout", res)
	}
}

func TestVisibleCropsAtHidesOutOfSeasonSeeds(t *testing.T) {
	c := testContent(t)
	s := newTestState(t, c)

	seen := func(now int64) map[string]bool {
		out := map[string]bool{}
		for _, crop := range VisibleCropsAt(s, c, now) {
			out[crop.ID] = true
		}
		return out
	}
	if !seen(halloweenNoon())["lanternberry"] {
		t.Fatal("halloween seed should be visible in october")
	}
	if seen(halloweenNoon())["snowbell"] {
		t.Fatal("christmas seed should be hidden in october")
	}
	if !seen(christmasNoon())["snowbell"] {
		t.Fatal("christmas seed should be visible in december")
	}
	if seen(plainNovember())["lanternberry"] || seen(plainNovember())["snowbell"] {
		t.Fatal("no seasonal seeds should be visible in mid-november")
	}
	// The clockless variant keeps its prestige-only contract for callers
	// without a timestamp.
	for _, crop := range VisibleCrops(s, c) {
		if crop.Unlock.Kind == "prestige" && crop.Unlock.Value > 1 && s.Rebirths < crop.Unlock.Value-1 {
			t.Fatalf("VisibleCrops leaked %q", crop.ID)
		}
	}
}

func TestReplantAllLocksOutOfSeasonSeeds(t *testing.T) {
	c := testContent(t)
	s := newTestState(t, c)
	s.Coins = 10_000

	if err := Plant(s, c, 0, "lanternberry", halloweenNoon()); err != nil {
		t.Fatal(err)
	}
	if _, err := Harvest(s, c, 0, halloweenNoon()+600); err != nil {
		t.Fatal(err)
	}
	// The harvested plot remembers lanternberry, and the other empty plots
	// fall back to it as the last-planted crop; out of season all three
	// count as locked rather than planting.
	res := ReplantAll(s, c, plainNovember())
	if res.Planted != 0 || res.Locked != 3 {
		t.Fatalf("out-of-season replant = %+v, want 0 planted 3 locked", res)
	}
	if s.Plots[0].Crop != "" {
		t.Fatal("out-of-season replant must leave the plot empty")
	}
	// In season it replants as usual.
	res = ReplantAll(s, c, halloweenNoon())
	if res.Planted != 3 {
		t.Fatalf("in-season replant = %+v, want 3 planted", res)
	}
}

func TestAutoSowStopsAfterTheSeason(t *testing.T) {
	c := testContent(t)
	s := newTestState(t, c)
	s.Coins = 10_000
	s.Plots[0].AutoHarvest = true
	s.Plots[0].AutoSow = true

	planted := halloweenNoon()
	if err := Plant(s, c, 0, "lanternberry", planted); err != nil {
		t.Fatal(err)
	}
	s.UpdatedAt = planted
	// Advance well past the window: the autumn harvests settle, then the
	// plot comes up empty instead of replanting out of season.
	after := time.Date(2024, time.November, 5, 12, 0, 0, 0, time.UTC).Unix()
	Advance(s, c, after)
	if s.Plots[0].Crop != "" {
		t.Fatalf("auto-sow replanted %q out of season; plot should be empty", s.Plots[0].Crop)
	}
}

func TestSetSeasonalTogglesAndPersists(t *testing.T) {
	c := testContent(t)
	s := newTestState(t, c)
	if !s.SeasonalEnabled() {
		t.Fatal("a fresh save should have festival skins on")
	}
	SetSeasonal(s, false)
	if s.SeasonalEnabled() {
		t.Fatal("SetSeasonal(false) did not opt out")
	}
	SetSeasonal(s, true)
	if !s.SeasonalEnabled() {
		t.Fatal("SetSeasonal(true) did not opt back in")
	}
	SetSeasonal(s, false)
	s.RunEarnings = 1_000_000
	s.LifetimeEarnings = 1_000_000
	if _, err := Rebirth(s, c, 2000); err != nil {
		t.Fatalf("rebirth: %v", err)
	}
	if s.SeasonalEnabled() {
		t.Fatal("seasonal opt-out must persist across rebirth, like the other settings")
	}
}

func TestSeasonalOffOmittedAtDefault(t *testing.T) {
	c := testContent(t)
	s := newTestState(t, c)
	b, err := s.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "seasonal_off") {
		t.Fatal("SeasonalOff at its default must be omitempty so v1 payloads round-trip byte-identically")
	}
	SetSeasonal(s, false)
	b, err = s.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "seasonal_off") {
		t.Fatal("opted-out SeasonalOff must serialize")
	}
	back, err := DecodeState(b)
	if err != nil {
		t.Fatal(err)
	}
	if back.SeasonalEnabled() {
		t.Fatal("opt-out did not survive a round trip")
	}
}
