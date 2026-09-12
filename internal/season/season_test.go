package season

import (
	"testing"
	"time"
)

func date(y, m, d int) time.Time {
	return time.Date(y, time.Month(m), d, 12, 0, 0, 0, time.UTC)
}

func TestAtCoversBothWindows(t *testing.T) {
	cases := []struct {
		name string
		t    time.Time
		want Season
	}{
		{"mid september is plain", date(2024, 9, 15), SeasonNone},
		{"sep 30 is plain", date(2024, 9, 30), SeasonNone},
		{"oct 1 opens halloween", date(2024, 10, 1), SeasonHalloween},
		{"mid october is halloween", date(2024, 10, 15), SeasonHalloween},
		{"oct 31 is still halloween", date(2024, 10, 31), SeasonHalloween},
		{"nov 1 closes halloween", date(2024, 11, 1), SeasonNone},
		{"nov 24 is plain", date(2024, 11, 24), SeasonNone},
		{"nov 25 opens christmas", date(2024, 11, 25), SeasonChristmas},
		{"mid december is christmas", date(2024, 12, 10), SeasonChristmas},
		{"dec 25 is still christmas", date(2024, 12, 25), SeasonChristmas},
		{"dec 26 closes christmas", date(2024, 12, 26), SeasonNone},
		{"january is plain", date(2025, 1, 5), SeasonNone},
		{"leap day is plain", date(2024, 2, 29), SeasonNone},
		{"halloween repeats yearly", date(2031, 10, 31), SeasonHalloween},
		{"christmas repeats yearly", date(2031, 12, 25), SeasonChristmas},
	}
	for _, c := range cases {
		if got := At(c.t); got != c.want {
			t.Errorf("%s: At(%v) = %v, want %v", c.name, c.t.Format("2006-01-02"), got, c.want)
		}
		if got := AtUnix(c.t.Unix()); got != c.want {
			t.Errorf("%s: AtUnix(%d) = %v, want %v", c.name, c.t.Unix(), got, c.want)
		}
	}
}

func TestActiveAtUnixHonorsDevelopmentOverride(t *testing.T) {
	now := date(2024, 9, 15).Unix()

	t.Setenv("FARM_DEV_SEASON", "halloween")
	if got := ActiveAtUnix(now); got != SeasonHalloween {
		t.Fatalf("halloween override = %v, want %v", got, SeasonHalloween)
	}

	t.Setenv("FARM_DEV_SEASON", " Christmas ")
	if got := ActiveAtUnix(now); got != SeasonChristmas {
		t.Fatalf("christmas override = %v, want %v", got, SeasonChristmas)
	}

	t.Setenv("FARM_DEV_SEASON", "spring")
	if got := ActiveAtUnix(now); got != SeasonNone {
		t.Fatalf("unknown override = %v, want calendar result %v", got, SeasonNone)
	}
}

func TestSpecialDayIsOnlyTheTwoShowpieces(t *testing.T) {
	for _, d := range []time.Time{date(2024, 10, 31), date(2030, 10, 31)} {
		if !SpecialDay(d) {
			t.Errorf("SpecialDay(%v) = false, want true (big moon day)", d.Format("2006-01-02"))
		}
	}
	for _, d := range []time.Time{date(2024, 12, 25), date(2030, 12, 25)} {
		if !SpecialDay(d) {
			t.Errorf("SpecialDay(%v) = false, want true (big star day)", d.Format("2006-01-02"))
		}
	}
	for _, d := range []time.Time{
		date(2024, 10, 30), date(2024, 12, 24), date(2024, 12, 26),
		date(2024, 11, 25), date(2024, 7, 4),
	} {
		if SpecialDay(d) {
			t.Errorf("SpecialDay(%v) = true, want false", d.Format("2006-01-02"))
		}
		if SpecialDayUnix(d.Unix()) {
			t.Errorf("SpecialDayUnix(%d) = true, want false", d.Unix())
		}
	}
}

func TestSeasonLabels(t *testing.T) {
	if SeasonNone.Name() != "" || SeasonNone.Glyph() != "" || SeasonNone.Key() != "" {
		t.Fatal("SeasonNone must have empty labels")
	}
	if SeasonHalloween.Name() != "Halloween" || SeasonHalloween.Glyph() != "🎃" || SeasonHalloween.Key() != "halloween" {
		t.Fatal("halloween labels wrong")
	}
	if SeasonChristmas.Name() != "Christmas" || SeasonChristmas.Glyph() != "🎄" || SeasonChristmas.Key() != "christmas" {
		t.Fatal("christmas labels wrong")
	}
	if !ValidKey("halloween") || !ValidKey("christmas") {
		t.Fatal("ValidKey rejects a real season")
	}
	if ValidKey("") || ValidKey("easter") {
		t.Fatal("ValidKey accepts a non-season")
	}
}
