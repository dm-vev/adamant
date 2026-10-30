package particle

import (
	"image/color"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// Flame is a particle shown around torches. It can have any colour specified with the Colour field.
// If the colour is not specified, it will default to the normal flame particle.
type Flame struct {
	particle
	// Colour is the colour of the Flame particle.
	Colour color.RGBA
}

// Dust is a particle shown for redstone. It can have any colour specified with the Colour field.
// If the colour is not specified, it will default to black.
type Dust struct {
	particle

	// Colour is the colour of the Dust particle.
	Colour color.RGBA
}

// BlockBreak is a particle sent when a block is broken. It represents a bunch of particles that are textured
// like the block that the particle holds.
type BlockBreak struct {
	particle
	// Block is the block of which particles should be shown. The particles will change depending on what
	// block is held.
	Block world.Block
}

// PunchBlock is a particle shown when a player is punching a block. It shows particles of a specific block
// type at a particular face of a block.
type PunchBlock struct {
	particle
	// Block is the block of which particles should be shown. The particles will change depending on what
	// block is punched.
	Block world.Block
	// Face is the face of the block that was punched. It is here that the particles will be shown.
	Face cube.Face
}

// BlockForceField is a particle that shows up as a block that turns invisible from an opaque black colour.
type BlockForceField struct{ particle }

// DispenserSmoke is the directional puff emitted by ordinary item ejection.
type DispenserSmoke struct {
	particle
	Face cube.Face
}

// BoneMeal is a particle that shows up on bone meal usage.
type BoneMeal struct {
	particle

	// Area specifies whether the particle effect should be for area. If false,
	// a small burst is used for minor growth. If true, a large burst is used
	// for significant growth.
	Area bool
}

// Note is a particle that shows up on note block interactions.
type Note struct {
	particle

	// Instrument is the instrument of the note block.
	Instrument sound.Instrument
	// Pitch is the pitch of the note.
	Pitch int
}

// DragonEggTeleport is a particle that shows up when a dragon egg teleports.
type DragonEggTeleport struct {
	particle

	// Diff is a Position with the values being the difference from the original position to the new position.
	Diff cube.Pos
}

// Evaporate is a particle that shows up when a water block evaporates
type Evaporate struct{ particle }

// WaterDrip is a particle that shows up when there is water above a block and it looks like a dripping effect.
type WaterDrip struct{ particle }

// LavaDrip is a particle that shows up when there is lava above a block and it looks like a dripping effect.
type LavaDrip struct{ particle }

// Lava is a particle that shows up randomly above lava.
type Lava struct{ particle }

// DustPlume is a particle that shows up when an item is successfully inserted into a decorated pot.
type DustPlume struct{ particle }

// SculkCharge is a sculk charge resting on a block. Charge is the logarithmic
// particle count and Faces the multi-face bits (down, up, south, west, north,
// east) the charge covers.
type SculkCharge struct {
	particle
	Charge int
	Faces  uint8
}

// SculkChargePop is a sculk charge dissipating at a block.
type SculkChargePop struct{ particle }

// SculkSoul is a soul released by a blooming sculk catalyst.
type SculkSoul struct{ particle }

// ElectricSpark is the spark of lightning striking a lightning rod or cleaning
// copper. Data is the protocol event payload; Bedrock rod hits use zero.
type ElectricSpark struct {
	particle
	Data int32
}

// EyeblossomTransform is the trail of an eyeblossom opening or closing.
type EyeblossomTransform struct {
	particle
	Open bool
}

// DripstoneDrip is a drop falling from the tip of a pointed dripstone.
type DripstoneDrip struct{ particle }

// PointedDripstoneLand is the shattering of a falling pointed dripstone, which
// the client plays as its landing sound.
type PointedDripstoneLand struct{ particle }

// EggCrack is the burst of particles around an egg cracking on a hatch boosting
// block.
type EggCrack struct{ particle }

// CauldronEffect is a Bedrock cauldron interaction, which the client plays as
// its particles and sound. Colour is the dye or potion colour involved.
type CauldronEffect struct {
	particle
	Kind   CauldronEffectKind
	Colour color.RGBA
}

// CauldronEffectKind is the interaction a CauldronEffect shows.
type CauldronEffectKind uint8

const (
	CauldronExplode CauldronEffectKind = iota
	CauldronDyeArmour
	CauldronCleanArmour
	CauldronFillPotion
	CauldronTakePotion
	CauldronFillWater
	CauldronTakeWater
	CauldronAddDye
	CauldronCleanBanner
	CauldronFillLava
	CauldronTakeLava
	CauldronFillPowderSnow
	CauldronTakePowderSnow
)

// particle serves as a base for all particles in this package.
type particle struct{}

// Spawn ...
func (particle) Spawn(*world.World, mgl64.Vec3) {}
