package hitbox

import "testing"

func TestAtTopmostWins(t *testing.T) {
	var reg Registry
	reg.Add(Box{X: 0, Y: 0, W: 10, H: 10, ID: "bottom"})
	reg.Add(Box{X: 2, Y: 2, W: 4, H: 4, ID: "top"})

	box, ok := reg.At(3, 3)
	if !ok || box.ID != "top" {
		t.Fatalf("expected top box, got %+v ok=%v", box, ok)
	}
}

func TestAtMiss(t *testing.T) {
	var reg Registry
	reg.Add(Box{X: 5, Y: 5, W: 2, H: 2, ID: "a"})
	if _, ok := reg.At(0, 0); ok {
		t.Fatal("expected miss")
	}
}
