package moderation

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/mynameis-nigel/ssh-farm/data"
)

// namewordsFile mirrors data/moderation/namewords.toml.
type namewordsFile struct {
	Adjectives []string `toml:"adjective"`
	Nouns      []string `toml:"noun"`
}

// namewords is the pure word-list type Generate draws from; exported at the
// package-internal level so tests can build one from a tiny fixture list
// without touching the embedded production file.
type namewords struct {
	adjectives []string
	nouns      []string
}

func newNamewords(adjectives, nouns []string) (*namewords, error) {
	if len(adjectives) == 0 || len(nouns) == 0 {
		return nil, fmt.Errorf("moderation: namewords needs at least one adjective and one noun")
	}
	return &namewords{adjectives: adjectives, nouns: nouns}, nil
}

func loadNamewords(fsys fs.FS, name string) (*namewords, error) {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, fmt.Errorf("moderation: read %s: %w", name, err)
	}
	var f namewordsFile
	if err := toml.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("moderation: parse %s: %w", name, err)
	}
	return newNamewords(f.Adjectives, f.Nouns)
}

// generate derives a deterministic "<adjective> <noun>" name for
// fingerprint from w, stable for the life of the key.
func (w *namewords) generate(fingerprint string) string {
	h := sha256.Sum256([]byte(fingerprint))
	adj := w.adjectives[int(h[0])%len(w.adjectives)]
	noun := w.nouns[int(h[1])%len(w.nouns)]
	return strings.ToUpper(adj + " " + noun)
}

var defaultNamewords = mustLoadDefaultNamewords()

func mustLoadDefaultNamewords() *namewords {
	w, err := loadNamewords(data.FS, "moderation/namewords.toml")
	if err != nil {
		panic(err)
	}
	return w
}

// Generate returns a deterministic, safe-by-construction "<adjective>
// <noun>" default name for the given key fingerprint — stable for the life
// of the key, seeded so the same player always gets the same default.
// Naming is opt-in polish (players never have to touch the rename
// overlay), so this is what the leaderboard shows for a farm with no
// stored name, and what import-v1 stores in place of a v1 name the current
// denylist denies (see Filter and docs/gameplay/03).
func Generate(fingerprint string) string {
	return defaultNamewords.generate(fingerprint)
}

// suffixLen is the number of trailing characters of the key's base64
// fingerprint shown as the always-visible disambiguator (see Suffix).
const suffixLen = 5

// Suffix returns the last 5 characters of the key's fingerprint (already
// base64, e.g. "SHA256:...") for public display next to a farm name, e.g.
// "SUNNY HOLLOW ·k3v9Q". Properties worth noting: stable for the life of
// the key, ~30 bits so collisions are rare and harmless (it disambiguates,
// it does not identify), derived from the already-public-ish fingerprint so
// it leaks nothing beyond what the fingerprint itself already does, and it
// makes impersonating another farm's display name pointless — the suffix
// won't match.
func Suffix(fingerprint string) string {
	if len(fingerprint) <= suffixLen {
		return fingerprint
	}
	return fingerprint[len(fingerprint)-suffixLen:]
}
