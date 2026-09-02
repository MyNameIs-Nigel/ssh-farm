package tui

import (
	"fmt"

	"github.com/mynameis-nigel/ssh-farm/internal/gameclock"
)

// formatClock renders the accelerated in-game time at a tick counter as a
// zero-padded 24-hour HH:MM string.
//
// The minute-of-day math lives in gameclock so the simulation's night-time
// Moonberry window and this display cannot disagree. It never reads the wall
// clock, which is what keeps golden renders deterministic.
func formatClock(now int64) string {
	hour, minute := gameclock.HourMinute(now)
	return fmt.Sprintf("%02d:%02d", hour, minute)
}
