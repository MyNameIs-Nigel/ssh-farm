package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mynameis-nigel/ssh-farm/internal/content"
	"github.com/mynameis-nigel/ssh-farm/internal/sim"
)

func TestGateTextNamesEveryCondition(t *testing.T) {
	c, err := content.Load("")
	if err != nil {
		t.Fatal(err)
	}
	g := &Game{content: c}
	for _, tc := range []struct {
		u    content.Unlock
		want string
	}{
		{content.Unlock{Kind: "prestige", Value: 2}, "rebirth 2×"},
		{content.Unlock{Kind: "zone", Zone: "greenhouse"}, "own the Greenhouse"},
		{content.Unlock{Kind: "zone", Zone: "greenhouse", Rebirths: 2}, "rebirth 2× + own the Greenhouse"},
		{content.Unlock{Kind: "start", Rebirths: 1}, "rebirth 1×"},
		{content.Unlock{Kind: "zone", Zone: "nowhere"}, "own a zone"},
		{c.Crop("goldengrain").Unlock, "rebirth 2× + own the Greenhouse"},
	} {
		if got := g.gateText(tc.u); got != tc.want {
			t.Errorf("gateText(%+v) = %q, want %q", tc.u, got, tc.want)
		}
	}
}

// From rebirth 1 the Rebirth screen previews Goldengrain as a tier-2 unlock,
// naming the Greenhouse it still needs on top of the rebirth.
func TestRebirthScreenPreviewsGoldengrain(t *testing.T) {
	f := newFixtureFrom(t, "")
	base := time.Now().Unix()

	res := f.attach(t, base)
	res.Session.Detach()
	st := sim.New(f.content, 7, base)
	st.Rebirths = 1
	payload, err := st.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.PersistSave(context.Background(), f.id.Fingerprint, f.id.Slot, payload, st.Version, base, st.Coins, st.LifetimeEarnings, st.Rebirths, st.FarmName); err != nil {
		t.Fatal(err)
	}

	g := f.newGame(t, base+1)
	g, _ = tick(t, g, base+1)
	g = press(t, g, "x", "4")
	if out := view(g); !strings.Contains(out, "Goldengrain (fast) — then own the Greenhouse") {
		t.Fatalf("rebirth screen should preview goldengrain with its greenhouse gate, got:\n%s", out)
	}
}
