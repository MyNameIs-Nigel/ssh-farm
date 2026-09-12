package server

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
)

func TestSupportsTrueColor(t *testing.T) {
	cases := []struct {
		name string
		env  []string
		term string
		want bool
	}{
		{"true color", []string{"COLORTERM=truecolor"}, "xterm-256color", true},
		{"24 bit alias", []string{"COLORTERM=24bit"}, "xterm-256color", true},
		{"ansi 256", nil, "xterm-256color", false},
		{"ansi", nil, "xterm", false},
		{"unknown", nil, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := supportsTrueColor(tc.env, tc.term); got != tc.want {
				t.Errorf("supportsTrueColor(%q, %q) = %v, want %v", tc.env, tc.term, got, tc.want)
			}
		})
	}
}

// TestCursorDownWriterRewritesNewlines checks the byte-level transform: every
// \n becomes ESC[B and all other bytes pass through unchanged, including when
// the input is split across multiple Write calls (the writer must be
// stateless, since a real renderer flushes in arbitrary chunks).
func TestCursorDownWriterRewritesNewlines(t *testing.T) {
	cases := []struct {
		name   string
		chunks []string
		want   string
	}{
		{"no newline", []string{"\x1b[5;1Hhello"}, "\x1b[5;1Hhello"},
		{"single", []string{"a\nb"}, "a\x1b[Bb"},
		{"crlf preserved as cr+down", []string{"a\r\nb"}, "a\r\x1b[Bb"},
		{"run of newlines", []string{"x\n\n\ny"}, "x\x1b[B\x1b[B\x1b[By"},
		{"split across writes", []string{"a", "\n", "b\n", "c"}, "a\x1b[Bb\x1b[Bc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sink bytes.Buffer
			w := &cursorDownWriter{w: &sink}
			total := 0
			for _, c := range tc.chunks {
				n, err := w.Write([]byte(c))
				if err != nil {
					t.Fatalf("Write(%q) error: %v", c, err)
				}
				if n != len(c) {
					t.Errorf("Write(%q) returned n=%d, want %d", c, n, len(c))
				}
				total += n
			}
			if got := sink.String(); got != tc.want {
				t.Errorf("output = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCursorDownWriterPreventsLeftBleed(t *testing.T) {
	const startCol = 50

	raw := "\x1b[2J" +
		"\x1b[6;51H" + "Plots owned: 1" +
		"\n" + "Your harvesting" +
		"\n" + "Each plot costs"

	rawCols := replayLeftmostCols(t, raw)

	var wrapped bytes.Buffer
	w := &cursorDownWriter{w: &wrapped}
	if _, err := w.Write([]byte(raw)); err != nil {
		t.Fatalf("writer error: %v", err)
	}
	fixedCols := replayLeftmostCols(t, wrapped.String())

	bled := false
	for _, c := range rawCols {
		if c == 0 {
			bled = true
		}
	}
	if !bled {
		t.Fatal("raw stream did not reproduce the col-0 bleed; test is not exercising the bug")
	}

	for row, c := range fixedCols {
		if c < startCol {
			t.Errorf("row %d rendered at col %d, inside the left margin (< %d) — bleed not fixed", row, c, startCol)
		}
	}
}

var (
	cupRe = regexp.MustCompile(`^\x1b\[(\d+);(\d+)H`)
	csiRe = regexp.MustCompile(`^\x1b\[([0-9;]*)([A-Za-z])`)
)

func replayLeftmostCols(t *testing.T, s string) map[int]int {
	t.Helper()
	const w, h = 200, 50
	first := map[int]int{}
	x, y := 0, 0
	clampX := func(v int) int {
		if v < 0 {
			return 0
		}
		if v >= w {
			return w - 1
		}
		return v
	}
	clampY := func(v int) int {
		if v < 0 {
			return 0
		}
		if v >= h {
			return h - 1
		}
		return v
	}
	for i := 0; i < len(s); {
		c := s[i]
		if c == '\x1b' {
			if m := cupRe.FindStringSubmatch(s[i:]); m != nil {
				y = clampY(atoi(m[1]) - 1)
				x = clampX(atoi(m[2]) - 1)
				i += len(m[0])
				continue
			}
			if m := csiRe.FindStringSubmatch(s[i:]); m != nil {
				n := atoi(m[1])
				if n == 0 {
					n = 1
				}
				switch m[2] {
				case "A":
					y = clampY(y - n)
				case "B":
					y = clampY(y + n)
				case "C":
					x = clampX(x + n)
				case "D":
					x = clampX(x - n)
				case "G":
					x = clampX(atoi(m[1]) - 1)
				case "d":
					y = clampY(atoi(m[1]) - 1)
				}
				i += len(m[0])
				continue
			}
			i++
			continue
		}
		switch c {
		case '\r':
			x = 0
		case '\n':
			y = clampY(y + 1)
			x = 0
		default:
			if c >= 0x20 {
				if _, seen := first[y]; !seen {
					first[y] = x
				}
				x = clampX(x + 1)
			}
		}
		i++
	}
	return first
}

func atoi(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}
