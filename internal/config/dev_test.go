package config

import "testing"

func TestDevModeConfig(t *testing.T) {
	for _, value := range []string{"", "false", "true", "1", "nope"} {
		t.Setenv("FARM_DEV_MODE", value)
		cfg, err := Load()
		if value == "nope" {
			if err == nil {
				t.Fatal("invalid dev boolean accepted")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if cfg.DevMode != (value == "true" || value == "1") {
			t.Fatalf("%q: %v", value, cfg.DevMode)
		}
	}
}
