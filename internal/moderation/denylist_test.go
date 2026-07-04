package moderation

import "testing"

// testDenylist builds a denylist from placeholder tokens only — the real
// embedded list (data/moderation/denylist.toml) is never read here or
// anywhere else in this test suite, per the project convention that real
// slur terms only ever appear in that one file.
func testDenylist(t *testing.T) *denylist {
	t.Helper()
	dl, err := newDenylist([]term{
		{Word: "badterm", Tier: "slur", Match: "substring"},
		{Word: "meanie", Tier: "profanity", Match: "word"},
	}, []string{"allowedexception"})
	if err != nil {
		t.Fatal(err)
	}
	return dl
}

// check runs the same pipeline Filter/Check use, against dl instead of the
// package's embedded default — true means the name passes.
func check(dl *denylist, name string) bool {
	folded, stripped, collapsed := candidates(name)
	return !dl.blocks(stripped, collapsed, tokens(folded))
}

func TestDenylistSubstringTierMatchesAnywhere(t *testing.T) {
	dl := testDenylist(t)
	cases := []string{
		"BADTERM",
		"MYBADTERMFARM", // embedded inside a longer name
		"BAD-TERM",      // separator-inserted evasion
		"B4DT3RM",       // leetspeak evasion (4 -> a, 3 -> e)
	}
	for _, c := range cases {
		if check(dl, c) {
			t.Errorf("check(%q) = pass, want denied (slur substring)", c)
		}
	}
}

func TestDenylistProfanityTierIsWholeWordOnly(t *testing.T) {
	dl := testDenylist(t)
	if check(dl, "MEANIE") {
		t.Fatal("standalone profanity word should be denied")
	}
	if check(dl, "YOU ARE A MEANIE") {
		t.Fatal("profanity word as one of several tokens should be denied")
	}
	// Scunthorpe-style: the profanity word embedded inside a bigger token
	// must NOT trip whole-word matching.
	if !check(dl, "MEANIEVILLE") {
		t.Error("MEANIEVILLE should pass: 'meanie' is not a whole token here")
	}
	if !check(dl, "SUPER MEANIEVILLE FARM") {
		t.Error("SUPER MEANIEVILLE FARM should pass: no whole-word match")
	}
}

func TestDenylistAllowListOverridesWholeName(t *testing.T) {
	dl := testDenylist(t)
	if check(dl, "BADTERM") {
		t.Fatal("test setup: BADTERM should be denied before allow-list applies")
	}
	if !check(dl, "ALLOWEDEXCEPTION") {
		t.Error("allow-listed exact name should pass despite containing an otherwise-banned pattern")
	}
}

func TestDenylistCollapseCatchesRepeatEvasion(t *testing.T) {
	dl := testDenylist(t)
	// "BAAADTERM" doubles the 'a' inside the term: the separator-stripped
	// candidate ("baaadterm") does NOT contain "badterm" contiguously, but
	// the repeat-collapsed candidate ("badterm") does — proving both sides
	// must be checked, not just one.
	if check(dl, "BAAADTERM") {
		t.Error("repeat-inserted evasion of the slur substring should still be caught")
	}
}

func TestDenylistCollapseCanDestroyAMatchSoBothSidesAreChecked(t *testing.T) {
	// A term with its own doubled letter ("book") only matches the
	// uncollapsed candidate: collapsing "BOOK" -> "bok" is too short to
	// contain "book" at all. Checking the stripped side too (not only
	// collapsed) is what catches this exact-match case.
	dl, err := newDenylist([]term{{Word: "book", Tier: "slur", Match: "substring"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if check(dl, "BOOK") {
		t.Error("exact term should be denied via the uncollapsed (stripped) candidate")
	}
}

func TestNewDenylistRejectsUnknownTierOrMatch(t *testing.T) {
	if _, err := newDenylist([]term{{Word: "x", Tier: "bogus", Match: "word"}}, nil); err == nil {
		t.Error("expected error for unknown tier")
	}
	if _, err := newDenylist([]term{{Word: "x", Tier: "slur", Match: "bogus"}}, nil); err == nil {
		t.Error("expected error for unknown match kind")
	}
}

func TestDefaultDenylistLoadsFromEmbeddedData(t *testing.T) {
	// Smoke test: the real embedded file parses and produces a usable
	// denylist (already proven by package init not panicking), and at
	// least flags a couple of the tamest profanity-tier entries so we know
	// the file isn't accidentally empty. Content itself is never asserted
	// here — see the package doc for why.
	if check(defaultDenylist, "FUCK") {
		t.Error("expected the default denylist to deny a whole-word profanity term")
	}
	if !check(defaultDenylist, "SUNNY HOLLOW") {
		t.Error("expected a clean name to pass the default denylist")
	}
}
