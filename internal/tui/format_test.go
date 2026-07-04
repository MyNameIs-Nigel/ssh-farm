package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestWrapIndent(t *testing.T) {
	got := wrapIndent(20, "  ", "one two three four five six")
	for _, line := range strings.Split(got, "\n") {
		if len([]rune(line)) > 20 {
			t.Fatalf("line exceeds width 20: %q", line)
		}
	}
	if !strings.HasPrefix(got, "  ") {
		t.Fatalf("expected indent prefix, got %q", got)
	}
}

func TestAlignSides(t *testing.T) {
	row := alignSides("left", "right", 12)
	if row != "left   right" {
		t.Fatalf("got %q", row)
	}
	long := alignSides("Stargazer's Almanac — a very long description indeed", "✦ 5", 40)
	if !strings.HasSuffix(long, "✦ 5") {
		t.Fatalf("price not at end: %q", long)
	}
	if lipgloss.Width(long) != 40 {
		t.Fatalf("width = %d, want 40: %q", lipgloss.Width(long), long)
	}
}

func TestCenterWrap(t *testing.T) {
	text := "The cosmos keeps its deeper rewards for those who begin anew."
	got := centerWrap(40, text)
	for _, line := range strings.Split(got, "\n") {
		if w := lipgloss.Width(line); w != 40 {
			t.Fatalf("line width %d != 40: %q", w, line)
		}
	}
	if strings.Count(got, "\n") < 1 {
		t.Fatalf("expected wrapped lines, got %q", got)
	}
}
