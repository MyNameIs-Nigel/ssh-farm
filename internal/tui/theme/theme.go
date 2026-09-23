// Package theme owns how ssh-farm looks: the near-black palette, the
// day/night cycle that shifts it, and the painting helper that makes a
// background survive lipgloss's nested styles.
//
// The normal backgrounds use ANSI-256 indices for consistent rendering. The
// festival backgrounds use deliberately near-black RGB tints; terminal color
// capability detection and fallback behavior are outside this package.
package theme

import (
	"image/color"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-farm/internal/gameclock"
	"github.com/mynameis-nigel/ssh-farm/internal/season"
)

// The palette. Backgrounds sit in the 232-237 greyscale ramp: 232 is almost
// pure black and each step is roughly +10 in RGB, so the cycle reads as a
// gentle drift rather than a flash, and every phase stays "really dark".
const (
	bgNight = "232"
	bgDusk  = "233"
	bgDawn  = "234"
	bgDay   = "235"

	// bgSolid is where the background sits when the player pins it.
	bgSolid = "233"

	// fgDefault is what a bare SGR reset falls back to. It has to be a real
	// colour, not the terminal's default, or resets would show the user's own
	// foreground against our background.
	fgDefault = "253"
)

// phaseBg and phaseEventBg are parallel to the Phase constants. The event
// variant is lifted two steps so an event is visible on the canvas itself,
// never colliding with the plain background of any phase.
var (
	phaseBg      = [...]string{PhaseDawn: bgDawn, PhaseDay: bgDay, PhaseDusk: bgDusk, PhaseNight: bgNight}
	phaseEventBg = [...]string{PhaseDawn: "236", PhaseDay: "237", PhaseDusk: "235", PhaseNight: "234"}
	phaseName    = [...]string{PhaseDawn: "Dawn", PhaseDay: "Day", PhaseDusk: "Dusk", PhaseNight: "Night"}

	// phaseSeasonBg values are deliberately almost black and stay within one
	// hue family per festival.
	phaseSeasonBg = map[season.Season][phaseCount]string{
		season.SeasonHalloween: {PhaseDawn: "#100606", PhaseDay: "#140808", PhaseDusk: "#0d0505", PhaseNight: "#070303"},
		season.SeasonChristmas: {PhaseDawn: "#060817", PhaseDay: "#080b1b", PhaseDusk: "#050816", PhaseNight: "#03040b"},
	}

	// seasonFrameAccent tints the frame border while a festival is active. A
	// live random event keeps priority (its accent wins below).
	seasonFrameAccent = map[season.Season]string{
		season.SeasonHalloween: "#5a2424",
		season.SeasonChristmas: "#30405f",
	}
)

// Phase is where we are in the accelerated day/night cycle.
type Phase int

const (
	PhaseDawn Phase = iota
	PhaseDay
	PhaseDusk
	PhaseNight
)

// PhaseSeconds is how long one phase lasts; four of them make a 24-minute day.
// It is re-exported from gameclock, which owns the cycle so the simulation and
// the TUI cannot drift apart.
const PhaseSeconds = gameclock.PhaseSeconds

const phaseCount = gameclock.PhaseCount

// PhaseAt returns the cycle phase at a unix timestamp. It is a pure function
// of now so every phase is reachable in a test by passing a constant.
func PhaseAt(now int64) Phase {
	p := (now / PhaseSeconds) % phaseCount
	if p < 0 {
		// Go's % keeps the sign of the dividend, so pre-epoch timestamps would
		// otherwise index the palette out of range.
		p += phaseCount
	}
	return Phase(p)
}

// Name is the player-facing label for a phase.
func (p Phase) Name() string {
	if p < 0 || int(p) >= len(phaseName) {
		return ""
	}
	return phaseName[p]
}

// eventAccents are the colours each event paints the frame and its banner.
// Content is data-driven, so an unknown id still has to get something.
var eventAccents = map[string]string{
	"market_day":    "220", // gold — coins
	"bumper_demand": "120", // green — growth
	"warm_front":    "215", // orange — warmth
}

const accentFallback = "213"

// eventAccentIdx is the ANSI-256 index behind EventAccent.
func eventAccentIdx(eventID string) string {
	if c, ok := eventAccents[eventID]; ok {
		return c
	}
	if eventID == "" {
		return ""
	}
	return accentFallback
}

// EventAccent is the colour an event paints the frame and its banner.
func EventAccent(eventID string) color.Color {
	idx := eventAccentIdx(eventID)
	if idx == "" {
		idx = accentFallback
	}
	return lipgloss.Color(idx)
}

