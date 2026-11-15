package sim

import (
	"errors"
	"strings"
	"testing"
)

// Contract 3 unlocks the wave for every colour (issue #59), not only purple.
// Purple's wave keeps its original ID so saves and board rows that already
// hold it need no migration.
func TestEveryColourHasAWaveUnlockedByContractThree(t *testing.T) {
	colours := []string{NameStyleLeaf, NameStyleGold, NameStyleSky, NameStyleRose, NameStyleViolet}
	s := &State{ContractsCompleted: 2}
	for _, colour := range colours {
		wave := NameStyleWave(colour)
		if wave == "" || wave == colour || !IsWaveNameStyle(wave) || IsWaveNameStyle(colour) {
			t.Fatalf("%s: wave id %q is not a distinct wave style", colour, wave)
		}
		if NameStyleBase(wave) != colour || NameStyleBase(colour) != colour {
			t.Fatalf("%s: base of %q = %q", colour, wave, NameStyleBase(wave))
		}
		if err := SetLeaderboardNameStyle(s, wave); !errors.Is(err, ErrNameStyleLocked) {
			t.Fatalf("%s before contract 3 = %v, want ErrNameStyleLocked", wave, err)
		}
	}
	if NameStyleWave(NameStyleViolet) != NameStylePurpleWave {
		t.Fatalf("violet wave = %q, want the stored id %q", NameStyleWave(NameStyleViolet), NameStylePurpleWave)
	}
	if NameStyleWave(NameStyleTraditional) != "" || IsWaveNameStyle(NameStyleTraditional) {
		t.Fatal("the traditional style has no colour to wave")
	}
	s.ContractsCompleted = 3
	for _, colour := range colours {
		if err := SetLeaderboardNameStyle(s, NameStyleWave(colour)); err != nil {
			t.Fatalf("%s wave after contract 3: %v", colour, err)
		}
	}
}

// cycleStyles presses the Stats screen's "l" n times through the sim.
func cycleStyles(t *testing.T, s *State, n int) string {
	t.Helper()
	out := make([]string, 0, n)
	for range n {
		if err := SetLeaderboardNameStyle(s, s.NextLeaderboardNameStyle()); err != nil {
			t.Fatal(err)
		}
		out = append(out, s.LeaderboardNameStyle)
	}
	return strings.Join(out, ",")
}

func TestStyleCycleBeforeTheWaveIsUnchanged(t *testing.T) {
	s := &State{ContractsCompleted: 2}
	if got, want := cycleStyles(t, s, 6), "leaf,gold,sky,rose,violet,"; got != want {
		t.Fatalf("cycle = %s, want %s", got, want)
	}
	if s.LeaderboardWaveOff {
		t.Fatal("picking colours before contract 3 must not record a wave choice")
	}
}

// With three contracts the wave is on by default. The colour cycle keeps the
// player's wave choice, including across the colourless traditional stop,
// where the choice is remembered rather than shown.
func TestStyleCycleKeepsTheWaveChoice(t *testing.T) {
	s := &State{ContractsCompleted: 3}
	if got, want := cycleStyles(t, s, 7), "leaf_wave,gold_wave,sky_wave,rose_wave,purple_wave,,leaf_wave"; got != want {
		t.Fatalf("wave cycle = %s, want %s", got, want)
	}
	if err := ToggleLeaderboardNameWave(s); err != nil {
		t.Fatal(err)
	}
	if s.LeaderboardNameStyle != NameStyleLeaf {
		t.Fatalf("toggle off leaf wave = %q, want leaf", s.LeaderboardNameStyle)
	}
	if got, want := cycleStyles(t, s, 6), "gold,sky,rose,violet,,leaf"; got != want {
		t.Fatalf("static cycle = %s, want %s", got, want)
	}
}

// "If you have 3 contracts completed, you get the wave": finishing the third
// contract on a static colour turns that colour's wave on, so L is the
// opt-out the issue describes. Traditional stays traditional.
func TestCompletingContractThreeTurnsTheWaveOn(t *testing.T) {
	for style, want := range map[string]string{
		NameStyleGold:        NameStyleGoldWave,
		NameStyleViolet:      NameStylePurpleWave,
		NameStyleTraditional: NameStyleTraditional,
	} {
		s := &State{ContractsCompleted: 2, ActiveContract: ContractClockworkDenied, ContractRebirths: contractClockworkRebirths, LeaderboardNameStyle: style}
		if !s.checkContractCompletion() {
			t.Fatalf("%s: fixture did not complete Clockwork Denied", style)
		}
		if s.LeaderboardNameStyle != want || s.LeaderboardWaveOff {
			t.Fatalf("finishing contract 3 on %q = %q off=%v, want %q", style, s.LeaderboardNameStyle, s.LeaderboardWaveOff, want)
		}
	}
	// Completing an earlier contract leaves the style alone.
	s := &State{ContractsCompleted: 1, ActiveContract: ContractLeanSeason, ContractStarseeds: contractLeanSeasonStarseeds, LeaderboardNameStyle: NameStyleTraditional}
	if !s.checkContractCompletion() || s.LeaderboardNameStyle != NameStyleTraditional {
		t.Fatalf("contract 2 changed the style to %q", s.LeaderboardNameStyle)
	}
}

func TestToggleNameWave(t *testing.T) {
	s := &State{ContractsCompleted: 2, LeaderboardNameStyle: NameStyleGold}
	if err := ToggleLeaderboardNameWave(s); !errors.Is(err, ErrNameStyleLocked) {
		t.Fatalf("toggle before contract 3 = %v, want ErrNameStyleLocked", err)
	}
	if s.LeaderboardNameStyle != NameStyleGold || s.LeaderboardWaveOff {
		t.Fatalf("a refused toggle changed state: %q off=%v", s.LeaderboardNameStyle, s.LeaderboardWaveOff)
	}

	// A save that picked a static colour before this change keeps it, and
	// the first L turns its wave on rather than flipping a hidden flag.
	s.ContractsCompleted = 3
	if err := ToggleLeaderboardNameWave(s); err != nil || s.LeaderboardNameStyle != "gold_wave" {
		t.Fatalf("toggle static gold = %q, %v; want gold_wave", s.LeaderboardNameStyle, err)
	}
	if err := ToggleLeaderboardNameWave(s); err != nil || s.LeaderboardNameStyle != NameStyleGold || !s.LeaderboardWaveOff {
		t.Fatalf("toggle gold wave = %q off=%v, %v; want static gold, off", s.LeaderboardNameStyle, s.LeaderboardWaveOff, err)
	}

	// Traditional has nothing to animate: L only records the choice for the
	// next colour.
	s.LeaderboardNameStyle = NameStyleTraditional
	if err := ToggleLeaderboardNameWave(s); err != nil || s.LeaderboardNameStyle != NameStyleTraditional || s.LeaderboardWaveOff {
		t.Fatalf("toggle on traditional = %q off=%v, %v", s.LeaderboardNameStyle, s.LeaderboardWaveOff, err)
	}
	if next := s.NextLeaderboardNameStyle(); next != "leaf_wave" {
		t.Fatalf("next after turning the wave on = %q, want leaf_wave", next)
	}
}
