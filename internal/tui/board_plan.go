package tui

// boardPinRadius is how many neighbours each side of your own row the pinned
// block shows when you are off-screen (gameplay/02's ±3).
const boardPinRadius = 3

// boardPlan says which rows of the full ranked list are on screen. It is a
// pure function of four integers so that viewBoard and the hitbox pass cannot
// disagree about the layout, and so the layout can be table-tested.
//
// The list is drawn from `start` for `end-start` rows. When you are ranked
// but below the viewport, a divider and a short block of your neighbours
// (rows pinLo..pinHi, inclusive) are pinned under the list. Once scrolling
// brings you into the viewport the pin is dropped and the list simply runs
// on, so "the divider is gone" reads as "you are in the list now".
//
// The frame is exactly `visible` lines tall whenever the list is longer than
// the viewport — the list shrinks by the block and its divider, never the
// frame — which keeps everything below it (scroll hint, "updated…") put.
type boardPlan struct {
	start, end   int // list rows [start, end)
	pinned       bool
	pinLo, pinHi int // pinned block rows, inclusive; meaningful only if pinned
}

// planBoard lays out `total` ranked rows in `visible` lines. youIdx is your
// index in the list, or -1 when you are unranked/guest. scroll is the desired
// first row and is clamped.
func planBoard(total, youIdx, scroll, visible int) boardPlan {
	visible = max(visible, 1)
	start := min(max(scroll, 0), max(total-visible, 0))
	p := boardPlan{start: start, end: min(start+visible, total)}

	// Pin only when you are strictly below the plain viewport. Above it, or
	// inside it, the plain list already tells the truth.
	if youIdx < 0 || youIdx >= total || youIdx < start+visible {
		return p
	}
	// Keep at least three list rows alongside the block and its divider
	// (visible >= 5 for just your own row), so short terminals shrink the
	// radius, then drop the pin, instead of losing the list.
	if visible < 5 {
		return p
	}
	radius := min(boardPinRadius, (visible-5)/2)
	p.pinLo = youIdx - radius
	p.pinHi = min(youIdx+radius, total-1)
	p.pinned = true
	p.end = start + visible - (p.pinHi - p.pinLo + 1) - 1
	return p
}

// lines is how many list rows plus pinned rows (divider included) are drawn.
func (p boardPlan) lines() int {
	n := p.end - p.start
	if p.pinned {
		n += p.pinHi - p.pinLo + 2
	}
	return n
}

// each calls fn with the index of every ranked row the plan draws.
func (p boardPlan) each(fn func(i int)) {
	for i := p.start; i < p.end; i++ {
		fn(i)
	}
	if p.pinned {
		for i := p.pinLo; i <= p.pinHi; i++ {
			fn(i)
		}
	}
}
