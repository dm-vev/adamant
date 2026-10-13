package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Torchflower is a flower grown from torchflower seeds dug up by sniffers.
type Torchflower struct {
	transparent
	empty
}

// HasLiquidDrops ...
func (Torchflower) HasLiquidDrops() bool { return true }

// FlammabilityInfo ...
func (Torchflower) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(60, 100, false)
}

// BreakInfo ...
func (t Torchflower) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(t))
}

// CompostChance ...
func (Torchflower) CompostChance() float64 { return 0.85 }

// UseOnBlock places the flower on soil.
func (t Torchflower) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, t)
	if !used || !supportsVegetation(t, tx.Block(pos.Side(cube.FaceDown))) {
		return false
	}
	place(tx, pos, t, user, ctx)
	return placed(ctx)
}

// NeighbourUpdateTick breaks the flower without soil below.
func (t Torchflower) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !supportsVegetation(t, tx.Block(pos.Side(cube.FaceDown))) {
		breakBlock(t, pos, tx)
	}
}

// EncodeItem ...
func (Torchflower) EncodeItem() (name string, meta int16) {
	return "minecraft:torchflower", 0
}

// EncodeBlock ...
func (Torchflower) EncodeBlock() (string, map[string]any) {
	return "minecraft:torchflower", nil
}

// cropSurvives ports CropBlock.canSurvive: farmland below and a light level of
// at least 8 or a view of the sky.
func cropSurvives(pos cube.Pos, tx *world.Tx) bool {
	if _, ok := tx.Block(pos.Side(cube.FaceDown)).(Farmland); !ok {
		return false
	}
	return tx.Light(pos) >= 8 || tx.HighestLightBlocker(pos.X(), pos.Z()) < pos.Y()
}

// cropGrowthRoll ports the random tick roll of CropBlock and PitcherCropBlock:
// one in floor(25/speed)+1.
func cropGrowthRoll(r *rand.Rand, speed float64) bool {
	return r.IntN(int(25/speed)+1) == 0
}

// trampledByRavager ports CropBlock.entityInside: ravagers destroy crops they
// walk through while mob_griefing is on.
func trampledByRavager(b world.Block, pos cube.Pos, tx *world.Tx, e world.Entity) {
	if e.H().Type().EncodeEntity() == "minecraft:ravager" && tx.World().GameRuleBool("mob_griefing") {
		breakBlock(b, pos, tx)
	}
}

// TorchflowerCrop is torchflower seeds planted on farmland. After two growth
// stages it turns into a torchflower.
type TorchflowerCrop struct {
	transparent
	empty

	// Growth is the growth stage, 0 or 1.
	Growth int
}

// HasLiquidDrops ...
func (TorchflowerCrop) HasLiquidDrops() bool { return true }

// BreakInfo ...
func (c TorchflowerCrop) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(TorchflowerCrop{}))
}

// CompostChance ...
func (TorchflowerCrop) CompostChance() float64 { return 0.3 }

// UseOnBlock plants the seeds on farmland.
func (c TorchflowerCrop) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, c)
	if !used || !cropSurvives(pos, tx) {
		return false
	}
	place(tx, pos, TorchflowerCrop{}, user, ctx)
	return placed(ctx)
}

// NeighbourUpdateTick ...
func (c TorchflowerCrop) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !cropSurvives(pos, tx) {
		breakBlock(c, pos, tx)
	}
}

// EntityInside ...
func (c TorchflowerCrop) EntityInside(pos cube.Pos, tx *world.Tx, e world.Entity) {
	trampledByRavager(c, pos, tx, e)
}

// RandomTick ports TorchflowerCropBlock.randomTick: two random ticks in
// three run CropBlock.randomTick, which grows the crop in light of at least 9.
func (c TorchflowerCrop) RandomTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	if r.IntN(3) == 0 || tx.Light(pos) < 9 {
		return
	}
	speed := cropGrowthSpeed(pos, tx, func(b world.Block) bool { _, ok := b.(TorchflowerCrop); return ok })
	if cropGrowthRoll(r, speed) {
		c.grow(pos, tx)
	}
}

// BoneMeal advances the crop one stage.
func (c TorchflowerCrop) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	c.grow(pos, tx)
	return item.BoneMealResultSmall
}

// grow advances one stage, turning the grown crop into a torchflower.
func (c TorchflowerCrop) grow(pos cube.Pos, tx *world.Tx) {
	if c.Growth >= 1 {
		tx.SetBlock(pos, Torchflower{}, nil)
		return
	}
	c.Growth++
	tx.SetBlock(pos, c, nil)
}

