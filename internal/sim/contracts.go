package sim

import (
	"errors"

	"github.com/mynameis-nigel/ssh-farm/internal/content"
)

// ContractID names one of the fixed, one-time endgame contracts.
type ContractID string

const (
	ContractBareHands       ContractID = "bare_hands"
	ContractLeanSeason      ContractID = "lean_season"
	ContractClockworkDenied ContractID = "clockwork_denied"

	contractBareHandsEarnings   int64 = 100_000
	contractLeanSeasonStarseeds int64 = 100
	contractLeanSeasonMaxPlots        = 6
	contractClockworkRebirths         = 5
)

const (
	NameStyleTraditional = ""
	NameStyleLeaf        = "leaf"
	NameStyleGold        = "gold"
	NameStyleSky         = "sky"
	NameStyleRose        = "rose"
	NameStyleViolet      = "violet"
	NameStylePurpleWave  = "purple_wave"
)

var (
	ErrContractsLocked    = errors.New("finish the base game to unlock contracts")
	ErrContractSequence   = errors.New("that contract is not available yet")
	ErrContractActive     = errors.New("a contract is already active")
	ErrNoActiveContract   = errors.New("no contract is active")
	ErrContractRestricted = errors.New("the active contract forbids that")
	ErrNameStyleLocked    = errors.New("that leaderboard style is still locked")
	ErrUnknownNameStyle   = errors.New("unknown leaderboard style")
)

// ContractDefinition is immutable display copy for the fixed campaign.
type ContractDefinition struct {
	ID           ContractID
	Name         string
	Premise      string
	Goal         string
	Restrictions []string
	Reward       string
}

var contractDefinitions = [...]ContractDefinition{
	{
		ID:      ContractBareHands,
		Name:    "Bare Hands",
		Premise: "Prove that the farm can prosper without becoming a machine.",
		Goal:    "Earn 100,000 coins after accepting the contract.",
		Restrictions: []string{
			"Auto-harvest and auto-sow are locked.",
		},
		Reward: "First contract seal on the leaderboard.",
	},
	{
		ID:      ContractLeanSeason,
		Name:    "Lean Season",
		Premise: "Build a prestige economy on a deliberately small farm.",
		Goal:    "Earn 100 total Starseeds after accepting the contract.",
		Restrictions: []string{
			"Land is capped at six plots.",
			"The Greenhouse is locked.",
		},
		Reward: "Second contract seal and curated leaderboard name colors.",
	},
	{
		ID:      ContractClockworkDenied,
		Name:    "Clockwork Denied",
		Premise: "Complete the rebirth loop with every passive accelerator removed.",
		Goal:    "Complete five contract-local rebirths.",
		Restrictions: []string{
			"Auto-harvest and auto-sow are locked.",
			"Random events and gifts are disabled.",
			"The Scarecrow is locked.",
		},
		Reward: "Third contract seal and the animated purple name wave.",
	},
}

// Contracts returns the three definitions in their required order.
func Contracts() []ContractDefinition {
	out := make([]ContractDefinition, len(contractDefinitions))
	copy(out, contractDefinitions[:])
	for i := range out {
		out[i].Restrictions = append([]string(nil), out[i].Restrictions...)
	}
	return out
}

// ContractByID returns a copy of a fixed contract definition.
func ContractByID(id ContractID) (ContractDefinition, bool) {
	for _, d := range contractDefinitions {
		if d.ID == id {
			d.Restrictions = append([]string(nil), d.Restrictions...)
			return d, true
		}
	}
	return ContractDefinition{}, false
}

// ContractsAvailable reports whether the campaign may be entered. Once begun,
// it stays available even though every contract reset removes base upgrades.
func (s *State) ContractsAvailable(c *content.Content) bool {
	return s.ContractsUnlocked || s.ContractsCompleted > 0 || s.ActiveContract != "" || s.baseGameComplete(c)
}

func (s *State) baseGameComplete(c *content.Content) bool {
	for _, u := range c.Upgrades {
		if s.UpgradeLevel(u.ID) < u.MaxLevel {
			return false
		}
	}
	for _, crop := range c.Crops {
		switch crop.Unlock.Kind {
		case "earnings":
			if s.LifetimeEarnings < crop.Unlock.Value {
				return false
			}
		case "prestige":
			if s.Rebirths < crop.Unlock.Value {
				return false
			}
		}
	}
	return true
}

// NextContract returns the only contract that can be started next.
func (s *State) NextContract() (ContractDefinition, bool) {
	if s.ContractsCompleted < 0 || s.ContractsCompleted >= len(contractDefinitions) {
		return ContractDefinition{}, false
	}
	return ContractByID(contractDefinitions[s.ContractsCompleted].ID)
}

