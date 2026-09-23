package game

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mynameis-nigel/ssh-farm/internal/content"
	"github.com/mynameis-nigel/ssh-farm/internal/identity"
	applog "github.com/mynameis-nigel/ssh-farm/internal/log"
	"github.com/mynameis-nigel/ssh-farm/internal/sim"
	"github.com/mynameis-nigel/ssh-farm/internal/store"
)

func testManager(t *testing.T, policy Policy, autosave time.Duration) (*Manager, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "game.db")
	return reopenManager(t, path, policy, autosave), path
}

func reopenManager(t *testing.T, path string, policy Policy, autosave time.Duration) *Manager {
	t.Helper()
	st, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	c, err := content.Load("../sim/testdata")
	if err != nil {
		t.Fatal(err)
	}
	return NewManager(st, c, applog.New("error", "text"), autosave, policy)
}

func ident(fp, slot string) identity.SessionIdentity {
	return identity.SessionIdentity{Fingerprint: fp, Slot: slot}
}

func mustAttach(t *testing.T, m *Manager, id identity.SessionIdentity, now int64) AttachResult {
	t.Helper()
	res, err := m.Attach(context.Background(), id, "ssh-ed25519 TEST", now, nil)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestAttachCreatesThenReloadsSave(t *testing.T) {
	m, _ := testManager(t, PolicyTakeover, time.Hour)
	id := ident("SHA256:k1", "farm")

	res := mustAttach(t, m, id, 1000)
	if !res.Created {
		t.Fatal("first attach should create the save")
	}
	// Plant something, detach.
	if _, _, err := res.Session.Plant(1000, 0, "turnip"); err != nil {
		t.Fatal(err)
	}
	res.Session.Detach()
	if m.ActiveSaves() != 0 {
		t.Fatal("actor should stop after last detach")
	}

	// Reattach much later: save persisted, crop matured while away.
	res2 := mustAttach(t, m, id, 5000)
	if res2.Created {
		t.Fatal("second attach must load, not create")
	}
	if res2.Away.Matured["turnip"] != 1 {
		t.Fatalf("away summary = %+v, want turnip matured", res2.Away)
	}
	snap, _, err := res2.Session.Advance(5001)
	if err != nil {
		t.Fatal(err)
	}
	if snap.State.Plots[0].Crop != "turnip" {
		t.Fatal("planted crop lost across detach/attach")
	}
	res2.Session.Detach()
}

func TestSaveIsolationAcrossKeysAndSlots(t *testing.T) {
	m, _ := testManager(t, PolicyTakeover, time.Hour)

	a := mustAttach(t, m, ident("SHA256:keyA", "farm"), 100)
	b := mustAttach(t, m, ident("SHA256:keyB", "farm"), 100)
	a2 := mustAttach(t, m, ident("SHA256:keyA", "second"), 100)

	if _, _, err := a.Session.Plant(100, 0, "turnip"); err != nil {
		t.Fatal(err)
	}
	snapB, _, _ := b.Session.Advance(101)
	snapA2, _, _ := a2.Session.Advance(101)
	if snapB.State.Plots[0].Crop != "" || snapA2.State.Plots[0].Crop != "" {
		t.Fatal("planting on keyA/farm leaked into another save")
	}
	if m.ActiveSaves() != 3 {
		t.Fatalf("active saves = %d, want 3", m.ActiveSaves())
	}
	a.Session.Detach()
	b.Session.Detach()
	a2.Session.Detach()
}

func TestTakeoverKicksOlderSession(t *testing.T) {
	m, _ := testManager(t, PolicyTakeover, time.Hour)
	id := ident("SHA256:k1", "farm")

	first := mustAttach(t, m, id, 100)
	second := mustAttach(t, m, id, 200)

	select {
	case reason := <-first.Session.Kicked():
		if reason == "" {
			t.Fatal("kick reason should be friendly, not empty")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("older session was never kicked on takeover")
	}

	// The new session works; the old one's intents are refused once the
	// kicked session detaches and only the new session drives the actor.
	if _, _, err := second.Session.Plant(200, 0, "turnip"); err != nil {
		t.Fatal(err)
	}
	first.Session.Detach() // kicked session cleans up; must not stop the actor
	snap, _, err := second.Session.Advance(201)
	if err != nil {
		t.Fatalf("takeover session lost its actor: %v", err)
	}
	if snap.State.Plots[0].Crop != "turnip" {
		t.Fatal("state lost during takeover")
	}
	second.Session.Detach()
}

func TestRefusePolicyTurnsNewcomerAway(t *testing.T) {
	m, _ := testManager(t, PolicyRefuse, time.Hour)
	id := ident("SHA256:k1", "farm")

	first := mustAttach(t, m, id, 100)
	_, err := m.Attach(context.Background(), id, "k", 200, nil)
	if err != ErrSaveBusy {
		t.Fatalf("expected ErrSaveBusy, got %v", err)
	}
	// The original session is unaffected.
	if _, _, err := first.Session.Advance(201); err != nil {
		t.Fatal(err)
	}
	// Different slots under the same key are not restricted.
	other := mustAttach(t, m, ident("SHA256:k1", "barn"), 200)
	other.Session.Detach()
	first.Session.Detach()
}

func TestAutosavePersistsWithoutDetach(t *testing.T) {
	m, path := testManager(t, PolicyTakeover, 30*time.Millisecond)
	id := ident("SHA256:k1", "farm")

	res := mustAttach(t, m, id, 1000)
	if _, _, err := res.Session.Plant(1000, 0, "turnip"); err != nil {
		t.Fatal(err)
	}

	// Read the row directly (other connection) until autosave lands.
	st2, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		row, _, err := st2.LoadOrCreateSave(context.Background(), id.Fingerprint, id.Slot, 1, func() (store.FreshSave, error) {
			t.Fatal("save must already exist")
			return store.FreshSave{}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		state, err := sim.DecodeState(row.State)
		if err != nil {
			t.Fatal(err)
		}
		if state.Plots[0].Crop == "turnip" {
			break // autosave wrote the planted state
		}
		if time.Now().After(deadline) {
			t.Fatal("autosave never persisted the change")
		}
		time.Sleep(10 * time.Millisecond)
	}
	res.Session.Detach()
}

func TestShutdownFlushesAndKicksEveryone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flush.db")
	m := reopenManager(t, path, PolicyTakeover, time.Hour)

	id := ident("SHA256:k1", "farm")
	res := mustAttach(t, m, id, 1000)
	if _, _, err := res.Session.Plant(1000, 1, "turnip"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-res.Session.Kicked():
	default:
		t.Fatal("shutdown must notify connected sessions")
	}
	if _, _, err := res.Session.Advance(2000); err != ErrSessionClosed {
		t.Fatalf("expected ErrSessionClosed after shutdown, got %v", err)
	}
	// The SSH listener may still be up while the flush runs; a connection
	// landing in that window must be refused, not given an unflushed actor.
	if _, err := m.Attach(context.Background(), id, "k", 2000, nil); err != ErrShuttingDown {
		t.Fatalf("expected ErrShuttingDown for attach during shutdown, got %v", err)
	}

	// A fresh manager over the same database sees the flushed change —
	// this is the redeploy-loses-nothing guarantee.
	m2 := reopenManager(t, path, PolicyTakeover, time.Hour)
	res2 := mustAttach(t, m2, id, 3000)
	snap, _, err := res2.Session.Advance(3001)
	if err != nil {
		t.Fatal(err)
	}
	if snap.State.Plots[1].Crop != "turnip" {
		t.Fatal("mid-session change lost across shutdown — flush failed")
	}
	res2.Session.Detach()
}

func TestIntentsValidateAgainstAuthoritativeState(t *testing.T) {
	m, _ := testManager(t, PolicyTakeover, time.Hour)
	res := mustAttach(t, m, ident("SHA256:k1", "farm"), 1000)
	defer res.Session.Detach()

	// A hostile client could send any intent; the engine refuses them all.
	if _, _, err := res.Session.Plant(1000, 99, "turnip"); err != sim.ErrUnknownPlot {
		t.Fatalf("expected ErrUnknownPlot, got %v", err)
	}
	if _, _, _, err := res.Session.Harvest(1000, 0); err != sim.ErrPlotEmpty {
		t.Fatalf("expected ErrPlotEmpty, got %v", err)
	}
	if _, _, _, err := res.Session.Rebirth(1000); err != sim.ErrRebirthTooSoon {
		t.Fatalf("expected ErrRebirthTooSoon, got %v", err)
	}
	if _, _, err := res.Session.UpgradePlotAuto(1000, 0, "harvest"); err != sim.ErrCantAfford {
		t.Fatalf("expected ErrCantAfford for plot auto at 25 coins, got %v", err)
	}
	snap, _, _ := res.Session.Advance(1001)
	if snap.State.Coins != 25 {
		t.Fatal("refused intents must not change state")
	}
}

func TestContractSessionIntentsAndMetadataPersist(t *testing.T) {
	m, path := testManager(t, PolicyTakeover, time.Hour)
	id := ident("SHA256:contracts", "farm")
	res := mustAttach(t, m, id, 1000)

	// Arrange the permanent base-game completion prerequisite directly on
	// the actor; simulation tests cover the prerequisite calculation itself.
	if ok := res.Session.actor.do(func() {
		st := res.Session.actor.state
		st.ContractsUnlocked = true
		st.LifetimeEarnings = 777
		st.FarmName = "CONTRACT FARM"
	}); !ok {
		t.Fatal("actor stopped")
	}
	snap, _, err := res.Session.StartContract(1001, sim.ContractBareHands)
	if err != nil {
		t.Fatal(err)
	}
	if snap.State.ActiveContract != sim.ContractBareHands || snap.State.LifetimeEarnings != 777 {
		t.Fatalf("started contract state = %+v", snap.State)
	}
	if snap, err = res.Session.SetLeaderboardNameStyle(1002, sim.NameStyleLeaf); err != sim.ErrNameStyleLocked {
		t.Fatalf("locked style error = %v, want %v", err, sim.ErrNameStyleLocked)
	}
	if snap, _, err = res.Session.AbandonContract(1003); err != nil || snap.State.ActiveContract != "" {
		t.Fatalf("abandon result active=%q err=%v", snap.State.ActiveContract, err)
	}

	if ok := res.Session.actor.do(func() {
		res.Session.actor.state.ContractsCompleted = 2
	}); !ok {
		t.Fatal("actor stopped")
	}
	if _, err := res.Session.SetLeaderboardNameStyle(1004, sim.NameStyleLeaf); err != nil {
		t.Fatal(err)
	}
	res.Session.Detach()

	st, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rows, err := st.LeaderboardSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ContractsCompleted != 2 || rows[0].LeaderboardNameStyle != sim.NameStyleLeaf {
		t.Fatalf("persisted leaderboard contract metadata = %+v", rows)
	}
}

// A contract can finish inside the catch-up that precedes every call (a
// Bare Hands scarecrow bounty), so both the tick path and an intent must
// say so explicitly — exactly once.
func TestSnapshotsReportContractCompletedOnce(t *testing.T) {
	m, _ := testManager(t, PolicyTakeover, time.Hour)
	res := mustAttach(t, m, ident("SHA256:contract-done", "farm"), 1000)
	armBareHands := func() {
		t.Helper()
		if ok := res.Session.actor.do(func() {
			st := res.Session.actor.state
			st.ActiveContract = sim.ContractBareHands
			st.ContractEarnings = 99_999
			st.Scarecrow = true
			st.Plots[0] = sim.Plot{Critter: "crow"}
		}); !ok {
			t.Fatal("actor stopped")
		}
	}

	armBareHands()
	snap, _, err := res.Session.Advance(1001)
	if err != nil || snap.ContractCompleted != sim.ContractBareHands {
		t.Fatalf("tick snapshot ContractCompleted = %q, err = %v", snap.ContractCompleted, err)
	}
	if snap, _, _ = res.Session.Advance(1002); snap.ContractCompleted != "" {
		t.Fatalf("next tick re-reported %q", snap.ContractCompleted)
	}

	// The same crossing inside an intent's catch-up, on a no-op action.
	if ok := res.Session.actor.do(func() { res.Session.actor.state.ContractsCompleted = 0 }); !ok {
		t.Fatal("actor stopped")
	}
	armBareHands()
	if snap, err = res.Session.SetLeaderboardNameStyle(1003, sim.NameStyleTraditional); err != nil || snap.ContractCompleted != sim.ContractBareHands {
		t.Fatalf("intent snapshot ContractCompleted = %q, err = %v", snap.ContractCompleted, err)
	}
}

func TestRenameFarmAppliesModerationAndRateLimit(t *testing.T) {
	m, _ := testManager(t, PolicyTakeover, time.Hour)
	res := mustAttach(t, m, ident("SHA256:k1", "farm"), 1000)
	defer res.Session.Detach()

	snap, err := res.Session.RenameFarm(context.Background(), 1000, "Sunny Hollow")
	if err != nil {
		t.Fatalf("expected a clean name to be accepted, got %v", err)
	}
	if snap.State.FarmName != "SUNNY HOLLOW" {
		t.Fatalf("FarmName = %q, want %q", snap.State.FarmName, "SUNNY HOLLOW")
	}

	// A denylisted name inside the burst is refused generically — the burst
	// exists so that a player who trips the filter can immediately try
	// again — and the stored name is unchanged.
	_, err = res.Session.RenameFarm(context.Background(), 1001, "ZZMEANIE")
	if err != ErrNameDenied {
		t.Fatalf("expected ErrNameDenied, got %v", err)
	}
	snap, _, advErr := res.Session.Advance(1002)
	if advErr != nil {
		t.Fatal(advErr)
	}
	if snap.State.FarmName != "SUNNY HOLLOW" {
		t.Fatal("a denied attempt must not change the stored name")
	}

	// Spend the rest of the burst; a denied attempt still costs a unit, so
	// the two above already consumed two of it.
	for i := 2; i < store.RenameBurstLimit; i++ {
		if _, err := res.Session.RenameFarm(context.Background(), 1000+int64(i), "Northfield"); err != nil {
			t.Fatalf("attempt %d inside the burst: %v", i+1, err)
		}
	}

	// One past the burst is rate limited, and the name is left unchanged.
	_, err = res.Session.RenameFarm(context.Background(), 1000+int64(store.RenameBurstLimit), "Eastmarch")
	if err != ErrNameRateLimited {
		t.Fatalf("expected ErrNameRateLimited, got %v", err)
	}
	snap, _, advErr = res.Session.Advance(1000 + int64(store.RenameBurstLimit) + 1)
	if advErr != nil {
		t.Fatal(advErr)
	}
	if snap.State.FarmName != "NORTHFIELD" {
		t.Fatalf("a rate-limited attempt must not change the stored name, got %q", snap.State.FarmName)
	}
}

func TestRenameFarmRespectsOperatorLock(t *testing.T) {
	m, path := testManager(t, PolicyTakeover, time.Hour)
	id := ident("SHA256:locked", "farm")
	res := mustAttach(t, m, id, 1000)

	st2, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	if err := st2.LockName(context.Background(), id.Fingerprint, id.Slot, true); err != nil {
		t.Fatal(err)
	}

	_, err = res.Session.RenameFarm(context.Background(), 1000, "Sunny Hollow")
	if err != ErrNameLocked {
		t.Fatalf("expected ErrNameLocked, got %v", err)
	}
	res.Session.Detach()
}

func TestRenameFarmCounterSurvivesReconnect(t *testing.T) {
	m, path := testManager(t, PolicyTakeover, time.Hour)
	id := ident("SHA256:reconnect", "farm")

	res := mustAttach(t, m, id, 1000)
	// Spend the whole burst, so a reconnect cannot be mistaken for a player
	// who simply had allowance left.
	for i := 0; i < store.RenameBurstLimit; i++ {
		if _, err := res.Session.RenameFarm(context.Background(), 1000+int64(i), "Sunny Hollow"); err != nil {
			t.Fatalf("burst attempt %d: %v", i+1, err)
		}
	}
	res.Session.Detach()

	// Reopen against the same database (simulating a process restart) and
	// reconnect within the cooldown: the rate limit must still apply, so
	// reconnecting is not a way to buy a fresh burst.
	m2 := reopenManager(t, path, PolicyTakeover, time.Hour)
	at := int64(1000 + store.RenameBurstLimit)
	res2 := mustAttach(t, m2, id, at)
	defer res2.Session.Detach()
	_, err := res2.Session.RenameFarm(context.Background(), at, "Northfield")
	if err != ErrNameRateLimited {
		t.Fatalf("expected ErrNameRateLimited after reconnect within cooldown, got %v", err)
	}
}
