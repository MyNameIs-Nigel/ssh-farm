package tui

import (
	"time"

	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-farm/internal/content"
	"github.com/mynameis-nigel/ssh-farm/internal/season"
	"github.com/mynameis-nigel/ssh-farm/internal/tui/theme"
)

// season reports the festival skin active for this frame: None outside the
// windows or when the player opted out. Seeds never consult it — they answer
// to the calendar via sim.SeasonalPlantable, so they stay in-season with the
// look switched off.
func (g *Game) season() season.Season {
	st := g.snap.State
	if st == nil || !st.SeasonalEnabled() {
		return season.SeasonNone
	}
	return season.AtUnix(g.now)
}

// titleGlyph is the farm glyph opening the header row: the usual wheat,
// or the festival marker while a season is active.
func (g *Game) titleGlyph() string {
	if sn := g.season(); sn != season.SeasonNone {
		return sn.Glyph() + " "
	}
	return "🌾 "
}

// --- Sky row ---------------------------------------------------------------

// Star, moon and pumpkin fields are pure functions of the date: three
// variants each, picked by day-of-year, so the sky is stable all day and
// golden-deterministic.
var (
	starFields = []string{
		"· ✦ ·   · ✧ ·   ✦ ·",
		"✧ ·   · ✦ ·   · ✧ ·",
		"· · ✦ ·   ✧ · · ✦",
	}
	batFields = []string{
		"🦇 · 🌙 · 🦇",
		"· 🦇 · 🌙 · 🦇 ·",
		"🦇 · · 🌙 · · 🦇",
	}
	pumpkinFields = []string{
		"🎃 · 🍂 · 🎃",
		"🍂 · 🎃 · 🍂",
		"🎃 · · 🍂 · · 🎃",
	}
)

// viewSky renders the seasonal sky row: stars on Christmas nights, bats and
// moonlight on Halloween nights, pumpkins by Halloween day, and the big
// showpiece (moon on Oct 31, star on Dec 25) all day and night. It is emitted
// from viewHeader — the tui/03 placement rule — so hitbox layout follows for
// free. Empty off-season, when themes are off, and on clear Christmas days.
func (g *Game) viewSky() string {
	sn := g.season()
	if sn == season.SeasonNone {
		return ""
	}
	th := g.theme()
	t := time.Unix(g.now, 0).UTC()
	night := theme.PhaseAt(g.now) == theme.PhaseNight

	var text string
	bright := false
	switch {
	case season.SpecialDay(t):
		bright = true
		if sn == season.SeasonHalloween {
			text = "🌕 ⋯ HALLOWEEN NIGHT ⋯ 🌕"
		} else {
			text = "★ ⋯ THE STARLIGHT ⋯ ★"
		}
	case sn == season.SeasonChristmas && night:
		text = starFields[t.YearDay()%len(starFields)]
	case sn == season.SeasonChristmas:
		return "" // clear skies by day; the stars are a night show
	case night:
		text = batFields[t.YearDay()%len(batFields)]
	default:
		text = pumpkinFields[t.YearDay()%len(pumpkinFields)]
	}

	style := th.Sky
	if bright {
		style = th.SkyBright
	}
	return lipgloss.PlaceHorizontal(g.contentWidth(), lipgloss.Center, style.Render(text))
}

// --- Seasonal headlines ----------------------------------------------------

var halloweenHeadlines = []string{
	"SPOOKY HARVEST: CANDYCORN PRICES SOAR",
	"WITCHHAZEL WANTED ACROSS THE COUNTY",
	"LOCAL SCARECROW SEEN WINKING AT MIDNIGHT",
	"BATS ROOST IN THE BARN, CROPS UNAFFECTED",
	"FULL HAUNT EXPECTED BY THE 31ST",
}

var christmasHeadlines = []string{
	"STARLIGHT FESTIVAL: TINSELTREES FETCH A FORTUNE",
	"PEPPERMINT DEMAND PEAKS AHEAD OF THE 25TH",
	"ROBINS NEST IN EVERY SCARECROW",
	"NIGHT SKIES CLEAR FOR STAR WATCHERS",
	"SNOWBELLS BLOOM UNDER THE BIG STAR",
}

// seasonalHeadline returns the festival ticker line for now, rotating daily.
// Empty when no season is active (the look is off or the calendar is plain).
func (g *Game) seasonalHeadline() string {
	switch g.season() {
	case season.SeasonHalloween:
		yday := time.Unix(g.now, 0).UTC().YearDay()
		return halloweenHeadlines[yday%len(halloweenHeadlines)]
	case season.SeasonChristmas:
		yday := time.Unix(g.now, 0).UTC().YearDay()
		return christmasHeadlines[yday%len(christmasHeadlines)]
	default:
		return ""
	}
}

// --- Critter display names -------------------------------------------------

// critterName reskins a visitor for the festival at render time. Saves keep
// the plain kind ("crow"), so rewards, caps and the scarecrow never notice.
func (g *Game) critterName(raw string) string {
	switch g.season() {
	case season.SeasonHalloween:
		switch raw {
		case "crow":
			return "bat"
		case "rabbit":
			return "ghost"
		case "mole":
			return "gremlin"
		}
	case season.SeasonChristmas:
		switch raw {
		case "crow":
			return "robin"
		case "rabbit":
			return "hare"
		case "mole":
			return "mouse"
		}
	}
	return raw
}

// --- Seasonal seed markers -------------------------------------------------

// seasonEndLabel tells the buyer when a seasonal seed vanishes: the morning
// after the window closes.
func seasonEndLabel(u content.Unlock) string {
	switch u.Season {
	case "halloween":
		return "gone Nov 1"
	case "christmas":
		return "gone Dec 26"
	default:
		return "limited time"
	}
}

// seasonMarker decorates a seasonal crop row with its festival glyph and
// expiry, e.g. "🎃 seasonal — gone Nov 1". Empty for ordinary crops.
func seasonMarker(crop content.Crop) string {
	if crop.Unlock.Kind != "season" {
		return ""
	}
	sn := season.SeasonHalloween
	if crop.Unlock.Season == "christmas" {
		sn = season.SeasonChristmas
	}
	return sn.Glyph() + " seasonal — " + seasonEndLabel(crop.Unlock)
}
