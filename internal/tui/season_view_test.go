package tui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/mynameis-nigel/ssh-farm/internal/tui/theme"
)

// festivalUnix is noon UTC on a calendar date: the season is set, and the
// in-game phase is whatever the cycle says (use alignToPhase to pick one).
func festivalUnix(y, m, d int) int64 {
	return time.Date(y, time.Month(m), d, 12, 0, 0, 0, time.UTC).Unix()
}

// alignToPhase walks forward in whole phases so the timestamp keeps its
// calendar date's season while landing on the wanted day/night phase.
func alignToPhase(base int64, want theme.Phase) int64 {
	for i := int64(0); i < 4; i++ {
		if cand := base + i*theme.PhaseSeconds; theme.PhaseAt(cand) == want {
			return cand
		}
	}
	return base
}

// seasonalGame builds the deterministic golden game with its clock parked on
// a festival date.
func seasonalGame(t *testing.T, w, h int, now int64) *Game {
	t.Helper()
	g := goldenGame(t, w, h)
	g.now = now
	g.snap.Now = now
	g.SetTrueColor(true)
	return g
}

func TestViewSkyEmptyOffSeason(t *testing.T) {
	g := seasonalGame(t, 100, 35, festivalUnix(2024, 11, 14))
	if got := g.viewSky(); got != "" {
		t.Fatalf("off-season sky = %q, want empty", stripAnsi(got))
	}
}

func TestViewSkyEmptyWhenThemesOff(t *testing.T) {
	g := seasonalGame(t, 100, 35, alignToPhase(festivalUnix(2024, 10, 15), theme.PhaseNight))
	g.snap.State.SeasonalOff = true
	if got := g.viewSky(); got != "" {
		t.Fatalf("disabled sky = %q, want empty", stripAnsi(got))
	}
}

func TestViewSkyChristmasStarsOnlyAtNight(t *testing.T) {
	night := seasonalGame(t, 100, 35, alignToPhase(festivalUnix(2024, 12, 10), theme.PhaseNight))
	sky := stripAnsi(night.viewSky())
	if sky == "" {
		t.Fatal("christmas night should have a star field")
	}
	if !strings.ContainsAny(sky, "✦✧") {
		t.Fatalf("christmas night sky = %q, want stars", sky)
	}
	day := seasonalGame(t, 100, 35, alignToPhase(festivalUnix(2024, 12, 10), theme.PhaseDay))
	if got := day.viewSky(); got != "" {
		t.Fatalf("christmas day sky = %q, want clear skies", stripAnsi(got))
	}
}

func TestViewSkyChristmasDayStarDayAndNight(t *testing.T) {
	for _, p := range []theme.Phase{theme.PhaseDay, theme.PhaseNight} {
		g := seasonalGame(t, 100, 35, alignToPhase(festivalUnix(2024, 12, 25), p))
		sky := stripAnsi(g.viewSky())
		if !strings.Contains(sky, "★") {
			t.Fatalf("phase %v: christmas day sky = %q, want the great star", p, sky)
		}
	}
}

func TestViewSkyHalloweenBatsAndPumpkins(t *testing.T) {
	night := seasonalGame(t, 100, 35, alignToPhase(festivalUnix(2024, 10, 15), theme.PhaseNight))
	if sky := stripAnsi(night.viewSky()); !strings.Contains(sky, "🦇") || !strings.Contains(sky, "🌙") {
		t.Fatalf("halloween night sky = %q, want bats and moon", sky)
	}
	day := seasonalGame(t, 100, 35, alignToPhase(festivalUnix(2024, 10, 15), theme.PhaseDay))
	if sky := stripAnsi(day.viewSky()); !strings.Contains(sky, "🎃") {
		t.Fatalf("halloween day sky = %q, want pumpkins", sky)
	}
}

func TestViewSkyHalloweenNightMoonDayAndNight(t *testing.T) {
	for _, p := range []theme.Phase{theme.PhaseDay, theme.PhaseNight} {
		g := seasonalGame(t, 100, 35, alignToPhase(festivalUnix(2024, 10, 31), p))
		sky := stripAnsi(g.viewSky())
		if !strings.Contains(sky, "🌕") {
			t.Fatalf("phase %v: oct 31 sky = %q, want the great moon", p, sky)
		}
	}
}

