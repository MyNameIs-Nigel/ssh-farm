package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("FARM_LISTEN_PORT", "")
	t.Setenv("FARM_DEFAULT_SLOT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenPort != 22 {
		t.Fatalf("ListenPort = %d, want 22", cfg.ListenPort)
	}
	if cfg.DefaultSlot != "default" {
		t.Fatalf("DefaultSlot = %q, want default", cfg.DefaultSlot)
	}
	if cfg.DBPath != "var/farm.db" {
		t.Fatalf("DBPath = %q, want var/farm.db", cfg.DBPath)
	}
	if cfg.LeaderboardTTL != 15*time.Second {
		t.Fatalf("LeaderboardTTL = %v, want 15s", cfg.LeaderboardTTL)
	}
	if cfg.LeaderboardActivityWindow != 90*24*time.Hour {
		t.Fatalf("LeaderboardActivityWindow = %v, want 90 days", cfg.LeaderboardActivityWindow)
	}
	if cfg.LeaderboardMinCoins != 1 {
		t.Fatalf("LeaderboardMinCoins = %d, want 1", cfg.LeaderboardMinCoins)
	}
}

func TestLoadRejectsInvalidDefaultSlot(t *testing.T) {
	t.Setenv("FARM_DEFAULT_SLOT", "!!!")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for invalid default slot")
	}
}

func TestLoadRejectsInvalidPort(t *testing.T) {
	t.Setenv("FARM_LISTEN_PORT", "not-a-port")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for invalid port")
	}
}
