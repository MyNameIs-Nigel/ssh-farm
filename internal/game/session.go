package game

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/mynameis-nigel/ssh-farm/internal/moderation"
	"github.com/mynameis-nigel/ssh-farm/internal/sim"
	"github.com/mynameis-nigel/ssh-farm/internal/store"
)

// ErrSessionClosed is returned when an intent arrives after the session's
// actor has stopped (the player was kicked or the server is shutting down).
var ErrSessionClosed = errors.New("session is closed")

// Rename sentinel errors (gameplay/03). ErrNameRateLimited and
// ErrNameDenied deliberately have generic, distinct-but-non-revealing
// messages: rate limiting is an expected, explainable UX ("try again in a
// bit"), while ErrNameDenied covers both an invalid name and a
// denylist-denied one identically — see internal/moderation's package doc
// for why those two must never be distinguishable.
var (
	ErrNameLocked      = errors.New("this farm's name has been locked by an operator")
	ErrNameRateLimited = errors.New("please wait a moment before renaming again")
	ErrNameDenied      = errors.New("that name isn't available")
)

// Session is one terminal's handle onto a save actor.
type Session struct {
	id       uint64
	actor    *actor
	kicked   chan string
	kickFn   func(reason string)
	kickOnce sync.Once
}

// Snapshot is a point-in-time deep copy of the save for rendering.
type Snapshot struct {
	State *sim.State
	Now   int64
}

// Kicked yields the takeover/shutdown notice for this session.
func (s *Session) Kicked() <-chan string { return s.kicked }

func (s *Session) deliverKick(reason string) {
	s.kickOnce.Do(func() {
		if reason != "" {
			s.kicked <- reason
			if s.kickFn != nil {
				s.kickFn(reason)
			}
		}
		close(s.kicked)
	})
}

// Detach persists the save and releases this session.
func (s *Session) Detach() {
	s.actor.mgr.detach(s)
	s.deliverKick("")
}

// Advance simulates up to now and returns a fresh snapshot plus whatever happened.
func (s *Session) Advance(now int64) (Snapshot, sim.Events, error) {
	var snap Snapshot
	var ev sim.Events
	ok := s.actor.do(func() {
		ev = sim.Advance(s.actor.state, s.actor.content(), now)
		if ev.Elapsed > 0 {
			s.actor.dirty = true
		}
		snap = Snapshot{State: s.actor.state.Clone(), Now: now}
	})
	if !ok {
		return Snapshot{}, sim.Events{}, ErrSessionClosed
	}
	return snap, ev, nil
}

func (s *Session) intent(now int64, apply func(st *sim.State) error) (Snapshot, []string, error) {
	var snap Snapshot
	var newly []string
	var actErr error
	ok := s.actor.do(func() {
		c := s.actor.content()
		ev := sim.Advance(s.actor.state, c, now)
		if ev.Elapsed > 0 {
			s.actor.dirty = true
		}
		actErr = apply(s.actor.state)
		if actErr == nil {
			s.actor.dirty = true
			newly = s.actor.state.CheckAchievements(c, now)
		}
		snap = Snapshot{State: s.actor.state.Clone(), Now: now}
	})
	if !ok {
		return Snapshot{}, nil, ErrSessionClosed
	}
	return snap, newly, actErr
}

// Plant plants cropID on the given plot.
func (s *Session) Plant(now int64, plot int, cropID string) (Snapshot, []string, error) {
	return s.intent(now, func(st *sim.State) error {
		return sim.Plant(st, s.actor.content(), plot, cropID, now)
	})
}

// Harvest gathers the given plot.
func (s *Session) Harvest(now int64, plot int) (sim.HarvestResult, Snapshot, []string, error) {
	var res sim.HarvestResult
	snap, newly, err := s.intent(now, func(st *sim.State) error {
		var herr error
		res, herr = sim.Harvest(st, s.actor.content(), plot, now)
		return herr
	})
	return res, snap, newly, err
}

// BuyPlot purchases the next plot, returning what it cost.
func (s *Session) BuyPlot(now int64) (int64, Snapshot, []string, error) {
	var cost int64
	snap, newly, err := s.intent(now, func(st *sim.State) error {
		var berr error
		cost, berr = sim.BuyPlot(st, s.actor.content())
		return berr
	})
	return cost, snap, newly, err
}

