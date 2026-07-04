// Package hitbox provides a per-frame clickable region registry for the farm TUI.
package hitbox

// Box is a clickable rectangle registered during View. Coordinates are
// absolute terminal cells (0,0 = top-left), matching mouse messages.
type Box struct {
	X, Y, W, H int
	ID         string
	Data       any
}

func (b Box) contains(x, y int) bool {
	return x >= b.X && x < b.X+b.W && y >= b.Y && y < b.Y+b.H
}

// Registry is rebuilt on every View pass, so stale boxes are impossible.
type Registry struct {
	boxes []Box
}

// Reset clears all boxes (start of a View pass).
func (h *Registry) Reset() {
	h.boxes = h.boxes[:0]
}

// Add registers a box. Later additions win on overlap (topmost-last).
func (h *Registry) Add(b Box) {
	h.boxes = append(h.boxes, b)
}

// At returns the topmost (last-registered) box containing the cell.
func (h *Registry) At(x, y int) (Box, bool) {
	for i := len(h.boxes) - 1; i >= 0; i-- {
		if h.boxes[i].contains(x, y) {
			return h.boxes[i], true
		}
	}
	return Box{}, false
}

// Boxes exposes registered boxes (for tests).
func (h *Registry) Boxes() []Box {
	return append([]Box(nil), h.boxes...)
}
