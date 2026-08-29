// Package theme owns how ssh-farm looks: the near-black palette, the
// day/night cycle that shifts it, and the painting helper that makes a
// background survive lipgloss's nested styles.
//
// Backgrounds are ANSI-256 indices rather than hex on purpose. wish forces a
// color profile and the common case (Terminal.app) reports xterm-256color, so
// truecolor would be quantized and adjacent near-blacks could collapse onto
// the same grey. Indices render identically everywhere.
package theme

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
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
//
// The cycle is deliberately accelerated rather than tied to the wall clock:
// SSH gives us no reliable player timezone, and a real-time cycle would never
// visibly change inside a single session.
const PhaseSeconds = 360

const phaseCount = 4

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
}

// New builds the theme for a phase. When solid is set the background is
// pinned to one colour and neither the cycle nor an active event moves it —
// that is the whole point of the setting. An event still recolours the frame
// and the banner, because that is event feedback, not the day/night cycle.
// eventID is "" when no event is running.
func New(p Phase, solid bool, eventID string) Theme {
	if p < 0 || int(p) >= phaseCount {
		p = PhaseDawn
	}

	bgIdx := phaseBg[p]
	switch {
	case solid:
		bgIdx = bgSolid
	case eventID != "":
		bgIdx = phaseEventBg[p]
	}

	bg := lipgloss.Color(bgIdx)
	fg := lipgloss.Color(fgDefault)

	// base is the one place the background is applied, so a style added later
	// cannot forget it.
	base := lipgloss.NewStyle().Background(bg)
	text := func(c string) lipgloss.Style { return base.Foreground(lipgloss.Color(c)) }

	frameAccent := lipgloss.Color("65")
	if eventID != "" {
		frameAccent = EventAccent(eventID)
	}

	eventStyle := base.Bold(true).Foreground(lipgloss.Color("232")).Padding(0, 1)
	if eventID != "" {
		eventStyle = eventStyle.Background(EventAccent(eventID))
	} else {
		eventStyle = text("229").Bold(true).Padding(0, 1)
	}

	return Theme{
		Bg:     bg,
		Fg:     fg,
		bgIdx:  bgIdx,
		fgIdx:  fgDefault,
		Title:  text("114").Bold(true),
		Header: text("180"),
		NavOn:  base.Bold(true).Foreground(lipgloss.Color("229")).Background(lipgloss.Color("22")).Padding(0, 1),
		// Lifted from v1's 245/240/243: those greys were tuned against a
		// terminal-default background and vanish against near-black.
		NavOff:   text("250").Padding(0, 1),
		NavLock:  text("244").Padding(0, 1),
		Hint:     text("245").Italic(true),
		Notice:   text("222"),
		Ready:    text("120").Bold(true),
		Growing:  text("108"),
		Empty:    text("244"),
		Locked:   text("243"),
		Selected: text("229").Bold(true),
		Value:    text("222"),
		Section:  text("151").Bold(true),
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
	}
}

// sgr is the sequence that re-establishes the theme's colours.
func (t Theme) sgr() string {
	return "\x1b[38;5;" + t.fgIdx + ";48;5;" + t.bgIdx + "m"
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
