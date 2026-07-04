// Package tui is the player-facing terminal interface.
package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-farm/internal/identity"
)

// Model is the Bubble Tea model type used by the Wish middleware.
type Model = tea.Model

// ProgramOption is a Bubble Tea program option.
type ProgramOption = tea.ProgramOption

type errScreen struct {
	width, height int
}

// NewErrScreen returns a minimal error screen if attachSave did not run.
func NewErrScreen() Model { return errScreen{} }

func (errScreen) Init() tea.Cmd { return nil }

func (e errScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		e.width, e.height = msg.Width, msg.Height
		return e, nil
	case tea.KeyPressMsg:
		return e, tea.Quit
	}
	return e, nil
}

func (e errScreen) View() tea.View {
	msg := "🌧 The farm could not be opened just now.\nPress any key to disconnect, then try again."
	v := tea.NewView(lipgloss.Place(max(e.width, 1), max(e.height, 1), lipgloss.Center, lipgloss.Center, msg))
	v.AltScreen = true
	v.WindowTitle = "ssh-farm"
	return v
}

type placeholder struct {
	id          identity.SessionIdentity
	width       int
	height      int
	idleTimeout int64
}

// NewPlaceholder is the framework/01 stand-in until tui/01 ports the real game UI.
func NewPlaceholder(id identity.SessionIdentity, width, height int, idleTimeout int64) Model {
	return placeholder{id: id, width: width, height: height, idleTimeout: idleTimeout}
}

func (placeholder) Init() tea.Cmd { return nil }

func (p placeholder) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
		return p, nil
	case tea.KeyPressMsg:
		return p, tea.Quit
	case tea.MouseClickMsg, tea.MouseMotionMsg, tea.MouseWheelMsg:
		return p, nil
	}
	return p, nil
}

func (p placeholder) View() tea.View {
	body := fmt.Sprintf(
		"🌾 ssh-farm\n\nSave slot: %s\n\nThe full farm UI lands in tui/01.\nPress any key to leave.",
		p.id.Slot,
	)
	v := tea.NewView(lipgloss.Place(max(p.width, 1), max(p.height, 1), lipgloss.Center, lipgloss.Center, body))
	v.AltScreen = true
	v.WindowTitle = "ssh-farm"
	return v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
