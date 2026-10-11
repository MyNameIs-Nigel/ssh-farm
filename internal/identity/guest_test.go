package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"

	"charm.land/ssh"
	gossh "golang.org/x/crypto/ssh"
)

func newKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := gossh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestGuestProxyTrustAndPolicy(t *testing.T) {
	proxy, stranger := newKey(t), newKey(t)
	token := strings.Repeat("a", 32)
	for _, tc := range []struct {
		name, user                       string
		trusted, enabled, metadata, want bool
	}{
		{"allowed", "guest." + token, true, true, false, true},
		{"disabled", "guest." + token, true, false, false, false},
		{"short", "guest.a", true, true, false, false},
		{"uppercase", "guest." + strings.Repeat("A", 32), true, true, false, false},
		{"trailing", "guest." + token + ".default", true, true, false, false},
		{"metadata", "guest." + token, true, true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewResolver("default", nil, WithGuests(tc.enabled))
			r.proxyKeys = append(r.proxyKeys, proxy)
			wire := stranger
			if tc.trusted {
				wire = proxy
			}
			s := resolverSession{user: tc.user, pub: wire}
			if tc.metadata {
				s.env = []string{"ARCADE_PLAYER_KEY=unexpected"}
			}
			got, err := r.Resolve(&s)
			if (err == nil) != tc.want {
				t.Fatalf("result=%+v error=%v", got, err)
			}
			if tc.want && (got.Identity.Kind != Guest || got.Identity.GuestID != token || got.Identity.Fingerprint != "" || got.Identity.Slot != "" || got.PublicKey != "") {
				t.Fatalf("guest leaked account: %+v", got)
			}
		})
	}
	r := NewResolver("default", nil, WithGuests(true))
	got, err := r.Resolve(&resolverSession{user: "guest." + token, pub: stranger})
	if err != nil || got.Identity.Kind != Keyed {
		t.Fatalf("direct keyed player: %+v %v", got, err)
	}
	if _, err = r.Resolve(&resolverSession{user: "guest"}); err == nil {
		t.Fatal("direct anonymous accepted")
	}
}

func TestSessionIdentityDiscriminantAndBuckets(t *testing.T) {
	guest := SessionIdentity{Kind: Guest, GuestID: strings.Repeat("d", 32)}
	keyed := SessionIdentity{Kind: Keyed, Fingerprint: "SHA256:key", Slot: "farm"}
	for _, id := range []SessionIdentity{guest, keyed} {
		if err := id.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	if guest.Bucket() == keyed.Bucket() || guest.Label() != "GUEST" || keyed.Label() != "farm" {
		t.Fatal("identity namespace or label")
	}
	for _, user := range []string{"guest", "guest." + strings.Repeat("z", 32), "key." + guest.GuestID} {
		if _, err := ParseGuestUsername(user); err == nil {
			t.Fatal("malformed guest accepted")
		}
	}
}
