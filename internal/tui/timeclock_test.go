package tui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-farm/internal/tui/theme"
)

func primaryHeaderLine(g *Game) string {
	return stripAnsi(strings.Split(g.viewHeader(), "\n")[0])
}

func TestHeaderShowsAcceleratedGameClock(t *testing.T) {
	tests := []struct {
		name   string
		offset int64
		want   string
	}{
		{name: "dawn starts at 06:00", offset: 0, want: "06:00"},
		{name: "last minute of dawn", offset: theme.PhaseSeconds - 1, want: "11:59"},
		{name: "day starts at noon", offset: theme.PhaseSeconds, want: "12:00"},
		{name: "dusk starts at 18:00", offset: 2 * theme.PhaseSeconds, want: "18:00"},
		{name: "night starts at midnight", offset: 3 * theme.PhaseSeconds, want: "00:00"},
		{name: "last minute of night", offset: 4*theme.PhaseSeconds - 1, want: "05:59"},
		{name: "clock wraps to the next dawn", offset: 4 * theme.PhaseSeconds, want: "06:00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := goldenGame(t, canvasMaxWidth, canvasMaxHeight)
			g.now = goldenBase + tt.offset

			header := primaryHeaderLine(g)
			want := g.id.Slot + "  ·  " + tt.want
			if !strings.Contains(header, want) {
				t.Errorf("primary header = %q, want slot followed by in-game clock %q", header, want)
			}
		})
	}
}

func TestHeaderClockSitsBetweenSlotAndWallet(t *testing.T) {
	g := goldenGame(t, canvasMaxWidth, canvasMaxHeight)
	header := primaryHeaderLine(g)

	slotAt := strings.Index(header, g.id.Slot)
	clockAt := strings.Index(header, "06:00")
	walletAt := strings.Index(header, "⛀")
	if slotAt < 0 || clockAt < 0 || walletAt < 0 {
		t.Fatalf("primary header = %q, want slot, clock, and wallet", header)
	}
	if !(slotAt < clockAt && clockAt < walletAt) {
		t.Errorf("primary header = %q, want slot before clock before wallet", header)
	}
}

func TestHeaderClockReplacesMoonPhase(t *testing.T) {
	g := goldenGame(t, canvasMaxWidth, canvasMaxHeight)
	st := g.snap.State
	st.UpdatedAt = (st.MoonEpoch + int64(g.content.Moon.CycleDays/2)) * 86400
	if got := st.MoonPhaseName(g.content); got != "Full Moon" {
		t.Fatalf("test setup moon phase = %q, want Full Moon", got)
	}

	header := primaryHeaderLine(g)
	for _, stale := range []string{"Full Moon", "🌕", "🌑", "🌙"} {
		if strings.Contains(header, stale) {
			t.Errorf("primary header = %q, still contains replaced moon display %q", header, stale)
		}
	}
}

func TestTimeClockHeaderFitsSupportedWidths(t *testing.T) {
	for _, width := range []int{80, canvasMaxWidth} {
		t.Run(fmt.Sprintf("%d-columns", width), func(t *testing.T) {
			g := goldenGame(t, width, 24)
			g.id.Slot = strings.Repeat("s", 32)
			g.snap.State.FarmName = strings.Repeat("F", 20)

			line := strings.Split(g.viewHeader(), "\n")[0]
			if got, want := lipgloss.Width(line), g.contentWidth(); got != want {
				t.Errorf("primary header width = %d, want content width %d at terminal width %d", got, want, width)
			}
		})
	}
}

func TestGameplayHelpExplainsTheMoonberryNightWindow(t *testing.T) {
	g := goldenGame(t, canvasMaxWidth, canvasMaxHeight)
	help := stripAnsi(g.viewHelpGameplay())

	if strings.Contains(help, "Moon phases cycle every "+money(g.content.Moon.CycleDays)+" days in the header") {
		t.Error("gameplay help still says moon phases appear in the header")
	}
	// The Full Moon trigger was replaced by the in-game clock window, so help
	// must describe the window rather than the moon phase.
	if strings.Contains(help, "Full Moon") {
		t.Error("gameplay help still credits Full Moon for the Moonberry bonus")
	}
	for _, want := range []string{"20:00", "03:59", "Moonberry"} {
		if !strings.Contains(help, want) {
			t.Errorf("gameplay help = %q, want it to mention %q", help, want)
		}
	}
}
