package block

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// FrogSpawn is laid on the surface of water by frogs and hatches into
// tadpoles.
type FrogSpawn struct {
	transparent
	empty
}

// BreakInfo ...
func (FrogSpawn) BreakInfo() BreakInfo {
	return newBreakInfo(0, neverHarvestable, nothingEffective, simpleDrops())
}

// FrogSpawnSupported ports FrogspawnBlock.mayPlaceOn for frogspawn at pos:
// the block below holds water, a fluid in #supports_frogspawn, and pos
// itself holds no fluid.
func FrogSpawnSupported(tx *world.Tx, pos cube.Pos) bool {
	// FrogspawnBlock.mayPlaceOn: a water source, supports_frogspawn, below
	// and no fluid in the block itself.
	if !waterSourceAt(tx, pos.Side(cube.FaceDown)) {
		return false
	}
	_, liquid := tx.Liquid(pos)
	return !liquid
}

// placeOnWater ports PlaceOnWaterBlockItem: the block goes above the water
// source the user looks at, wherever the user clicked.
func placeOnWater(tx *world.Tx, user item.User, ctx *item.UseContext, b world.Block, supported func(*world.Tx, cube.Pos) bool) bool {
	if user == nil {
		return false
	}
	at, _, ok := item.LookAt(tx, user, true)
	if !ok {
		return false
	}
	pos, _, used := firstReplaceable(tx, at.Side(cube.FaceUp), cube.FaceUp, b)
	if !used || pos != at.Side(cube.FaceUp) || !supported(tx, pos) {
		return false
	}
	place(tx, pos, b, user, ctx)
	return placed(ctx)
}

// UseOnBlock places frogspawn on the water the user looks at.
func (f FrogSpawn) UseOnBlock(_ cube.Pos, _ cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	return placeOnWater(tx, user, ctx, f, FrogSpawnSupported)
}

// Use places frogspawn on the water the user looks at.
func (f FrogSpawn) Use(tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	return placeOnWater(tx, user, ctx, f, FrogSpawnSupported)
}

// Placed schedules the hatching 3600 to 11999 ticks later.
func (f FrogSpawn) Placed(pos cube.Pos, tx *world.Tx, _ world.Block) {
	tx.ScheduleBlockUpdate(pos, f, time.Duration(3600+rand.IntN(8400))*time.Second/20)
}

// NeighbourUpdateTick removes frogspawn that lost its water.
func (f FrogSpawn) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !FrogSpawnSupported(tx, pos) {
		breakBlockNoDrops(f, pos, tx)
	}
}

// ScheduledTick ports FrogspawnBlock.tick: frogspawn that is still on water
// hatches into 2 to 5 tadpoles below it.
func (f FrogSpawn) ScheduledTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	breakBlockNoDrops(f, pos, tx)
	if !FrogSpawnSupported(tx, pos) {
		return
	}
	tx.PlaySound(pos.Vec3Centre(), sound.FrogSpawnHatch{})
	for range 2 + r.IntN(4) {
		offset := func() float64 { return min(max(r.Float64(), 0.2), 0.8) }
		opts := world.EntitySpawnOpts{
			Position:       mgl64.Vec3{float64(pos.X()) + offset(), float64(pos.Y()) - 0.5, float64(pos.Z()) + offset()},
			Rotation:       cube.Rotation{float64(1 + r.IntN(360))},
			SpawnReason:    "BREEDING",
			SpawnFinalized: true,
		}
		if h := tx.World().EntityRegistry().Config().SpawnMob("minecraft:tadpole", opts); h != nil {
			tx.AddEntity(h)
		}
	}
}

// EntityInside ports FrogspawnBlock.entityInside: falling blocks crush
// frogspawn.
func (f FrogSpawn) EntityInside(pos cube.Pos, tx *world.Tx, e world.Entity) {
	if e.H().Type().EncodeEntity() == "minecraft:falling_block" {
		breakBlockNoDrops(f, pos, tx)
	}
}

// EncodeItem ...
func (FrogSpawn) EncodeItem() (name string, meta int16) {
	return "minecraft:frog_spawn", 0
}

// EncodeBlock ...
func (FrogSpawn) EncodeBlock() (string, map[string]any) {
	return "minecraft:frog_spawn", nil
}