// EncodeItem ...
func (TorchflowerCrop) EncodeItem() (name string, meta int16) {
	return "minecraft:torchflower_seeds", 0
}

// EncodeBlock ...
func (c TorchflowerCrop) EncodeBlock() (string, map[string]any) {
	return "minecraft:torchflower_crop", map[string]any{"growth": int32(c.Growth)}
}

// PitcherCrop is a pitcher pod planted on farmland. It grows through five
// stages, reaching into a second block from the fourth.
type PitcherCrop struct {
	transparent

	// Growth is the growth stage, from 0 to 4.
	Growth int
	// UpperPart is set for the top half of a tall crop.
	UpperPart bool
}

// Model ports PitcherCropBlock.getCollisionShape: a bulb, then a wider crop,
// both reaching a pixel into the farmland. The top half has no collision.
func (c PitcherCrop) Model() world.BlockModel { return pitcherCropModel{c} }

// HasLiquidDrops ...
func (PitcherCrop) HasLiquidDrops() bool { return true }

// BreakInfo ports the pitcher crop loot table: a pitcher pod, or a pitcher
// plant when fully grown. Either half drops it, as the other half then breaks
// without drops.
func (c PitcherCrop) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, func(item.Tool, []item.Enchantment) []item.Stack {
		if c.Growth >= 4 {
			return []item.Stack{item.NewStack(PitcherPlant{}, 1)}
		}
		return []item.Stack{item.NewStack(PitcherCrop{}, 1)}
	})
}

// CompostChance ...
func (PitcherCrop) CompostChance() float64 { return 0.3 }

// UseOnBlock plants the pod on farmland.
func (c PitcherCrop) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, c)
	if !used || !pitcherCropSurvives(PitcherCrop{}, pos, tx) {
		return false
	}
	place(tx, pos, PitcherCrop{}, user, ctx)
	return placed(ctx)
}

// pitcherCropSurvives ports PitcherCropBlock.canSurvive: the lower half needs
// light of at least 8 and farmland, the upper half the lower half below.
func pitcherCropSurvives(c PitcherCrop, pos cube.Pos, tx *world.Tx) bool {
	if c.UpperPart {
		lower, ok := tx.Block(pos.Side(cube.FaceDown)).(PitcherCrop)
		return ok && !lower.UpperPart
	}
	_, farmland := tx.Block(pos.Side(cube.FaceDown)).(Farmland)
	return farmland && tx.Light(pos) >= 8
}

// NeighbourUpdateTick ports PitcherCropBlock.updateShape: a tall crop breaks
// with its other half or support, a short one only without support.
func (c PitcherCrop) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if c.Growth >= 3 && !c.UpperPart {
		if upper, ok := tx.Block(pos.Side(cube.FaceUp)).(PitcherCrop); !ok || !upper.UpperPart {
			breakBlockNoDrops(c, pos, tx)
			return
		}
	}
	if !pitcherCropSurvives(c, pos, tx) {
		if c.UpperPart {
			breakBlockNoDrops(c, pos, tx)
			return
		}
		breakBlock(c, pos, tx)
	}
}

// EntityInside ...
func (c PitcherCrop) EntityInside(pos cube.Pos, tx *world.Tx, e world.Entity) {
	trampledByRavager(c, pos, tx, e)
}

// RandomTick ports PitcherCropBlock.randomTick for the growing lower half.
func (c PitcherCrop) RandomTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	if c.UpperPart || c.Growth >= 4 {
		return
	}
	speed := cropGrowthSpeed(pos, tx, func(b world.Block) bool { _, ok := b.(PitcherCrop); return ok })
	if cropGrowthRoll(r, speed) {
		c.grow(pos, tx)
	}
}

// BoneMeal grows the crop one stage from either half.
func (c PitcherCrop) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	if c.UpperPart {
		pos = pos.Side(cube.FaceDown)
		lower, ok := tx.Block(pos).(PitcherCrop)
		if !ok || lower.UpperPart {
			return item.BoneMealResultNone
		}
		c = lower
	}
	if !c.canGrow(pos, tx, c.Growth+1) {
		return item.BoneMealResultNone
	}
	c.grow(pos, tx)
	return item.BoneMealResultSmall
}

