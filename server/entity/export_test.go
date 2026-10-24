package entity

import "github.com/df-mc/dragonfly/server/world"

// ForceVillageSiegeTonight makes the coming night of w a siege night, as one
// in ten VillageSiege rolls.
func ForceVillageSiegeTonight(w *world.World) {
	s := customSpawnersOf(w)
	s.siegeTonight, s.siegeRolledAt = true, int64(w.Time())-int64(w.Time())%24000+18000
}

// OminousBannerStack returns the ominous banner raiders carry.
var OminousBannerStack = ominousBannerStack
