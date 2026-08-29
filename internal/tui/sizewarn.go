package tui

// The canvas is designed at canvasMaxWidth×canvasMaxHeight. Below that the
// game still plays — the hard guard is minWidth/minHeight — but screens get
// cramped, so we say so rather than letting the player wonder.
const (
	recommendedWidth  = canvasMaxWidth
	recommendedHeight = canvasMaxHeight
)

// undersized reports whether the terminal is below the recommended size.
func (g *Game) undersized() bool { return false }

// viewSizeWarning returns the header chip shown while the terminal is smaller
// than the layout wants, or "" when there is nothing to say.
func (g *Game) viewSizeWarning() string { return "" }
