package moderation

import "testing"

func TestFoldLeet(t *testing.T) {
	cases := map[string]string{
		"L33T":      "leet",
		"5LUR":      "slur",
		"h4x0r":     "haxor",
		"@$!+":      "asit",
		"NoChange":  "nochange",
		"6angster7": "gangstert",
	}
	for in, want := range cases {
		if got := foldLeet(in); got != want {
			t.Errorf("foldLeet(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStripSeparators(t *testing.T) {
	cases := map[string]string{
		"s-l-u-r":    "slur",
		"s_l_u_r":    "slur",
		"s l u r":    "slur",
		"no-sep here": "nosephere",
		"already":    "already",
	}
	for in, want := range cases {
		if got := stripSeparators(in); got != want {
			t.Errorf("stripSeparators(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCollapseRepeats(t *testing.T) {
	cases := map[string]string{
		"niiice":  "nice",
		"aaassss": "as",
		"normal":  "normal",
		"":        "",
		"aaaa":    "a",
	}
	for in, want := range cases {
		if got := collapseRepeats(in); got != want {
			t.Errorf("collapseRepeats(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTokens(t *testing.T) {
	got := tokens(foldLeet("CLASS ACT-FARM_HERE"))
	want := []string{"class", "act", "farm", "here"}
	if len(got) != len(want) {
		t.Fatalf("tokens = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tokens = %v, want %v", got, want)
		}
	}
}

func TestCandidatesCombinesPipelineStages(t *testing.T) {
	// "s-l-u-r" folds to itself (no leet chars), strips to "slur", and
	// collapsing a already-non-repeating string is a no-op.
	folded, stripped, collapsed := candidates("s-l-u-r")
	if folded != "s-l-u-r" {
		t.Errorf("folded = %q, want %q", folded, "s-l-u-r")
	}
	if stripped != "slur" {
		t.Errorf("stripped = %q, want %q", stripped, "slur")
	}
	if collapsed != "slur" {
		t.Errorf("collapsed = %q, want %q", collapsed, "slur")
	}

	// "SL00ORR" leet-folds the zeros to "o" (joining the existing "O"),
	// strips no separators, then collapses the resulting "ooo"/"rr" runs
	// down to "slor" — proving collapse runs on top of the leet+strip
	// result, not instead of it.
	_, stripped2, collapsed2 := candidates("SL00ORR")
	if stripped2 != "slooorr" {
		t.Errorf("stripped2 = %q, want %q", stripped2, "slooorr")
	}
	if collapsed2 != "slor" {
		t.Errorf("collapsed2 = %q, want %q", collapsed2, "slor")
	}
}