// BuyZone purchases a zone expansion.
func (s *Session) BuyZone(now int64, id string) (Snapshot, []string, error) {
	return s.intent(now, func(st *sim.State) error {
		return sim.BuyZone(st, s.actor.content(), id)
	})
}

// BuyMultiplier purchases a run-scoped market multiplier.
func (s *Session) BuyMultiplier(now int64, id string) (Snapshot, []string, error) {
	return s.intent(now, func(st *sim.State) error {
		return sim.BuyMultiplier(st, s.actor.content(), id)
	})
}

// BuySeedUpgrade purchases a Hardier Strain level.
func (s *Session) BuySeedUpgrade(now int64, id string) (Snapshot, []string, error) {
	return s.intent(now, func(st *sim.State) error {
		return sim.BuySeedUpgrade(st, s.actor.content(), id)
	})
}

// UpgradePlotAuto buys auto-harvest or auto-sow for a plot.
func (s *Session) UpgradePlotAuto(now int64, plot int, kind string) (Snapshot, []string, error) {
	return s.intent(now, func(st *sim.State) error {
		return sim.UpgradePlotAuto(st, s.actor.content(), plot, kind)
	})
}

// SetAutoSowCrop changes the crop an auto-sow plot replants.
func (s *Session) SetAutoSowCrop(now int64, plot int, cropID string) (Snapshot, []string, error) {
	return s.intent(now, func(st *sim.State) error {
		return sim.SetAutoSowCrop(st, s.actor.content(), plot, cropID)
	})
}

// BuyUpgrade spends Starseeds on a permanent upgrade.
func (s *Session) BuyUpgrade(now int64, id string) (Snapshot, []string, error) {
	return s.intent(now, func(st *sim.State) error {
		return sim.BuyUpgrade(st, s.actor.content(), id)
	})
}

// RedeemGift opens the pending parcel.
func (s *Session) RedeemGift(now int64) (sim.GiftResult, Snapshot, []string, error) {
	var res sim.GiftResult
	snap, newly, err := s.intent(now, func(st *sim.State) error {
		var gerr error
		res, gerr = sim.RedeemGift(st, s.actor.content())
		return gerr
	})
	return res, snap, newly, err
}

// ReplantAll fills every empty plot with its remembered crop.
func (s *Session) ReplantAll(now int64) (sim.ReplantResult, Snapshot, []string, error) {
	var res sim.ReplantResult
	snap, newly, err := s.intent(now, func(st *sim.State) error {
		res = sim.ReplantAll(st, s.actor.content(), now)
		return nil
	})
	return res, snap, newly, err
}

// BuyScarecrow purchases the run-scoped scarecrow.
func (s *Session) BuyScarecrow(now int64) (int64, Snapshot, []string, error) {
	var cost int64
	snap, newly, err := s.intent(now, func(st *sim.State) error {
		var berr error
		cost, berr = sim.BuyScarecrow(st, s.actor.content())
		return berr
	})
	return cost, snap, newly, err
}

// AckReplantWarning records that the player has seen the replant-all warning.
func (s *Session) AckReplantWarning(now int64) (Snapshot, error) {
	snap, _, err := s.intent(now, func(st *sim.State) error {
		st.ReplantWarned = true
		return nil
	})
	return snap, err
}

// SetNews toggles the Daily Furrow headline banner for this save.
func (s *Session) SetNews(now int64, enabled bool) (Snapshot, error) {
	snap, _, err := s.intent(now, func(st *sim.State) error {
		st.NewsEnabled = enabled
		return nil
	})
	return snap, err
}

// SetCritters toggles cosmetic critter visits for this save.
func (s *Session) SetCritters(now int64, enabled bool) (Snapshot, error) {
	snap, _, err := s.intent(now, func(st *sim.State) error {
		st.CrittersEnabled = enabled
		return nil
	})
	return snap, err
}

// SetThemeSolid pins the background colour for this save.
func (s *Session) SetThemeSolid(now int64, enabled bool) (Snapshot, error) {
	snap, _, err := s.intent(now, func(st *sim.State) error {
		sim.SetThemeSolid(st, enabled)
		return nil
	})
	return snap, err
}

