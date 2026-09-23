// Package leaderboard ranks every farm by lifetime coin earnings and
// answers the three questions the board UI needs: where am I ("#13/261"),
// who's on top (the top list), and who's near me (a window around my
// rank). See docs/gameplay/02-leaderboard-engine.md.
//
// All reads come from the store's denormalized lifetime_earnings/rebirths/
// farm_name columns — never from decoding the state blob, never from sim
// state (see internal/store's package doc: the actor writes those columns
// in the same transaction as the blob, so they can never disagree with it,
// just lag it by at most one autosave interval).
package leaderboard

// SaveRef identifies one save for lookup in a Board — the same
// (fingerprint, slot) pair used everywhere else in the fleet.
type SaveRef struct {
	Fingerprint string
	Slot        string
}

// Row is one entry on the board.
type Row struct {
	// Rank is competition ranking (1224): ties share a rank, and the next
	// distinct value skips to reflect the tie's size.
	Rank int
	// DisplayName is the moderated name to show, already passed through
	// gameplay/03's render-time re-check: a name that has since become
	// denylisted is replaced with a deterministically-generated one. Empty
	// means the farm was simply never renamed — the UI shows its own
	// "FARM ·suffix" placeholder for that case (not a moderation outcome).
	DisplayName string
	// Suffix is the 5-character fingerprint suffix used to disambiguate
	// same-named farms and the generic unnamed FARM fallback.
	Suffix             string
	ShowSuffix         bool
	ContractsCompleted int
	NameStyle          string
	// Coins is lifetime coin earnings, not the save's current spendable
	// balance (rebirth resets the balance but never this figure) — the
	// metric the board ranks and displays.
	Coins int64
	// Rebirths is the save's permanent rebirth count, shown alongside Coins.
	Rebirths int64
	// IsYou marks the row matching the SaveRef passed to Get.
	IsYou bool
}

// Board is the answer to one Get call.
type Board struct {
	// You is nil when the requested save is unranked (see gameplay/02's
	// floor) or not present in the current snapshot at all.
	You *Row
	// Total is the number of saves counted on the board — i.e. after the
	// activity window and coin floor are applied, not every save that
	// exists.
	Total int
	// Top holds up to the top 10 rows.
	Top []Row
	// Window holds up to 3 rows on each side of You when You.Rank places
	// outside Top, deduplicated against Top (never repeats a row already
	// shown there). Empty whenever You is nil or already within Top.
	Window []Row
	// AsOf is the unix time the underlying snapshot was built — the
	// "updated ~30s ago" staleness hint the UI can show.
	AsOf int64
}
