package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// viewEventBar renders the active-event bar: name, effect, a draining
// progress bar and the time left. Returns "" when no event is running.
//
// Events fire every 15-25 minutes and last 90-180 seconds, so the single most
// interesting thing in the idle loop used to be the easiest to miss: one
// quiet line of text in the header. The bar spans the content width and the
// frame takes the event's accent colour, which is hard to overlook.
func (g *Game) viewEventBar() string {
	st := g.snap.State
	if st == nil || !st.EventActive(g.now) {
		return ""
	}
	ev := g.content.EventByID(st.EventID)
	if ev == nil {
		return ""
	}
	th := g.theme()

	left := "⚡ " + strings.ToUpper(sanitizeText(ev.Name))
	if effect := eventHelpEffect(*ev); effect != "" {
		left += " · " + sanitizeText(effect)
	}
	right := duration(st.EventEndsAt-g.now) + " left"

	cw := g.contentWidth()
	// The bar is whatever room is left between the two labels, inside a
	// sensible range; on a narrow terminal it is dropped entirely rather than
	// squeezing the name.
	barW := cw - lipgloss.Width(left) - lipgloss.Width(right) - 6
	if barW > 16 {
		barW = 16
	}
	if barW >= 4 {
		right = progressBar(g.eventPct(), barW) + "  " + right
	}
	return th.Event.Render(alignSides(left, right, cw-2))
}

// eventPct is how much of the active event remains, 0-100. It tolerates a
// zero EventStartedAt (a save written before that field existed) by reporting
// a full bar rather than dividing by a zero span.
func (g *Game) eventPct() int {
	st := g.snap.State
	if st == nil || st.EventEndsAt == 0 {
		return 0
	}
	if st.EventStartedAt == 0 || st.EventStartedAt >= st.EventEndsAt {
		return 100
	}
	total := st.EventEndsAt - st.EventStartedAt
	remaining := st.EventEndsAt - g.now
	switch {
	case remaining <= 0:
		return 0
	case remaining >= total:
		return 100
	}
	return int(remaining * 100 / total)
}
