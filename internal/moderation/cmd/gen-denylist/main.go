// Command gen-denylist converts a classified profanity corpus into the
// denylist TOML that internal/moderation loads from FARM_MODERATION_PATH.
//
// It exists because the hand-written list it replaces was both too strict and
// unclassified: every entry was a judgement call, and tightening one tier
// meant re-reading all of it. A classified corpus carries an intensity per
// word, so the policy becomes one number (-min-intensity) instead of 29
// individual decisions.
//
// Input is the schema published by better-profane-words (npm), a JSON array of
// {word, categories, intensity} with intensity 1..5:
//
//	1 mild · 2 moderate · 3 strong · 4 very strong · 5 slurs and hate speech
//
// Fetch it with:
//
//	curl -sLO https://raw.githubusercontent.com/awdev1/better-profane-words/main/words.json
//
// The output is host state. It is deliberately NOT committed: the corpus is
// GPL-3.0-only and ssh-farm ships a public binary, so the generated list lives
// at FARM_MODERATION_PATH on the host and on the operator's machine and
// nowhere else. See docs/gameplay/03-farm-names-and-moderation.md.
//
// # Why the tier assignment is the whole job
//
// internal/moderation has two match kinds, and picking the wrong one is how a
// word filter earns its reputation:
//
//   - "substring" catches a term anywhere inside a name, which is what a slur
//     needs — nobody types a slur as a standalone token when evading a filter
//     — but it is also how ANALYSIS, SCUNTHORPE, CUMBRIA and HITCHCOCK get
//     blocked. Worst of all, the corpus lists "nig" as an intensity-5 racial
//     slur, and substring-matching it blocks NIGEL.
//   - "word" matches a whole separator-delimited token, which has effectively
//     no false positives but is trivially evaded by embedding.
//
// So a term is promoted to substring matching only when it is BOTH severe
// enough to warrant the cost AND long and distinctive enough to be safe: at
// least -min-substring-len characters, and a substring of no word in the
// system dictionary. Everything else matches as a whole word. That test is
// mechanical and auditable, which is the point — the previous list's tiering
// was a series of judgement calls nobody could re-derive.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

// entry is one record of the input corpus.
type entry struct {
	Word       string   `json:"word"`
	Categories []string `json:"categories"`
	Intensity  int      `json:"intensity"`
}

// severeCategories are the categories whose terms may be considered for
// substring matching. Restricting promotion to slurs and hate speech, rather
// than to intensity alone, keeps ordinary vulgarity on the safe whole-word
// path — players put it in names far more often than bigots do, and it is far
// more likely to collide with a place name.
var severeCategories = map[string]bool{
	"slur_racial":      true,
	"slur_gender":      true,
	"hateful_ideology": true,
}

// leetFold mirrors internal/moderation/normalize.go. The corpus contains
// pre-leeted variants ("b!tch", "4r5e", "sh!+") that fold onto terms already
// in the list; folding here lets dedupe collapse them, instead of emitting a
// term in a spelling the runtime folds away before it ever compares.
var leetFold = strings.NewReplacer(
	"0", "o", "1", "i", "3", "e", "4", "a", "5", "s",
	"6", "g", "7", "t", "8", "b",
	"@", "a", "$", "s", "!", "i", "+", "t",
)

// nameCharset is the alphabet a term can still be spelled in after
// moderation.Validate has run. Anything outside it can never appear in a
// candidate, so emitting it would be dead weight in the list.
var nameCharset = regexp.MustCompile(`^[a-z0-9 _-]+$`)

func main() {
	var (
		in           = flag.String("in", "words.json", "classified corpus JSON (better-profane-words schema)")
		out          = flag.String("out", "-", `output TOML path, or "-" for stdout`)
		dictPath     = flag.String("dict", "/usr/share/dict/words", "word list used for the substring-collision test")
		allowPath    = flag.String("must-allow", "internal/moderation/testdata/must_allow.txt", "corpus of names the generated list must never block")
		minIntensity = flag.Int("min-intensity", 2, "lowest intensity to include (1 mild .. 5 slurs)")
		minSubLen    = flag.Int("min-substring-len", 5, "shortest term allowed to match as a substring")
	)
	flag.Parse()

	if err := run(*in, *out, *dictPath, *allowPath, *minIntensity, *minSubLen); err != nil {
		fmt.Fprintln(os.Stderr, "gen-denylist:", err)
		os.Exit(1)
	}
}

