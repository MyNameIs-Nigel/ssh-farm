// Package moderation validates, normalizes, and filters farm names shown on
// the public leaderboard: permissive enough that "SUNNY HOLLOW" and
// "XX_POTATO_LORD_XX" sail through, strict enough that slurs and their
// leetspeak mutations don't. See docs/gameplay/03-farm-names-and-moderation.md
// for the full design and docs/runbooks/moderation.md for operating it.
//
// Every check in this package is a pure function over strings: no I/O, no
// clocks, no per-save state. That keeps it trivially safe to call from
// every enforcement point that needs it — the write-time rename action
// (internal/game), the one-time v1 import (cmd/ssh-farm), and the
// leaderboard's render-time re-check (internal/leaderboard) — without this
// package depending on any of them. Abuse-friction rate limiting (the 4th
// piece of gameplay/03's design) is deliberately NOT here: it needs
// per-save counters that must survive process restarts, which live in
// internal/store instead (see Store.GateRename) so internal/sim's diff
// from v1 stays frozen to the two accessors gameplay/01 added.
package moderation

// Check reports whether name — already validated and normalized by
// Validate — is free of denylisted content. It is exported separately from
// Filter so callers that already have a normalized name (the leaderboard's
// render-time re-check) can skip re-running Validate.
func Check(name string) bool {
	folded, stripped, collapsed := candidates(name)
	toks := tokens(folded)
	return !loaded().blocks(stripped, collapsed, toks)
}

// Filter is the single entry point for turning arbitrary player input into
// either a name safe to store, or a rejection. It is called at all three
// gameplay/03 enforcement points:
//
//  1. Write — the in-game rename action (internal/game.Session.RenameFarm):
//     locked=true means refuse the whole attempt with a generic message and
//     leave the farm's current name untouched.
//  2. Import — cmd/ssh-farm's import-v1: locked=true means store
//     Generate(fingerprint) instead of the v1 name (see that package).
//  3. Render — internal/leaderboard's snapshot build: locked=true means
//     display Generate(fingerprint) instead of the stored name, without
//     touching storage — a denylist update thus retroactively masks a name
//     that was fine when it was written.
//
// An empty input returns ("", false): "no name" is not a violation, it's
// the default zero value a name should never actually be. Everything else
// that fails Validate's shape rules, or Check's content rules, returns
// ("", true) — the two causes are intentionally indistinguishable to the
// caller (see docs/gameplay/03 §"no oracle").
func Filter(name string) (out string, locked bool) {
	if name == "" {
		return "", false
	}
	// Dev mode is running on the placeholder fixture, which blocks two invented
	// tokens and nothing else. Accepting player-set names against it would look
	// like moderation while providing none, so refuse them all and let the
	// caller fall back to Generate — the same path a denied name already takes.
	if devMode.Load() {
		return "", true
	}
	norm, ok := Validate(name)
	if !ok {
		return "", true
	}
	if !Check(norm) {
		return "", true
	}
	return norm, false
}
