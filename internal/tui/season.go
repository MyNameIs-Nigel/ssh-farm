package tui

import (
	"strings"
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
	return season.ActiveAtUnix(g.now)
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
// showpiece (moon on Oct 31, star on Dec 25) all day and night. composeCanvas
// emits it immediately above the frame, while canvasSize and computeLayout
// reserve the row. Empty off-season, when themes are off, and on clear
// Christmas days.
func (g *Game) viewSky() string {
	text, bright := g.seasonalSky()
	if text == "" {
		return ""
	}
	th := g.theme()
	style := th.Sky
	if bright {
		style = th.SkyBright
	}
	w, _ := g.canvasSize()
	styled := style.Render(text)
	return strings.Repeat(" ", max((w-lipgloss.Width(styled))/2, 0)) + styled
}

// seasonalSky returns unstyled sky content so layout can reserve its row
// without recursing through viewSky -> canvasSize -> viewSky.
func (g *Game) seasonalSky() (text string, bright bool) {
	sn := g.season()
	if sn == season.SeasonNone {
		return "", false
	}
	t := time.Unix(g.now, 0).UTC()
	night := theme.PhaseAt(g.now) == theme.PhaseNight

	switch {
	case season.At(t) == sn && season.SpecialDay(t):
		bright = true
		if sn == season.SeasonHalloween {
			text = "🌕 ⋯ HALLOWEEN NIGHT ⋯ 🌕"
		} else {
			text = "★ ⋯ THE STARLIGHT ⋯ ★"
		}
	case sn == season.SeasonChristmas && night:
		text = starFields[t.YearDay()%len(starFields)]
	case sn == season.SeasonChristmas:
		return "", false // clear skies by day; the stars are a night show
	case night:
		text = batFields[t.YearDay()%len(batFields)]
	default:
		text = pumpkinFields[t.YearDay()%len(pumpkinFields)]
	}

	return text, bright
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

// seasonMarker decorates a seasonal crop row with a compact festival label.
// Exact cutoff times live in Help, where they can wrap without breaking the
// picker or Land catalog viewport.
func seasonMarker(crop content.Crop) string {
	if crop.Unlock.Kind != "season" {
		return ""
	}
	sn := season.SeasonHalloween
	if crop.Unlock.Season == "christmas" {
		sn = season.SeasonChristmas
	}
	return sn.Glyph() + " seasonal"
}
