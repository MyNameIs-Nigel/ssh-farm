package moderation

import "testing"

func TestGenerateIsDeterministicPerFingerprint(t *testing.T) {
	a1 := Generate("SHA256:abc123")
	a2 := Generate("SHA256:abc123")
	if a1 != a2 {
		t.Fatalf("Generate is not deterministic: %q != %q", a1, a2)
	}
	b := Generate("SHA256:different")
	if a1 == b {
		t.Errorf("two different fingerprints produced the same name %q (extremely unlikely, check the hash mix)", a1)
	}
}

func TestGenerateProducesAValidatingUppercaseName(t *testing.T) {
	for _, fp := range []string{"SHA256:one", "SHA256:two", "SHA256:three", "", "x"} {
		name := Generate(fp)
		norm, ok := Validate(name)
		if !ok {
			t.Fatalf("Generate(%q) = %q, which fails Validate", fp, name)
		}
		if norm != name {
			t.Fatalf("Generate(%q) = %q is not already in normalized form (got %q)", fp, name, norm)
		}
	}
}

func TestGenerateAllPairsPassFilter(t *testing.T) {
	// Exhaustive product test (gameplay/03 acceptance criteria): every
	// adjective x noun combination the real word lists can produce must be
	// safe by construction.
	for _, adj := range defaultNamewords.adjectives {
		for _, noun := range defaultNamewords.nouns {
			name := adj + " " + noun
			if _, ok := Validate(name); !ok {
				t.Fatalf("generated combination %q fails Validate", name)
			}
			if !Check(name) {
				t.Fatalf("generated combination %q fails Check (denylist)", name)
			}
		}
	}
}

func TestSuffixIsLastFiveCharacters(t *testing.T) {
	fp := "SHA256:abcdefghijklmnop"
	got := Suffix(fp)
	want := fp[len(fp)-5:]
	if got != want {
		t.Fatalf("Suffix(%q) = %q, want %q", fp, got, want)
	}
	if len(got) != 5 {
		t.Fatalf("Suffix length = %d, want 5", len(got))
	}
}

func TestSuffixHandlesShortInput(t *testing.T) {
	if got := Suffix("ab"); got != "ab" {
		t.Fatalf("Suffix(short) = %q, want the input unchanged", got)
	}
}

func TestNewNamewordsRejectsEmptyLists(t *testing.T) {
	if _, err := newNamewords(nil, []string{"noun"}); err == nil {
		t.Error("expected error for empty adjective list")
	}
	if _, err := newNamewords([]string{"adj"}, nil); err == nil {
		t.Error("expected error for empty noun list")
	}
}
