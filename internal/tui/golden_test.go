package tui

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mynameis-nigel/ssh-farm/internal/leaderboard"
	"github.com/mynameis-nigel/ssh-farm/internal/sim"
	"github.com/mynameis-nigel/ssh-farm/internal/tui/theme"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/golden")

// goldenBase is a fixed timestamp so every golden is reproducible. It is a
// multiple of the phase length, so goldenBase itself sits at the start of
// PhaseDawn and each phase is reachable by adding whole phases.
const goldenBase int64 = 1_700_000_000 - (1_700_000_000 % (4 * theme.PhaseSeconds))

// goldenGame builds a fully deterministic game. A fresh save is created with a
// random RNG seed and the fixture clock is time.Now(), so the snapshot is
// replaced wholesale with a known state — goldens must not depend on when the
// suite happens to run.
func goldenGame(t *testing.T, w, h int) *Game {
	t.Helper()
	f := newFixture(t)
	g := f.newGame(t, goldenBase)
	g = dismissIntro(t, g)
	g, _ = tick(t, g, goldenBase)

	g.snap.State = sim.New(f.content, 7, goldenBase)
	g.snap.Now = goldenBase
	g.now = goldenBase
	g.notices = nil
	g.scr = scrFarm
	g.overlay = ovNone
	return resize(t, g, w, h)
}

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".txt")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v\n\nrun: go test ./internal/tui -run TestGolden -update", err)
	}
	if got == string(want) {
		return
	}
	// Show the visible diff first — a styled golden is unreadable raw.
	gotLines := strings.Split(stripAnsi(got), "\n")
	wantLines := strings.Split(stripAnsi(string(want)), "\n")
	for i := 0; i < len(gotLines) || i < len(wantLines); i++ {
		var gl, wl string
		if i < len(gotLines) {
			gl = gotLines[i]
		}
		if i < len(wantLines) {
			wl = wantLines[i]
		}
		if gl != wl {
			t.Errorf("%s: first visible difference on line %d:\n got: %q\nwant: %q", name, i, gl, wl)
			break
		}
	}
	if stripAnsi(got) == stripAnsi(string(want)) {
		t.Errorf("%s: text matches but styling changed (this is a palette change)", name)
	}
	t.Fatalf("%s differs from its golden; re-run with -update to accept", name)
}

