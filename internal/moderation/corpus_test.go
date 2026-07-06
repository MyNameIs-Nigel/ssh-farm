package moderation

import (
	_ "embed"
	"strings"
	"testing"
)

//go:embed testdata/must_block.txt
var mustBlockCorpus string

//go:embed testdata/must_allow.txt
var mustAllowCorpus string

// corpusLines splits an embedded corpus file into its content lines,
// dropping blank lines and "#"-prefixed comments.
func corpusLines(raw string) []string {
	var lines []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

// TestMustBlockCorpusIsDeniedByEveryEvasionClass runs must_block.txt against
// the same test-only, placeholder-token denylist denylist_test.go's other
// tests use (testDenylist) — never the real embedded list. Every line must
// resolve to blocked; a line that passes is a regression in the
// leet/separator/repeat/embedding evasion handling itself.
func TestMustBlockCorpusIsDeniedByEveryEvasionClass(t *testing.T) {
	dl := testDenylist(t)
	lines := corpusLines(mustBlockCorpus)
	if len(lines) == 0 {
		t.Fatal("must_block.txt produced no test cases — check the embed path")
	}
	for _, name := range lines {
		if check(dl, name) {
			t.Errorf("must_block: %q passed, want denied", name)
		}
	}
}

// TestMustAllowCorpusPassesTheRealDenylist runs must_allow.txt against the
// actual production Filter — this is the regression guard: CI fails the
// moment a future denylist edit accidentally denies a legitimate word.
// Complements TestGenerateAllPairsPassFilter (generate_test.go), which
// exhaustively covers the full adjective x noun product; this test also
// spot-checks a sample of Generate's output directly for the same reason
// documented in testdata/must_allow.txt.
func TestMustAllowCorpusPassesTheRealDenylist(t *testing.T) {
	lines := corpusLines(mustAllowCorpus)
	if len(lines) == 0 {
		t.Fatal("must_allow.txt produced no test cases — check the embed path")
	}
	for _, name := range lines {
		if _, locked := Filter(name); locked {
			t.Errorf("must_allow: %q was denied by the real denylist, want it to pass", name)
		}
	}

	for _, fp := range []string{"SHA256:corpus-a", "SHA256:corpus-b", "SHA256:corpus-c", "SHA256:corpus-d"} {
		name := Generate(fp)
		if _, locked := Filter(name); locked {
			t.Errorf("Generate(%q) = %q was denied by the real denylist", fp, name)
		}
	}
}
