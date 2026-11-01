package game

import (
	"errors"

	"github.com/mynameis-nigel/ssh-farm/internal/sim"
)

var ErrDevModeDisabled = errors.New("dev tools are disabled")

// DevMode reports the immutable server setting, never a persisted save flag.
func (s *Session) DevMode() bool { return s.actor.mgr.devMode }

// DevApply serializes debug actions through the same actor as normal gameplay.
// The gate is enforced here as well as in the UI so production cannot mutate
// saves through debug methods even if a caller bypasses the overlay.
func (s *Session) DevApply(now int64, action string, value int64) (Snapshot, []string, error) {
	if !s.DevMode() {
		return Snapshot{}, nil, ErrDevModeDisabled
	}
	return s.intent(now, func(st *sim.State) error {
		return sim.DevApply(st, s.actor.content(), action, value, now)
	})
}
