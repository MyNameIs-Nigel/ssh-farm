package tui

import (
	tea "charm.land/bubbletea/v2"
)

func (g *Game) guestLocked(action string) {
	g.addNotice("Guest: an SSH key is required to " + action + " and save progress. Run ssh-keygen -t ed25519 locally, then reconnect.")
}
func (g *Game) requestQuit() (tea.Model, tea.Cmd) {
	if g.id.IsGuest() {
		g.overlay = ovGuestQuit
		return g, nil
	}
	return g, tea.Quit
}
func (g *Game) savedCopy(keyed, guest string) string {
	if g.id.IsGuest() {
		return guest
	}
	return keyed
}
func (g *Game) viewGuestQuit() string {
	th := g.theme()
	return th.Box.Render(th.Section.Render("Leave this guest farm?") + "\n\n" + th.Value.Render(rewrap("Your progress will be discarded. Reconnect with an SSH key to save future progress.", g.overlayTextWidth())) + "\n\n" + th.Hint.Render("Enter: leave · Esc: stay"))
}
