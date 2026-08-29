package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func configGame(t *testing.T) *Game {
	t.Helper()
	f := newFixture(t)
	base := time.Now().Unix()
	g := f.newGame(t, base)
	g = dismissIntro(t, g)
	g, _ = tick(t, g, base)
	g = resize(t, g, canvasMaxWidth, canvasMaxHeight)
	g.overlay = ovConfig
	g.configIdx = 0
	return g
}

// TestConfigRowsIsTheSingleSourceOfTruth mirrors TestMarketLinesMatchViewOutput.
// The settings count used to be hardcoded in four places (the key handler's
// const, the wheel clamp, the hitbox loop and the render slice); anything that
// reads a different count than configRows() will desync the moment a setting
// is added.
func TestConfigRowsIsTheSingleSourceOfTruth(t *testing.T) {
	g := configGame(t)
	rows := g.configRows()
	if len(rows) == 0 {
		t.Fatal("configRows() returned nothing")
	}

	rendered := stripAnsi(g.viewConfig())
	for _, r := range rows {
		if !strings.Contains(rendered, r.label) {
			t.Errorf("row %q is in configRows() but not rendered:\n%s", r.label, rendered)
		}
	}

	view(g) // register hitboxes
	for i := range rows {
		id := "config:" + itoa(i)
		found := false
		for _, b := range g.hits.Boxes() {
			if b.ID == id {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("row %d (%q) has no hitbox %q", i, rows[i].label, id)
		}
	}
	for _, b := range g.hits.Boxes() {
		if strings.HasPrefix(b.ID, "config:") {
			idx, ok := b.Data.(int)
			if !ok || idx < 0 || idx >= len(rows) {
				t.Errorf("hitbox %s points at row %v, which configRows() does not have", b.ID, b.Data)
			}
		}
	}
}

// TestConfigRowHitboxesMatchRenderedText is the market-row pattern applied to
// settings: a row must be clickable exactly where its text appears.
func TestConfigRowHitboxesMatchRenderedText(t *testing.T) {
	g := configGame(t)
	view(g)
	lines := strings.Split(view(g), "\n")

	for i, r := range g.configRows() {
		want := -1
		for y := g.layout.bodyY; y < len(lines); y++ {
			if strings.Contains(stripAnsi(lines[y]), r.label) {
				want = y
				break
			}
		}
		if want < 0 {
			t.Fatalf("row %q not found in the rendered view", r.label)
		}
		got := -1
		for _, b := range g.hits.Boxes() {
			if b.ID == "config:"+itoa(i) {
				got = b.Y
				break
			}
		}
		if got != want {
			t.Errorf("row %q renders on line %d but its hitbox is at %d", r.label, want, got)
		}
	}
}

// TestSolidBackgroundIsAConfigurableSetting is the setting the player asked
// for, reachable the same way as every other one.
func TestSolidBackgroundIsAConfigurableSetting(t *testing.T) {
	g := configGame(t)
	idx := -1
	for i, r := range g.configRows() {
		if strings.Contains(strings.ToLower(r.label), "background") {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("no background setting in the Settings overlay: %+v", g.configRows())
	}

	before := g.snap.State.ThemeSolid
	g.configIdx = idx
	g = press(t, g, "enter")
	if g.snap.State.ThemeSolid == before {
		t.Fatal("toggling the background setting did not change ThemeSolid")
	}

	g = press(t, g, "enter")
	if g.snap.State.ThemeSolid != before {
		t.Fatal("toggling twice did not return to the original value")
	}
}

func TestConfigKeyboardReachesEveryRow(t *testing.T) {
	g := configGame(t)
	n := len(g.configRows())

	for i := 0; i < n*2; i++ {
		g = press(t, g, "down")
	}
	if g.configIdx != n-1 {
		t.Errorf("holding down lands on row %d, want the last row %d", g.configIdx, n-1)
	}
	for i := 0; i < n*2; i++ {
		g = press(t, g, "up")
	}
	if g.configIdx != 0 {
		t.Errorf("holding up lands on row %d, want 0", g.configIdx)
	}
}

func TestConfigWheelReachesEveryRow(t *testing.T) {
	g := configGame(t)
	n := len(g.configRows())

	for i := 0; i < n*2; i++ {
		m, _ := g.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
		g = m.(*Game)
	}
	if g.configIdx != n-1 {
		t.Errorf("wheeling down clamps at row %d, want the last row %d", g.configIdx, n-1)
	}
}

func TestConfigClickTogglesEveryRow(t *testing.T) {
	g := configGame(t)
	for i, r := range g.configRows() {
		view(g)
		var x, y int
		found := false
		for _, b := range g.hits.Boxes() {
			if b.ID == "config:"+itoa(i) {
				x, y, found = b.X, b.Y, true
				break
			}
		}
		if !found {
			t.Fatalf("row %d (%q) has no hitbox", i, r.label)
		}
		before := g.configRows()[i].on
		// The settings box is centred, so click inside it: a click on the same
		// row but outside the box is a background dismiss, not a toggle.
		m, _ := g.handleMouseClick(tea.Mouse(tea.MouseClickMsg{X: x + 1, Y: y, Button: tea.MouseLeft}))
		g = m.(*Game)
		if g.configRows()[i].on == before {
			t.Errorf("clicking row %d (%q) did not toggle it", i, r.label)
		}
	}
}

// TestStatsSummaryMentionsEverySetting keeps the at-a-glance line on the Stats
// screen from silently going stale as settings are added.
func TestStatsSummaryMentionsEverySetting(t *testing.T) {
	g := configGame(t)
	g.overlay = ovNone
	g.scr = scrStats
	summary := strings.ToLower(stripAnsi(g.viewStats()))
	for _, r := range g.configRows() {
		word := strings.ToLower(strings.Fields(r.label)[0])
		if !strings.Contains(summary, word) {
			t.Errorf("Stats settings line does not mention %q:\n%s", r.label, summary)
		}
	}
}
