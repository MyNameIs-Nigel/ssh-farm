package identity

import (
	"crypto/rand"
	"crypto/rsa"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/ssh"
	gossh "golang.org/x/crypto/ssh"
)

func TestResolverDirectConnection(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}

	playerKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	playerSigner, err := gossh.NewSignerFromKey(playerKey)
	if err != nil {
		t.Fatal(err)
	}
	forgedUser, err := EncodeProxiedUsername(playerSigner.PublicKey(), "victim")
	if err != nil {
		t.Fatal(err)
	}

	resolver := NewResolver("default", nil)
	got, err := resolver.Resolve(&resolverSession{user: forgedUser, pub: signer.PublicKey()})
	if err != nil {
		t.Fatal(err)
	}
	if got.Proxied {
		t.Fatal("expected direct connection")
	}
	if got.Identity.Fingerprint == Fingerprint(playerSigner.PublicKey()) {
		t.Fatal("direct connection must not adopt proxied fingerprint")
	}
	if got.Identity.Fingerprint != Fingerprint(signer.PublicKey()) {
		t.Fatalf("fingerprint = %q, want attacker key", got.Identity.Fingerprint)
	}
}

func TestResolverProxiedConnection(t *testing.T) {
	proxyKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	proxySigner, err := gossh.NewSignerFromKey(proxyKey)
	if err != nil {
		t.Fatal(err)
	}

	playerKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	playerSigner, err := gossh.NewSignerFromKey(playerKey)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodeProxiedUsername(playerSigner.PublicKey(), "beta")
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "proxy_keys")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(string(gossh.MarshalAuthorizedKey(proxySigner.PublicKey())))+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	keys, err := LoadProxyKeys(path)
	if err != nil {
		t.Fatal(err)
	}
	resolver := NewResolver("default", keys)

	playerPub := string(gossh.MarshalAuthorizedKey(playerSigner.PublicKey()))
	got, err := resolver.Resolve(&resolverSession{
		user: encoded,
		pub:  proxySigner.PublicKey(),
		env:  []string{"ARCADE_PLAYER_KEY=" + playerPub},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Proxied {
		t.Fatal("expected proxied connection")
	}
	if got.Identity.Slot != "beta" {
		t.Fatalf("slot = %q, want beta", got.Identity.Slot)
	}
	if got.Identity.Fingerprint != Fingerprint(playerSigner.PublicKey()) {
		t.Fatalf("fingerprint = %q, want player fingerprint", got.Identity.Fingerprint)
	}
	if got.PublicKey != playerPub {
		t.Fatalf("public key = %q, want player key", got.PublicKey)
	}
}

func TestResolverProxiedMalformedUsername(t *testing.T) {
	proxyKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	proxySigner, err := gossh.NewSignerFromKey(proxyKey)
	if err != nil {
		t.Fatal(err)
	}

	resolver := NewResolver("default", []ssh.PublicKey{proxySigner.PublicKey()})
	_, err = resolver.Resolve(&resolverSession{user: "bad-user", pub: proxySigner.PublicKey()})
	if !IsProxiedIdentityError(err) {
		t.Fatalf("expected proxied identity error, got %v", err)
	}
}

type resolverSession struct {
	user string
	pub  ssh.PublicKey
	env  []string
}

func (r *resolverSession) User() string        { return r.user }
func (r *resolverSession) PublicKey() ssh.PublicKey { return r.pub }
func (r *resolverSession) Environ() []string   { return r.env }
