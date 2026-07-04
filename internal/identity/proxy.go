package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/ssh"
)

// ErrProxiedIdentity means the router sent a username that does not match
// protocol v1 on a proxied connection.
var ErrProxiedIdentity = errors.New("proxied identity protocol error")

const proxiedIdentityMessage = "🌧 Could not verify who you are through the arcade.\r\n" +
	"Please reconnect from the lobby.\r\n"

// ProxiedIdentityMessage is the player-facing refusal for malformed proxied usernames.
func ProxiedIdentityMessage() string { return proxiedIdentityMessage }

// IsProxiedIdentityError reports protocol-v1 username failures on proxied sessions.
func IsProxiedIdentityError(err error) bool {
	return errors.Is(err, ErrProxiedIdentity)
}

// ParseProxiedUsername decodes a router-encoded username per protocol v1.
func ParseProxiedUsername(username string) ([32]byte, string, error) {
	var zero [32]byte
	if len(username) < 66 {
		return zero, "", fmt.Errorf("%w: username too short", ErrProxiedIdentity)
	}
	if username[64] != '.' {
		return zero, "", fmt.Errorf("%w: missing separator", ErrProxiedIdentity)
	}
	if len(username) > 97 {
		return zero, "", fmt.Errorf("%w: username too long", ErrProxiedIdentity)
	}

	hexPart := username[:64]
	for _, c := range hexPart {
		if c >= '0' && c <= '9' || c >= 'a' && c <= 'f' {
			continue
		}
		return zero, "", fmt.Errorf("%w: invalid fingerprint hex", ErrProxiedIdentity)
	}

	raw, err := hex.DecodeString(hexPart)
	if err != nil || len(raw) != 32 {
		return zero, "", fmt.Errorf("%w: invalid fingerprint hex", ErrProxiedIdentity)
	}

	slotPart := username[65:]
	if slotPart == "" {
		return zero, "", fmt.Errorf("%w: empty slot", ErrProxiedIdentity)
	}
	slot := SanitizeSlot(slotPart)
	if slot == "" || slot != slotPart {
		return zero, "", fmt.Errorf("%w: invalid slot %q", ErrProxiedIdentity, slotPart)
	}

	var fp [32]byte
	copy(fp[:], raw)
	return fp, slot, nil
}

// EncodeProxiedUsername builds a protocol-v1 username for tests and tooling.
func EncodeProxiedUsername(key ssh.PublicKey, slot string) (string, error) {
	if key == nil {
		return "", errors.New("nil key")
	}
	slot = SanitizeSlot(slot)
	if slot == "" {
		return "", errors.New("invalid slot")
	}
	sum := sha256.Sum256(key.Marshal())
	return strings.ToLower(hex.EncodeToString(sum[:])) + "." + slot, nil
}
