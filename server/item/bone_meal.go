package item

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
)

// BoneMealResult represents the outcome of a bone meal interaction with a block,
// determining the intensity of the particle effect displayed.
type BoneMealResult int

const (
	// BoneMealResultNone indicates that the bone meal had no effect on the block.
	BoneMealResultNone BoneMealResult = iota
	// BoneMealResultSmall indicates a minor growth effect, produces a small particle burst.
	BoneMealResultSmall
	// BoneMealResultArea indicates a significant growth effect over an area, produces a large particle burst.
	BoneMealResultArea
)

// BoneMeal is an item used to force growth in plants & crops.
type BoneMeal struct{}

// BoneMealAffected represents a block that is affected when bone meal is used on it.
type BoneMealAffected interface {
	// BoneMeal attempts to affect the block using a bone meal item.
	BoneMeal(pos cube.Pos, tx *world.Tx) BoneMealResult
}

// waterPlantGrower is a block, water, that bone meal used on the sturdy
// face next to it grows seagrass and corals in.
type waterPlantGrower interface {
	GrowWaterPlants(pos cube.Pos, face cube.Face, tx *world.Tx) bool
}

// UseOnBlock ports BoneMealItem.useOn: the clicked block grows if it takes
// bone meal, and otherwise, if the clicked face is sturdy, water beside it
// grows seagrass and corals. Either way one bone meal is used up.
func (b BoneMeal) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user User, ctx *UseContext) bool {
	if bm, ok := tx.Block(pos).(BoneMealAffected); ok {
		if result := bm.BoneMeal(pos, tx); result != BoneMealResultNone {
			ctx.SubtractFromCount(1)
			tx.AddParticle(pos.Vec3(), particle.BoneMeal{Area: result == BoneMealResultArea})
			tx.EmitGameEvent(world.GameEventItemInteractFinish, user.Position(), world.GameEventContext{Source: user})
			return true
		}
	}
	if !tx.Block(pos).Model().FaceSolid(pos, face, tx) {
		return false
	}
	beside := pos.Side(face)
	if w, ok := tx.Block(beside).(waterPlantGrower); ok && w.GrowWaterPlants(beside, face, tx) {
		ctx.SubtractFromCount(1)
		tx.AddParticle(beside.Vec3(), particle.BoneMeal{})
		tx.EmitGameEvent(world.GameEventItemInteractFinish, user.Position(), world.GameEventContext{Source: user})
		return true
	}
	return false
}

// EncodeItem ...
func (b BoneMeal) EncodeItem() (name string, meta int16) {
	return "minecraft:bone_meal", 0
}
