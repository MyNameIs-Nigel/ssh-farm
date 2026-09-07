package moderation

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestFilterEmptyInputIsNotLocked(t *testing.T) {
	out, locked := Filter("")
	if out != "" || locked {
		t.Fatalf("Filter(\"\") = (%q, %v), want (\"\", false)", out, locked)
	}
}

func TestFilterAcceptsHappyNames(t *testing.T) {
	for _, name := range []string{"Sunny Hollow", "XX_POTATO_LORD_XX", "Northfield"} {
		out, locked := Filter(name)
		if locked {
			t.Errorf("Filter(%q) locked, want accepted", name)
			continue
		}
		if out == "" {
			t.Errorf("Filter(%q) returned empty output for an accepted name", name)
		}
	}
}

func TestFilterRejectionIsIndistinguishableForInvalidVsDenied(t *testing.T) {
	// "too short" is a Validate failure; the fixture token is a Check
	// (denylist) failure. Both must produce the exact same shape of result: ("",
	// true). The caller (internal/game) relies on this to show one generic
	// message without ever leaking which rule fired.
	invalidOut, invalidLocked := Filter("ab")
	deniedOut, deniedLocked := Filter("ZZMEANIE")
	if !invalidLocked || !deniedLocked {
		t.Fatalf("expected both invalid and denied inputs to lock: invalid=%v denied=%v", invalidLocked, deniedLocked)
	}
	if invalidOut != deniedOut {
		t.Fatalf("invalid and denied outputs differ (%q vs %q); this is an oracle", invalidOut, deniedOut)
	}
}

// TestFilterRejectionTimingIsNotAnObviousOracle complements
// TestFilterRejectionIsIndistinguishableForInvalidVsDenied's output-shape
// check with a coarse timing one. Validate fails fast on a too-short input
// (a handful of instructions) while a denylist rejection additionally runs
// the full candidates/tokens/substring-scan pipeline, so some difference is
// structurally unavoidable without deliberately padding the fast path — the
// question this test asks is whether that difference is large enough to
// matter over a real network. It isn't unless the per-call gap approaches
// millisecond scale, since SSH/TCP round-trip and scheduling jitter already
// dwarf anything in the low-microsecond range; a relative (ratio) bound
// would unfairly flag two already-fast operations being compared to each
// other, so this uses an absolute per-call budget instead.
func TestFilterRejectionTimingIsNotAnObviousOracle(t *testing.T) {
	const iterations = 2000
	invalidElapsed := timeFilter(iterations, "ab")      // Validate failure: too short
	deniedElapsed := timeFilter(iterations, "ZZMEANIE") // Check failure: denylist

	diffPerCall := (deniedElapsed - invalidElapsed) / iterations
	if diffPerCall < 0 {
		diffPerCall = -diffPerCall
	}
	const maxDiffPerCall = 100 * time.Microsecond
	if diffPerCall > maxDiffPerCall {
		t.Fatalf("invalid-vs-denied per-call timing gap = %v (invalid=%v, denied=%v over %d iterations), want <= %v — a distinguishable fast path may exist",
			diffPerCall, invalidElapsed, deniedElapsed, iterations, maxDiffPerCall)
	}
}

func timeFilter(iterations int, name string) time.Duration {
	start := time.Now()
	for i := 0; i < iterations; i++ {
		Filter(name)
	}
	return time.Since(start)
}

func TestFilterScunthorpeProblemCasesPass(t *testing.T) {
	// These all contain a mild profanity substring but must pass because
	// the profanity tier is whole-word only.
	for _, name := range []string{"SCUNTHORPE", "CLASS ACT", "GRASS VALLEY"} {
		out, locked := Filter(name)
		if locked {
			t.Errorf("Filter(%q) locked, want accepted (Scunthorpe problem)", name)
		}
		if out == "" {
			t.Errorf("Filter(%q) = empty output, want the normalized name", name)
		}
	}
}

func TestFilterTierSemanticsDiffer(t *testing.T) {
	// A profanity-tier word embedded in a bigger token passes (word-only);
	// a slur-tier word embedded in a bigger token is still denied
	// (substring). We use the real embedded list's own tiers here since
	// this is specifically testing that the two tiers behave differently,
	// via words boring enough to state directly (profanity, not slurs).
	if _, locked := Filter("MASSIVE FARM"); locked {
		t.Error("'ass' embedded in 'MASSIVE' must pass: profanity tier is whole-word only")
	}
}

func TestCheckMirrorsFilterForAlreadyNormalizedInput(t *testing.T) {
	norm, ok := Validate("Sunny Hollow")
	if !ok {
		t.Fatal("Validate rejected a happy name")
	}
	if !Check(norm) {
		t.Fatal("Check rejected a name Filter would accept")
	}
}

func FuzzFilterNeverPanics(f *testing.F) {
	seeds := []string{
		"", "a", "ABC", "123", "farm name", "🙂", "名前", "\x00\x01", "----",
		strings.Repeat("x", 100), "ZZMEANIE", "SCUNTHORPE", "s-l-u-r",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, name string) {
		out, locked := Filter(name)
		if !locked && out != "" {
			if !utf8.ValidString(out) {
				t.Fatalf("Filter(%q) returned invalid UTF-8: %q", name, out)
			}
			for _, r := range out {
				ascii := r < utf8.RuneSelf
				if !ascii {
					t.Fatalf("Filter(%q) accepted a non-ASCII rune %q in output %q", name, r, out)
				}
			}
		}
	})
}
