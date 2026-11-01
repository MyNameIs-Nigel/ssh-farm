package tui

import (
	"testing"

	"github.com/mynameis-nigel/ssh-farm/internal/content"
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
		{c.Crop("goldengrain").Unlock, "rebirth 2× + own the Greenhouse"},
	} {
		if got := g.gateText(tc.u); got != tc.want {
			t.Errorf("gateText(%+v) = %q, want %q", tc.u, got, tc.want)
		}
	}
}
