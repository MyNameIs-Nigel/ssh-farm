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

// TestSeasonalColorsRequireTrueColor protects terminals whose palettes would
// quantize subtle RGB tints into the harsh ANSI colors this feature replaced.
func TestSeasonalColorsRequireTrueColor(t *testing.T) {
	for _, sn := range allSeasons() {
		for _, p := range allPhases() {
			plain := New(p, false, "")
			limited := NewWithSeason(p, false, "", sn, false)
			if limited.Bg != plain.Bg {
				t.Errorf("season %v phase %v: limited-color background = %v, want normal %v", sn, p, limited.Bg, plain.Bg)
			}
			if limited.Frame.GetBorderTopForeground() == plain.Frame.GetBorderTopForeground() {
				t.Errorf("season %v phase %v: limited-color frame lost its seasonal accent", sn, p)
			}
			if limited.Title.GetForeground() == plain.Title.GetForeground() {
				t.Errorf("season %v phase %v: limited-color title lost its seasonal accent", sn, p)
			}
			if during := NewWithSeason(p, false, "", sn, true).Bg; during == plain.Bg {
				t.Errorf("season %v phase %v: True Color background unchanged (%v)", sn, p, plain.Bg)
			}
		}
	}
}

// TestSeasonalBackgroundsStayInOneMutedHue verifies both the requested color
// families and that night is the darkest point in each festival cycle.
func TestSeasonalBackgroundsStayInOneMutedHue(t *testing.T) {
	for _, sn := range allSeasons() {
		brightness := func(c color.Color) uint32 {
			r, g, b, _ := c.RGBA()
			if sn == season.SeasonHalloween && !(r > g && g == b) {
				t.Errorf("halloween background RGB = %04x/%04x/%04x, want red-only tint", r, g, b)
			}
			if sn == season.SeasonChristmas && !(b > r && b > g) {
				t.Errorf("christmas background RGB = %04x/%04x/%04x, want blue-only tint", r, g, b)
			}
			return r + g + b
		}
		for _, p := range allPhases() {
			brightness(NewWithSeason(p, false, "", sn, true).Bg)
		}
		day := brightness(NewWithSeason(PhaseDay, false, "", sn, true).Bg)
		night := brightness(NewWithSeason(PhaseNight, false, "", sn, true).Bg)
		if night >= day {
			t.Errorf("season %v: night brightness %d, want darker than day %d", sn, night, day)
		}
	}
}

// TestSolidPinsTheSeasonalBackground: the pin is about the canvas, not the
// feedback — accents still apply (pinned below).
func TestSolidPinsTheSeasonalBackground(t *testing.T) {
	want := New(PhaseDawn, true, "").Bg
	for _, sn := range allSeasons() {
		for _, p := range allPhases() {
			if got := NewWithSeason(p, true, "", sn, true).Bg; got != want {
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
			festive := NewWithSeason(p, false, "market_day", sn, true)
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
		got := NewWithSeason(PhaseDay, false, "", sn, true).Frame.GetBorderTopForeground()
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
			th := NewWithSeason(PhaseNight, solid, "", sn, true)
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
			th := NewWithSeason(p, false, "", sn, true)
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
