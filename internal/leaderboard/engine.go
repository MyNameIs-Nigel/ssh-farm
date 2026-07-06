package leaderboard

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mynameis-nigel/ssh-farm/internal/moderation"
	"github.com/mynameis-nigel/ssh-farm/internal/store"
)

// topSize is the number of leading rows always returned in Board.Top.
const topSize = 10

// windowRadius is how many rows on each side of You are included in
// Board.Window, before deduping against Top.
const windowRadius = 3

// Source is the read side the engine needs from storage; *store.Store
// satisfies it via Store.LeaderboardSnapshot. Kept as an interface so
// tests can supply canned rows (and inject artificial latency, to exercise
// the cache's singleflight behavior) without a real database.
type Source interface {
	LeaderboardSnapshot(ctx context.Context) ([]store.LeaderboardRow, error)
}

type rankedRow struct {
	store.LeaderboardRow
	Rank int
}

// Engine answers gameplay/02's three questions ("where am I", "who's on
// top", "who's near me") from one in-process, periodically-rebuilt
// snapshot of every save's denormalized coins/farm_name columns. It never
// reads the state blob or touches sim.
//
// Scaling note (see docs/gameplay/02-leaderboard-engine.md "Caching"): at
// thousands of saves the full snapshot is a few hundred KB, trivial to
// hold in memory and rebuild wholesale on a timer. Past roughly 100k
// saves, switch to querying per Get with idx_saves_coins instead — the
// Get/Board API here doesn't need to change, only rebuild's
// implementation.
type Engine struct {
	src            Source
	ttl            time.Duration
	activityWindow time.Duration // <=0 disables the activity filter
	minCoins       int64
	now            func() time.Time

	mu       sync.Mutex
	ranked   []rankedRow // last successful build; nil until the first one
	builtAt  time.Time
	building *buildState // non-nil while a rebuild is in flight
}

// buildState is the outcome of one rebuild, shared with every Get that was
// waiting on it when it started. It is deliberately separate from the
// engine's ranked/builtAt fields: a failed rebuild must not evict the last
// known-good snapshot (callers keep serving slightly-stale data and can
// retry), but it must still surface the error to whoever specifically
// triggered or was waiting on that attempt.
type buildState struct {
	done chan struct{}
	rows []rankedRow
	at   time.Time
	err  error
}

// New creates a leaderboard engine reading from src. ttl bounds how often
// the snapshot is rebuilt (FARM_LEADERBOARD_TTL); activityWindow and
// minCoins are the population filters from FARM_LEADERBOARD_ACTIVITY_DAYS
// and FARM_LEADERBOARD_MIN_COINS (see internal/config). now is injected
// clock so tests can control TTL expiry deterministically; production
// callers pass time.Now.
func New(src Source, ttl, activityWindow time.Duration, minCoins int64, now func() time.Time) *Engine {
	return &Engine{
		src:            src,
		ttl:            ttl,
		activityWindow: activityWindow,
		minCoins:       minCoins,
		now:            now,
	}
}

// Get answers the board as of the current (possibly cached) snapshot. you
// identifies which row, if any, is "You" — pass a zero SaveRef if there is
// none (You will simply come back nil).
func (e *Engine) Get(ctx context.Context, you SaveRef) (Board, error) {
	rows, asOf, err := e.snapshot(ctx)
	if err != nil {
		return Board{}, err
	}
	return buildBoard(rows, asOf, you), nil
}

// snapshot returns the current ranked rows, rebuilding at most once every
// ttl. Concurrent callers during a rebuild share the single in-flight
// database read (singleflight) rather than each issuing their own.
func (e *Engine) snapshot(ctx context.Context) ([]rankedRow, int64, error) {
	e.mu.Lock()
	if e.building == nil && e.ranked != nil && e.now().Sub(e.builtAt) < e.ttl {
		rows, asOf := e.ranked, e.builtAt.Unix()
		e.mu.Unlock()
		return rows, asOf, nil
	}
	if b := e.building; b != nil {
		e.mu.Unlock()
		<-b.done
		return b.rows, b.at.Unix(), b.err
	}

	b := &buildState{done: make(chan struct{})}
	e.building = b
	e.mu.Unlock()

	rows, err := e.rebuild(ctx)

	e.mu.Lock()
	at := e.now()
	if err == nil {
		e.ranked, e.builtAt = rows, at
	}
	e.building = nil
	e.mu.Unlock()

	b.rows, b.at, b.err = rows, at, err
	close(b.done)

	return rows, at.Unix(), err
}