// TestGoldenRenders pins the styled output of the screens this feature
// changes. Goldens store the ANSI, not just the text, because the whole point
// of the change is what colour each cell is — a text-only golden would pass
// even if the background vanished entirely.
func TestGoldenRenders(t *testing.T) {
	cases := []struct {
		name  string
		w, h  int
		setup func(g *Game)
	}{
		{"farm-dawn", canvasMaxWidth, canvasMaxHeight, nil},
		{"farm-day", canvasMaxWidth, canvasMaxHeight, func(g *Game) {
			g.now = goldenBase + theme.PhaseSeconds
		}},
		{"farm-night", canvasMaxWidth, canvasMaxHeight, func(g *Game) {
			g.now = goldenBase + 3*theme.PhaseSeconds
		}},
		{"farm-solid", canvasMaxWidth, canvasMaxHeight, func(g *Game) {
			g.now = goldenBase + theme.PhaseSeconds
			g.snap.State.ThemeSolid = true
		}},
		{"farm-event", canvasMaxWidth, canvasMaxHeight, func(g *Game) {
			st := g.snap.State
			st.EventID = "market_day"
			st.EventStartedAt = goldenBase
			st.EventEndsAt = goldenBase + 120
		}},
		// The reported environment: a stock 80x24 terminal, which is below
		// the recommended 100x30 and so renders the Help tab as a warning.
		{"farm-night-80x24", 80, 24, func(g *Game) {
			g.now = goldenBase + 3*theme.PhaseSeconds
		}},
		{"settings-80x24", 80, 24, func(g *Game) {
			g.overlay = ovConfig
		}},
		// Festival goldens pin the seasonal palette, sky row and glyphs. The
		// dates are fixed, so headlines and sky variants are deterministic.
		{"farm-halloween", canvasMaxWidth, canvasMaxHeight, func(g *Game) {
			g.now = alignToPhase(festivalUnix(2024, 10, 15), theme.PhaseDay)
		}},
		{"farm-halloween-night", canvasMaxWidth, canvasMaxHeight, func(g *Game) {
			g.now = alignToPhase(festivalUnix(2024, 10, 15), theme.PhaseNight)
		}},
		{"farm-christmas-night", canvasMaxWidth, canvasMaxHeight, func(g *Game) {
			g.now = alignToPhase(festivalUnix(2024, 12, 10), theme.PhaseNight)
		}},
		{"farm-christmas-day", canvasMaxWidth, canvasMaxHeight, func(g *Game) {
			g.now = alignToPhase(festivalUnix(2024, 12, 25), theme.PhaseDay)
		}},
	}

	// Scarecrow goldens (gameplay/05) pin the column beside a full farm in
	// each palette the game can show, plus the selected, for-sale, compact
	// and Market renders.
	cases = append(cases, []struct {
		name  string
		w, h  int
		setup func(g *Game)
	}{
		{"farm-scarecrows", canvasMaxWidth, canvasMaxHeight, func(g *Game) {
			g.now = goldenBase + theme.PhaseSeconds
			lateFarm(g)
		}},
		{"farm-scarecrows-selected", canvasMaxWidth, canvasMaxHeight, func(g *Game) {
			g.now = goldenBase + theme.PhaseSeconds
			lateFarm(g)
			g.scareSel = 1
		}},
		{"farm-scarecrows-night", canvasMaxWidth, canvasMaxHeight, func(g *Game) {
			g.now = goldenBase + 3*theme.PhaseSeconds
			lateFarm(g)
		}},
		{"farm-scarecrows-event", canvasMaxWidth, canvasMaxHeight, func(g *Game) {
			g.now = goldenBase + theme.PhaseSeconds
			lateFarm(g)
			st := g.snap.State
			st.EventID = "market_day"
			st.EventStartedAt = g.now - 30
			st.EventEndsAt = g.now + 90
		}},
		{"farm-scarecrows-halloween", canvasMaxWidth, canvasMaxHeight, func(g *Game) {
			g.now = alignToPhase(festivalUnix(2024, 10, 15), theme.PhaseNight)
			lateFarm(g)
		}},
		{"farm-scarecrows-forsale", canvasMaxWidth, canvasMaxHeight, func(g *Game) {
			g.now = goldenBase + theme.PhaseSeconds
			st := g.snap.State
			st.Plots = []sim.Plot{
				{Crop: "turnip", PlantedAt: g.now - 30}, {Crop: "pumpkin", PlantedAt: g.now - 10},
				{Crop: "turnip", PlantedAt: g.now - 600}, {},
				{Crop: "turnip", PlantedAt: g.now - 45}, {Critter: "crow"},
			}
			st.Coins = 250
			st.RunEarnings = 4_000
			st.Rebirths = 1 // past the 3rd's unlock_rebirths gate, so it is for sale
			st.Scarecrows = []sim.Scarecrow{{Pouch: 18}, {Pouch: 7}}
			st.ScarecrowClock = 11 * 20
		}},
		{"farm-scarecrows-locked", canvasMaxWidth, canvasMaxHeight, func(g *Game) {
			// A first run with two scarecrows: the 3rd waits on a rebirth.
			g.now = goldenBase + theme.PhaseSeconds
			st := g.snap.State
			st.Plots = make([]sim.Plot, 12)
			for i := range st.Plots {
				st.Plots[i] = sim.Plot{Crop: "turnip", PlantedAt: g.now - int64(i*7)}
			}
			st.Plots[2] = sim.Plot{Crop: "pumpkin", PlantedAt: g.now - 900}
			st.Plots[7] = sim.Plot{Crop: "turnip", PlantedAt: g.now - 600}
			st.Plots[9] = sim.Plot{Critter: "crow"}
			st.Coins = 4_210
			st.RunEarnings = 6_500
			st.Scarecrows = []sim.Scarecrow{{Pouch: 64}, {Pouch: 0}}
			st.ScarecrowClock = 4 * 20
		}},
		{"farm-scarecrows-80x24", 80, 24, func(g *Game) {
			g.now = goldenBase + theme.PhaseSeconds
			lateFarm(g)
			g.cursor = 15
			g.scareSel = 2
		}},
		{"market-scarecrows", canvasMaxWidth, canvasMaxHeight, func(g *Game) {
			g.now = goldenBase + theme.PhaseSeconds
			st := g.snap.State
			st.Coins = 5_000
			st.Rebirths = 1
			st.Scarecrows = []sim.Scarecrow{{}, {}}
			g.scr = scrMarket
			g.marketIdx = 2
		}},
		{"market-scarecrows-locked", canvasMaxWidth, canvasMaxHeight, func(g *Game) {
			g.now = goldenBase + theme.PhaseSeconds
			st := g.snap.State
			st.Coins = 5_000
			st.Scarecrows = []sim.Scarecrow{{}, {}}
			g.scr = scrMarket
			g.marketIdx = 2
		}},
	}...)

	// Fertilizer goldens (gameplay/06): the prompt at both sizes, the
	// StarShop row, a compacted header wallet, and the contract reward.
	growing := func(g *Game) {
		st := g.snap.State
		st.Plots = []sim.Plot{
			{Crop: "turnip", PlantedAt: g.now - 20}, {Crop: "pumpkin", PlantedAt: g.now - 600},
			{Crop: "turnip", PlantedAt: g.now - 600},
		}
	}
	cases = append(cases, []struct {
		name  string
		w, h  int
		setup func(g *Game)
	}{
		{"fertilizer-confirm", canvasMaxWidth, canvasMaxHeight, func(g *Game) {
			g.now = goldenBase + theme.PhaseSeconds
			growing(g)
			g.snap.State.FertilizerHeld, g.snap.State.FertilizerTracked = 2, true
			g.overlay = ovFertilizerConfirm
		}},
		{"fertilizer-confirm-80x24", 80, 24, func(g *Game) {
			g.now = goldenBase + 3*theme.PhaseSeconds
			growing(g)
			g.overlay = ovFertilizerConfirm
			g.fertDontAsk = true
		}},
		{"starshop-fertilizer", canvasMaxWidth, canvasMaxHeight, func(g *Game) {
			g.now = goldenBase + theme.PhaseSeconds
			st := g.snap.State
			st.Rebirths, st.PrestigeCurrency = 2, 14
			st.ContractsUnlocked = true
			g.scr = scrStarShop
			g.progressIdx = fertilizerRowIdx(g.content)
		}},
		{"starshop-fertilizer-80x24", 80, 24, func(g *Game) {
			g.now = goldenBase + theme.PhaseSeconds
			st := g.snap.State
			st.Rebirths, st.PrestigeCurrency = 2, 3
			st.ContractsUnlocked = true
			st.ContractsCompleted = 1
			st.FertilizerHeld, st.FertilizerTracked = 4, true
			g.scr = scrStarShop
			g.progressIdx = fertilizerRowIdx(g.content)
		}},
		{"header-wallet-compact-80x24", 80, 24, func(g *Game) {
			g.now = goldenBase + theme.PhaseSeconds
			st := g.snap.State
			st.FarmName = "Northfield"
			st.Coins = 9_123_456_789_012
			st.Rebirths, st.PrestigeCurrency = 40, 1_234_567
			growing(g)
		}},
		{"contract-reward", canvasMaxWidth, canvasMaxHeight, func(g *Game) {
			g.now = goldenBase + theme.PhaseSeconds
			st := g.snap.State
			st.ContractsCompleted = 1
			st.FertilizerHeld, st.FertilizerTracked = 4, true
			g.completedContract = sim.ContractBareHands
			g.overlay = ovContractReward
		}},
		// Issue #59: every colour has a wave. One row per wave, mid-phase so
		// each ramp is visibly travelling, beside the static colours and a
		// traditional name for comparison.
		{"board-name-waves", canvasMaxWidth, canvasMaxHeight, func(g *Game) {
			g.now = goldenBase + 3*theme.PhaseSeconds
			g.scr = scrBoard
			g.boardAnimPhase = 4
			g.lbBoard = leaderboard.Board{Total: 12, AsOf: g.now - 42, Rows: []leaderboard.Row{
				{Rank: 1, DisplayName: "LEAF WAVE FARM", ContractsCompleted: 3, NameStyle: sim.NameStyleLeafWave, Coins: 9_000_000, Rebirths: 40},
				{Rank: 2, DisplayName: "GOLD WAVE FARM", ContractsCompleted: 3, NameStyle: sim.NameStyleGoldWave, Coins: 8_000_000, Rebirths: 38},
				{Rank: 3, DisplayName: "SKY WAVE FARM", ContractsCompleted: 3, NameStyle: sim.NameStyleSkyWave, Coins: 7_000_000, Rebirths: 35},
				{Rank: 4, DisplayName: "ROSE WAVE FARM", ContractsCompleted: 3, NameStyle: sim.NameStyleRoseWave, Coins: 6_000_000, Rebirths: 30},
				{Rank: 5, DisplayName: "VIOLET WAVE FARM", ContractsCompleted: 3, NameStyle: sim.NameStylePurpleWave, Coins: 5_000_000, Rebirths: 29},
				{Rank: 6, DisplayName: "Leaf Static", ContractsCompleted: 3, NameStyle: sim.NameStyleLeaf, Coins: 4_000_000, Rebirths: 20},
				{Rank: 7, DisplayName: "Gold Static", ContractsCompleted: 2, NameStyle: sim.NameStyleGold, Coins: 3_000_000, Rebirths: 18},
				{Rank: 8, DisplayName: "Sky Static", ContractsCompleted: 2, NameStyle: sim.NameStyleSky, Coins: 2_000_000, Rebirths: 15},
				{Rank: 9, DisplayName: "Rose Static", ContractsCompleted: 2, NameStyle: sim.NameStyleRose, Coins: 1_000_000, Rebirths: 12},
				{Rank: 10, DisplayName: "Violet Static", ContractsCompleted: 2, NameStyle: sim.NameStyleViolet, Coins: 900_000, Rebirths: 10},
			}}
		}},
		// The board is the whole ranked list. Fits on screen: no divider, You
		// in the list ("#15 of 19", the layout this was reported from).
		{"board-whole-list-fits", 100, 32, func(g *Game) {
			g.scr = scrBoard
			g.lbBoard = goldenBoardOf(19, 15, g.now)
		}},
		// Longer than the screen with You below it: the list from #1, a
		// divider, then You ±3.
		{"board-pinned-you", 100, 32, func(g *Game) {
			g.scr = scrBoard
			g.lbBoard = goldenBoardOf(60, 41, g.now)
		}},
		// The same board scrolled until You is in the list: the divider and
		// the pinned block are gone and the ranks run on unbroken.
		{"board-merged-you", 100, 32, func(g *Game) {
			g.scr = scrBoard
			g.lbBoard = goldenBoardOf(60, 41, g.now)
			g.lbScroll = 32
		}},
		// Last place, scrolled to the end: You on the bottom row of the list.
		{"board-last-place", 100, 32, func(g *Game) {
			g.scr = scrBoard
			g.lbBoard = goldenBoardOf(60, 60, g.now)
			g.lbScroll = 60
		}},
		{"stats-name-wave-80x24", 80, 24, func(g *Game) {
			g.now = goldenBase + 3*theme.PhaseSeconds
			st := g.snap.State
			st.FarmName = "Northfield"
			st.ContractsUnlocked = true
			st.ContractsCompleted = 3
			st.LeaderboardNameStyle = sim.NameStyleSkyWave
			g.scr = scrStats
		}},
	}...)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := goldenGame(t, c.w, c.h)
			if c.setup != nil {
				c.setup(g)
			}
			assertGolden(t, c.name, view(g))
		})
	}
}

