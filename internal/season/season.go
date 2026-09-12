// Package season owns the real-calendar festival windows behind the
// Halloween and Christmas themes.
//
// Seasons are month/day windows in UTC, derived from a timestamp the caller
// passes in — the package never reads a clock, so every date is reachable in
// a test by constructing it with time.Date. The game feeds its tick counter
// (wall-clock unix, the same counter theme.PhaseAt already reads) via AtUnix.
package season

import (
	"os"
	"strings"
	"time"
)

// Season is which festival skin is active, if any.
type Season int

const (
	SeasonNone Season = iota
	SeasonHalloween
	SeasonChristmas
)

// HalloweenMonth is the only month Halloween is active: the whole of October.
const halloweenMonth = time.October

// Christmas runs Nov 25 – Dec 25 inclusive, so each festival lasts about one
// month.
const (
	christmasStartMonth = time.November
	christmasStartDay   = 25
	christmasEndMonth   = time.December
	christmasEndDay     = 25
)

// At reports the season active on the calendar date of t (UTC).
func At(t time.Time) Season {
	t = t.UTC()
	switch m, d := t.Month(), t.Day(); {
	case m == halloweenMonth:
		return SeasonHalloween
	case m == christmasStartMonth && d >= christmasStartDay:
		return SeasonChristmas
	case m == christmasEndMonth && d <= christmasEndDay:
		return SeasonChristmas
	default:
		return SeasonNone
	}
}

// AtUnix reports the season active at a wall-clock unix timestamp.
func AtUnix(now int64) Season { return At(time.Unix(now, 0)) }

// ActiveAtUnix reports the season to use while the game is running. In
// development, FARM_DEV_SEASON may be set to "halloween" or "christmas" to
// exercise that festival outside its calendar window. Any other value,
// including an unset variable, leaves the real UTC calendar in control.
//
// At and AtUnix deliberately remain pure calendar functions for callers that
// need the real date (and for deterministic tests).
func ActiveAtUnix(now int64) Season {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("FARM_DEV_SEASON"))) {
	case "halloween":
		return SeasonHalloween
	case "christmas":
		return SeasonChristmas
	default:
		return AtUnix(now)
	}
}

// Name is the player-facing label for a season ("" when none is active).
func (s Season) Name() string {
	switch s {
	case SeasonHalloween:
		return "Halloween"
	case SeasonChristmas:
		return "Christmas"
	default:
		return ""
	}
}

// Glyph is the festival marker shown in the header title while active.
func (s Season) Glyph() string {
	switch s {
	case SeasonHalloween:
		return "🎃"
	case SeasonChristmas:
		return "🎄"
	default:
		return ""
	}
}

// Key is the content-unlock identifier for a season's seeds, matching the
// `season = "..."` value in crops.toml.
func (s Season) Key() string {
	switch s {
	case SeasonHalloween:
		return "halloween"
	case SeasonChristmas:
		return "christmas"
	default:
		return ""
	}
}

// ValidKey reports whether k names a real festival season.
func ValidKey(k string) bool {
	return k == "halloween" || k == "christmas"
}

// SpecialDay reports whether t is a festival's showpiece day: Oct 31 gets
// the big moon and Dec 25 gets the big star, each lasting all day and night.
func SpecialDay(t time.Time) bool {
	t = t.UTC()
	return (t.Month() == time.October && t.Day() == 31) ||
		(t.Month() == time.December && t.Day() == 25)
}

// SpecialDayUnix reports whether a wall-clock unix timestamp falls on a
// festival showpiece day.
func SpecialDayUnix(now int64) bool { return SpecialDay(time.Unix(now, 0)) }