// canGrow ports PitcherCropBlock.canGrow: a crop below full growth in light of
// at least 8, with air or its own top half above once it grows tall.
func (c PitcherCrop) canGrow(pos cube.Pos, tx *world.Tx, growth int) bool {
	above := pos.Side(cube.FaceUp)
	if c.Growth >= 4 || tx.Light(pos) < 8 || above.OutOfBounds(tx.Range()) {
		return false
	}
	if growth < 3 {
		return true
	}
	switch tx.Block(above).(type) {
	case Air, PitcherCrop:
		return true
	}
	return false
}

// grow ports PitcherCropBlock.grow by one stage.
func (c PitcherCrop) grow(pos cube.Pos, tx *world.Tx) {
	growth := min(c.Growth+1, 4)
	if !c.canGrow(pos, tx, growth) {
		return
	}
	c.Growth = growth
	tx.SetBlock(pos, c, &world.SetOpts{DisableBlockUpdates: true})
	if growth >= 3 {
		tx.SetBlock(pos.Side(cube.FaceUp), PitcherCrop{Growth: growth, UpperPart: true}, nil)
	}
}

// EncodeItem ...
func (PitcherCrop) EncodeItem() (name string, meta int16) {
	return "minecraft:pitcher_pod", 0
}

// EncodeBlock ...
func (c PitcherCrop) EncodeBlock() (string, map[string]any) {
	return "minecraft:pitcher_crop", map[string]any{"growth": int32(c.Growth), "upper_block_bit": c.UpperPart}
}

// pitcherCropModel is the collision of a pitcher crop.
type pitcherCropModel struct{ c PitcherCrop }

// BBox ...
func (m pitcherCropModel) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	switch {
	case m.c.UpperPart:
		return nil
	case m.c.Growth == 0:
		return []cube.BBox{cube.Box(5.0/16, -1.0/16, 5.0/16, 11.0/16, 3.0/16, 11.0/16)}
	}
	return []cube.BBox{cube.Box(3.0/16, -1.0/16, 3.0/16, 13.0/16, 5.0/16, 13.0/16)}
}

// FaceSolid ...
func (pitcherCropModel) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool { return false }

// PitcherPlant is the two block tall flower of a fully grown pitcher crop.
type PitcherPlant struct {
	transparent
	empty

	// UpperPart is set for the top half of the plant.
	UpperPart bool
}

// HasLiquidDrops ...
func (PitcherPlant) HasLiquidDrops() bool { return true }

// FlammabilityInfo ...
func (PitcherPlant) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(60, 100, true)
}

// BreakInfo drops the plant from either half, as the other half then breaks
// without drops.
func (p PitcherPlant) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(PitcherPlant{}))
}

// CompostChance ...
func (PitcherPlant) CompostChance() float64 { return 0.85 }

// UseOnBlock places both halves on soil.
func (p PitcherPlant) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, p)
	if !used || !replaceableWith(tx, pos.Side(cube.FaceUp), p) || !supportsVegetation(p, tx.Block(pos.Side(cube.FaceDown))) {
		return false
	}
	place(tx, pos, PitcherPlant{}, user, ctx)
	place(tx, pos.Side(cube.FaceUp), PitcherPlant{UpperPart: true}, user, ctx)
	return placed(ctx)
}

// NeighbourUpdateTick ports DoublePlantBlock.updateShape.
func (p PitcherPlant) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if p.UpperPart {
		if lower, ok := tx.Block(pos.Side(cube.FaceDown)).(PitcherPlant); !ok || lower.UpperPart {
			breakBlockNoDrops(p, pos, tx)
		}
		return
	}
	if upper, ok := tx.Block(pos.Side(cube.FaceUp)).(PitcherPlant); !ok || !upper.UpperPart {
		breakBlockNoDrops(p, pos, tx)
		return
	}
	if !supportsVegetation(p, tx.Block(pos.Side(cube.FaceDown))) {
		breakBlock(p, pos, tx)
	}
}

// EncodeItem ...
func (PitcherPlant) EncodeItem() (name string, meta int16) {
	return "minecraft:pitcher_plant", 0
}

// EncodeBlock ...
func (p PitcherPlant) EncodeBlock() (string, map[string]any) {
	return "minecraft:pitcher_plant", map[string]any{"upper_block_bit": p.UpperPart}
}

// allSnifferPlants returns every torchflower crop, pitcher crop and pitcher
// plant state.
func allSnifferPlants() (b []world.Block) {
	b = append(b, TorchflowerCrop{}, TorchflowerCrop{Growth: 1}, PitcherPlant{}, PitcherPlant{UpperPart: true})
	for growth := range 5 {
		b = append(b, PitcherCrop{Growth: growth}, PitcherCrop{Growth: growth, UpperPart: true})
	}
	return
}
