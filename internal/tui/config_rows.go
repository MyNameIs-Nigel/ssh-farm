package tui

// configRow is one line of the Settings overlay. configRows is the single
// source of truth for the overlay: render, hitboxes, the wheel clamp and the
// toggle dispatch all read it, so a new setting cannot desync them. Before
// this existed the count was hardcoded in four separate places.
type configRow struct {
	label  string
	hint   string
	on     bool
	toggle func(g *Game, enabled bool)
}

// configRows returns the settings rows in display order.
func (g *Game) configRows() []configRow { return nil }