// TestGoldenRendersAreDeterministic catches the failure mode that makes a
// golden suite worthless: a render that varies run to run would otherwise
// only show up as a mysterious CI failure later.
func TestGoldenRendersAreDeterministic(t *testing.T) {
	a := view(goldenGame(t, canvasMaxWidth, canvasMaxHeight))
	b := view(goldenGame(t, canvasMaxWidth, canvasMaxHeight))
	if a != b {
		t.Fatal("two identically-built games rendered differently; the golden fixture is not deterministic")
	}
}

// lateFarm is the screenshot that motivated gameplay/05: sixteen plots, all
// busy, and four scarecrows whose pouches span empty to compacted-huge.
func lateFarm(g *Game) {
	st := g.snap.State
	st.Plots = make([]sim.Plot, 16)
	for i := range st.Plots {
		st.Plots[i] = sim.Plot{Crop: "pumpkin", PlantedAt: g.now - int64(i*97), AutoHarvest: i%3 == 0, AutoSow: i%6 == 0}
	}
	st.Plots[5] = sim.Plot{Crop: "turnip", PlantedAt: g.now - 600}
	st.Coins = 358_716_959
	st.RunEarnings = 400_000_000
	st.Scarecrows = []sim.Scarecrow{{Pouch: 0}, {Pouch: 46}, {Pouch: 12_480}, {Pouch: 123_456_789}}
	st.ScarecrowClock = 6 * 20
}

