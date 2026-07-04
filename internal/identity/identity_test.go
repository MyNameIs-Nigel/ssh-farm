package identity

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gossh "golang.org/x/crypto/ssh"
)

func TestSanitizeSlot(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"Nigel", "nigel"},
		{"other-name_2", "other-name_2"},
		{"bad/name", "badname"},
		{"", ""},
		{"!!!", ""},
		{"a" + string(make([]byte, 40)), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}
	for _, tc := range tests {
		if tc.in != "" && len(tc.in) > 40 {
			tc.in = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		}
		got := SanitizeSlot(tc.in)
		if got != tc.want {
			t.Errorf("SanitizeSlot(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestResolveSlotUsesDefault(t *testing.T) {
	if got := ResolveSlot("", "default"); got != "default" {
		t.Fatalf("ResolveSlot empty = %q", got)
	}
	if got := ResolveSlot("!!!", "default"); got != "default" {
		t.Fatalf("ResolveSlot invalid = %q", got)
	}
}

func TestFingerprintStable(t *testing.T) {
	pub := mustPublicKey(t)
	fp := Fingerprint(pub)

	sum := sha256.Sum256(pub.Marshal())
	if got := FingerprintFromBytes(sum); got != fp {
		t.Fatalf("FingerprintFromBytes = %q, want %q", got, fp)
	}
}

func TestProxiedFingerprintMatchesDirect(t *testing.T) {
	pub := mustPublicKey(t)

	username, err := EncodeProxiedUsername(pub, "scout")
	if err != nil {
		t.Fatal(err)
	}
	fpBytes, slot, err := ParseProxiedUsername(username)
	if err != nil {
		t.Fatal(err)
	}
	if slot != "scout" {
		t.Fatalf("slot = %q, want scout", slot)
	}
	if got := FingerprintFromBytes(fpBytes); got != Fingerprint(pub) {
		t.Fatalf("proxied fp %q != direct fp %q", got, Fingerprint(pub))
	}
}

func TestParseProxiedUsernameTable(t *testing.T) {
	pub := mustPublicKey(t)
	validUser, err := EncodeProxiedUsername(pub, "default")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		user    string
		wantErr bool
	}{
		{"valid", validUser, false},
		{"too short", "abc.default", true},
		{"missing dot", strings.Repeat("a", 64) + "default", true},
		{"uppercase hex", strings.ToUpper(validUser[:64]) + validUser[64:], true},
		{"empty slot", validUser[:65], true},
		{"invalid slot chars", validUser[:65] + "bad/slot", true},
		{"trailing junk", validUser + ".extra", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ParseProxiedUsername(tc.user)
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErr && !IsProxiedIdentityError(err) {
				t.Fatalf("expected ErrProxiedIdentity, got %v", err)
			}
		})
	}
}

func TestLoadProxyKeys(t *testing.T) {
	pub := mustPublicKey(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy_keys")
	line := strings.TrimSpace(string(gossh.MarshalAuthorizedKey(pub))) + " arcade-proxy\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}

	keys, err := LoadProxyKeys(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 {
		t.Fatalf("len(keys) = %d, want 1", len(keys))
	}
	if string(keys[0].Marshal()) != string(pub.Marshal()) {
		t.Fatal("loaded key does not match")
	}
}

func TestLoadProxyKeysEmptyPath(t *testing.T) {
	keys, err := LoadProxyKeys("")
	if err != nil {
		t.Fatal(err)
	}
	if keys != nil {
		t.Fatalf("expected nil keys, got %v", keys)
	}
}

func mustPublicKey(t *testing.T) gossh.PublicKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return signer.PublicKey()
}
