package game

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mynameis-nigel/ssh-farm/internal/identity"
)

func TestGuestLifecycleNeverUsesStore(t *testing.T) {
	m, _ := testManager(t, PolicyTakeover, time.Millisecond)
	m.store = nil // Any accidental account, save, rename or flush access panics.
	id := identity.SessionIdentity{Kind: identity.Guest, GuestID: strings.Repeat("a", 32)}
	a, err := m.Attach(t.Context(), id, "", 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	bID := id
	bID.GuestID = strings.Repeat("b", 32)
	b, err := m.Attach(t.Context(), bID, "", 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = a.Session.Plant(100, 0, "turnip"); err != nil {
		t.Fatal(err)
	}
	bs, _, err := b.Session.Advance(101)
	if err != nil || bs.State.Plots[0].Crop != "" {
		t.Fatal("guest states merged", err)
	}
	if _, err = a.Session.RenameFarm(t.Context(), 101, "NEW NAME"); !errors.Is(err, ErrGuestRequiresKey) {
		t.Fatal(err)
	}
	if _, err = a.Session.SetFarmName(101, "NEW NAME"); !errors.Is(err, ErrGuestRequiresKey) {
		t.Fatal(err)
	}
	if _, err = a.Session.SetLeaderboardNameStyle(101, "traditional"); !errors.Is(err, ErrGuestRequiresKey) {
		t.Fatal(err)
	}
	a.Session.actor.do(func() { a.Session.actor.persist("test autosave") })
	a.Session.Detach()
	b.Session.Detach()
	if m.ActiveSaves() != 0 {
		t.Fatal("guest actors retained")
	}
	fresh, err := m.Attach(t.Context(), id, "", 5000, nil)
	if err != nil {
		t.Fatal(err)
	}
	snap, _, err := fresh.Session.Advance(5000)
	if err != nil || snap.State.Plots[0].Crop != "" {
		t.Fatal("guest restored", err)
	}
	if err = m.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if m.ActiveSaves() != 0 {
		t.Fatal("shutdown retained guest")
	}
}

func TestInvalidIdentityCannotReachStorage(t *testing.T) {
	m, _ := testManager(t, PolicyTakeover, time.Hour)
	m.store = nil
	for _, id := range []identity.SessionIdentity{{}, {Kind: identity.Guest, GuestID: "short"}, {Kind: identity.Guest, GuestID: strings.Repeat("a", 32), Fingerprint: "fake"}, {Kind: identity.Keyed, GuestID: strings.Repeat("a", 32), Fingerprint: "key", Slot: "farm"}} {
		if _, err := m.Attach(t.Context(), id, "", 100, nil); err == nil {
			t.Fatalf("accepted %+v", id)
		}
	}
}

func TestTakenOverSessionCannotMutateOrRead(t *testing.T) {
	m, _ := testManager(t, PolicyTakeover, time.Hour)
	id := ident("SHA256:ownership", "farm")
	old := mustAttach(t, m, id, 100)
	current := mustAttach(t, m, id, 101)
	defer current.Session.Detach()
	defer old.Session.Detach()
	if _, _, err := old.Session.Plant(102, 0, "turnip"); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("replaced session mutated farm: %v", err)
	}
	if _, _, err := old.Session.Advance(103); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("replaced session read farm: %v", err)
	}
}
