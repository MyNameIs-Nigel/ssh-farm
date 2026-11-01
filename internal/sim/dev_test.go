package sim

import (
	"errors"
	"math"
	"reflect"
	"testing"
)

func TestDevBalancesAndProgression(t *testing.T) {
	c := testContent(t)
	s := New(c, 1, 1000)
	for _, action := range []string{"coins", "starseeds", "earnings", "rebirths"} {
		before := s.Clone()
		if err := DevApply(s, c, action, -1, 1000); err == nil {
			t.Fatalf("%s accepted negative", action)
		}
		if !reflect.DeepEqual(s, before) {
			t.Fatalf("%s mutated on error", action)
		}
		if err := DevApply(s, c, action, math.MaxInt64, 1000); err != nil {
			t.Fatal(err)
		}
		if err := DevApply(s, c, action, 0, 1000); err != nil {
			t.Fatal(err)
		}
	}
	if s.LifetimeEarnings != math.MaxInt64 || s.Coins != 0 || s.RunEarnings != 0 || s.PrestigeCurrency != 0 || s.Rebirths != 0 {
		t.Fatalf("unexpected counters: %+v", s)
	}
	if s.ContractEarnings != 0 || s.ContractStarseeds != 0 || s.ContractRebirths != 0 {
		t.Fatal("debug edits awarded contract progress")
	}
	before := s.Clone()
	if err := DevApply(s, c, "unknown", 1, 1000); err == nil {
		t.Fatal("unknown action accepted")
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatal("unknown action changed state")
	}
}

func TestDevFixturesUseNormalGameplay(t *testing.T) {
	c := testContent(t)
	s := New(c, 1, 1000)
	if err := Plant(s, c, 0, "turnip", 1000); err != nil {
		t.Fatal(err)
	}
	if err := DevApply(s, c, "mature", 0, 1000); err != nil {
		t.Fatal(err)
	}
	if !s.PlotReady(c, 0, 1000) {
		t.Fatal("crop not ready")
	}
	if _, err := Harvest(s, c, 0, 1000); err != nil {
		t.Fatal(err)
	}
	if err := DevApply(s, c, "gift", 0, 1000); err != nil {
		t.Fatal(err)
	}
	if !s.GiftPending || s.GiftArrivedAt != 1000 {
		t.Fatal("no gift")
	}
	if _, err := RedeemGift(s, c); err != nil {
		t.Fatal(err)
	}
	if err := DevApply(s, c, "contracts", 0, 1000); err != nil {
		t.Fatal(err)
	}
	if err := StartContract(s, c, ContractBareHands, 1000); err != nil {
		t.Fatal(err)
	}
	if s.ContractsCompleted != 0 {
		t.Fatal("unlock granted seals")
	}
}

func TestDevEventsAndContractRestrictions(t *testing.T) {
	c := testContent(t)
	s := New(c, 1, 1000)
	for i, event := range c.Events {
		if err := DevApply(s, c, "event", int64(i+1), 1000); err != nil {
			t.Fatal(err)
		}
		if s.EventID != event.ID || s.EventStartedAt != 1000 || s.EventEndsAt <= 1000 {
			t.Fatal("bad event fixture")
		}
	}
	before := s.Clone()
	for _, value := range []int64{-1, int64(len(c.Events) + 1)} {
		if err := DevApply(s, c, "event", value, 1000); err == nil {
			t.Fatal("invalid event accepted")
		}
		if !reflect.DeepEqual(s, before) {
			t.Fatal("invalid event mutated state")
		}
	}
	if err := DevApply(s, c, "event", 0, 1000); err != nil {
		t.Fatal(err)
	}
	if s.EventID != "" || s.EventEndsAt != 0 || s.EventStartedAt != 0 {
		t.Fatal("event not cleared")
	}
	s.ActiveContract = ContractClockworkDenied
	for _, action := range []string{"gift", "event"} {
		if err := DevApply(s, c, action, 1, 1000); !errors.Is(err, ErrContractRestricted) {
			t.Fatalf("%s: %v", action, err)
		}
	}
}