// SetSeasonal toggles the Halloween/Christmas look for this save. Seasonal
// seeds stay gated by the calendar either way.
func (s *Session) SetSeasonal(now int64, enabled bool) (Snapshot, error) {
	snap, _, err := s.intent(now, func(st *sim.State) error {
		sim.SetSeasonal(st, enabled)
		return nil
	})
	return snap, err
}

// ShooCritter removes a critter from a plot.
func (s *Session) ShooCritter(now int64, plot int) (int64, Snapshot, []string, error) {
	var reward int64
	snap, newly, err := s.intent(now, func(st *sim.State) error {
		var serr error
		reward, serr = sim.ShooCritter(st, s.actor.content(), plot)
		return serr
	})
	return reward, snap, newly, err
}

// SetFarmName sets the farm's display name directly, with no rate limiting
// or content moderation. It exists for callers that have already applied
// gameplay/03's checks themselves (or, as here, none — see RenameFarm,
// which every player-facing entry point should call instead).
func (s *Session) SetFarmName(now int64, name string) (Snapshot, error) {
	snap, _, err := s.intent(now, func(st *sim.State) error {
		return sim.SetFarmName(st, name)
	})
	return snap, err
}

// RenameFarm is gameplay/03's player-facing rename action: the one path
// that should ever turn raw player input into a stored farm name. In
// order:
//
//  1. Store.GateRename checks the operator lock and atomically consumes
//     one unit of the rename rate-limit budget (a 5-attempt burst refilled
//     60s after the last allowed attempt, 60/day) — before
//     moderation runs, so a rate-limited request never even reaches the
//     denylist, and the budget is spent whether or not the name below
//     turns out to be denied (retrying denied names buys no extra
//     probes).
//  2. moderation.Filter validates and content-checks the name.
//  3. sim.SetFarmName applies it.
//
// Every rejection reason maps to its own sentinel error, but ErrNameDenied
// covers both "invalid" and "denylist-denied" identically by design (see
// internal/moderation) — callers must not show a different message for
// the two.
func (s *Session) RenameFarm(ctx context.Context, now int64, rawName string) (Snapshot, error) {
	gate, err := s.actor.mgr.store.GateRename(ctx, s.actor.key.fingerprint, s.actor.key.slot, now)
	if err != nil {
		return Snapshot{}, fmt.Errorf("game: rename gate: %w", err)
	}
	switch gate {
	case store.RenameLocked:
		return Snapshot{}, ErrNameLocked
	case store.RenameRateLimited:
		return Snapshot{}, ErrNameRateLimited
	}

	filtered, denied := moderation.Filter(rawName)
	if denied {
		return Snapshot{}, ErrNameDenied
	}

	snap, _, err := s.intent(now, func(st *sim.State) error {
		return sim.SetFarmName(st, filtered)
	})
	return snap, err
}

// Rebirth resets the run for Starseeds.
func (s *Session) Rebirth(now int64) (int64, Snapshot, []string, error) {
	var gain int64
	snap, newly, err := s.intent(now, func(st *sim.State) error {
		var rerr error
		gain, rerr = sim.Rebirth(st, s.actor.content(), now)
		return rerr
	})
	return gain, snap, newly, err
}

// StartContract accepts the next contract in the fixed campaign order.
func (s *Session) StartContract(now int64, id sim.ContractID) (Snapshot, []string, error) {
	return s.intent(now, func(st *sim.State) error {
		return sim.StartContract(st, s.actor.content(), id, now)
	})
}

// AbandonContract discards the active attempt and returns to a fresh farm.
func (s *Session) AbandonContract(now int64) (Snapshot, []string, error) {
	return s.intent(now, func(st *sim.State) error {
		return sim.AbandonContract(st, s.actor.content(), now)
	})
}

// SetLeaderboardNameStyle selects one of the contract-earned cosmetics.
func (s *Session) SetLeaderboardNameStyle(now int64, style string) (Snapshot, error) {
	snap, _, err := s.intent(now, func(st *sim.State) error {
		return sim.SetLeaderboardNameStyle(st, style)
	})
	return snap, err
}

// SetFlavor toggles ambient discoveries for this save.
func (s *Session) SetFlavor(now int64, enabled bool) (Snapshot, error) {
	snap, _, err := s.intent(now, func(st *sim.State) error {
		sim.SetFlavor(st, enabled)
		return nil
	})
	return snap, err
}
