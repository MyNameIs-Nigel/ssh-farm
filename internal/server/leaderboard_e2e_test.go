package server

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/mynameis-nigel/ssh-farm/internal/config"
	"github.com/mynameis-nigel/ssh-farm/internal/identity"
	"github.com/mynameis-nigel/ssh-farm/internal/store"
)

// TestLeaderboardShowsNewRankAfterReconnect is tests/02's leaderboard
// end-to-end scenario: a scripted SSH session earns coins, disconnects
// (flushing to the store — see internal/game/actor.go's autosave/detach
// path, exercised here the same way TestSaveFlushMovesBoardAfterRebuild and
// TestProxiedOfflineCatchUp write through Store.PersistSave directly rather
// than driving the full sim through keystrokes, since the leaderboard
// couldn't tell the difference either way), then reconnects and opens the
// board screen (key "7", tui/02) to confirm the new rank is shown.
//
// A second, pre-seeded "leader" farm gives the scenario a rank to actually
// move across: a fresh farm starts with zero lifetime earnings (the board's
// ranked metric, gameplay/02 — distinct from its starting spendable
// balance), so it starts UNRANKED, below the coin floor — a fresh save has
// a starting balance but hasn't "earned" anything yet. "ranker" only enters
// the board (#1/2, ahead of the leader) once its lifetime earnings are
// bumped past the leader's seeded total.
func TestLeaderboardShowsNewRankAfterReconnect(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "farm.db")

	_, addr := testServer(t, func(c *config.Config) {
		c.DBPath = dbPath
		// A short TTL (rather than a fake clock — the board engine here
		// runs on the real time.Now, see testServer) lets the second
		// connection's board fetch observe a rebuild shortly after the
		// direct coin write below, without waiting out the 15s default.
		c.LeaderboardTTL = 20 * time.Millisecond
	})
	signer := testSigner(t)

	st, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.TouchAccount(context.Background(), "SHA256:leader", "leader-key", time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.LoadOrCreateSave(context.Background(), "SHA256:leader", "leader", time.Now().Unix(), func() (store.FreshSave, error) {
		return store.FreshSave{State: []byte("blob"), Version: 1, Coins: 1_000_000, LifetimeEarnings: 1_000_000, FarmName: "Leader"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()

	// First connection: attach (creating the "ranker" save) and open the
	// board. A fresh save has zero lifetime earnings, below the coin floor,
	// so it starts unranked regardless of its starting spendable balance.
	first := connectAndOpenBoard(t, addr, "ranker", signer)
	if !strings.Contains(first, "YOU: UNRANKED") {
		t.Fatalf("expected YOU: UNRANKED (zero lifetime earnings), got:\n%s", first)
	}

	// connectAndOpenBoard closing its client only tears down the local
	// socket; the server's own disconnect handling (Session.Detach ->
	// manager.detach -> actor.persist("disconnect")) runs asynchronously
	// relative to that. If it lands after the direct write just below, it
	// flushes the actor's stale (lower) in-memory coin balance right back
	// over it, silently undoing the write this test depends on. Usually
	// fast enough to lose this race under normal execution; -race's much
	// heavier scheduling overhead made it easy to win instead (see the
	// identical fix in TestProxiedOfflineCatchUp).
	time.Sleep(500 * time.Millisecond)

	// Earn lifetime coins: write directly through the store, the same
	// durable path the game actor's autosave/detach flush uses.
	st, err = store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	fp := identity.Fingerprint(signer.PublicKey())
	row, created, err := st.LoadOrCreateSave(context.Background(), fp, "ranker", time.Now().Unix(), func() (store.FreshSave, error) {
		t.Fatal("save should already exist from the first connection")
		return store.FreshSave{}, nil
	})
	if err != nil || created {
		t.Fatalf("load save: created=%v err=%v", created, err)
	}
	if err := st.PersistSave(context.Background(), fp, "ranker", row.State, row.StateVersion, time.Now().Unix(), 2_000_000, 2_000_000, 0, "Sunny Hollow"); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()

	time.Sleep(50 * time.Millisecond) // past the 20ms leaderboard TTL above

	second := connectAndOpenBoard(t, addr, "ranker", signer)
	if !strings.Contains(second, "YOU: #1/2") {
		t.Fatalf("expected YOU: #1/2 after overtaking the leader, got:\n%s", second)
	}
}

// connectAndOpenBoard opens a session as user, requests a PTY, waits for the
// game to render, dismisses the new-player tutorial overlay (esc — it
// otherwise swallows every key but s/enter/arrows, see
// internal/tui/game.go's handleTutorialKey), sends "7" to open the board
// screen (tui/02's nav key), then closes the session and drains whatever
// was rendered — the same connect/write/sleep/close/ReadAll shape as this
// package's readScreen helper (server_test.go), rather than a live
// read-loop: an SSH channel Read has no deadline of its own, so racing it
// against a wall-clock deadline risks blocking forever if the server ever
// stops sending mid-loop, which closing first and draining after does not.
func connectAndOpenBoard(t *testing.T, addr, user string, signer gossh.Signer) string {
	t.Helper()
	client, err := gossh.Dial("tcp", addr, clientConfig(t, user, signer))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	sess, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	if err := sess.RequestPty("xterm", 30, 100, gossh.TerminalModes{}); err != nil {
		t.Fatal(err)
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Shell(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	_, _ = stdin.Write([]byte("\x1b"))
	time.Sleep(200 * time.Millisecond)
	_, _ = stdin.Write([]byte("7"))
	time.Sleep(300 * time.Millisecond)
	_ = sess.Close()
	b, _ := io.ReadAll(stdout)
	return stripANSI(string(b))
}
