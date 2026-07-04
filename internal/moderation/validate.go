package moderation

import "strings"

// MinNameLen and MaxNameLen bound a farm name after normalization
// (gameplay/03's charset rules). sim.SetFarmName's own 24-rune cap is a
// looser backstop at the state layer; Filter is the real gate and never
// lets a name through that sim would reject.
const (
	MinNameLen = 3
	MaxNameLen = 20
)

// Validate checks a raw name against gameplay/03's charset and shape rules
// and returns the normalized (collapsed, uppercased) form to store. It does
// not consult the denylist — that is Check's job, run separately so the two
// concerns (shape vs. content) stay independently testable.
//
// Rules: ASCII only (letters, digits, space, hyphen, underscore); collapse
// runs of spaces; no leading/trailing separators; 3-20 characters after
// normalization; not purely digits/separators. Rejecting all non-ASCII
// input isn't unfriendly, it's the single most effective anti-abuse
// measure — it kills homoglyph evasion (Cyrillic lookalikes, zalgo, RTL
// tricks) and matches the terminal aesthetic anyway.
func Validate(raw string) (norm string, ok bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false
	}

	var b strings.Builder
	b.Grow(len(trimmed))
	lastWasSpace := false
	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - ('a' - 'A'))
			lastWasSpace = false
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
			lastWasSpace = false
		case r == ' ':
			if lastWasSpace {
				continue // collapse runs of spaces
			}
			b.WriteRune(' ')
			lastWasSpace = true
		default:
			return "", false // any other rune (including all non-ASCII) is invalid
		}
	}

	norm = b.String()
	if norm == "" {
		return "", false
	}
	first, last := norm[0], norm[len(norm)-1]
	if isSeparator(first) || isSeparator(last) {
		return "", false
	}
	if len(norm) < MinNameLen || len(norm) > MaxNameLen {
		return "", false
	}
	if isAllDigitsAndSeparators(norm) {
		return "", false
	}
	return norm, true
}

func isSeparator(b byte) bool { return b == ' ' || b == '-' || b == '_' }

func isAllDigitsAndSeparators(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9') && !isSeparator(c) {
			return false
		}
	}
	return true
}
