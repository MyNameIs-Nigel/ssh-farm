package moderation

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// testDenylist builds a denylist from placeholder tokens only. The real list
// is host state that exists in no repository, and no test may reach for it —
// per the project convention that real slur terms never appear in this tree.
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

func TestFixtureLoadsAndMatchesItsOwnTokens(t *testing.T) {
	// Smoke test on the compiled-in fixture: it parses, produces a usable
	// denylist, and flags its own invented tokens — so a clone with no real
	// list still has a working pipeline to test against. Real terms are never
	// asserted here, or anywhere else in this suite; see the package doc.
	dl := loaded()
	if check(dl, "ZZMEANIE") {
		t.Error("fixture should deny its own whole-word profanity token")
	}
	if check(dl, "MYZZBADWORDFARM") {
		t.Error("fixture should deny its substring token inside a longer name")
	}
	if !check(dl, "SUNNY HOLLOW") {
		t.Error("a clean name should pass the fixture")
	}
}

func TestInitRequiresAPathWhenModerationIsRequired(t *testing.T) {
	// Fail-closed: production must refuse to boot rather than serve players
	// against no list at all.
	if err := Init("", true); err == nil {
		t.Fatal("Init with no path and require=true should fail, got nil")
	}
	restoreDefaults(t)
}

func TestInitFailsOnAMissingOrMalformedFileInBothModes(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.toml")
	bad := filepath.Join(t.TempDir(), "bad.toml")
	if err := os.WriteFile(bad, []byte("this is not = valid = toml"), 0o600); err != nil {
		t.Fatal(err)
	}
	unknownTier := filepath.Join(t.TempDir(), "tier.toml")
	if err := os.WriteFile(unknownTier, []byte("[[term]]\nword=\"x\"\ntier=\"bogus\"\nmatch=\"word\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// require=false must not soften this. A list that fails to parse is an
	// operator error, and quietly falling back to the stub would be the exact
	// silent downgrade the fail-closed rule exists to prevent.
	for _, require := range []bool{true, false} {
		for _, path := range []string{missing, bad, unknownTier} {
			if err := Init(path, require); err == nil {
				t.Errorf("Init(%q, require=%v) should fail, got nil", path, require)
			}
		}
	}
	restoreDefaults(t)
}

func TestInitLoadsARealFileAndLeavesDevModeOff(t *testing.T) {
	path := writeList(t, "zztestword")
	if err := Init(path, true); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer restoreDefaults(t)

	if DevMode() {
		t.Error("DevMode should be false after loading a real list")
	}
	if _, locked := Filter("ZZTESTWORD FARM"); !locked {
		t.Error("a name containing the loaded term should be denied")
	}
	if _, locked := Filter("SUNNY HOLLOW"); locked {
		t.Error("a clean name should pass a loaded list")
	}
}

func TestInitWithNoPathEntersDevModeAndRefusesPlayerNames(t *testing.T) {
	if err := Init("", false); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer restoreDefaults(t)

	if !DevMode() {
		t.Fatal("DevMode should be true when running on the fixture")
	}
	// Even a name the fixture would happily pass must be refused: accepting
	// arbitrary names against two invented tokens looks like moderation while
	// providing none.
	if _, locked := Filter("SUNNY HOLLOW"); !locked {
		t.Error("dev mode should refuse player-set names, want locked=true")
	}
	// "no name" is still not a violation.
	if _, locked := Filter(""); locked {
		t.Error(`Filter("") should stay ("", false) in dev mode`)
	}
}

func TestReloadPicksUpAnEditedFile(t *testing.T) {
	path := writeList(t, "zzfirstword")
	if err := Init(path, true); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer restoreDefaults(t)

	if _, locked := Filter("ZZSECONDWORD"); locked {
		t.Fatal("ZZSECONDWORD should pass before the edit")
	}
	if err := os.WriteFile(path, listTOML("zzsecondword"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if _, locked := Filter("ZZSECONDWORD"); !locked {
		t.Error("ZZSECONDWORD should be denied after reloading the edited file")
	}
}

func TestReloadKeepsTheOldListWhenTheEditIsBroken(t *testing.T) {
	// A typo in a live edit must not leave the server running with no filter.
	path := writeList(t, "zzfirstword")
	if err := Init(path, true); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer restoreDefaults(t)

	if err := os.WriteFile(path, []byte("not = valid = toml"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Reload(); err == nil {
		t.Error("Reload should report the parse failure")
	}
	if _, locked := Filter("ZZFIRSTWORD"); !locked {
		t.Error("the previously loaded list must stay in force after a failed reload")
	}
}

func TestReloadFailsInDevMode(t *testing.T) {
	if err := Init("", false); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer restoreDefaults(t)
	if err := Reload(); err == nil {
		t.Error("Reload should fail when there is no file behind the fixture")
	}
}

func listTOML(word string) []byte {
	return []byte("[[term]]\nword = \"" + word + "\"\ntier = \"slur\"\nmatch = \"substring\"\n")
}

func writeList(t *testing.T, word string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "denylist.toml")
	if err := os.WriteFile(path, listTOML(word), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// restoreDefaults puts the package back on the fixture with dev mode off, which
// is the state the rest of the suite (and Filter's callers in other packages)
// assume. These tests mutate package-level state, so they must not run in
// parallel with anything that reads it.
func restoreDefaults(t *testing.T) {
	t.Helper()
	current.Store(nil)
	sourcePath.Store(nil)
	devMode.Store(false)
	fixtureOnce = sync.Once{}
}

// A denylist entry is a slur. Any error that names one puts it in the
// container logs, which is the one route by which the real list — host state
// that exists in no repository — can escape by accident. These tests use an
// invented token and assert it never survives into an error string.
func TestDenylistErrorsNeverNameTheTerm(t *testing.T) {
	const invented = "zzsecretterm"

	cases := []struct {
		name  string
		terms []term
	}{
		{"unknown tier", []term{{Word: invented, Tier: "nonsense", Match: "word"}}},
		{"unknown match kind", []term{{Word: invented, Tier: "slur", Match: "nonsense"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newDenylist(tc.terms, nil)
			if err == nil {
				t.Fatal("expected an error for a malformed entry")
			}
			if strings.Contains(err.Error(), invented) {
				t.Fatalf("error names the offending term: %v", err)
			}
			if !strings.Contains(err.Error(), "entry 0") {
				t.Fatalf("error should locate the entry by index, got: %v", err)
			}
		})
	}
}

// The parse path is the other way a term could reach the logs: BurntSushi
// exposes the offending source line through ErrorWithPosition, and a syntax
// error on a term's own line would reprint it. Error() does not include that
// excerpt today, so this guards the property rather than fixing a live leak —
// it fails if the library starts quoting source, or if someone reaches for the
// richer formatter.
func TestParseErrorDoesNotQuoteTheSource(t *testing.T) {
	const invented = "zzsecretterm"
	malformed := []byte("[[term]]\nword = \"" + invented + "\" this is a syntax error\n")

	_, err := parseDenylist(malformed, "denylist.toml")
	if err == nil {
		t.Fatal("expected a parse error")
	}
	if strings.Contains(err.Error(), invented) {
		t.Fatalf("parse error quotes the source line: %v", err)
	}
}
