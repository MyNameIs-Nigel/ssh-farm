package sim

import (
	"github.com/mynameis-nigel/ssh-farm/internal/content"
	"github.com/mynameis-nigel/ssh-farm/internal/season"
)

// SeasonalPlantable reports whether a crop's unlock gate passes the festival
// calendar at now (a wall-clock unix timestamp). Non-seasonal unlocks always
// pass — the calendar only closes the door on `kind = "season"` crops whose
// festival is not active. It takes now as a parameter, like every other sim
// time input, so the engine never reads a clock.
func SeasonalPlantable(u content.Unlock, now int64) bool {
	if u.Kind != "season" {
		return true
	}
	return season.AtUnix(now).Key() == u.Season
}

// VisibleCropsAt returns the crops that should appear in pickers and
// catalogs at now: VisibleCrops' prestige preview plus the festival filter.
// Out-of-season seeds are hidden (they cannot be planted), while in-season
// seeds show whether or not the player enabled the festival look — seeds
// answer to the calendar, not the toggle.
func VisibleCropsAt(s *State, c *content.Content, now int64) []content.Crop {
	var out []content.Crop
	for _, crop := range VisibleCrops(s, c) {
		if !SeasonalPlantable(crop.Unlock, now) {
			continue
		}
		out = append(out, crop)
	}
	return out
}
