package identity

import (
	"bytes"
	"fmt"
	"os"

	"github.com/charmbracelet/ssh"
	gossh "golang.org/x/crypto/ssh"
)

// LoadProxyKeys reads trusted arcade proxy public keys from an
// authorized_keys-format file. An empty path yields no proxy keys.
func LoadProxyKeys(path string) ([]ssh.PublicKey, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read proxy keys %q: %w", path, err)
	}
	var keys []ssh.PublicKey
	rest := data
	for len(bytes.TrimSpace(rest)) > 0 {
		key, _, _, remaining, err := gossh.ParseAuthorizedKey(rest)
		if err != nil {
			return nil, fmt.Errorf("parse proxy keys %q: %w", path, err)
		}
		keys = append(keys, key)
		rest = remaining
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("parse proxy keys %q: no keys found", path)
	}
	return keys, nil
}

// SessionSource exposes the session fields needed for identity resolution.
type SessionSource interface {
	User() string
	PublicKey() ssh.PublicKey
	Environ() []string
}

// Resolver resolves player identity for direct and proxied SSH sessions.
type Resolver struct {
	defaultSlot string
	proxyKeys   []ssh.PublicKey
}

// NewResolver builds an identity resolver with optional trusted proxy keys.
func NewResolver(defaultSlot string, proxyKeys []ssh.PublicKey) *Resolver {
	return &Resolver{defaultSlot: defaultSlot, proxyKeys: proxyKeys}
}

// Resolve derives the player fingerprint and save slot from a session.
func (r *Resolver) Resolve(s SessionSource) (ResolveResult, error) {
	wire := s.PublicKey()
	if wire == nil {
		return ResolveResult{}, fmt.Errorf("public key authentication is required")
	}

	if r.isProxyKey(wire) {
		return r.resolveProxied(s, wire)
	}
	return r.resolveDirect(s, wire), nil
}

func (r *Resolver) isProxyKey(key ssh.PublicKey) bool {
	wire := key.Marshal()
	for _, pk := range r.proxyKeys {
		if bytes.Equal(wire, pk.Marshal()) {
			return true
		}
	}
	return false
}

func (r *Resolver) resolveDirect(s SessionSource, wire ssh.PublicKey) ResolveResult {
	return ResolveResult{
		Identity: SessionIdentity{
			Fingerprint: Fingerprint(wire),
			Slot:        ResolveSlot(s.User(), r.defaultSlot),
		},
		PublicKey: marshalAuthorizedKey(wire),
		Proxied:   false,
	}
}

func (r *Resolver) resolveProxied(s SessionSource, wire ssh.PublicKey) (ResolveResult, error) {
	fpBytes, slot, err := ParseProxiedUsername(s.User())
	if err != nil {
		return ResolveResult{}, err
	}
	pub := envValue(s, "ARCADE_PLAYER_KEY")
	if pub == "" {
		pub = marshalAuthorizedKey(wire)
	}
	return ResolveResult{
		Identity: SessionIdentity{
			Fingerprint: FingerprintFromBytes(fpBytes),
			Slot:        slot,
		},
		PublicKey: pub,
		Proxied:   true,
	}, nil
}

func envValue(s SessionSource, key string) string {
	prefix := key + "="
	for _, e := range s.Environ() {
		if len(e) > len(prefix) && e[:len(prefix)] == prefix {
			return e[len(prefix):]
		}
	}
	return ""
}

func marshalAuthorizedKey(key ssh.PublicKey) string {
	if key == nil {
		return ""
	}
	return string(gossh.MarshalAuthorizedKey(key))
}
