package tui

import "strconv"

// The game is played anywhere from 80×24 (the macOS Terminal.app default) up
// through 120×30 (the Windows Terminal default) and beyond. All of those are
// playable; only the roomier end is comfortable, so below the recommended
// size we say so.
//
// recommendedHeight is deliberately *not* canvasMaxHeight. The canvas is
// drawn at up to 100×38, but 38 rows is taller than either stock terminal
// ships with, so warning at 38 would fire for almost everyone and mean
// nothing. 30 is the height at which no screen has to scroll; it puts the
// Windows default comfortably inside the range while still catching the
// macOS default.
const (
	recommendedWidth  = canvasMaxWidth
	recommendedHeight = 30
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

// helpNavLabel is the Help tab's text. While the window is undersized the
// leading "?" becomes a warning glyph in the Warn style, which is the whole
// of the in-frame signal: the explanation lives on the Help screen the tab
// opens.
//
// "⚠" is U+26A0 with no variation selector, which measures one display
// column exactly like "?" — so the nav strip's width, and every nav hitbox
// after this tab, are identical either way. Adding the VS16 emoji form would
// make it two columns wide and shift them.
func (g *Game) helpNavLabel() string {
	if g.undersized() {
		return "⚠ Help"
	}
	return "? Help"
}

// viewHelpSizeNotice is the explanation the nav indicator points at: what
// size the window is, what the game wants, and what to do. It returns "" at
// or above the recommended size so Help carries no permanent scold.
func (g *Game) viewHelpSizeNotice() string {
	if !g.undersized() {
		return ""
	}
	// Deliberately terse. Help shows only helpVisibleLines() rows at a time,
	// which is 8 at 80×24 — so a chatty notice would push "How it works" and
	// the whole key list off the screen at exactly the size that raises it.
	th := g.theme()
	return th.Warn.Render("⚠ Small window — "+size(g.width, g.height)+", best at "+
		size(recommendedWidth, recommendedHeight)+" or larger") + "\n" +
		g.helpBody("It still plays: screens are just cramped and long lines get "+
			"trimmed. Widen the window or drop the font size a step.") + "\n"
}

func size(w, h int) string {
	return strconv.Itoa(w) + "×" + strconv.Itoa(h)
}
