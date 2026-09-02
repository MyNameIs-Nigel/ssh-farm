package sim

import (
	"testing"

	"github.com/mynameis-nigel/ssh-farm/internal/gameclock"
)

// atClock returns a tick counter whose in-game time is hh:mm. MinuteOfDay adds
// the dawn offset, so undo it to land on the requested minute of the day.
func atClock(hh, mm int64) int64 {
	return (hh*60 + mm - gameclock.DawnOffsetMinutes + gameclock.MinutesPerDay) % gameclock.MinutesPerDay
}

func TestMoonberryNightWindowBoundaries(t *testing.T) {
	c := testContent(t)
	moonberry := c.Moon.MoonberryCropID

	tests := []struct {
		name   string
		hh, mm int64
		want   bool
	}{
		{name: "just before the window opens", hh: 19, mm: 59, want: false},
		{name: "window opens at 20:00", hh: 20, mm: 0, want: true},
		{name: "late evening", hh: 22, mm: 30, want: true},
		{name: "last minute before midnight", hh: 23, mm: 59, want: true},
		{name: "window survives the midnight wrap", hh: 0, mm: 0, want: true},
		{name: "small hours", hh: 2, mm: 15, want: true},
		{name: "last minute of the window", hh: 3, mm: 59, want: true},
		{name: "window closes at 04:00", hh: 4, mm: 0, want: false},
		{name: "dawn", hh: 6, mm: 0, want: false},
		{name: "midday", hh: 12, mm: 0, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := atClock(tt.hh, tt.mm)
			if got := isMoonberryHour(c, moonberry, now); got != tt.want {
				t.Errorf("isMoonberryHour at %02d:%02d (now=%d) = %v, want %v",
					tt.hh, tt.mm, now, got, tt.want)
			}
		})
	}
}

func TestMoonberryWindowOnlyAppliesToMoonberry(t *testing.T) {
	c := testContent(t)
	night := atClock(22, 0)

	if !isMoonberryHour(c, c.Moon.MoonberryCropID, night) {
		t.Fatal("test setup: 22:00 should be inside the night window")
	}
	for _, cropID := range []string{"turnip", "", "moonberry_lookalike"} {
		if isMoonberryHour(c, cropID, night) {
			t.Errorf("isMoonberryHour(%q) = true at 22:00, want the bonus limited to Moonberry", cropID)
		}
	}
}

func TestMoonberryNightBonusChangesSellPrice(t *testing.T) {
	c := testContent(t)
	s := newTestState(t, c)
	moonberry := c.Moon.MoonberryCropID

	const base = 100
	day := s.sellMultiplied(c, base, moonberry, atClock(12, 0))
	night := s.sellMultiplied(c, base, moonberry, atClock(22, 0))
	other := s.sellMultiplied(c, base, "turnip", atClock(22, 0))

	if day != other {
		t.Errorf("Moonberry by day = %d, want the same as an unbonused crop (%d)", day, other)
	}
	if night <= day {
		t.Errorf("Moonberry at night = %d, want more than by day (%d)", night, day)
	}
	if want := day * (100 + c.Moon.FullMoonSellBonusPct) / 100; night != want {
		t.Errorf("Moonberry at night = %d, want day price plus the %d%% night bonus (%d)",
			night, c.Moon.FullMoonSellBonusPct, want)
	}
}