func run(inPath, outPath, dictPath, allowPath string, minIntensity, minSubLen int) error {
	raw, err := os.ReadFile(inPath)
	if err != nil {
		return fmt.Errorf("read corpus: %w", err)
	}
	var corpus []entry
	if err := json.Unmarshal(raw, &corpus); err != nil {
		return fmt.Errorf("parse corpus: %w", err)
	}
	if len(corpus) == 0 {
		return fmt.Errorf("corpus %s is empty", inPath)
	}

	dict, err := loadDict(dictPath)
	if err != nil {
		return err
	}

	subs, words, st := classify(corpus, dict, minIntensity, minSubLen)

	if err := verifyMustAllow(allowPath, subs, words); err != nil {
		return err
	}

	body := render(subs, words, inPath, dictPath, minIntensity, minSubLen, st)

	if outPath == "-" {
		_, err = os.Stdout.WriteString(body)
		return err
	}
	if err := os.WriteFile(outPath, []byte(body), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", outPath, err)
	}
	fmt.Fprintf(os.Stderr,
		"gen-denylist: %d terms (%d substring, %d word) from %d corpus entries at intensity>=%d -> %s\n",
		len(subs)+len(words), len(subs), len(words), st.considered, minIntensity, outPath)
	return nil
}

// longPhraseLen is the length at which a term is safe to substring-match on
// length alone, without needing a slur category to justify it. Nothing this
// long survives inside an innocent farm name.
const longPhraseLen = 10

// stats records why terms landed where they did, so the generated header can
// explain itself without the operator re-running the tool to find out.
type stats struct {
	considered       int
	skippedCharset   int
	demotedShort     int
	demotedCollision int
	droppedPhrase    int
}

// demote sends a term that failed the substring gate to the whole-word tier.
// A multi-word term has no whole-word form — tokens() splits on the very
// separators it contains, so no token could ever equal it — and its base word
// is carried separately by the corpus anyway ("a s s" alongside "ass"). Such a
// term is dropped rather than emitted as an entry that could never fire.
func demote(wordSet map[string]bool, term string, multiword bool, st *stats) {
	if multiword {
		st.droppedPhrase++
		return
	}
	wordSet[term] = true
}

// classify splits the corpus into the two match tiers, returning sorted,
// deduplicated term lists.
func classify(corpus []entry, dict map[string]bool, minIntensity, minSubLen int) (subs, words []string, st stats) {
	subSet := map[string]bool{}
	wordSet := map[string]bool{}

	for _, e := range corpus {
		if e.Intensity < minIntensity {
			continue
		}
		st.considered++

		term := normalizeTerm(e.Word)
		if term == "" || !nameCharset.MatchString(term) {
			st.skippedCharset++
			continue
		}

		// A multi-word term can never equal a single token, so whole-word
		// matching would silently never fire for it. The runtime strips
		// separators before substring matching, so the stripped form is the
		// only shape that can ever match.
		//
		// Crucially, the stripped form gets the SAME safety gate as any other
		// substring candidate. It is tempting to promote it unconditionally on
		// the grounds that a phrase is long and distinctive, but the corpus
		// also carries letter-spaced evasion variants — "a s s", "c o c k",
		// "t i t" — which strip to exactly the short, collision-prone stems
		// the gate exists to keep out. Promoting those blocked ASSISI,
		// BRASSICA, HITCHCOCK and POPPYCOCK.
		multiword := strings.ContainsAny(term, " _-")
		cand := term
		if multiword {
			cand = stripSeparators(term)
		}

		switch {
		case collides(cand, dict):
			// Appears inside a legitimate English word. Substring matching it
			// would block innocent names ("nig" -> NIGEL), so it falls back to
			// whole-word matching rather than being dropped.
			st.demotedCollision++
			demote(wordSet, term, multiword, &st)
		case severe(e) && len(cand) >= minSubLen:
			subSet[cand] = true
		case len(cand) >= longPhraseLen:
			// Long enough that a false positive is implausible regardless of
			// category — this is what keeps genuine multi-word phrases working
			// without handing promotion to every spaced-out stem.
			subSet[cand] = true
		case severe(e):
			st.demotedShort++
			demote(wordSet, term, multiword, &st)
		default:
			demote(wordSet, term, multiword, &st)
		}
	}

	// A substring term already covers its own whole-word case, so carrying it
	// in both tiers is redundant work on every candidate.
	for t := range subSet {
		delete(wordSet, t)
	}
	return sortedKeys(subSet), sortedKeys(wordSet), st
}

// severe reports whether e is a slur or hate-speech term, the only categories
// eligible for substring matching.
func severe(e entry) bool {
	for _, c := range e.Categories {
		if severeCategories[c] {
			return true
		}
	}
	return false
}

// collides reports whether term appears inside any longer dictionary word,
// in either the raw or the repeat-collapsed spelling.
//
// Checking the collapsed spelling is not optional. The runtime matches slur
// terms against BOTH the separator-stripped candidate and that candidate with
// runs of repeated letters collapsed, so a term only has to appear in one of
// them to fire. The corpus carries misspelled slur variants, and one of them —
// "nigar" — is not inside any dictionary word, yet collapsing "niggardly"
// yields "nigardly", which contains it. Testing the raw spelling alone
// promoted that term to substring matching and blocked the whole niggard/
// niggardly family, which is unrelated in origin and simply means stingy.
//
// Equality is deliberately not a collision: a term that IS a dictionary word,
// as several slurs are in an unrelated sense, still needs to match itself, and
// the allow list is the escape hatch for a specific whole name.
func collides(term string, dict map[string]bool) bool {
	for w := range dict {
		if len(w) > len(term) && strings.Contains(w, term) {
			return true
		}
		if c := collapseRepeats(w); len(c) > len(term) && strings.Contains(c, term) {
			return true
		}
	}
	return false
}

