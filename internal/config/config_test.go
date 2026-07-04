package config

import "testing"

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
