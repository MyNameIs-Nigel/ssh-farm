package moderation

import "testing"

func TestValidateAcceptsHappyNames(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Sunny Hollow", "SUNNY HOLLOW"},
		{"XX_POTATO_LORD_XX", "XX_POTATO_LORD_XX"},
		{"L33T-Farm", "L33T-FARM"},
		{"abc", "ABC"},
		{"a1234567890123456789", "A1234567890123456789"}, // 20 chars, at the max
		{"  Spaced   Out  ", "SPACED OUT"},               // trim + collapse internal runs
	}
	for _, c := range cases {
		got, ok := Validate(c.in)
		if !ok {
			t.Errorf("Validate(%q) rejected, want accepted as %q", c.in, c.want)
			continue
		}
		if got != c.want {
			t.Errorf("Validate(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestValidateRejectsBadShapes(t *testing.T) {
	cases := map[string]string{
		"":                       "empty",
		"   ":                    "all whitespace",
		"ab":                     "too short (2 chars)",
		"a234567890123456789012": "too long (>20 chars)",
		"-leading":               "leading separator",
		"trailing-":              "trailing separator",
		"_leading":               "leading underscore",
		"trailing_":              "trailing underscore",
		"12345":                  "purely digits",
		"123-456":                "digits and separators only",
		"---":                    "purely separators",
		"café":                   "non-ASCII letter",
		"名前":                     "non-ASCII (CJK)",
		"farm!":                  "disallowed punctuation",
		"farm@home":              "disallowed punctuation",
		"tab\tname":              "control character",
	}
	for in, why := range cases {
		if _, ok := Validate(in); ok {
			t.Errorf("Validate(%q) accepted, want rejected (%s)", in, why)
		}
	}
}

func TestValidateBoundaryLengths(t *testing.T) {
	if _, ok := Validate("ab"); ok {
		t.Error("2-char name should be rejected (min is 3)")
	}
	if _, ok := Validate("abc"); !ok {
		t.Error("3-char name should be accepted (min is 3)")
	}
	twenty := "12345678901234567890"[:20-1] + "a" // 20 letters/digits, not all-digit
	if got, ok := Validate(twenty); !ok || len(got) != 20 {
		t.Errorf("20-char name should be accepted at exactly the max, got ok=%v len=%d", ok, len(got))
	}
	twentyOne := twenty + "x"
	if _, ok := Validate(twentyOne); ok {
		t.Error("21-char name should be rejected (max is 20)")
	}
}
