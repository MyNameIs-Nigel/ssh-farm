package moderation

import (
	"fmt"
	"io/fs"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/mynameis-nigel/ssh-farm/data"
)

// term is one entry in data/moderation/denylist.toml.
type term struct {
	Word  string `toml:"word"`
	Tier  string `toml:"tier"`  // "slur" | "profanity"
	Match string `toml:"match"` // "substring" | "word"
}

type denylistFile struct {
	Term  []term   `toml:"term"`
	Allow []string `toml:"allow"`
}

// denylist is the loaded, normalized tiered word list. It is immutable
// after construction, so a single instance is safe to share across
// goroutines (every rename attempt and every leaderboard render-time
// re-check reads it).
type denylist struct {
	substrings []string        // slur tier: substring match on folded/stripped candidates
	words      map[string]bool // profanity tier: whole-word match on folded tokens
	allow      map[string]bool // exceptions, matched against the stripped candidate
}

// newDenylist builds a denylist from parsed terms, validating tier/match
// values so a bad content edit fails fast at load time rather than
// silently matching nothing. Exported at the package-internal level (not
// via TOML) so tests can build a denylist from placeholder tokens without
// ever reading the real embedded file — the real slur list must never
// appear in test source.
func newDenylist(terms []term, allow []string) (*denylist, error) {
	dl := &denylist{words: map[string]bool{}, allow: map[string]bool{}}
	for _, t := range terms {
		w := strings.ToLower(strings.TrimSpace(t.Word))
		if w == "" {
			continue
		}
		switch t.Tier {
		case "slur", "profanity":
		default:
			return nil, fmt.Errorf("moderation: denylist term %q has unknown tier %q", t.Word, t.Tier)
		}
		switch t.Match {
		case "substring":
			dl.substrings = append(dl.substrings, w)
		case "word":
			dl.words[w] = true
		default:
			return nil, fmt.Errorf("moderation: denylist term %q has unknown match kind %q", t.Word, t.Match)
		}
	}
	for _, a := range allow {
		a = strings.ToLower(strings.TrimSpace(a))
		if a != "" {
			dl.allow[a] = true
		}
	}
	return dl, nil
}

// loadDenylist reads and parses the denylist TOML from fsys at name.
func loadDenylist(fsys fs.FS, name string) (*denylist, error) {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, fmt.Errorf("moderation: read %s: %w", name, err)
	}
	var f denylistFile
	if err := toml.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("moderation: parse %s: %w", name, err)
	}
	return newDenylist(f.Term, f.Allow)
}

// blocks reports whether the (already-validated, uppercase) name is denied.
// folded/stripped/collapsed come from candidates(name); toks from
// tokens(folded). The allow-list is checked first against the
// separator-stripped candidate and, if present, short-circuits both tiers
// for this name — it is an exception for a specific whole name, not a
// license to reuse the flagged word inside a longer one.
func (dl *denylist) blocks(stripped, collapsed string, toks []string) bool {
	if dl.allow[stripped] {
		return false
	}
	for _, bad := range dl.substrings {
		if bad == "" {
			continue
		}
		if strings.Contains(stripped, bad) || strings.Contains(collapsed, bad) {
			return true
		}
	}
	for _, tok := range toks {
		if dl.words[tok] {
			return true
		}
	}
	return false
}

// defaultDenylist is loaded once from the embedded, private denylist file
// and used by the package-level Check/Filter. Loading is fatal-at-startup
// on failure: a broken denylist file is a content error the operator must
// fix before the process serves anyone (mirrors internal/content's
// fail-fast validation).
var defaultDenylist = mustLoadDefaultDenylist()

func mustLoadDefaultDenylist() *denylist {
	dl, err := loadDenylist(data.FS, "moderation/denylist.toml")
	if err != nil {
		panic(err)
	}
	return dl
}
