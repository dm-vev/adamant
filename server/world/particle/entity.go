package particle

import "image/color"

// HugeExplosion is a particle shown when TNT or a creeper explodes.
type HugeExplosion struct{ particle }

// DragonDestroyBlock is the explosion shown where an ender dragon flies
// through blocks.
type DragonDestroyBlock struct{ particle }

// EndermanTeleport is a particle that shows up when an enderman teleports.
type EndermanTeleport struct{ particle }

// SnowballPoof is a particle shown when a snowball collides with something.
type SnowballPoof struct{ particle }

// EggSmash is a particle shown when an egg smashes on something.
type EggSmash struct{ particle }

// SonicExplosion is a particle of the blast of the sonic boom of a warden.
type SonicExplosion struct{ particle }

// WindBurst is a particle shown when a wind charge detonates.
type WindBurst struct{ particle }

// MobGrowth is shown over a baby whose growth a golden dandelion paused or
// resumed.
type MobGrowth struct {
	particle
	// Paused is set when the growth was paused.
	Paused bool
}

// Splash is a particle that shows up when a splash potion is splashed.
type Splash struct {
	particle

	// Colour is the colour that should be splashed.
	Colour color.RGBA
}

// Effect is a particle that shows up around an entity when it has effects on.
type Effect struct {
	particle

	// Colour is the colour of the particle.
	Colour color.RGBA
}

// EntityFlame is a particle shown when an entity is set on fire.
type EntityFlame struct{ particle }
