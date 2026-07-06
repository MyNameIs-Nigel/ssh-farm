package tui

import (
	"fmt"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
)

// sanitizeText strips control characters and escape sequences from any
// user- or content-influenced string before it reaches the terminal. Slots
// are already sanitized at the door (Task 1) and content files are operator
// config, but output escaping is defense in depth: nothing dynamic may carry
// a terminal escape.
func sanitizeText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\x1b' || unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// money renders coins with thousands separators: 1234567 -> "1,234,567".
func money(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	if len(s) > 3 {
		var b strings.Builder
		lead := len(s) % 3
		if lead > 0 {
			b.WriteString(s[:lead])
		}
		for i := lead; i < len(s); i += 3 {
			if b.Len() > 0 {
				b.WriteByte(',')
			}
			b.WriteString(s[i : i+3])
		}
		s = b.String()
	}
	if neg {
		return "-" + s
	}
	return s
}

// duration humanizes a second count: 94 -> "1m 34s", 90061 -> "1d 1h".
func duration(secs int64) string {
	if secs < 0 {
		secs = 0
	}
	switch {
	case secs < 60:
		return fmt.Sprintf("%ds", secs)
	case secs < 3600:
		return fmt.Sprintf("%dm %02ds", secs/60, secs%60)
	case secs < 86400:
		return fmt.Sprintf("%dh %02dm", secs/3600, (secs%3600)/60)
	default:
		return fmt.Sprintf("%dd %dh", secs/86400, (secs%86400)/3600)
	}
}

// progressBar renders a width-character bar at pct (0-100).
func progressBar(pct, width int) string {
	if width < 1 {
		width = 1
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := pct * width / 100
	return strings.Repeat("▰", filled) + strings.Repeat("▱", width-filled)
}

// plural returns one or other depending on whether n is exactly 1.
func plural(n int, one, other string) string {
	if n == 1 {
		return one
	}
	return other
}

// shortFingerprint trims "SHA256:..." for display.
func shortFingerprint(fp string) string {
	const max = 19 // "SHA256:" + 12 chars
	if len(fp) <= max {
		return fp
	}
	return fp[:max] + "…"
}

// truncate cuts s to max runes, appending "…" when cut.
func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	return string(runes[:max-1]) + "…"
}

// wrapIndent word-wraps plain text so each line fits within width runes,
// prefixing every line with indent. Used for help text before styling so
// lipgloss does not re-wrap ANSI-colored lines (which pads badly).
func wrapIndent(width int, indent, text string) string {
	if width < 1 {
		width = 1
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return indent
	}
	indentWidth := len([]rune(indent))
	words := strings.Fields(text)
	var lines []string
	var cur strings.Builder
	cur.WriteString(indent)
	used := indentWidth
	for _, word := range words {
		wordWidth := len([]rune(word))
		extra := wordWidth
		if cur.Len() > len(indent) {
			extra++ // space before word
		}
		if used+extra > width && cur.Len() > len(indent) {
			lines = append(lines, cur.String())
			cur.Reset()
			cur.WriteString(indent)
			used = indentWidth
			extra = wordWidth
		}
		if cur.Len() > len(indent) {
			cur.WriteByte(' ')
			used++
		}
		cur.WriteString(word)
		used += wordWidth
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	return strings.Join(lines, "\n")
}

// alignSides lays out left and right within width terminal columns, pushing
// right to the edge and truncating left with "…" when needed.
func alignSides(left, right string, width int) string {
	if width < 1 {
		width = 1
	}
	rightW := lipgloss.Width(right)
	if rightW > width {
		return truncate(right, width)
	}
	gap := 1
	maxLeft := width - rightW - gap
	if maxLeft < 0 {
		maxLeft = 0
	}
	if lipgloss.Width(left) > maxLeft {
		left = truncate(left, maxLeft)
	}
	pad := width - lipgloss.Width(left) - rightW
	if pad < gap {
		pad = gap
	}
	return left + strings.Repeat(" ", pad) + right
}

// centerWrap word-wraps plain text to width, centering each line. Wrap before
// styling so lipgloss does not re-wrap ANSI-colored lines.
func centerWrap(width int, text string) string {
	if width < 1 {
		width = 1
	}
	wrapped := wrapIndent(width, "", text)
	lines := strings.Split(wrapped, "\n")
	for i, line := range lines {
		lines[i] = lipgloss.PlaceHorizontal(width, lipgloss.Center, line)
	}
	return strings.Join(lines, "\n")
}
