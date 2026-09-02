package gameclock

import (
	"fmt"
	"testing"
)

func TestMinuteOfDayMapsCyclePositionToInGameTime(t *testing.T) {
	tests := []struct {
		name string
		now  int64
		want int64
	}{
		{name: "cycle start is 06:00", now: 0, want: 6 * 60},
		{name: "last minute of dawn is 11:59", now: PhaseSeconds - 1, want: 11*60 + 59},
		{name: "day starts at noon", now: PhaseSeconds, want: 12 * 60},
		{name: "dusk starts at 18:00", now: 2 * PhaseSeconds, want: 18 * 60},
		{name: "night starts at midnight", now: 3 * PhaseSeconds, want: 0},
		{name: "last minute of night is 05:59", now: 4*PhaseSeconds - 1, want: 5*60 + 59},
		{name: "cycle wraps to the next dawn", now: 4 * PhaseSeconds, want: 6 * 60},
		{name: "many cycles later", now: 1_000 * MinutesPerDay, want: 6 * 60},
		{name: "pre-epoch timestamps stay in range", now: -1, want: 5*60 + 59},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MinuteOfDay(tt.now); got != tt.want {
				t.Errorf("MinuteOfDay(%d) = %d, want %d", tt.now, got, tt.want)
			}
		})
	}
}

func TestMinuteOfDayAlwaysInRange(t *testing.T) {
	for now := int64(-5000); now < 5000; now++ {
		if got := MinuteOfDay(now); got < 0 || got >= MinutesPerDay {
			t.Fatalf("MinuteOfDay(%d) = %d, want 0 <= m < %d", now, got, MinutesPerDay)
		}
	}
}

func TestHourMinuteSplitsOneInstant(t *testing.T) {
	for now := int64(0); now < MinutesPerDay; now++ {
		h, m := HourMinute(now)
		if got, want := h*60+m, MinuteOfDay(now); got != want {
			t.Fatalf("HourMinute(%d) = %d:%d, which is minute %d, want %d", now, h, m, got, want)
		}
		if h < 0 || h > 23 || m < 0 || m > 59 {
			t.Fatalf("HourMinute(%d) = %s, want a valid 24-hour time", now, fmt.Sprintf("%02d:%02d", h, m))
		}
	}
}
