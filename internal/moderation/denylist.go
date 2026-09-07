package moderation

import (
	_ "embed"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/BurntSushi/toml"
)

// term is one entry in the denylist TOML (see FARM_MODERATION_PATH).
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

// The denylist used to be embedded in the binary via go:embed. It is now host
// state loaded from a file, because a public repository cannot carry a private
// list — and, just as importantly, because a file can be edited and reloaded
// without a rebuild and redeploy, which is what the retroactive-tightening
// requirement in docs/gameplay/03 actually needs.
//
// The fixture below is the dev and test list. It ships in the binary on purpose:
// a fresh clone must build, test, and run without the real list existing.

//go:embed testdata/denylist.fixture.toml
var fixtureTOML []byte

var (
	// current is swapped atomically rather than locked. Every rename attempt and
	// every leaderboard render re-reads it, and SIGHUP replaces it underneath
	// them; an atomic pointer means a reload can never tear a concurrent read.
	current atomic.Pointer[denylist]

	// sourcePath is the file current was loaded from, empty when running on the
	// fixture. Reload needs it; nothing else should.
	sourcePath atomic.Pointer[string]

	// devMode records that this process is running against the fixture instead
	// of a real list. It makes Filter refuse player-set names entirely: a stub
	// list must never be mistaken for a working filter, and silently accepting
	// arbitrary names against two invented tokens is exactly that mistake.
	devMode atomic.Bool

	// Tests never call Init, so the package has to be usable without it. They
	// get the fixture, but NOT devMode — the suite's whole job is to exercise
	// the matching pipeline, which a blanket refusal would hide.
	fixtureOnce sync.Once
)

// Init loads the denylist and decides whether this process may serve at all.
// Call it once, early in boot, before anything can call Check or Filter.
//
// Fail-closed, per docs/gameplay/03:
//
//   - path set: it must load. A missing, unreadable, or malformed file is an
//     error in both modes — falling back to a stub because the real list failed
//     to parse is precisely the silent downgrade this design exists to prevent.
//   - path empty and require true: an error. Production must not start without
//     a real list.
//   - path empty and require false: the fixture, loudly, in dev mode.
func Init(path string, require bool) error {
	if path == "" {
		if require {
			return fmt.Errorf("moderation: FARM_REQUIRE_MODERATION is true but FARM_MODERATION_PATH is empty; set it to the denylist file or unset FARM_REQUIRE_MODERATION for dev")
		}
		dl, err := parseDenylist(fixtureTOML, "denylist fixture")
		if err != nil {
			return err
		}
		current.Store(dl)
		devMode.Store(true)
		slog.Warn("moderation running in DEV MODE on the placeholder fixture — player-set farm names are refused and generated names used instead; set FARM_MODERATION_PATH to a real denylist to enable moderation")
		return nil
	}

	dl, err := loadDenylistFile(path)
	if err != nil {
		return err
	}
	current.Store(dl)
	sourcePath.Store(&path)
	devMode.Store(false)
	return nil
}

// Reload re-reads the denylist from the same file Init used, so tightening the
// list is a file edit plus a signal rather than a rebuild and redeploy. On any
// error the previously loaded list stays in force: a typo in an edit must not
// leave the server running with no filter.
func Reload() error {
	p := sourcePath.Load()
	if p == nil || *p == "" {
		return fmt.Errorf("moderation: no denylist file to reload (running on the dev fixture)")
	}
	dl, err := loadDenylistFile(*p)
	if err != nil {
		return err
	}
	current.Store(dl)
	return nil
}

// DevMode reports whether the process is running on the placeholder fixture.
func DevMode() bool { return devMode.Load() }

// loaded returns the active denylist, falling back to the fixture for tests and
// for any caller that reaches the package before Init.
func loaded() *denylist {
	if dl := current.Load(); dl != nil {
		return dl
	}
	fixtureOnce.Do(func() {
		dl, err := parseDenylist(fixtureTOML, "denylist fixture")
		if err != nil {
			// The fixture is compiled in, so this is a build-time content error
			// rather than anything an operator can hit at runtime.
			panic(err)
		}
		current.CompareAndSwap(nil, dl)
	})
	return current.Load()
}

func parseDenylist(b []byte, name string) (*denylist, error) {
	var f denylistFile
	if err := toml.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("moderation: parse %s: %w", name, err)
	}
	return newDenylist(f.Term, f.Allow)
}

func loadDenylistFile(path string) (*denylist, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("moderation: read denylist %s: %w", path, err)
	}
	return parseDenylist(b, path)
}
