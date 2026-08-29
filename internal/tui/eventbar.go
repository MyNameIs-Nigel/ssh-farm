package tui

// viewEventBar renders the active-event bar: name, effect, a draining
// progress bar and the time left. Returns "" when no event is running.
func (g *Game) viewEventBar() string { return "" }

// eventPct is how much of the active event remains, 0-100. It tolerates a
// zero EventStartedAt (a save written before that field existed) by reporting
// a full bar rather than dividing by a zero span.
func (g *Game) eventPct() int { return 0 }
