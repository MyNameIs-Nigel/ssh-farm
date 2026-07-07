package server

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/wish/v2/testsession"
	gossh "golang.org/x/crypto/ssh"

	"github.com/mynameis-nigel/ssh-farm/internal/config"
	"github.com/mynameis-nigel/ssh-farm/internal/content"
	"github.com/mynameis-nigel/ssh-farm/internal/game"
	"github.com/mynameis-nigel/ssh-farm/internal/identity"
	"github.com/mynameis-nigel/ssh-farm/internal/leaderboard"
	applog "github.com/mynameis-nigel/ssh-farm/internal/log"
	"github.com/mynameis-nigel/ssh-farm/internal/store"
	"github.com/mynameis-nigel/ssh-farm/internal/version"
)

func testServer(t *testing.T, mutate func(*config.Config)) (*Server, string) {
	t.Helper()

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cfg.ListenPort = 0
	cfg.HostKeyPath = filepath.Join(dir, "host_key")
	cfg.DBPath = filepath.Join(dir, "farm.db")
	cfg.IdleTimeout = time.Hour
	cfg.MaxSessionsPerKey = 2
	cfg.MaxConnections = 10
	cfg.RateLimitPerSecond = 100
	cfg.ProxyKeysPath = ""
	if mutate != nil {
		mutate(&cfg)
	}

	logger := applog.New("error", "text")

	st, err := store.Open(context.Background(), cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	c, err := content.Load("../sim/testdata")
	if err != nil {
		t.Fatal(err)
	}
	games := game.NewManager(st, c, logger, cfg.AutosaveInterval, game.Policy(cfg.SessionPolicy))

	proxyKeys, err := identity.LoadProxyKeys(cfg.ProxyKeysPath)
	if err != nil {
		t.Fatal(err)
	}
	resolver := identity.NewResolver(cfg.DefaultSlot, proxyKeys)

	board := leaderboard.New(st, cfg.LeaderboardTTL, cfg.LeaderboardActivityWindow, cfg.LeaderboardMinCoins, time.Now)

	srv, err := New(cfg, logger, games, resolver, board)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = games.Shutdown(ctx)
		_ = srv.Shutdown(ctx)
	})

	addr := testsession.Listen(t, srv.ssh)
	return srv, addr
}

func testSigner(t *testing.T) gossh.Signer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func clientConfig(t *testing.T, user string, signer gossh.Signer) *gossh.ClientConfig {
	t.Helper()
	return &gossh.ClientConfig{
		User:            user,
		Auth:            []gossh.AuthMethod{gossh.PublicKeys(signer)},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(), //nolint:gosec // test only
		Timeout:         5 * time.Second,
	}
}

// TestServerVersionBanner: the raw SSH version-exchange banner embeds this
// game's fleet version so the arcade router's health-check prober can read
// it live (see ../../ssh-arcadelobby/docs/03-games-registry-and-health.md)
// instead of a hand-maintained games.toml field. A raw TCP dial (not
// gossh.Dial, which performs a full handshake) reads the literal bytes the
// prober itself reads.
func TestServerVersionBanner(t *testing.T) {
	_, addr := testServer(t, nil)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	want := "SSH-2.0-" + version.Version
	if !strings.HasPrefix(strings.TrimRight(line, "\r\n"), want) {
		t.Fatalf("banner = %q, want prefix %q", line, want)
	}
}

func TestRejectsPasswordAuth(t *testing.T) {
	_, addr := testServer(t, nil)

	_, err := gossh.Dial("tcp", addr, &gossh.ClientConfig{
		User:            "alice",
		Auth:            []gossh.AuthMethod{gossh.Password("secret")},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(), //nolint:gosec
		Timeout:         5 * time.Second,
	})
	if err == nil {
		t.Fatal("expected password auth to fail")
	}
}

func TestRejectsSessionWithoutPTY(t *testing.T) {
	_, addr := testServer(t, nil)
	signer := testSigner(t)

	sess, err := testsession.NewClientSession(t, addr, clientConfig(t, "alice", signer))
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	sess.Stdout = &out
	if err := sess.Run(""); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.String(), "interactive terminal") {
		t.Fatalf("expected PTY message, got: %q", out.String())
	}
}