// rebuild reads every save's board columns and returns them ranked,
// already restricted to gameplay/02's population filters (activity
// window + coin floor). Input rows are assumed sorted
// coins DESC, last_active ASC, fingerprint ASC (Store.LeaderboardSnapshot's
// contract) — filtering a sorted slice preserves that order, so ranking is
// a single linear pass.
func (e *Engine) rebuild(ctx context.Context) ([]rankedRow, error) {
	rows, err := e.src.LeaderboardSnapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("leaderboard: rebuild: %w", err)
	}

	now := e.now().Unix()
	var cutoff int64
	filterActivity := e.activityWindow > 0
	if filterActivity {
		cutoff = now - int64(e.activityWindow/time.Second)
	}

	filtered := make([]store.LeaderboardRow, 0, len(rows))
	for _, r := range rows {
		if r.Coins < e.minCoins {
			continue
		}
		if filterActivity && r.UpdatedAt <= cutoff {
			continue
		}
		filtered = append(filtered, r)
	}

	ranked := make([]rankedRow, len(filtered))
	rank := 1
	for i, r := range filtered {
		// Standard "1224" competition ranking: a strictly-lower coin
		// balance than the row immediately before starts a new rank at
		// this position; a tie inherits the same rank (skipping the
		// intervening values entirely, since nothing has that rank).
		if i > 0 && r.Coins < filtered[i-1].Coins {
			rank = i + 1
		}
		ranked[i] = rankedRow{LeaderboardRow: r, Rank: rank}
	}
	return ranked, nil
}

// buildBoard turns a ranked, filtered snapshot into the Board for one
// viewer. rows must already be in rank order (ascending rank / index).
func buildBoard(rows []rankedRow, asOf int64, you SaveRef) Board {
	board := Board{Total: len(rows), AsOf: asOf}

	topN := len(rows)
	if topN > topSize {
		topN = topSize
	}
	board.Top = make([]Row, topN)
	youIdx := -1
	for i := 0; i < topN; i++ {
		board.Top[i] = toRow(rows[i], you)
		if isYou(rows[i], you) {
			youIdx = i
		}
	}
	if youIdx == -1 {
		for i := topN; i < len(rows); i++ {
			if isYou(rows[i], you) {
				youIdx = i
				break
			}
		}
	}
	if youIdx == -1 {
		return board
	}

	yourRow := toRow(rows[youIdx], you)
	board.You = &yourRow

	// Window only kicks in once You falls outside Top by rank (not merely
	// by index — a tie whose rank is exactly topSize but whose index sits
	// past it, e.g. three-way-tied 10th place, is fully described by
	// Board.You already and does not get a window; see
	// docs/gameplay/02-leaderboard-engine.md's acceptance criteria).
	if yourRow.Rank <= topSize {
		return board
	}

	lo, hi := youIdx-windowRadius, youIdx+windowRadius
	if lo < topN {
		lo = topN
	}
	if hi > len(rows)-1 {
		hi = len(rows) - 1
	}
	for i := lo; i <= hi; i++ {
		board.Window = append(board.Window, toRow(rows[i], you))
	}
	return board
}

func isYou(r rankedRow, you SaveRef) bool {
	return you.Fingerprint != "" && r.Fingerprint == you.Fingerprint && r.Slot == you.Slot
}

// toRow applies gameplay/03's render-time re-check (Filter's third
// enforcement point, see internal/moderation's doc comment): a name that
// was fine when stored but has since become denylisted is masked with a
// deterministically-generated name rather than shown or left blank, so a
// denylist update retroactively fixes the board without ever touching
// storage. A save that was simply never renamed keeps its empty
// DisplayName, which the UI (tui/02) renders as a "FARM ·suffix"
// placeholder — that is not a moderation case at all.
func toRow(r rankedRow, you SaveRef) Row {
	name, locked := moderation.Filter(r.FarmName)
	if locked {
		name = moderation.Generate(r.Fingerprint)
	}
	return Row{
		Rank:        r.Rank,
		DisplayName: name,
		Suffix:      moderation.Suffix(r.Fingerprint),
		Coins:       r.Coins,
		IsYou:       isYou(r, you),
	}
}
