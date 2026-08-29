package tui

// configRow is one line of the Settings overlay. configRows is the single
// source of truth for the overlay: render, hitboxes, the wheel clamp and the
// toggle dispatch all read it, so a new setting cannot desync them. Before
// this existed the count was hardcoded in four separate places (a const in the
// key handler, the wheel clamp, the hitbox loop and the render slice), which
// is three chances to get it wrong every time a setting is added.
type configRow struct {
	label  string
	hint   string
	on     bool
	toggle func(g *Game, enabled bool)
}

// configRows returns the settings rows in display order.
func (g *Game) configRows() []configRow {
	st := g.snap.State
	if st == nil {
		return nil
	}
	return []configRow{
		{
			label: "Lucky finds", hint: "rare coin discoveries when harvesting", on: st.FlavorEnabled,
			toggle: func(g *Game, enabled bool) {
				if snap, err := g.sess.SetFlavor(g.now, enabled); err == nil {
					g.snap = snap
					g.addNotice("Lucky finds " + onOff(enabled) + ".")
				}
			},
		},
		{
			label: "News headlines", hint: "the Daily Furrow ticker at the top", on: st.NewsEnabled,
			toggle: func(g *Game, enabled bool) {
				if snap, err := g.sess.SetNews(g.now, enabled); err == nil {
					g.snap = snap
					g.addNotice("News headlines " + onOff(enabled) + ".")
				}
			},
		},
		{
			label: "Critter visits", hint: "cosmetic critters on empty plots", on: st.CrittersEnabled,
			toggle: func(g *Game, enabled bool) {
				if snap, err := g.sess.SetCritters(g.now, enabled); err == nil {
					g.snap = snap
					g.addNotice("Critter visits " + onOff(enabled) + ".")
				}
			},
		},
		{
			label: "Solid background", hint: "one fixed dark colour instead of the day/night cycle", on: st.ThemeSolid,
			toggle: func(g *Game, enabled bool) {
				if snap, err := g.sess.SetThemeSolid(g.now, enabled); err == nil {
					g.snap = snap
					g.addNotice("Solid background " + onOff(enabled) + ".")
				}
			},
		},
	}
}

// toggleConfig flips the setting at idx.
func (g *Game) toggleConfig(idx int) {
	rows := g.configRows()
	if idx < 0 || idx >= len(rows) {
		return
	}
	rows[idx].toggle(g, !rows[idx].on)
}
