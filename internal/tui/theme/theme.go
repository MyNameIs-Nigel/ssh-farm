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

	"charm.land/lipgloss/v2"
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
const PhaseSeconds = 360

// PhaseAt returns the cycle phase at a unix timestamp. It is a pure function
// of now so every phase is reachable in a test by passing a constant.
func PhaseAt(now int64) Phase { return PhaseDawn }

// Name is the player-facing label for a phase.
func (p Phase) Name() string { return "" }

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
func New(p Phase, solid bool, eventID string) Theme { return Theme{} }

// EventAccent is the colour an event paints the frame and its banner.
func EventAccent(eventID string) color.Color { return lipgloss.Color(eventAccentIdx(eventID)) }

// eventAccentIdx is the ANSI-256 index behind EventAccent.
func eventAccentIdx(eventID string) string { return "" }

// Paint re-asserts the theme's fg/bg after every SGR reset in s.
//
// This is what makes the background actually solid. lipgloss closes every
// styled span with a reset, which drops the background for the rest of that
// line, so a Background() on the frame alone renders as stripes. Paint only
// inserts SGR sequences: printable width and ANSI-stripped text are unchanged,
// so every existing layout assertion keeps holding.
func (t Theme) Paint(s string) string { return s }
