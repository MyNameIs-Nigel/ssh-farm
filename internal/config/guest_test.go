package config

import "testing"

func TestGuestConfigIsExplicit(t *testing.T) {
	for _, tc := range []struct {
		value      string
		want, fail bool
	}{{"", false, false}, {"true", true, false}, {"false", false, false}, {"yes", false, true}} {
		t.Setenv("FARM_ALLOW_GUESTS", tc.value)
		cfg, err := Load()
		if (err != nil) != tc.fail || (err == nil && cfg.AllowGuests != tc.want) {
			t.Fatalf("%q: %+v %v", tc.value, cfg, err)
		}
	}
}
