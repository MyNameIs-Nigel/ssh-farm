// Package gameclock owns the accelerated in-game day that the simulation and
// the TUI must agree on.
//
// One real second is one in-game minute, so four visual phases make a 24-minute
// real day and a 1440-minute in-game one. The cycle is deliberately accelerated
// rather than tied to the wall clock: SSH gives us no reliable player timezone,
// and a real-time cycle would never visibly change inside a single session.
//
// This package is a leaf so both internal/sim and internal/tui can depend on it
// without the simulation importing UI code.
package gameclock

// PhaseSeconds is how long one phase (dawn, day, dusk, night) lasts.
const PhaseSeconds = 360

// PhaseCount is the number of phases in one full cycle.
const PhaseCount = 4

// MinutesPerDay is one in-game day. It equals the cycle length in seconds
// because a real second maps to an in-game minute.
const MinutesPerDay = PhaseCount * PhaseSeconds

// DawnOffsetMinutes shifts cycle position 0 to 06:00. Dawn is the first phase
// and each phase spans six in-game hours, so the offset is exactly one phase.
const DawnOffsetMinutes = PhaseSeconds

// MinuteOfDay maps a tick counter to minutes since in-game midnight, in
// [0, MinutesPerDay). It is a pure function of now so every minute of the day
// is reachable in a test by passing a constant.
func MinuteOfDay(now int64) int64 {
	// Go's % keeps the sign of the dividend, so fold twice to stay in range for
	// pre-epoch timestamps.
	cyclePos := ((now % MinutesPerDay) + MinutesPerDay) % MinutesPerDay
	return (cyclePos + DawnOffsetMinutes) % MinutesPerDay
}

// HourMinute splits a tick counter into the in-game hour and minute.
func HourMinute(now int64) (hour, minute int64) {
	m := MinuteOfDay(now)
	return m / 60, m % 60
}