// goldenBoardOf builds a believable n-farm board with You at youRank: a steep
// coin curve (the real top ten of a live board), a few veterans with contract seals and earned name colours, and
// a tail of unnamed farms that show their suffix.
func goldenBoardOf(n, youRank int, now int64) leaderboard.Board {
	names := []string{"BCHECKETTS", "PARZIVAL", "ERNIE ACRES", "NIGEL", "EL LLANITO", "AMELIE FARMS", "JOKIFARM", "CHICKEN BANANA", "SNOIGEL", "ZAMPANO"}
	styles := []string{sim.NameStyleViolet, sim.NameStyleSky, sim.NameStylePurpleWave, sim.NameStyleLeaf}
	suffixes := []string{"XWZ0c", "hgYY8", "Hp060", "ta6WU", "Ugyeo", "aZSRw", "q3LmP", "Rk7vD"}
	head := []int64{1_081_398_488, 437_519_604, 63_914_429, 36_138_088, 8_556_775, 93_566, 44_856, 14_057, 4_648, 2_461}
	rows := make([]leaderboard.Row, n)
	for i := range rows {
		coins := max(300-4*int64(i-len(head)), 20) // a strictly falling tail
		if i < len(head) {
			coins = head[i]
		}
		r := leaderboard.Row{Rank: i + 1, Coins: coins}
		switch {
		case i < len(names):
			r.DisplayName, r.Rebirths = names[i], int64(13-i)
			if i < 4 {
				r.ContractsCompleted, r.NameStyle = 3, styles[i]
			}
		default:
			r.Suffix, r.ShowSuffix = suffixes[i%len(suffixes)], true
		}
		if i+1 == youRank {
			r.DisplayName, r.Suffix, r.ShowSuffix, r.IsYou = "ASDFLOL", "", false, true
		}
		rows[i] = r
	}
	b := leaderboard.Board{Rows: rows, Total: n, AsOf: now - 2}
	if youRank > 0 {
		b.You, b.YouIndex = &rows[youRank-1], youRank-1
	}
	return b
}
