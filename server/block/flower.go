package block

import (
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Flower is a non-solid plant that occur in a variety of shapes and colours. They are primarily used for decoration
// and crafted into dyes.
type Flower struct {
	empty
	transparent

	// Type is the type of flower.
	Type FlowerType
}

// EntityInside ...
func (f Flower) EntityInside(_ cube.Pos, _ *world.Tx, e world.Entity) {
	if f.Type == WitherRose() {
		if living, ok := e.(interface {
			AddEffect(effect.Effect)
		}); ok {
			living.AddEffect(effect.New(effect.Wither, 1, 2*time.Second))
		}
	}
}

// NeighbourUpdateTick ...
func (f Flower) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !f.canSurviveOn(tx.Block(pos.Side(cube.FaceDown))) {
		breakBlock(f, pos, tx)
	}
}

// UseOnBlock ...
func (f Flower) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, f)
	if !used || !f.canSurviveOn(tx.Block(pos.Side(cube.FaceDown))) {
		return false
	}

	place(tx, pos, f, user, ctx)
	return placed(ctx)
}

// canSurviveOn ports FlowerBlock.mayPlaceOn, which WitherRoseBlock extends to
// netherrack, soul sand and soul soil.
func (f Flower) canSurviveOn(below world.Block) bool {
	if f.Type == WitherRose() {
		switch below.(type) {
		case Netherrack, SoulSand, SoulSoil:
			return true
		}
	}
	return supportsVegetation(f, below)
}

// PlantWitherRose ports LivingEntity.createWitherRose for a mob killed by a
// Wither while mob griefing is enabled: a wither rose is planted at pos if
// the space is air and the rose can survive there. It reports whether the
// rose was planted.
func PlantWitherRose(tx *world.Tx, pos cube.Pos) bool {
	rose := Flower{Type: WitherRose()}
	if _, air := tx.Block(pos).(Air); !air || !rose.canSurviveOn(tx.Block(pos.Side(cube.FaceDown))) {
		return false
	}
	tx.SetBlock(pos, rose, nil)
	return true
}

// HasLiquidDrops ...
func (Flower) HasLiquidDrops() bool {
	return true
}

// FlammabilityInfo ...
func (f Flower) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(60, 100, false)
}

// BreakInfo ...
func (f Flower) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(f))
}

// EncodeItem ...
func (f Flower) EncodeItem() (name string, meta int16) {
	return "minecraft:" + f.Type.String(), 0
}

// EncodeBlock ...
func (f Flower) EncodeBlock() (string, map[string]any) {
	return "minecraft:" + f.Type.String(), nil
}

// allFlowers ...
func allFlowers() (b []world.Block) {
	for _, f := range FlowerTypes() {
		b = append(b, Flower{Type: f})
	}
	return
}
