package item

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// EggVariant distinguishes the temperate, cold, and warm chicken eggs.
type EggVariant uint8

const (
	EggTemperate EggVariant = iota
	EggBlue
	EggBrown
)

// Egg is a throwable item that can spawn chicks.
type Egg struct {
	Variant EggVariant
}
// MaxCount returns the egg stack limit.
func (Egg) MaxCount() int { return 16 }

// Use throws the egg and preserves its chicken variant.
func (e Egg) Use(tx *world.Tx, user User, ctx *UseContext) bool {
	config := tx.World().EntityRegistry().Config()
	if e.Variant == EggTemperate && config.Egg == nil {
		return false
	}
	opts := world.EntitySpawnOpts{Position: eyePosition(user), Velocity: user.Rotation().Vec3().Mul(1.5)}
	if e.Variant == EggTemperate {
		tx.AddEntity(config.Egg(opts, user))
	} else {
		if config.EggVariant == nil {
			return false
		}
		tx.AddEntity(config.EggVariant(opts, user, int(e.Variant)))
	}
	tx.PlaySound(user.Position(), sound.ItemThrow{})
	ctx.SubtractFromCount(1)
	return true
}

// EncodeItem returns the Bedrock item identifier.
func (e Egg) EncodeItem() (string, int16) {
	switch e.Variant {
	case EggBlue:
		return "minecraft:blue_egg", 0
	case EggBrown:
		return "minecraft:brown_egg", 0
	default:
		return "minecraft:egg", 0
	}
}