// Theme is the full set of styles a render pass needs. Every style carries a
// background: a foreground-only style would punch a hole in the canvas.
type Theme struct {
	// Bg and Fg are the canvas defaults, also pushed to the terminal itself
	// via tea.View.BackgroundColor/ForegroundColor.
	Bg, Fg color.Color

	// bgIdx and fgIdx are the same two colours as raw ANSI-256 indices, which
	// is what Paint needs to emit SGR directly.
	bgIdx, fgIdx string

	Title    lipgloss.Style
	Header   lipgloss.Style
	NavOn    lipgloss.Style
	NavOff   lipgloss.Style
	NavLock  lipgloss.Style
	NavWarn  lipgloss.Style
	Hint     lipgloss.Style
	Notice   lipgloss.Style
	Ready    lipgloss.Style
	Growing  lipgloss.Style
	Empty    lipgloss.Style
	Locked   lipgloss.Style
	Selected lipgloss.Style
	Value    lipgloss.Style
	Section  lipgloss.Style
	Box      lipgloss.Style
	PlotCard lipgloss.Style
	PlotSel  lipgloss.Style
	Banner   lipgloss.Style
	Event    lipgloss.Style
	Frame    lipgloss.Style
	Rule     lipgloss.Style
	Warn     lipgloss.Style

	BoardGold   lipgloss.Style
	BoardSilver lipgloss.Style
	BoardBronze lipgloss.Style

	// Sky and SkyBright render the seasonal sky row (stars, moon, showpiece
	// star). They are built in every constructor so the all-styles
	// background test keeps holding; off-season they are quiet defaults.
	Sky       lipgloss.Style
	SkyBright lipgloss.Style
}

// New builds the theme for a phase. When solid is set the background is
// pinned to one colour and neither the cycle nor an active event moves it —
// that is the whole point of the setting. An event still recolours the frame
// and the banner, because that is event feedback, not the day/night cycle.
// eventID is "" when no event is running.
func New(p Phase, solid bool, eventID string) Theme {
	return NewWithSeason(p, solid, eventID, season.SeasonNone)
}

// NewWithSeason builds the theme for a phase while a festival skin is
// active. Precedence is deliberate: solid pins the canvas background (as with
// the cycle and the event lift), a live random event wins the frame accent and
// background lift, and the festival takes everything else. Pass
// season.SeasonNone off-season.
func NewWithSeason(p Phase, solid bool, eventID string, sn season.Season) Theme {
	if p < 0 || int(p) >= phaseCount {
		p = PhaseDawn
	}

	bgIdx := phaseBg[p]
	switch {
	case solid:
		bgIdx = bgSolid
	case eventID != "":
		bgIdx = phaseEventBg[p]
	default:
		if tint, ok := phaseSeasonBg[sn]; ok {
			bgIdx = tint[p]
		}
	}

	bg := lipgloss.Color(bgIdx)
	fg := lipgloss.Color(fgDefault)

	// base is the one place the background is applied, so a style added later
	// cannot forget it.
	base := lipgloss.NewStyle().Background(bg)
	text := func(c string) lipgloss.Style { return base.Foreground(lipgloss.Color(c)) }

	frameAccent := lipgloss.Color("65")
	if tint, ok := seasonFrameAccent[sn]; ok {
		frameAccent = lipgloss.Color(tint)
	}
	if eventID != "" {
		frameAccent = EventAccent(eventID)
	}

	eventStyle := base.Bold(true).Foreground(lipgloss.Color("232")).Padding(0, 1)
	if eventID != "" {
		eventStyle = eventStyle.Background(EventAccent(eventID))
	} else {
		eventStyle = text("229").Bold(true).Padding(0, 1)
	}

	titleColor, sectionColor := "114", "151"
	skyColor, skyBrightColor := "250", "229"
	switch sn {
	case season.SeasonHalloween:
		titleColor, sectionColor = "#c06b6b", "#aa7777"
		skyColor, skyBrightColor = "#9b5555", "#d18b8b"
	case season.SeasonChristmas:
		titleColor, sectionColor = "#8398bd", "#7189b4"
		skyColor, skyBrightColor = "#667da8", "#a9bce0"
	}

	return Theme{
		Bg:     bg,
		Fg:     fg,
		bgIdx:  bgIdx,
		fgIdx:  fgDefault,
		Title:  text(titleColor).Bold(true),
		Header: text("180"),
		NavOn:  base.Bold(true).Foreground(lipgloss.Color("229")).Background(lipgloss.Color("22")).Padding(0, 1),
		// Lifted from v1's 245/240/243: those greys were tuned against a
		// terminal-default background and vanish against near-black.
		NavOff:  text("250").Padding(0, 1),
		NavLock: text("244").Padding(0, 1),
		// NavWarn is Warn wearing the nav strip's chrome. It must keep the
		// same padding as the other tabs: the Help tab swaps to it while the
		// window is undersized, and any width difference would shift the
		// strip and every hitbox on it.
		NavWarn:  text("214").Bold(true).Padding(0, 1),
		Hint:     text("245").Italic(true),
		Notice:   text("222"),
		Ready:    text("120").Bold(true),
		Growing:  text("108"),
		Empty:    text("244"),
		Locked:   text("243"),
		Selected: text("229").Bold(true),
		Value:    text("222"),
		Section:  text(sectionColor).Bold(true),
		Box:      base.Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("65")).BorderBackground(bg).Padding(1, 2),
		PlotCard: base.Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("244")).BorderBackground(bg).Padding(0, 1).Width(20),
		PlotSel:  base.Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("114")).BorderBackground(bg).Padding(0, 1).Width(20),
		Banner:   text("222").Italic(true),
		Event:    eventStyle,
		Frame:    base.Border(lipgloss.RoundedBorder()).BorderForeground(frameAccent).BorderBackground(bg).Padding(0, 2),
		Rule:     text("240"),
		Warn:     text("214").Bold(true),

		BoardGold:   text("220").Bold(true),
		BoardSilver: text("152").Bold(true),
		BoardBronze: text("183").Bold(true),

		Sky:       text(skyColor),
		SkyBright: text(skyBrightColor).Bold(true),
	}
}

