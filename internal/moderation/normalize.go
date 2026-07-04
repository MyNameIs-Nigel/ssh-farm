package moderation

import "strings"

// leetFold maps common leetspeak substitutions to the letters they mimic.
// Filters that check raw input are decorative, so every denylist check runs
// against folded text first. The symbol entries (@ $ ! +) never occur in a
// name that already passed Validate (they are outside the allowed
// charset), but normalize is also exercised directly in tests against
// arbitrary strings, so the full table from the spec is implemented here
// rather than only the digit subset Validate's charset would ever produce.
var leetFold = map[rune]rune{
	'0': 'o', '1': 'i', '3': 'e', '4': 'a', '5': 's',
	'6': 'g', '7': 't', '8': 'b',
	'@': 'a', '$': 's', '!': 'i', '+': 't',
}

// foldLeet lowercases s and folds leetspeak substitutions.
func foldLeet(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if f, ok := leetFold[r]; ok {
			r = f
		}
		b.WriteRune(r)
	}
	return b.String()
}

// stripSeparators removes spaces, hyphens, and underscores, catching
// separator-inserted evasion like "s-l-u-r".
func stripSeparators(s string) string {
	return strings.NewReplacer(" ", "", "-", "", "_", "").Replace(s)
}

// collapseRepeats collapses runs of the same rune to a single instance,
// catching repeat-inserted evasion like "niiice" -> "nice".
func collapseRepeats(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	last := rune(-1)
	for _, r := range s {
		if r == last {
			continue
		}
		b.WriteRune(r)
		last = r
	}
	return b.String()
}

// isSep reports whether r is one of the three name separators.
func isSep(r rune) bool { return r == ' ' || r == '-' || r == '_' }

// tokens splits the folded (but not separator-stripped) form on separators,
// for whole-word profanity matching that avoids the Scunthorpe problem:
// "CLASS ACT FARM" tokenizes to ["class","act","farm"], none of which is
// the profanity term "ass" even though the raw string contains it.
func tokens(folded string) []string {
	return strings.FieldsFunc(folded, isSep)
}

// candidates returns the folded name plus the two substring-match
// candidates for slur-tier checking: separator-stripped, and additionally
// repeat-collapsed on top of that. Collapsing can both create and destroy a
// match (doubled letters can be part of a legitimate word), so both sides
// are checked rather than one replacing the other.
func candidates(name string) (folded, stripped, collapsed string) {
	folded = foldLeet(name)
	stripped = stripSeparators(folded)
	collapsed = collapseRepeats(stripped)
	return folded, stripped, collapsed
}
