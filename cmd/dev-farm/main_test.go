package main

import (
	"os"
	"testing"

	"github.com/mynameis-nigel/ssh-farm/internal/config"
)

func TestDevDefaultsAndOverrides(t *testing.T) {
	keys := []string{"FARM_LISTEN_HOST", "FARM_LISTEN_PORT", "FARM_DB_PATH", "FARM_HOST_KEY_PATH", "FARM_LOG_LEVEL", "FARM_DEV_MODE", "FARM_IDLE_TIMEOUT"}
	for _, key := range keys {
		t.Setenv(key, "")
		_ = os.Unsetenv(key)
	}
	setDefaults()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.DevMode || cfg.ListenHost != "127.0.0.1" || cfg.ListenPort != 2223 || cfg.DBPath != "var/dev/farm-dev.db" || cfg.HostKeyPath != "var/dev/ssh_host_key" || cfg.IdleTimeout != 0 || cfg.LogLevel != "debug" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	t.Setenv("FARM_LISTEN_PORT", "2229")
	t.Setenv("FARM_DEV_MODE", "false")
	t.Setenv("FARM_DB_PATH", "var/custom.db")
	setDefaults()
	cfg, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenPort != 2229 || cfg.DevMode || cfg.DBPath != "var/custom.db" {
		t.Fatal("overrides discarded")
	}
}