// leaderboardNameColors maps the static leaderboard name styles a player
// earns from Contract 2 (gameplay/04) to foregrounds. Styles are stored as
// IDs, never colours, so this table is the one place a colour is chosen.
// Every canvas background is near-black under every phase, event lift, and
// festival tint, so one bright tone per style stays readable throughout.
var leaderboardNameColors = map[string]string{
	"leaf":   "120",
	"gold":   "220",
	"sky":    "117",
	"rose":   "211",
	"violet": "183",
}

// nameWaveStops is the purple ramp Contract 3's animated name travels
// through. Every stop is an xterm-256 index rather than RGB: the server
// forces the TrueColor profile, so hex colours reach every client as 24-bit
// escapes, while an index is sent as-is and a 256-colour terminal draws the
// same wave a truecolour one does.
var nameWaveStops = [...]string{"97", "98", "134", "135", "141", "177", "183"}

// NameWavePeriod is how many animation steps one full wave takes.
const NameWavePeriod = 2 * (len(nameWaveStops) - 1)

// LeaderboardName returns the style for a static leaderboard name style ID,
// or false for the traditional treatment and any ID it does not know.
func (t Theme) LeaderboardName(style string) (lipgloss.Style, bool) {
	c, ok := leaderboardNameColors[style]
	if !ok {
		return lipgloss.Style{}, false
	}
	return lipgloss.NewStyle().Background(t.Bg).Foreground(lipgloss.Color(c)).Bold(true), true
}

// NameWave returns the style at step along the animated name's ramp. The
// ramp runs out and back, so consecutive steps never jump in colour.
func (t Theme) NameWave(step int) lipgloss.Style {
	step %= NameWavePeriod
	if step < 0 {
		step += NameWavePeriod
	}
	if step >= len(nameWaveStops) {
		step = NameWavePeriod - step
	}
	return lipgloss.NewStyle().Background(t.Bg).Foreground(lipgloss.Color(nameWaveStops[step])).Bold(true)
}

// sgr is the sequence that re-establishes the theme's colours.
func (t Theme) sgr() string {
	return "\x1b[" + sgrColor("38", t.fgIdx) + ";" + sgrColor("48", t.bgIdx) + "m"
}

func sgrColor(kind, value string) string {
	if len(value) == 7 && value[0] == '#' {
		parts := make([]string, 0, 3)
		for i := 1; i < 7; i += 2 {
			n, err := strconv.ParseUint(value[i:i+2], 16, 8)
			if err != nil {
				return kind + ";5;0"
			}
			parts = append(parts, strconv.FormatUint(n, 10))
		}
		return kind + ";2;" + strings.Join(parts, ";")
	}
	return kind + ";5;" + value
}

// Paint re-asserts the theme's fg/bg after every SGR reset in s.
//
// This is what makes the background actually solid. lipgloss closes every
// styled span with a reset, which drops the background for the rest of that
// line, so a Background() on the frame alone renders as stripes. Paint only
// inserts SGR sequences: printable width and ANSI-stripped text are unchanged,
// so every existing layout assertion keeps holding.
func (t Theme) Paint(s string) string {
	if t.bgIdx == "" || t.fgIdx == "" {
		return s // zero-value theme (bare Game in a unit test): nothing to paint
	}
	open := t.sgr()
	// Two things drop the palette and both are re-asserted:
	//   - a span close. lipgloss emits ESC[m; ESC[0m is the equivalent
	//     longhand, handled too so the pass does not depend on which is used.
	//   - a line break. Every line has to stand on its own, because the
	//     renderer paints cells per row and a row that opens bare shows the
	//     terminal's own background until the first styled span.
	r := strings.NewReplacer(
		"\x1b[m", "\x1b[m"+open,
		"\x1b[0m", "\x1b[0m"+open,
		"\n", "\n"+open,
	)
	return open + r.Replace(s)
}
