package identity

import (
	"fmt"
	"strings"
)

func ValidGuestID(token string) bool {
	if len(token) != 32 {
		return false
	}
	for _, c := range token {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func (id SessionIdentity) Validate() error {
	switch id.Kind {
	case Keyed:
		if id.Fingerprint != "" && id.Slot != "" && SanitizeSlot(id.Slot) == id.Slot && id.GuestID == "" {
			return nil
		}
	case Guest:
		if ValidGuestID(id.GuestID) && id.Fingerprint == "" && id.Slot == "" {
			return nil
		}
	}
	return fmt.Errorf("invalid session identity")
}
func (id SessionIdentity) IsGuest() bool { return id.Kind == Guest }
func (id SessionIdentity) Bucket() string {
	if id.IsGuest() {
		return "guest:" + id.GuestID
	}
	return "key:" + id.Fingerprint
}
func (id SessionIdentity) Label() string {
	if id.IsGuest() {
		return "GUEST"
	}
	return id.Slot
}
func ParseGuestUsername(user string) (string, error) {
	token := strings.TrimPrefix(user, "guest.")
	if !strings.HasPrefix(user, "guest.") || !ValidGuestID(token) {
		return "", fmt.Errorf("%w: invalid guest token", ErrProxiedIdentity)
	}
	return token, nil
}
