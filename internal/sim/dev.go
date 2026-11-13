package sim

import (
	"fmt"

	"github.com/mynameis-nigel/ssh-farm/internal/content"
)

// DevApply changes a test fixture without spending currency or granting contract
// rewards. Callers must enforce dev mode before invoking it.
func DevApply(s *State, c *content.Content, action string, value, now int64) error {
	switch action {
	case "coins", "starseeds", "earnings", "rebirths":
		if value < 0 {
			return fmt.Errorf("dev value must be non-negative")
		}
		switch action {
		case "coins":
			s.Coins = value
		case "starseeds":
			s.PrestigeCurrency = value
		case "earnings":
			s.RunEarnings = value
			s.LifetimeEarnings = max(s.LifetimeEarnings, value)
		case "rebirths":
			s.Rebirths = value
		}
	case "mature":
		for i := range s.Plots {
			p := &s.Plots[i]
			if crop := c.Crop(p.Crop); crop != nil {
				p.PlantedAt = now - s.GrowSeconds(c, crop)
			}
		}
	case "gift":
		if s.contractBlocksEvents() {
			return ErrContractRestricted
		}
		s.GiftPending = true
		s.GiftArrivedAt = now
	case "event":
		if value < 0 || value > int64(len(c.Events)) {
			return fmt.Errorf("unknown dev event")
		}
		if value != 0 && s.contractBlocksEvents() {
			return ErrContractRestricted
		}
		s.endEvent(c, now)
		if value > 0 {
			s.EventID = c.Events[value-1].ID
			s.EventStartedAt = now
			s.EventEndsAt = now + c.EventsConfig.MaxDurationSec
		}
	case "contracts":
		s.ContractsUnlocked = true
	default:
		return fmt.Errorf("unknown dev action %q", action)
	}
	return nil
}
