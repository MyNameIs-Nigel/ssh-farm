// Package moderation validates and normalizes farm names shown on the
// leaderboard.
//
// This is a placeholder for gameplay/03 (name validation, normalization
// pipeline, tiered denylist, generated-name fallback). It exists now purely
// as the seam framework/02's import-v1 calls, per
// docs/framework/02-store-durability-and-v1-import.md: "Name filtering
// rules → gameplay/03 (import calls its exported filter)." gameplay/03
// replaces the body of Filter with the real pipeline and keeps this exact
// signature so its callers (import-v1, and later the in-game SetFarmName
// action wrapper) need no changes.
//
// Until gameplay/03 lands, Filter only enforces the length limit
// sim.SetFarmName already applies at write time — it never denies a
// currently-valid v1 name for content reasons. A future denylist hit locks
// the name and clears it to empty (an anonymous farm) rather than inventing
// a generated name, since the generated-name word lists are gameplay/03's
// deliverable too.
package moderation

import "unicode/utf8"

// MaxNameLen mirrors sim.SetFarmName's limit so import-v1 and any other
// caller can validate before writing without importing internal/sim.
const MaxNameLen = 24

// Filter validates name for storage, returning the name to keep and whether
// it was locked (denied, in which case out is the safe replacement — today
// always empty). A locked name is re-checked at render time once
// gameplay/03's real denylist lands, so a later denylist update
// retroactively masks names this placeholder let through.
func Filter(name string) (out string, locked bool) {
	if name == "" {
		return "", false
	}
	if utf8.RuneCountInString(name) > MaxNameLen {
		return "", true
	}
	return name, false
}