func readScreen(t *testing.T, addr, user string, signer gossh.Signer) string {
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
	stdout, err := sess.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Shell(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	_ = sess.Close()
	b, _ := io.ReadAll(stdout)
	return stripANSI(string(b))
}

func TestGameShowsTitleAndSlot(t *testing.T) {
	_, addr := testServer(t, nil)
	signer := testSigner(t)

	screenDefault := readScreen(t, addr, "alice", signer)
	if !strings.Contains(screenDefault, "ssh-farm") {
		t.Fatalf("expected game title in %q", screenDefault)
	}
	if !strings.Contains(screenDefault, "alice") {
		t.Fatalf("expected slot alice in %q", screenDefault)
	}
	if !strings.Contains(screenDefault, "Farm") {
		t.Fatalf("expected farm nav in %q", screenDefault)
	}

	screenOther := readScreen(t, addr, "other", signer)
	if !strings.Contains(screenOther, "other") {
		t.Fatalf("expected slot other in %q", screenOther)
	}
}

func TestGlobalSessionCap(t *testing.T) {
	_, addr := testServer(t, func(c *config.Config) {
		c.MaxConnections = 1
		c.MaxSessionsPerKey = 10
	})

	hold := func(signer gossh.Signer) {
		t.Helper()
		client, err := gossh.Dial("tcp", addr, clientConfig(t, "u1", signer))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { client.Close() })
		sess, err := client.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		if err := sess.RequestPty("xterm", 80, 24, gossh.TerminalModes{}); err != nil {
			t.Fatal(err)
		}
		if err := sess.Shell(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(200 * time.Millisecond)
	}

	hold(testSigner(t))

	client2, err := gossh.Dial("tcp", addr, clientConfig(t, "u2", testSigner(t)))
	if err != nil {
		t.Fatal(err)
	}
	defer client2.Close()
	sess2, err := client2.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	sess2.Stdout = &buf
	_ = sess2.Run("")
	if !strings.Contains(buf.String(), "Too many active sessions") {
		t.Fatalf("expected global cap message, got %q", buf.String())
	}
}

func TestPerKeySessionCap(t *testing.T) {
	_, addr := testServer(t, func(c *config.Config) {
		c.MaxSessionsPerKey = 1
	})
	signer := testSigner(t)
	cfg := clientConfig(t, "captest", signer)

	client1, err := gossh.Dial("tcp", addr, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client1.Close()

	sess1, err := client1.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if err := sess1.RequestPty("xterm", 80, 24, gossh.TerminalModes{}); err != nil {
		t.Fatal(err)
	}
	if err := sess1.Shell(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)

	client2, err := gossh.Dial("tcp", addr, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client2.Close()

	sess2, err := client2.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	sess2.Stdout = &buf
	_ = sess2.Run("")
	if !strings.Contains(buf.String(), "Too many active sessions") {
		t.Fatalf("expected cap message, got %q", buf.String())
	}
}

func TestProxiedSessionsCapByPlayerFingerprint(t *testing.T) {
	proxySigner := testSigner(t)
	playerA := testSigner(t)
	playerB := testSigner(t)

	userA, err := identity.EncodeProxiedUsername(playerA.PublicKey(), "farm")
	if err != nil {
		t.Fatal(err)
	}
	userB, err := identity.EncodeProxiedUsername(playerB.PublicKey(), "farm")
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	proxyPath := filepath.Join(dir, "proxy_keys")
	if err := os.WriteFile(proxyPath, []byte(strings.TrimSpace(string(gossh.MarshalAuthorizedKey(proxySigner.PublicKey())))+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, addr := testServer(t, func(c *config.Config) {
		c.MaxSessionsPerKey = 1
		c.MaxConnections = 10
		c.ProxyKeysPath = proxyPath
	})

	holdProxied := func(user string) {
		t.Helper()
		client, err := gossh.Dial("tcp", addr, clientConfig(t, user, proxySigner))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { client.Close() })
		sess, err := client.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		if err := sess.RequestPty("xterm", 80, 24, gossh.TerminalModes{}); err != nil {
			t.Fatal(err)
		}
		if err := sess.Shell(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(200 * time.Millisecond)
	}

	holdProxied(userA)

	client2, err := gossh.Dial("tcp", addr, clientConfig(t, userA, proxySigner))
	if err != nil {
		t.Fatal(err)
	}
	defer client2.Close()
	sess2, err := client2.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	var bufSamePlayer bytes.Buffer
	sess2.Stdout = &bufSamePlayer
	_ = sess2.Run("")
	if !strings.Contains(bufSamePlayer.String(), "Too many active sessions") {
		t.Fatalf("same player via proxy should hit per-key cap, got %q", bufSamePlayer.String())
	}

	screenB := readScreen(t, addr, userB, proxySigner)
	if !strings.Contains(screenB, "farm") {
		t.Fatalf("different proxied player should connect, got %q", screenB)
	}
}

func TestProxiedMalformedUsernameRejected(t *testing.T) {
	proxySigner := testSigner(t)
	dir := t.TempDir()
	proxyPath := filepath.Join(dir, "proxy_keys")
	if err := os.WriteFile(proxyPath, []byte(strings.TrimSpace(string(gossh.MarshalAuthorizedKey(proxySigner.PublicKey())))+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, addr := testServer(t, func(c *config.Config) {
		c.ProxyKeysPath = proxyPath
	})

	client, err := gossh.Dial("tcp", addr, clientConfig(t, "not-valid-proxied", proxySigner))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	sess, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	sess.Stdout = &buf
	_ = sess.Run("")
	if !strings.Contains(buf.String(), "Could not verify who you are through the arcade") {
		t.Fatalf("expected proxied identity refusal, got %q", buf.String())
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		if c != '\x1b' {
			if c >= 32 || c == '\n' {
				b.WriteByte(c)
			}
			i++
			continue
		}
		i++
		if i >= len(s) {
			break
		}
		switch s[i] {
		case '[':
			i++
			for i < len(s) && (s[i] < '@' || s[i] > '~') {
				i++
			}
			i++
		case ']':
			i++
			for i < len(s) && s[i] != '\a' {
				if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '\\' {
					i++
					break
				}
				i++
			}
			i++
		default:
			i++
		}
	}
	return b.String()
}
