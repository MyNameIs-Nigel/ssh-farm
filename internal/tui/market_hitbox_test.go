package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mynameis-nigel/ssh-farm/internal/content"
	"github.com/mynameis-nigel/ssh-farm/internal/game"
	"github.com/mynameis-nigel/ssh-farm/internal/identity"
	"github.com/mynameis-nigel/ssh-farm/internal/leaderboard"
	applog "github.com/mynameis-nigel/ssh-farm/internal/log"
	"github.com/mynameis-nigel/ssh-farm/internal/store"
)

func marketGame(t *testing.T, c *content.Content) *Game {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "market.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	mgr := game.NewManager(st, c, applog.New("error", "text"), time.Hour, game.PolicyTakeover)
	id := identity.SessionIdentity{Fingerprint: "SHA256:markethit", Slot: "farm"}
	res, err := mgr.Attach(context.Background(), id, "k", time.Now().Unix(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(res.Session.Detach)
	g := NewGame(id, res, c, leaderboard.New(st, 15*time.Second, 0, 0, time.Now), 100, 38, time.Now().Unix(), 0)
	return dismissIntro(t, g)
}

func marketScreen(t *testing.T, g *Game) *Game {
	t.Helper()
	g, _ = tick(t, g, time.Now().Unix())
	g = resize(t, g, 100, 38)
	g.scr = scrMarket
	view(g)
	return g
}

func bodyRowForText(t *testing.T, g *Game, needle string) int {
	t.Helper()
	lines := strings.Split(view(g), "\n")
	for y := g.layout.bodyY; y < len(lines); y++ {
		if strings.Contains(stripAnsi(lines[y]), needle) {
			return y
		}
	}
	t.Fatalf("%q not found in rendered view from body row %d:\n%s", needle, g.layout.bodyY, view(g))
	return -1
}

func marketHitboxRow(t *testing.T, g *Game, idx int) int {
	t.Helper()
	want := "market:" + itoa(idx)
	for _, b := range g.hits.Boxes() {
		if b.ID == want {
			return b.Y
		}
	}
	t.Fatalf("expected hitbox %q to be registered", want)
	return -1
}

func marketLineRow(t *testing.T, g *Game, match func(marketLine) bool) int {
	t.Helper()
	for i, l := range g.marketLines() {
		if match(l) {
			return g.layout.bodyY + i
		}
	}
	t.Fatal("no matching market line")
	return -1
}

// TestMarketRowHitboxesMatchRenderedText ensures every selectable market row
// has a one-row hitbox exactly where its text renders — not shifted up/down
// by section headers or blank separators.
func TestMarketRowHitboxesMatchRenderedText(t *testing.T) {
	f := newFixture(t)
	g := marketScreen(t, dismissIntro(t, f.newGame(t, time.Now().Unix())))

	for _, want := range []struct {
		name string
		idx  int
	}{
		{"Fertilizer", 0},
		{"Hardier Gamble", 1},
		{"Greenhouse", 2},
	} {
		row := bodyRowForText(t, g, want.name)
		if got := marketHitboxRow(t, g, want.idx); got != row {
			t.Fatalf("%q renders on row %d but hitbox registers at row %d", want.name, row, got)
		}
		box, ok := g.hits.At(g.layout.cx+2, row)
		if !ok || box.ID != "market:"+itoa(want.idx) {
			t.Fatalf("click at %q row %d: got %+v ok=%v", want.name, row, box, ok)
		}
	}
}

// TestMarketBlankLineBetweenSectionsHasNoHitbox guards against the old bug
// where two-row hitboxes spilled onto blank separator lines and section headers.
func TestMarketBlankLineBetweenSectionsHasNoHitbox(t *testing.T) {
	f := newFixture(t)
	g := marketScreen(t, dismissIntro(t, f.newGame(t, time.Now().Unix())))

	blankRow := marketLineRow(t, g, func(l marketLine) bool {
		return l.idx == noMarketItem && l.text == ""
	})
	if box, ok := g.hits.At(g.layout.cx, blankRow); ok {
		t.Fatalf("blank separator row %d should not be clickable, got %+v", blankRow, box)
	}
}

// TestMarketClickMerchantScaleNotFertilizer reproduces the reported bug: with
// two multipliers stacked one row apart, clicking Merchant's Scale must select
// merchant_scale — not fertilizer — and the blank line below must not select it.
func TestMarketClickMerchantScaleNotFertilizer(t *testing.T) {
	c, err := content.Load("")
	if err != nil {
		t.Fatal(err)
	}
	g := marketScreen(t, marketGame(t, c))

	fertRow := bodyRowForText(t, g, "Fertilizer")
	merchantRow := bodyRowForText(t, g, "Merchant's Scale")
	if merchantRow <= fertRow {
		t.Fatalf("expected Merchant's Scale below Fertilizer, got rows %d and %d", fertRow, merchantRow)
	}
	if merchantRow != fertRow+1 {
		t.Fatalf("expected consecutive multiplier rows, got Fertilizer@%d Merchant@%d", fertRow, merchantRow)
	}

	m, _ := g.handleMouseClick(tea.Mouse(tea.MouseClickMsg{X: g.layout.cx + 2, Y: merchantRow, Button: tea.MouseLeft}))
	g = m.(*Game)
	items := g.marketItems()
	if items[g.marketIdx].id != "merchant_scale" {
		t.Fatalf("clicking Merchant's Scale selected %q (idx %d)", items[g.marketIdx].id, g.marketIdx)
	}

	blankRow := merchantRow + 1
	if box, ok := g.hits.At(g.layout.cx, blankRow); ok && strings.HasPrefix(box.ID, "market:") {
		t.Fatalf("blank row below Merchant's Scale should not carry a market hitbox, got %+v", box)
	}

	m, _ = g.handleMouseClick(tea.Mouse(tea.MouseClickMsg{X: g.layout.cx + 2, Y: blankRow, Button: tea.MouseLeft}))
	g = m.(*Game)
	if items[g.marketIdx].id != "merchant_scale" {
		t.Fatalf("clicking blank row should keep Merchant's Scale selected, got %q", items[g.marketIdx].id)
	}

	m, _ = g.handleMouseClick(tea.Mouse(tea.MouseClickMsg{X: g.layout.cx + 2, Y: fertRow, Button: tea.MouseLeft}))
	g = m.(*Game)
	if items[g.marketIdx].id != "fertilizer" {
		t.Fatalf("clicking Fertilizer selected %q", items[g.marketIdx].id)
	}
}

// TestMarketLinesMatchViewOutput ensures marketLines stays the single source of
// truth for both rendering and hitbox registration.
func TestMarketLinesMatchViewOutput(t *testing.T) {
	f := newFixture(t)
	g := marketScreen(t, dismissIntro(t, f.newGame(t, time.Now().Unix())))

	parts := make([]string, len(g.marketLines()))
	for i, l := range g.marketLines() {
		parts[i] = l.text
	}
	want := strings.TrimRight(strings.Join(parts, "\n"), "\n")
	if got := g.viewMarket(); got != want {
		t.Fatalf("viewMarket diverged from marketLines:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

// TestMarketHitboxesOnlyOnSelectableRows verifies headers and blank separators
// never accidentally inherit idx=0 from Go's zero value.
func TestMarketHitboxesOnlyOnSelectableRows(t *testing.T) {
	f := newFixture(t)
	g := marketScreen(t, dismissIntro(t, f.newGame(t, time.Now().Unix())))

	selectable := map[int]bool{}
	for _, l := range g.marketLines() {
		if l.idx >= 0 {
			selectable[l.idx] = true
		}
	}
	for _, b := range g.hits.Boxes() {
		if !strings.HasPrefix(b.ID, "market:") {
			continue
		}
		row := b.Y - g.layout.bodyY
		if row < 0 || row >= len(g.marketLines()) {
			t.Fatalf("market hitbox %s at out-of-range row %d", b.ID, b.Y)
		}
		if g.marketLines()[row].idx < 0 {
			t.Fatalf("market hitbox %s on non-selectable line %d: %+v", b.ID, row, g.marketLines()[row])
		}
	}
	if len(selectable) == 0 {
		t.Fatal("expected at least one selectable market row")
	}
}
