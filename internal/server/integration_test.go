package server

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/mynameis-nigel/ssh-farm/internal/config"
	"github.com/mynameis-nigel/ssh-farm/internal/content"
	"github.com/mynameis-nigel/ssh-farm/internal/identity"
	"github.com/mynameis-nigel/ssh-farm/internal/sim"
	"github.com/mynameis-nigel/ssh-farm/internal/store"
)

// TestProxiedOfflineCatchUp proves identity continuity: disconnect, advance
// offline time in the save, reconnect via the arcade proxy → away-summary.
func TestProxiedOfflineCatchUp(t *testing.T) {
	proxySigner := testSigner(t)
	player := testSigner(t)
	user, err := identity.EncodeProxiedUsername(player.PublicKey(), "catchup")
	if err != nil {
		t.Fatal(err)
	}
	fp := identity.Fingerprint(player.PublicKey())

	dir := t.TempDir()
	proxyPath := filepath.Join(dir, "proxy_keys")
	if err := os.WriteFile(proxyPath, []byte(strings.TrimSpace(string(gossh.MarshalAuthorizedKey(proxySigner.PublicKey())))+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var dbPath string
	_, addr := testServer(t, func(c *config.Config) {
		c.ProxyKeysPath = proxyPath
		c.DBPath = filepath.Join(dir, "farm.db")
		dbPath = c.DBPath
	})

	cfg := clientConfig(t, user, proxySigner)

	client1, err := gossh.Dial("tcp", addr, cfg)
	if err != nil {
		t.Fatal(err)
	}
	sess1, err := client1.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if err := sess1.RequestPty("xterm", 30, 100, gossh.TerminalModes{}); err != nil {
		t.Fatal(err)
	}
	stdin1, err := sess1.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := sess1.Shell(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)
	_, _ = stdin1.Write([]byte("s\r"))
	time.Sleep(100 * time.Millisecond)
	_, _ = stdin1.Write([]byte("\r"))
	time.Sleep(300 * time.Millisecond)
	_ = sess1.Close()
	_ = client1.Close()

	st, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	row, created, err := st.LoadOrCreateSave(context.Background(), fp, "catchup", time.Now().Unix(), func() (store.FreshSave, error) {
		t.Fatal("save should already exist from first session")
		return store.FreshSave{}, nil
	})
	if err != nil || created {
		t.Fatalf("load save: created=%v err=%v", created, err)
	}
	state, err := sim.DecodeState(row.State)
	if err != nil {
		t.Fatal(err)
	}
	c, err := content.Load("../sim/testdata")
	if err != nil {
		t.Fatal(err)
	}
	back := time.Now().Unix() - 120
	state.UpdatedAt = back
	if err := sim.Plant(state, c, 0, "turnip", back-30); err != nil {
		t.Fatal(err)
	}
	payload, err := state.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.PersistSave(context.Background(), fp, "catchup", payload, state.Version, back, state.Coins, state.FarmName); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()

	client2, err := gossh.Dial("tcp", addr, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client2.Close()
	sess2, err := client2.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer sess2.Close()
	if err := sess2.RequestPty("xterm", 30, 100, gossh.TerminalModes{}); err != nil {
		t.Fatal(err)
	}
	stdout, err := sess2.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := sess2.Shell(); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(8 * time.Second)
	var buf bytes.Buffer
	for time.Now().Before(deadline) {
		b := make([]byte, 4096)
		n, _ := stdout.Read(b)
		buf.Write(b[:n])
		if strings.Contains(stripANSI(buf.String()), "Welcome back") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("expected away summary after proxied reconnect, got:\n%s", stripANSI(buf.String()))
}