// collapseRepeats mirrors internal/moderation/normalize.go.
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

// verifyMustAllow refuses to emit a list that would block any name in the
// must-allow corpus, reproducing exactly what denylist.blocks does at runtime.
//
// This check lives here rather than only in the package's tests because the
// tests cannot see this list: the generated file is host state and never
// enters the repository, so CI only ever exercises the placeholder fixture.
// The moment of generation is therefore the only place a real list can be
// held to the corpus, and a false positive found here costs a re-run, while
// one found in production costs a player their farm name.
func verifyMustAllow(path string, subs, words []string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read must-allow corpus %s (pass -must-allow to override): %w", path, err)
	}
	wordSet := map[string]bool{}
	for _, w := range words {
		wordSet[w] = true
	}

	var blocked []string
	for _, line := range strings.Split(string(b), "\n") {
		name := strings.TrimSpace(line)
		if name == "" || strings.HasPrefix(name, "#") {
			continue
		}
		folded := normalizeTerm(name)
		stripped := stripSeparators(folded)
		collapsed := collapseRepeats(stripped)

		hit := ""
		for _, s := range subs {
			if strings.Contains(stripped, s) || strings.Contains(collapsed, s) {
				hit = s
				break
			}
		}
		if hit == "" {
			for _, tok := range strings.FieldsFunc(folded, func(r rune) bool {
				return r == ' ' || r == '-' || r == '_'
			}) {
				if wordSet[tok] {
					hit = tok
					break
				}
			}
		}
		if hit != "" {
			blocked = append(blocked, fmt.Sprintf("%s (by %q)", name, hit))
		}
	}
	if len(blocked) > 0 {
		return fmt.Errorf(
			"refusing to write: %d name(s) in %s would be blocked:\n  %s\nAdd them to the allow list, or tighten the substring gate",
			len(blocked), path, strings.Join(blocked, "\n  "))
	}
	return nil
}

// normalizeTerm lowercases, leet-folds, and trims a corpus word into the shape
// the runtime compares against.
func normalizeTerm(w string) string {
	w = strings.ToLower(strings.TrimSpace(w))
	w = leetFold.Replace(w)
	return strings.TrimSpace(w)
}

func stripSeparators(s string) string {
	return strings.NewReplacer(" ", "", "-", "", "_", "").Replace(s)
}

func loadDict(path string) (map[string]bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read dictionary %s (pass -dict to override): %w", path, err)
	}
	dict := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		if w := strings.ToLower(strings.TrimSpace(line)); w != "" {
			dict[w] = true
		}
	}
	if len(dict) == 0 {
		return nil, fmt.Errorf("dictionary %s is empty", path)
	}
	return dict, nil
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func render(subs, words []string, inPath, dictPath string, minIntensity, minSubLen int, st stats) string {
	var b strings.Builder
	fmt.Fprintf(&b, `# GENERATED by internal/moderation/cmd/gen-denylist — DO NOT EDIT BY HAND.
#
# Regenerate:
#   curl -sLO https://raw.githubusercontent.com/awdev1/better-profane-words/main/words.json
#   go run ./internal/moderation/cmd/gen-denylist -in words.json -out denylist.toml
#
# This file is HOST STATE. It belongs at FARM_MODERATION_PATH and must never be
# committed to a public repository: the upstream corpus is GPL-3.0-only.
#
# Generated   %s
# Corpus      %s
# Dictionary  %s
# Policy      intensity >= %d. Substring matching requires no collision with a
#             dictionary word, plus EITHER a slur/hate category and >= %d
#             chars, OR >= %d chars on length alone. Everything else matches
#             as a whole word.
# Terms       %d substring, %d word
#             (%d demoted to word: too short; %d demoted: dictionary collision;
#             %d phrases dropped: no whole-word form; %d skipped: outside the
#             farm-name charset)
#
# Edit the policy, not the output. To unblock one specific whole name, add it
# to the allow list at the bottom — that is an exception for that name only,
# not a licence to reuse the term inside a longer one.

`,
		time.Now().UTC().Format(time.RFC3339), inPath, dictPath,
		minIntensity, minSubLen, longPhraseLen, len(subs), len(words),
		st.demotedShort, st.demotedCollision, st.droppedPhrase, st.skippedCharset)

	b.WriteString("# --- slur tier: matched anywhere inside a name ---\n\n")
	for _, w := range subs {
		fmt.Fprintf(&b, "[[term]]\nword = %q\ntier = \"slur\"\nmatch = \"substring\"\n\n", w)
	}
	b.WriteString("# --- profanity tier: matched as a whole word only ---\n\n")
	for _, w := range words {
		fmt.Fprintf(&b, "[[term]]\nword = %q\ntier = \"profanity\"\nmatch = \"word\"\n\n", w)
	}
	b.WriteString("# Whole-name exceptions, checked before both tiers.\nallow = []\n")
	return b.String()
}
