package tui

import "strconv"

// The canvas is designed at canvasMaxWidth×canvasMaxHeight. Below that the
// game still plays — the hard guard is minWidth/minHeight — but screens get
// cramped, so we say so rather than letting the player wonder.
const (
	recommendedWidth  = canvasMaxWidth
	recommendedHeight = canvasMaxHeight
)

// undersized reports whether the terminal is below the recommended size.
// It is false on the blocking path (below minWidth/minHeight), where the
// dedicated "needs a bigger window" screen already says everything.
func (g *Game) undersized() bool {
	if g.width < minWidth || g.height < minHeight {
		return false
	}
	return g.width < recommendedWidth || g.height < recommendedHeight
}

// viewSizeWarning returns the header chip shown while the terminal is smaller
// than the layout wants, or "" when there is nothing to say.
//
// It is emitted from viewHeader() rather than added as a row in
// composeCanvas: coords.go:computeLayout duplicates composeCanvas's height
// arithmetic to keep mouse hitboxes aligned, and it already calls viewHeader,
// so anything routed through the header stays mirrored for free.
func (g *Game) viewSizeWarning() string {
	if !g.undersized() {
		return ""
	}
	return g.theme().Warn.Render("⚠ " + size(g.width, g.height) +
		" · best at " + size(recommendedWidth, recommendedHeight))
}

func size(w, h int) string {
	return strconv.Itoa(w) + "×" + strconv.Itoa(h)
}

// noteWindowSize raises a one-shot toast when the terminal crosses from
// adequate to undersized. The chip in the header is easy to miss on first
// connect; re-firing on every resize inside the undersized range would be
// nagging, so the flag only clears once the window is big enough again.
func (g *Game) noteWindowSize() {
	if !g.undersized() {
		g.sizeWarned = false
		return
	}
	if g.sizeWarned {
		return
	}
	g.sizeWarned = true
	g.addNotice("⚠ This window is smaller than the farm is drawn for — " +
		size(g.width, g.height) + ", best at " + size(recommendedWidth, recommendedHeight) + ".")
}
