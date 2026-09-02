package tui

import (
	"fmt"

	"github.com/mynameis-nigel/ssh-farm/internal/tui/theme"
)

// Time clock formatter for the 24-hour in-game clock.
func formatClock(now int64) string {
	// Define what a full cycle of the game is in seconds
	const cycle = 4 * theme.PhaseSeconds

	// Calculate the current second within the cycle
	seconds := ((now % cycle) + cycle) % cycle

	// Offset the clock to start at 06:00 instead of 00:00
	const offsetSeconds = theme.PhaseSeconds

	total := seconds + offsetSeconds

	// Calculate the hour and minute for the in-game clock
	clockMinute := total % 60
	clockHour := (total % cycle) / 60

	return fmt.Sprintf("%02d:%02d", clockHour, clockMinute)
}
