package sim

import (
	"testing"

	"github.com/mynameis-nigel/ssh-farm/internal/content"
)

// Simulate reconnects from the same save at increasingly long absences.
func BenchmarkFarmCatchUp(b *testing.B) {
	c, err := content.Load("testdata")
	if err != nil {
		b.Fatal(err)
	}
	const start = 1000
	seed := New(c, 42, start)
	seed.Scarecrows = make([]Scarecrow, 4)
	if err := Plant(seed, c, 0, "turnip", start); err != nil {
		b.Fatal(err)
	}
	for _, span := range []struct {
		name    string
		seconds int64
	}{{"hour", 3600}, {"week", 7 * 86400}, {"year", 365 * 86400}} {
		b.Run(span.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				state := seed.Clone()
				Advance(state, c, start+span.seconds)
				if state.UpdatedAt != start+span.seconds {
					b.Fatal("catch-up did not reach reconnect time")
				}
			}
		})
	}
}
