package block

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// Eyeblossom is a pale garden flower that opens at night and closes by day.
type Eyeblossom struct {
	empty
	transparent

	Open bool
}

// RandomTick ports EyeblossomBlock.randomTick: the flower switches to the
// state of the time of day and nearby eyeblossoms follow shortly after.
func (e Eyeblossom) RandomTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	if e.transform(pos, tx, r) {
		tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: eyeblossomSound(!e.Open, true), Volume: 1, Pitch: 1})
	}
}

// ScheduledTick ports EyeblossomBlock.tick, triggered by a neighbouring
// eyeblossom switching.
func (e Eyeblossom) ScheduledTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	if e.transform(pos, tx, r) {
		tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: eyeblossomSound(!e.Open, false), Volume: 1, Pitch: 1})
	}
}

// transform ports tryChangingState: it switches the flower when the time of
// day disagrees with it and schedules matching eyeblossoms within three
// blocks to follow after 5 to 10 ticks per block of distance.
func (e Eyeblossom) transform(pos cube.Pos, tx *world.Tx, r *rand.Rand) bool {
	open, ok := eyeblossomOpen(tx)
	if !ok || open == e.Open {
		return false
	}
	tx.SetBlock(pos, Eyeblossom{Open: open}, nil)
	tx.EmitGameEvent(world.GameEventBlockChange, pos.Vec3Centre(), world.GameEventContext{AffectedState: e})
	tx.AddParticle(pos.Vec3Centre(), particle.EyeblossomTransform{Open: open})
	for x := -3; x <= 3; x++ {
		for y := -2; y <= 2; y++ {
			for z := -3; z <= 3; z++ {
				nearby := pos.Add(cube.Pos{x, y, z})
				if b, ok := tx.Block(nearby).(Eyeblossom); !ok || b != e {
					continue
				}
				distance := math.Sqrt(float64(x*x + y*y + z*z))
				lo, hi := int(distance*5), int(distance*10)
				tx.ScheduleBlockUpdate(nearby, e, time.Duration(lo+r.IntN(hi-lo+1))*time.Second/20)
			}
		}
	}
	return true
}

// eyeblossomOpen ports the EYEBLOSSOM_OPEN attribute of the Overworld day
// timeline: open from tick 12600 until 23401. It is undefined in dimensions
// without a day cycle.
func eyeblossomOpen(tx *world.Tx) (open, defined bool) {
	if tx.World().Dimension() != world.Overworld {
		return false, false
	}
	t := tx.World().Time() % world.TimeFull
	if t < 0 {
		t += world.TimeFull
	}
	return t >= 12600 && t < 23401, true
}

// eyeblossomSound returns the sound of an eyeblossom switching to open.
func eyeblossomSound(open, long bool) string {
	name := "block.eyeblossom.close"
	if open {
		name = "block.eyeblossom.open"
	}
	if long {
		name += "_long"
	}
	return name
}

// NeighbourUpdateTick ...
func (e Eyeblossom) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !supportsVegetation(e, tx.Block(pos.Side(cube.FaceDown))) {
		breakBlock(e, pos, tx)
	}
}

// UseOnBlock ...
func (e Eyeblossom) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, e)
	if !used || !supportsVegetation(e, tx.Block(pos.Side(cube.FaceDown))) {
		return false
	}

	place(tx, pos, e, user, ctx)
	return placed(ctx)
}

// HasLiquidDrops ...
func (Eyeblossom) HasLiquidDrops() bool {
	return true
}

// FlammabilityInfo ...
func (Eyeblossom) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(60, 100, false)
}

// BreakInfo ...
func (e Eyeblossom) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(e))
}

// CompostChance ...
func (Eyeblossom) CompostChance() float64 {
	return 0.65
}

// EncodeItem ...
func (e Eyeblossom) EncodeItem() (name string, meta int16) {
	if e.Open {
		return "minecraft:open_eyeblossom", 0
	}
	return "minecraft:closed_eyeblossom", 0
}

// EncodeBlock ...
func (e Eyeblossom) EncodeBlock() (string, map[string]any) {
	if e.Open {
		return "minecraft:open_eyeblossom", nil
	}
	return "minecraft:closed_eyeblossom", nil
}