func TestTitleGlyphFollowsSeason(t *testing.T) {
	cases := []struct {
		name string
		now  int64
		want string
	}{
		{"plain november", festivalUnix(2024, 11, 14), "🌾"},
		{"halloween", festivalUnix(2024, 10, 15), "🎃"},
		{"christmas", festivalUnix(2024, 12, 10), "🎄"},
	}
	for _, c := range cases {
		g := seasonalGame(t, 100, 35, c.now)
		if header := stripAnsi(g.viewHeader()); !strings.Contains(header, c.want+" ") {
			t.Errorf("%s: header %q does not open with %q", c.name, header, c.want)
		}
	}
	off := seasonalGame(t, 100, 35, festivalUnix(2024, 10, 15))
	off.snap.State.SeasonalOff = true
	if header := stripAnsi(off.viewHeader()); !strings.Contains(header, "🌾") {
		t.Errorf("disabled themes: header %q should keep the wheat glyph", header)
	}
}

func TestSeasonalHeadlineRotatesDaily(t *testing.T) {
	g := seasonalGame(t, 100, 35, festivalUnix(2024, 10, 15))
	got := g.dailyHeadline()
	found := false
	for _, h := range halloweenHeadlines {
		if got == h {
			found = true
		}
	}
	if !found {
		t.Fatalf("halloween headline = %q, want one of the festival pool", got)
	}
	// A different festival day may rotate to a different line, but it stays
	// in the pool (5 headlines, day-of-year rotation).
	other := seasonalGame(t, 100, 35, festivalUnix(2024, 10, 20))
	inPool := false
	for _, h := range halloweenHeadlines {
		if other.dailyHeadline() == h {
			inPool = true
		}
	}
	if !inPool {
		t.Fatalf("oct 20 headline = %q, outside the festival pool", other.dailyHeadline())
	}
	if g := seasonalGame(t, 100, 35, festivalUnix(2024, 11, 14)); g.seasonalHeadline() != "" {
		t.Fatal("off-season seasonal headline should be empty")
	}
}

func TestBannerRespectsNewsToggleInSeason(t *testing.T) {
	g := seasonalGame(t, 100, 35, festivalUnix(2024, 12, 10))
	g.snap.State.NewsEnabled = false
	if got := g.viewBanner(); got != "" {
		t.Fatalf("news off should silence the ticker even in season, got %q", stripAnsi(got))
	}
}

func TestCritterNameReskin(t *testing.T) {
	halloween := seasonalGame(t, 100, 35, festivalUnix(2024, 10, 15))
	for raw, want := range map[string]string{"crow": "bat", "rabbit": "ghost", "mole": "gremlin"} {
		if got := halloween.critterName(raw); got != want {
			t.Errorf("halloween %q = %q, want %q", raw, got, want)
		}
	}
	christmas := seasonalGame(t, 100, 35, festivalUnix(2024, 12, 10))
	for raw, want := range map[string]string{"crow": "robin", "rabbit": "hare", "mole": "mouse"} {
		if got := christmas.critterName(raw); got != want {
			t.Errorf("christmas %q = %q, want %q", raw, got, want)
		}
	}
	plain := seasonalGame(t, 100, 35, festivalUnix(2024, 11, 14))
	if got := plain.critterName("crow"); got != "crow" {
		t.Errorf("off-season crow = %q, want crow", got)
	}
	if got := halloween.critterName("capybara"); got != "capybara" {
		t.Errorf("unknown critter = %q, want passthrough", got)
	}
	off := seasonalGame(t, 100, 35, festivalUnix(2024, 10, 15))
	off.snap.State.SeasonalOff = true
	if got := off.critterName("crow"); got != "crow" {
		t.Errorf("disabled themes: crow = %q, want crow", got)
	}
}

func TestSeasonMarker(t *testing.T) {
	g := seasonalGame(t, 100, 35, festivalUnix(2024, 10, 15))
	for _, crop := range g.content.Crops {
		m := seasonMarker(crop)
		if crop.Unlock.Kind != "season" {
			if m != "" {
				t.Errorf("ordinary crop %q has marker %q", crop.ID, m)
			}
			continue
		}
		if !strings.HasSuffix(m, " seasonal") {
			t.Errorf("seasonal crop %q marker = %q, want compact seasonal label", crop.ID, m)
		}
		if strings.Contains(m, "gone ") {
			t.Errorf("seasonal crop %q marker = %q, expiry belongs in Help", crop.ID, m)
		}
	}
}