// StartContract validates strict campaign order, then irreversibly replaces
// current economic state with a fresh farm.
func StartContract(s *State, c *content.Content, id ContractID, now int64) error {
	if s.ActiveContract != "" {
		return ErrContractActive
	}
	next, ok := s.NextContract()
	if !ok || next.ID != id {
		return ErrContractSequence
	}
	if !s.ContractsAvailable(c) {
		return ErrContractsLocked
	}

	resetGameplay(s, c, now)
	s.ContractsUnlocked = true
	s.ActiveContract = id
	return nil
}

// AbandonContract discards the active attempt and starts a fresh ordinary
// farm. It never restores the state that existed before acceptance.
func AbandonContract(s *State, c *content.Content, now int64) error {
	if s.ActiveContract == "" {
		return ErrNoActiveContract
	}
	resetGameplay(s, c, now)
	s.ActiveContract = ""
	return nil
}

func resetGameplay(s *State, c *content.Content, now int64) {
	s.Coins = c.Start.Coins
	s.Plots = make([]Plot, c.Start.Plots)
	s.PurchasedPlots = 0
	s.Tools = map[string]bool{}
	s.Zones = map[string]bool{}
	s.RunEarnings = 0
	s.Multipliers = map[string]int{}
	s.SeedUpgrades = map[string]int{}
	s.Scarecrow = false
	s.GiftPending = false
	s.GiftArrivedAt = 0
	s.EventID = ""
	s.EventStartedAt = 0
	s.EventEndsAt = 0
	s.PrestigeCurrency = 0
	s.Upgrades = map[string]int{}
	s.LastCrop = ""
	s.ContractRebirths = 0
	s.ContractEarnings = 0
	s.ContractStarseeds = 0
	s.UpdatedAt = now
}

// ProgressionRebirths is the count gameplay gates use. The public historical
// count keeps growing while a contract starts its own progression at zero.
func (s *State) ProgressionRebirths() int64 {
	if s.ActiveContract != "" {
		return s.ContractRebirths
	}
	return s.Rebirths
}

func (s *State) contractBlocksAutomation() bool {
	return s.ActiveContract == ContractBareHands || s.ActiveContract == ContractClockworkDenied
}

func (s *State) contractBlocksEvents() bool {
	return s.ActiveContract == ContractClockworkDenied
}

func (s *State) contractBlocksGifts() bool {
	return s.ActiveContract == ContractClockworkDenied
}

func (s *State) contractBlocksScarecrow() bool {
	return s.ActiveContract == ContractClockworkDenied
}

func (s *State) contractCapsLand() bool {
	return s.ActiveContract == ContractLeanSeason
}

func (s *State) creditStarseeds(amount int64) {
	if amount <= 0 {
		return
	}
	s.PrestigeCurrency = satAdd(s.PrestigeCurrency, amount)
	if s.ActiveContract != "" {
		s.ContractStarseeds = satAdd(s.ContractStarseeds, amount)
		s.checkContractCompletion()
	}
}

func (s *State) checkContractCompletion() bool {
	complete := false
	switch s.ActiveContract {
	case ContractBareHands:
		complete = s.ContractEarnings >= contractBareHandsEarnings
	case ContractLeanSeason:
		complete = s.ContractStarseeds >= contractLeanSeasonStarseeds
	case ContractClockworkDenied:
		complete = s.ContractRebirths >= contractClockworkRebirths
	}
	if !complete {
		return false
	}
	s.ContractsCompleted++
	if s.ContractsCompleted > len(contractDefinitions) {
		s.ContractsCompleted = len(contractDefinitions)
	}
	s.ActiveContract = ""
	s.ContractRebirths = 0
	s.ContractEarnings = 0
	s.ContractStarseeds = 0
	return true
}

// AvailableLeaderboardNameStyles returns style IDs unlocked by the campaign.
func (s *State) AvailableLeaderboardNameStyles() []string {
	styles := []string{NameStyleTraditional}
	if s.ContractsCompleted >= 2 {
		styles = append(styles, NameStyleLeaf, NameStyleGold, NameStyleSky, NameStyleRose, NameStyleViolet)
	}
	if s.ContractsCompleted >= 3 {
		styles = append(styles, NameStylePurpleWave)
	}
	return styles
}

// SetLeaderboardNameStyle selects one earned cosmetic.
func SetLeaderboardNameStyle(s *State, style string) error {
	known := style == NameStyleTraditional
	for _, candidate := range []string{NameStyleLeaf, NameStyleGold, NameStyleSky, NameStyleRose, NameStyleViolet, NameStylePurpleWave} {
		if style == candidate {
			known = true
			break
		}
	}
	if !known {
		return ErrUnknownNameStyle
	}
	for _, candidate := range s.AvailableLeaderboardNameStyles() {
		if style == candidate {
			s.LeaderboardNameStyle = style
			return nil
		}
	}
	return ErrNameStyleLocked
}
