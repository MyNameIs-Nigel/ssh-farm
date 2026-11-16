package tui

import "testing"

// TestPlanBoardInvariants sweeps every (total, you, scroll, visible) in a
// small box and checks the properties the screen depends on, rather than
// spot-checking a few layouts.
func TestPlanBoardInvariants(t *testing.T) {
	for total := 0; total <= 30; total++ {
		for visible := 1; visible <= 24; visible++ {
			for you := -1; you < total; you++ {
				for scroll := -2; scroll <= total+2; scroll++ {
					p := planBoard(total, you, scroll, visible)
					ctx := func() string {
						return "total=" + itoa(total) + " you=" + itoa(you) + " scroll=" + itoa(scroll) + " visible=" + itoa(visible)
					}

					seen := map[int]bool{}
					p.each(func(i int) {
						if i < 0 || i >= total {
							t.Fatalf("%s: row %d out of range", ctx(), i)
						}
						if seen[i] {
							t.Fatalf("%s: row %d drawn twice", ctx(), i)
						}
						seen[i] = true
					})
					if len(seen) != p.end-p.start+boolInt(p.pinned)*(p.pinHi-p.pinLo+1) {
						t.Fatalf("%s: each() disagrees with the plan: %+v", ctx(), p)
					}

					// The frame never exceeds the viewport, and is exactly it
					// whenever there is more list than viewport.
					if p.lines() > visible {
						t.Fatalf("%s: %d lines exceed visible %d (%+v)", ctx(), p.lines(), visible, p)
					}
					if total >= visible && p.lines() != visible {
						t.Fatalf("%s: frame is %d lines, want exactly %d (%+v)", ctx(), p.lines(), visible, p)
					}

					if p.pinned {
						if you < 0 {
							t.Fatalf("%s: an unranked viewer must never pin", ctx())
						}
						if !seen[you] {
							t.Fatalf("%s: pinned block lacks your row (%+v)", ctx(), p)
						}
						if you < p.start+visible {
							t.Fatalf("%s: pinned although you are inside the plain viewport", ctx())
						}
						// The divider must mean a real gap: at least two hidden rows.
						if p.pinLo-p.end < 2 {
							t.Fatalf("%s: divider hides only %d rows (%+v)", ctx(), p.pinLo-p.end, p)
						}
						if p.end-p.start < 3 {
							t.Fatalf("%s: pinned block squeezed the list below 3 rows (%+v)", ctx(), p)
						}
					} else if you >= p.start && you < p.start+visible && !seen[you] {
						t.Fatalf("%s: you are inside the viewport but not drawn", ctx())
					}
				}
			}
		}
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// TestPlanBoardEveryRowIsReachableAndMergeSkipsNothing: scrolling down one
// row at a time must show every row at some point, and across the step where
// the pin drops away no row may be skipped (the list only ever grows by the
// block it gives back).
func TestPlanBoardEveryRowIsReachableAndMergeSkipsNothing(t *testing.T) {
	const total, visible, you = 60, 14, 40
	reached := map[int]bool{}
	prevEnd := 0
	for s := 0; s <= total-visible; s++ {
		p := planBoard(total, you, s, visible)
		p.each(func(i int) { reached[i] = true })
		if p.start != s {
			t.Fatalf("scroll %d drew from %d", s, p.start)
		}
		if p.end < prevEnd {
			t.Fatalf("scroll %d: list end went backwards (%d -> %d)", s, prevEnd, p.end)
		}
		prevEnd = p.end
	}
	for i := 0; i < total; i++ {
		if !reached[i] {
			t.Fatalf("row %d was never on screen", i)
		}
	}

	// Specific shape: pinned near the top, merged once you fit.
	if p := planBoard(total, you, 0, visible); !p.pinned || p.pinLo != you-3 || p.pinHi != you+3 {
		t.Fatalf("scroll 0 should pin you±3, got %+v", p)
	}
	if p := planBoard(total, you, you-visible+1, visible); p.pinned {
		t.Fatalf("you at the bottom edge of the viewport must merge, got %+v", p)
	}
}

// TestPlanBoardPinShrinksOnShortTerminals: tiny viewports lose neighbours,
// then the pin, before they lose the list.
func TestPlanBoardPinShrinksOnShortTerminals(t *testing.T) {
	cases := []struct {
		visible, wantRadius int // -1: no pin
	}{{3, -1}, {4, -1}, {5, 0}, {6, 0}, {7, 1}, {9, 2}, {11, 3}, {40, 3}}
	for _, c := range cases {
		p := planBoard(100, 50, 0, c.visible)
		if c.wantRadius < 0 {
			if p.pinned {
				t.Fatalf("visible=%d should not pin, got %+v", c.visible, p)
			}
			continue
		}
		if !p.pinned || p.pinHi-p.pinLo != 2*c.wantRadius {
			t.Fatalf("visible=%d: want radius %d, got %+v", c.visible, c.wantRadius, p)
		}
	}
}

// TestPlanBoardPinClampsAtTheEndOfTheList: rank last has no neighbours below.
func TestPlanBoardPinClampsAtTheEndOfTheList(t *testing.T) {
	p := planBoard(100, 99, 0, 20)
	if !p.pinned || p.pinHi != 99 || p.pinLo != 96 {
		t.Fatalf("last place should pin 96..99, got %+v", p)
	}
	if p.lines() != 20 {
		t.Fatalf("frame = %d lines, want 20", p.lines())
	}
}