func TestSeasonalSeedsInPickerOnlyInSeason(t *testing.T) {
	inSeason := seasonalGame(t, 100, 35, festivalUnix(2024, 10, 15))
	inSeason.overlay = ovPicker
	picker := stripAnsi(inSeason.viewPicker())
	if !strings.Contains(picker, "Lanternberry") {
		t.Fatalf("october picker should list Lanternberry:\n%s", picker)
	}
	if strings.Contains(picker, "Snowbell") {
		t.Fatalf("october picker should not list Snowbell:\n%s", picker)
	}
	if strings.Contains(picker, "gone ") {
		t.Fatalf("picker expiry copy can overflow the viewport:\n%s", picker)
	}
	off := seasonalGame(t, 100, 35, festivalUnix(2024, 11, 14))
	off.overlay = ovPicker
	novPicker := stripAnsi(off.viewPicker())
	if strings.Contains(novPicker, "Lanternberry") || strings.Contains(novPicker, "Snowbell") {
		t.Fatalf("november picker should hide seasonal seeds:\n%s", novPicker)
	}
	// The look toggle never hides seeds: off but in-season still lists them.
	dark := seasonalGame(t, 100, 35, festivalUnix(2024, 10, 15))
	dark.snap.State.SeasonalOff = true
	dark.overlay = ovPicker
	if darkPicker := stripAnsi(dark.viewPicker()); !strings.Contains(darkPicker, "Lanternberry") {
		t.Fatalf("disabled look should still list in-season seeds:\n%s", darkPicker)
	}
}

func TestSkyRendersAboveFrameAndFitsViewport(t *testing.T) {
	g := seasonalGame(t, canvasMaxWidth, canvasMaxHeight, alignToPhase(festivalUnix(2024, 10, 15), theme.PhaseNight))
	if strings.Contains(stripAnsi(g.viewHeader()), "🦇") {
		t.Fatal("sky must not be part of the framed header")
	}
	rendered := stripAnsi(g.composeCanvas(g.screenBody(), false))
	lines := strings.Split(rendered, "\n")
	if !strings.Contains(lines[0], "🦇") || !strings.Contains(lines[1], "╭") {
		t.Fatalf("first two rows should be sky then frame border:\n%s", rendered)
	}
	if got := lipgloss.Height(rendered); got > g.height {
		t.Fatalf("seasonal canvas height = %d, exceeds %d-row viewport", got, g.height)
	}
}

func TestSeasonalHelpOwnsExactEndTimes(t *testing.T) {
	g := seasonalGame(t, 100, 38, festivalUnix(2024, 10, 15))
	help := stripAnsi(g.viewHelpGameplay())
	for _, cutoff := range []string{"Oct 31 at 23:59 UTC", "Dec 25 at 23:59 UTC"} {
		if !strings.Contains(help, cutoff) {
			t.Errorf("seasonal Help missing cutoff %q", cutoff)
		}
	}
}

func TestConfigSeasonalToggle(t *testing.T) {
	f := newFixture(t)
	g := f.newGame(t, festivalUnix(2024, 10, 15))
	g = dismissIntro(t, g)
	g.scr = scrStats
	g = press(t, g, "c")
	if g.overlay != ovConfig {
		t.Fatal("c should open the config overlay")
	}
	if n := len(g.configRows()); n != 5 {
		t.Fatalf("config rows = %d, want 5 with seasonal themes", n)
	}
	if label := g.configRows()[4].label; label != "Seasonal themes" {
		t.Fatalf("row 4 = %q, want the seasonal toggle last", label)
	}
	g = press(t, g, "down", "down", "down", "down", "enter")
	if g.snap.State.SeasonalEnabled() {
		t.Fatal("toggling row 4 should opt out of seasonal themes")
	}
	if got := stripAnsi(g.viewSky()); got != "" {
		t.Fatalf("sky should vanish when opted out, got %q", got)
	}
	g = press(t, g, "enter")
	if !g.snap.State.SeasonalEnabled() {
		t.Fatal("toggling row 4 again should opt back in")
	}
}
