package sim

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mynameis-nigel/ssh-farm/internal/content"
)

// realContent loads the production content (data/*.toml), the same content
// the v1 golden generator ran against — as opposed to testContent, which
// loads the minimal fixture under testdata/.
func realContent(t *testing.T) *content.Content {
	t.Helper()
	c, err := content.Load("")
	if err != nil {
		t.Fatalf("load embedded content: %v", err)
	}
	return c
}

func loadGolden(t *testing.T, name string) (*State, []byte) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "v1", name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	s, err := DecodeState(b)
	if err != nil {
		t.Fatalf("decode golden %s: %v", name, err)
	}
	return s, b
}

// TestGoldensRoundTripByteIdentical proves every v1-produced golden decodes
// in v2 and re-encodes to the exact same bytes it shipped with — the basis
// of the v1 save import (framework/02).
func TestGoldensRoundTripByteIdentical(t *testing.T) {
	goldens := []string{
		"fresh.json",
		"named_farm.json",
		"max_land.json",
		"mid_rebirth.json",
		"offline_pending.json",
		"scripted_start.json",
		"scripted_end.json",
	}
	for _, name := range goldens {
		t.Run(name, func(t *testing.T) {
			s, want := loadGolden(t, name)
			got, err := s.Encode()
			if err != nil {
				t.Fatalf("re-encode: %v", err)
			}
			if string(got) != string(want) {
				t.Fatalf("re-encoded bytes differ:\ngot:  %s\nwant: %s", got, want)
			}
		})
	}
}

// TestScriptedSequenceMatchesV1Golden replays, in v2, the exact scripted
// action/advance sequence the v1 generator ran (see
// testdata/v1/README.md), starting from the v1-produced "before" golden,
// and asserts the result is identical to the v1-produced "after" golden.
// This is the parity harness's core claim: same inputs, same code path,
// same seeded RNG stream, bit-identical state.
func TestScriptedSequenceMatchesV1Golden(t *testing.T) {
	c := realContent(t)
	s, _ := loadGolden(t, "scripted_start.json")
	want, _ := loadGolden(t, "scripted_end.json")

	startedAt := s.UpdatedAt

	Advance(s, c, startedAt+300)
	if _, err := Harvest(s, c, 0, startedAt+300); err != nil {
		t.Fatalf("harvest plot 0: %v", err)
	}
	if _, err := Harvest(s, c, 1, startedAt+300); err != nil {
		t.Fatalf("harvest plot 1: %v", err)
	}
	if _, err := BuyPlot(s, c); err != nil {
		t.Fatalf("buy plot: %v", err)
	}
	if err := SetFarmName(s, "Northfield"); err != nil {
		t.Fatalf("set farm name: %v", err)
	}
	Advance(s, c, startedAt+50_000)
	if s.GiftPending {
		if _, err := RedeemGift(s, c); err != nil {
			t.Fatalf("redeem gift: %v", err)
		}
	}

	if !reflect.DeepEqual(s, want) {
		t.Fatalf("scripted replay diverged from v1 golden:\ngot:  %+v\nwant: %+v", s, want)
	}
}

// TestLongRunAdvanceInvariants runs 10,000 one-second advances and asserts
// the sim stays well-formed — catches accumulation bugs short goldens miss.
func TestLongRunAdvanceInvariants(t *testing.T) {
	c := realContent(t)
	s, _ := loadGolden(t, "scripted_start.json")
	for i := 0; i < 10000; i++ {
		Advance(s, c, s.UpdatedAt+1)
		if s.Coins < 0 {
			t.Fatalf("negative coins at step %d", i+1)
		}
		if len(s.Plots) < 1 {
			t.Fatalf("plots vanished at step %d", i+1)
		}
	}
	if s.UpdatedAt <= 0 {
		t.Fatal("updated at not advancing")
	}
}
