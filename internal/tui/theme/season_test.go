package theme

import (
	"image/color"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-farm/internal/season"
)

func allSeasons() []season.Season {
	return []season.Season{season.SeasonHalloween, season.SeasonChristmas}
}

func allPhases() []Phase {
	return []Phase{PhaseDawn, PhaseDay, PhaseDusk, PhaseNight}
}

// TestSeasonalBackgroundsDifferFromPlain: the festival has to be visible on
// the canvas itself, not only in the accents.
func TestSeasonalBackgroundsDifferFromPlain(t *testing.T) {
	for _, sn := range allSeasons() {
		for _, p := range allPhases() {
			plain := New(p, false, "").Bg
			during := NewWithSeason(p, false, "", sn).Bg
			if plain == during {
				t.Errorf("season %v phase %v: background unchanged (%v)", sn, p, plain)
			}
		}
	}
}

// TestSeasonalBackgroundsDistinctPerPhase: if two phases collapsed onto the
// same canvas the tint would read as a bug, not a season.
func TestSeasonalBackgroundsDistinctPerPhase(t *testing.T) {
	for _, sn := range allSeasons() {
		for _, p := range allPhases() {
			bg := NewWithSeason(p, false, "", sn).Bg
			for _, q := range allPhases() {
				if q != p && NewWithSeason(q, false, "", sn).Bg == bg {
					t.Errorf("season %v: phase %v reuses phase %v's background", sn, p, q)
				}
			}
		}
	}
}

// TestSolidPinsTheSeasonalBackground: the pin is about the canvas, not the
// feedback — accents still apply (pinned below).
func TestSolidPinsTheSeasonalBackground(t *testing.T) {
	want := New(PhaseDawn, true, "").Bg
	for _, sn := range allSeasons() {
		for _, p := range allPhases() {
			if got := NewWithSeason(p, true, "", sn).Bg; got != want {
				t.Errorf("season %v phase %v: solid background = %v, want pinned %v", sn, p, got, want)
			}
		}
	}
}

// TestEventBeatsSeason: a live random event keeps priority on both the frame
// accent and the background lift.
func TestEventBeatsSeason(t *testing.T) {
	for _, sn := range allSeasons() {
		for _, p := range allPhases() {
			plain := New(p, false, "market_day")
			festive := NewWithSeason(p, false, "market_day", sn)
			if festive.Bg != plain.Bg {
				t.Errorf("season %v phase %v: event lift lost the background", sn, p)
			}
			if festive.Frame.GetBorderTopForeground() != plain.Frame.GetBorderTopForeground() {
				t.Errorf("season %v phase %v: event lost the frame accent", sn, p)
			}
		}
	}
}

// TestSeasonalFrameAccentAppliesWithoutEvents: pinning the background must
// not mute the festival frame, and the two festivals must not share it.
func TestSeasonalFrameAccentAppliesWithoutEvents(t *testing.T) {
	plain := New(PhaseDay, false, "").Frame.GetBorderTopForeground()
	var accs []color.Color
	for _, sn := range allSeasons() {
		got := NewWithSeason(PhaseDay, false, "", sn).Frame.GetBorderTopForeground()
		if got == plain {
			t.Errorf("season %v: frame accent unchanged from plain", sn)
		}
		for i, prev := range accs {
			if got == prev {
				t.Errorf("season %v reuses season %v's frame accent", sn, allSeasons()[i])
			}
		}
		accs = append(accs, got)
	}
}

// TestSeasonalStylesCarryBackgrounds: the all-styles anti-hole test only
// exercises New, so pin the seasonal constructor explicitly (including the
// new Sky styles and solid mode).
func TestSeasonalStylesCarryBackgrounds(t *testing.T) {
	for _, sn := range append(allSeasons(), season.SeasonNone) {
		for _, solid := range []bool{false, true} {
			th := NewWithSeason(PhaseNight, solid, "", sn)
			for name, st := range styleFields(t, th) {
				if bg := st.GetBackground(); bg == (lipgloss.NoColor{}) {
					t.Errorf("season %v solid %v: style %s has no background", sn, solid, name)
				}
			}
		}
	}
}

// TestSeasonalPaintKeepsInvariants: Paint only inserts SGR, whatever the
// palette, so width and stripped text survive the festival tints.
func TestSeasonalPaintKeepsInvariants(t *testing.T) {
	in := "before · ✦ · after\nsecond 🦇 line"
	for _, sn := range allSeasons() {
		for _, p := range allPhases() {
			th := NewWithSeason(p, false, "", sn)
			got := th.Paint(in)
			if stripAnsi(got) != stripAnsi(in) {
				t.Errorf("season %v phase %v: Paint changed the text", sn, p)
			}
			if len(got) == len(in) {
				t.Errorf("season %v phase %v: Paint emitted no SGR at all", sn, p)
			}
		}
	}
}
