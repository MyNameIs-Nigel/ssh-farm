package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/ssh"
	gossh "golang.org/x/crypto/ssh"

	"github.com/mynameis-nigel/ssh-farm/internal/config"
	"github.com/mynameis-nigel/ssh-farm/internal/game"
	"github.com/mynameis-nigel/ssh-farm/internal/store"
)

type contextBase struct {
	ssh.Context
	base context.Context
}

func (c contextBase) Value(key any) any { return c.base.Value(key) }
func TestShellStateIsIsolatedBetweenChannels(t *testing.T) {
	type testKey struct{}
	base := contextBase{base: t.Context()}
	shared := shellSession{ctx: base}
	var scopes []ssh.Context
	handler := scopeSession()(func(s ssh.Session) {
		scopes = append(scopes, s.Context())
		s.Context().SetValue(testKey{}, len(scopes))
	})
	handler(shared)
	handler(shared)
	if scopes[0].Value(testKey{}) != 1 || scopes[1].Value(testKey{}) != 2 {
		t.Fatal("channels share game state")
	}
}

func TestTrustedGuestAdmissionAndNoDatabaseRows(t *testing.T) {
	proxy := testSigner(t)
	path := filepath.Join(t.TempDir(), "proxy.pub")
	if err := os.WriteFile(path, gossh.MarshalAuthorizedKey(proxy.PublicKey()), 0600); err != nil {
		t.Fatal(err)
	}
	for _, allowed := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[allowed], func(t *testing.T) {
			srv, addr := testServer(t, func(c *config.Config) { c.AllowGuests = allowed; c.ProxyKeysPath = path })
			output := readScreen(t, addr, "guest."+strings.Repeat("a", 32), proxy)
			if allowed && (!strings.Contains(output, "GUEST") || !strings.Contains(output, "NOT SAVED")) {
				t.Fatal(output)
			}
			if !allowed && !strings.Contains(output, "Could not verify") {
				t.Fatal("disabled guest entered", output)
			}
			manager := srv.games.(*game.Manager)
			deadline := time.Now().Add(time.Second)
			for manager.ActiveSaves() > 0 {
				if time.Now().After(deadline) {
					t.Fatal("guest retained")
				} // readScreen closes the owning channel; wait for disposal.
				time.Sleep(10 * time.Millisecond)
			}
			st, err := store.Open(t.Context(), srv.cfg.DBPath)
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			rows, err := st.LeaderboardSnapshot(t.Context())
			if err != nil || len(rows) != 0 {
				t.Fatal("guest wrote save", err)
			}
		})
	}
}
