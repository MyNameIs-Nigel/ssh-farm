package game

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mynameis-nigel/ssh-farm/internal/content"
	"github.com/mynameis-nigel/ssh-farm/internal/identity"
	"github.com/mynameis-nigel/ssh-farm/internal/sim"
	"github.com/mynameis-nigel/ssh-farm/internal/store"
)

// Policy decides what happens when a save that is already open is opened
// again from a second session.
type Policy string

const (
	// PolicyTakeover attaches the new session and gently disconnects the
	// old one (the project's locked default).
	PolicyTakeover Policy = "takeover"
	// PolicyRefuse keeps the old session and turns the newcomer away.
	PolicyRefuse Policy = "refuse"
)

// ErrSaveBusy is returned under PolicyRefuse when the save is already open.
var ErrSaveBusy = errors.New("this farm is already open in another session")

// Manager is the in-memory registry of active saves. It is process-local by
// design: a restart clears any stale locks automatically.
type Manager struct {
	store    *store.Store
	content  *content.Content
	logger   *slog.Logger
	autosave time.Duration
	policy   Policy

	mu      sync.Mutex
	actors  map[saveKey]*actor
	closing bool // set by Shutdown; refuses new attaches during the flush
	nextID  atomic.Uint64
}

// ErrShuttingDown is returned by Attach once Shutdown has begun.
var ErrShuttingDown = errors.New("the farm is closing for maintenance")

// NewManager wires the save registry over the given store and content.
func NewManager(st *store.Store, c *content.Content, logger *slog.Logger, autosave time.Duration, policy Policy) *Manager {
	if autosave <= 0 {
		autosave = 30 * time.Second
	}
	if policy != PolicyRefuse {
		policy = PolicyTakeover
	}
	return &Manager{
		store:    st,
		content:  c,
		logger:   logger,
		autosave: autosave,
		policy:   policy,
		actors:   make(map[saveKey]*actor),
	}
}

// Content exposes the immutable game content for the UI layer.
func (m *Manager) Content() *content.Content { return m.content }

// AttachResult is what a successfully attached session starts from.
type AttachResult struct {
	Session *Session
	Away    sim.Events // offline catch-up outcome for the welcome-back summary
	Created bool       // true when this connect created the save (onboarding)
}

// Attach opens (loading or creating) the save for id and attaches a session
// to its actor, applying the concurrency policy if the save is already open.
// publicKey is the connecting key in authorized_keys format, stored for
// auditing. kick is invoked (once, from the actor goroutine) if this session
// is later taken over or the server shuts down.
func (m *Manager) Attach(ctx context.Context, id identity.SessionIdentity, publicKey string, now int64, kick func(reason string)) (AttachResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closing {
		return AttachResult{}, ErrShuttingDown
	}
	if err := m.store.TouchAccount(ctx, id.Fingerprint, publicKey, now); err != nil {
		return AttachResult{}, err
	}

	key := saveKey{fingerprint: id.Fingerprint, slot: id.Slot}
	a, ok := m.actors[key]
	created := false
	if !ok {
		row, isNew, err := m.store.LoadOrCreateSave(ctx, id.Fingerprint, id.Slot, now, func() (store.FreshSave, error) {
			state := sim.New(m.content, randomSeed(), now)
			payload, err := state.Encode()
			if err != nil {
				return store.FreshSave{}, err
			}
			return store.FreshSave{State: payload, Version: state.Version, Coins: state.Coins, FarmName: state.FarmName}, nil
		})
		if err != nil {
			return AttachResult{}, err
		}
		state, err := sim.DecodeState(row.State)
		if err != nil {
			return AttachResult{}, fmt.Errorf("load save %q: %w", id.Slot, err)
		}
		created = isNew
		a = newActor(m, key, state)
		m.actors[key] = a
	}

	sess := &Session{
		id:     m.nextID.Add(1),
		actor:  a,
		kicked: make(chan string, 1),
		kickFn: kick,
	}

	var refused bool
	var away sim.Events
	ok = a.do(func() {
		if a.session != nil {
			if m.policy == PolicyRefuse {
				refused = true
				return
			}
			a.session.deliverKick("Your farm was opened from another session, so this one is signing off.")
		}
		a.session = sess
		// Offline catch-up: advance from the save's last simulated moment
		// to now in one computation.
		away = sim.Advance(a.state, m.content, now)
		if away.Elapsed > 0 {
			a.dirty = true
		}
	})
	if !ok {
		// Cannot happen while we hold m.mu (actors only stop under the same
		// lock), but if it ever does, fail politely rather than wedge.
		delete(m.actors, key)
		return AttachResult{}, errors.New("save is closing, try again")
	}
	if refused {
		return AttachResult{}, ErrSaveBusy
	}

	m.logger.Info("session attached",
		"fingerprint", id.Fingerprint, "slot", id.Slot, "created", created)
	return AttachResult{Session: sess, Away: away, Created: created}, nil
}

// detach is called by Session.Detach: persist, release the session, and stop
// the actor if it has no session left.
func (m *Manager) detach(s *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()

	a := s.actor
	idle := false
	a.do(func() {
		if a.session == s {
			a.session = nil
			idle = true
		}
		a.persist("disconnect")
	})
	if idle {
		m.stopActorLocked(a)
	}
}

// stopActorLocked ends the actor loop (running its final flush) and removes
// it from the registry. Caller holds m.mu.
func (m *Manager) stopActorLocked(a *actor) {
	select {
	case <-a.done:
	default:
		close(a.stop)
		<-a.done
	}
	if m.actors[a.key] == a {
		delete(m.actors, a.key)
	}
}

// Shutdown gently disconnects every session and flushes every active save.
// It fulfills the Task 1 shutdown hook: Docker stops deliver SIGTERM, this
// runs, and no in-flight progress is lost across a redeploy.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	m.closing = true // the SSH listener is still up; refuse attaches mid-flush
	actors := make([]*actor, 0, len(m.actors))
	for _, a := range m.actors {
		actors = append(actors, a)
	}
	m.mu.Unlock()

	m.logger.Info("flushing active saves", "count", len(actors))
	flushed := make(chan struct{})
	go func() {
		defer close(flushed)
		for _, a := range actors {
			a.do(func() {
				if a.session != nil {
					a.session.deliverKick("The farm is closing for a moment of maintenance. Your progress is saved — come right back!")
					a.session = nil
				}
			})
			m.mu.Lock()
			m.stopActorLocked(a)
			m.mu.Unlock()
		}
	}()

	select {
	case <-flushed:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("game: shutdown flush interrupted: %w", ctx.Err())
	}
}

// ActiveSaves reports how many saves are currently open (for diagnostics).
func (m *Manager) ActiveSaves() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.actors)
}

func randomSeed() uint64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Extremely unlikely; fall back to a time-derived seed. Determinism
		// only matters after creation, not for the seed itself.
		return uint64(time.Now().UnixNano())
	}
	return binary.LittleEndian.Uint64(b[:])
}
