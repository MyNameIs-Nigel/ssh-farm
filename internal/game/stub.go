package game

import (
	"context"
	"errors"
	"sync"

	"github.com/mynameis-nigel/ssh-farm/internal/identity"
)

// ErrSaveBusy is returned under PolicyRefuse when the save is already open.
var ErrSaveBusy = errors.New("this farm is already open in another session")

// ErrShuttingDown is returned by Attach once Shutdown has begun.
var ErrShuttingDown = errors.New("the farm is closing for maintenance")

// Policy decides what happens when a save that is already open is opened again.
type Policy string

const (
	PolicyTakeover Policy = "takeover"
	PolicyRefuse   Policy = "refuse"
)

// AttachResult is what a successfully attached session starts from.
type AttachResult struct {
	Session *Session
	Created bool
}

// Session is one terminal's handle onto a save. The stub implementation is
// replaced when framework/02 ports the real game manager.
type Session struct {
	detached chan struct{}
}

// Detach releases the session handle.
func (s *Session) Detach() {
	if s == nil {
		return
	}
	select {
	case <-s.detached:
	default:
		close(s.detached)
	}
}

// StubManager is a temporary in-memory attach registry for framework/01.
type StubManager struct {
	policy   Policy
	mu       sync.Mutex
	open     map[saveKey]int
	closing  bool
	sessions []*Session
}

type saveKey struct {
	fingerprint string
	slot        string
}

// NewStubManager creates a placeholder save manager until framework/02 lands.
func NewStubManager(policy Policy) *StubManager {
	if policy != PolicyRefuse {
		policy = PolicyTakeover
	}
	return &StubManager{
		policy: policy,
		open:   make(map[saveKey]int),
	}
}

// Attach opens a stub session for the resolved identity.
func (m *StubManager) Attach(_ context.Context, id identity.SessionIdentity, _ string, _ int64, _ func(reason string)) (AttachResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closing {
		return AttachResult{}, ErrShuttingDown
	}

	key := saveKey{fingerprint: id.Fingerprint, slot: id.Slot}
	if m.policy == PolicyRefuse && m.open[key] > 0 {
		return AttachResult{}, ErrSaveBusy
	}

	sess := &Session{detached: make(chan struct{})}
	m.sessions = append(m.sessions, sess)
	m.open[key]++
	return AttachResult{Session: sess}, nil
}

func (m *StubManager) release(id identity.SessionIdentity, s *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := saveKey{fingerprint: id.Fingerprint, slot: id.Slot}
	if m.open[key] > 0 {
		m.open[key]--
		if m.open[key] == 0 {
			delete(m.open, key)
		}
	}
	s.Detach()
}

// Shutdown marks the manager closed and detaches every stub session.
func (m *StubManager) Shutdown(_ context.Context) error {
	m.mu.Lock()
	m.closing = true
	sessions := append([]*Session(nil), m.sessions...)
	m.mu.Unlock()

	for _, s := range sessions {
		s.Detach()
	}
	return nil
}

// DetachFor removes a session from the stub manager's open count.
func (m *StubManager) DetachFor(id identity.SessionIdentity, s *Session) {
	m.release(id, s)
}
