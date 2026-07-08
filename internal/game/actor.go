// Package game owns the runtime save lifecycle: a registry of active saves,
// one actor goroutine per active save, autosave, the takeover/refuse
// concurrency policy, and the flush that runs on disconnect and shutdown.
//
// The actor is the single writer of its save: the engine (internal/sim) runs
// only on the actor goroutine, and every UI interaction is a closure mailed
// to that goroutine. No save is ever mutated by two sessions at once.
package game

import (
	"context"
	"time"

	"github.com/mynameis-nigel/ssh-farm/internal/content"
	"github.com/mynameis-nigel/ssh-farm/internal/sim"
)

type saveKey struct {
	fingerprint string
	slot        string
}

// actor owns one active save. All fields below the mailbox are touched only
// from the run loop goroutine.
type actor struct {
	key  saveKey
	mgr  *Manager
	reqs chan func()
	stop chan struct{} // closed by the manager to end the loop
	done chan struct{} // closed by the loop on exit, after the final flush

	state   *sim.State
	dirty   bool
	session *Session
}

func newActor(m *Manager, key saveKey, state *sim.State) *actor {
	a := &actor{
		key:   key,
		mgr:   m,
		reqs:  make(chan func()),
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
		state: state,
	}
	go a.run()
	return a
}

// do runs fn on the actor goroutine and waits for it. It returns false if
// the actor has already stopped (fn did not run).
func (a *actor) do(fn func()) bool {
	wrapped := make(chan struct{})
	select {
	case a.reqs <- func() { fn(); close(wrapped) }:
		<-wrapped
		return true
	case <-a.done:
		return false
	}
}

func (a *actor) run() {
	defer close(a.done)
	ticker := time.NewTicker(a.mgr.autosave)
	defer ticker.Stop()

	for {
		select {
		case fn := <-a.reqs:
			fn()
		case <-ticker.C:
			a.persist("autosave")
		case <-a.stop:
			a.persist("final flush")
			return
		}
	}
}

// persist writes the save if it has changed since the last write. Failures
// are logged and the state stays dirty so the next tick retries.
func (a *actor) persist(reason string) {
	if !a.dirty {
		return
	}
	payload, err := a.state.Encode()
	if err != nil {
		a.mgr.logger.Error("encode save failed", "slot", a.key.slot, "error", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = a.mgr.store.PersistSave(ctx, a.key.fingerprint, a.key.slot,
		payload, a.state.Version, a.state.UpdatedAt, a.state.Coins, a.state.LifetimeEarnings, a.state.Rebirths, a.state.FarmName)
	if err != nil {
		a.mgr.logger.Error("persist save failed",
			"reason", reason, "fingerprint", a.key.fingerprint, "slot", a.key.slot, "error", err)
		return
	}
	a.dirty = false
	a.mgr.logger.Debug("save persisted", "reason", reason, "slot", a.key.slot)
}

// content is immutable shared config; safe to read from any goroutine.
func (a *actor) content() *content.Content { return a.mgr.content }
