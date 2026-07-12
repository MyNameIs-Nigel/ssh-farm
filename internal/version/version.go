// Package version pins the game's release version.
//
// The whole ssharcade fleet shares one scheme, <channel>.<major>.<minor>:
// the leading number is the release channel (1 = alpha, 2 = beta), the
// second is the major release, and the third is the minor patch/hotfix.
// internal/server wires this into the wish server's Version option, which
// becomes the literal SSH version-exchange banner — the arcade router's
// health-check prober reads it live from there on every probe cycle, so
// bumping this constant is the only place a release needs to change; no
// games.toml edit required. The help screen also renders this constant
// directly.
package version

// Version is the game's current release. Idle Farmer v2 is in beta.
const Version = "2.2.2"

// Channel is the human-readable release channel derived from Version's
// leading component.
const Channel = "beta"
