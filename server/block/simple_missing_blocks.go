package block

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// HoneyBlock is a sticky block made from honey bottles.
type HoneyBlock struct {
	transparent
	solid
}

// Model returns the native collision shape used for entities and piston moves.
func (HoneyBlock) Model() world.BlockModel { return model.Honey{} }

// Friction ...
func (HoneyBlock) Friction() float64 {
	return 0.8
}

// EntityLand ports HoneyBlock.fallOn: landing entities take a fifth of the
// fall damage.
func (HoneyBlock) EntityLand(_ cube.Pos, _ *world.Tx, e world.Entity, distance *float64) {
	scaleFallDamage(e, distance, 0.2)
}

// EntityStepOn follows HoneyBlock::onStandOn separately from inside contacts.
func (HoneyBlock) EntityStepOn(_ cube.Pos, _ *world.Tx, e world.Entity) {
	if v, ok := e.(velocityEntity); ok {
		sneaking := false
		if s, ok := e.(interface{ Sneaking() bool }); ok {
			sneaking = s.Sneaking()
		}
		v.SetVelocity(honeyStandVelocity(v.Velocity(), sneaking))
	}
}

func honeyStandVelocity(velocity mgl64.Vec3, sneaking bool) mgl64.Vec3 {
	if sneaking || float32(velocity[1]) >= float32(.1) {
		return velocity
	}
	factor := float32(math.Abs(float64(float32(velocity[1]))))*float32(.2) + float32(.4)
	velocity[0] = float64(float32(velocity[0]) * factor)
	velocity[2] = float64(float32(velocity[2]) * factor)
	return velocity
}

// EntityInside ...
func (HoneyBlock) EntityInside(pos cube.Pos, _ *world.Tx, e world.Entity) {
	v, ok := e.(velocityEntity)
	if !ok {
		return
	}
	origin := e.Position()
	if offset, ok := e.H().Type().(interface{ NetworkOffset() float64 }); ok {
		origin[1] += offset.NetworkOffset()
	}
	width := e.H().Type().BBox(e).Width()
	if size, ok := e.H().Type().(interface{ CollisionWidth() float64 }); ok {
		width = size.CollisionWidth()
	}
	velocity, resetFall := honeyContactVelocity(pos, origin, v.Velocity(), width)
	v.SetVelocity(velocity)
	if fall, ok := e.(interface{ ResetFallDistance() }); ok && resetFall {
		fall.ResetFallDistance()
	}
}

// honeyContactVelocity follows the original client's honey-contact leaf at
// 0x63af270. Edge eligibility controls fall-distance reset, not the damping.
func honeyContactVelocity(pos cube.Pos, origin, velocity mgl64.Vec3, width float64) (mgl64.Vec3, bool) {
	x, y, z := float32(velocity[0])*float32(.4), max(float32(velocity[1]), float32(-.12)), float32(velocity[2])*float32(.4)
	velocity = mgl64.Vec3{float64(x), float64(y), float64(z)}
	if float32(origin[1]) > float32(pos[1])+float32(.9375) || y >= 0 {
		return velocity, false
	}
	distance := float32(width)*float32(.5) + float32(.43125)
	dx := float32(pos[0]) + float32(.5) - float32(origin[0])
	dz := float32(pos[2]) + float32(.5) - float32(origin[2])
	return velocity, float32(math.Abs(float64(dx))) > distance || float32(math.Abs(float64(dz))) > distance
}

// BreakInfo ...
func (h HoneyBlock) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(h))
}

// EncodeItem ...
func (HoneyBlock) EncodeItem() (name string, meta int16) {
	return "minecraft:honey_block", 0
}

// EncodeBlock ...
func (HoneyBlock) EncodeBlock() (string, map[string]any) {
	return "minecraft:honey_block", nil
}

// FrostedIce is temporary ice created by the Frost Walker enchantment.
type FrostedIce struct {
	transparent
	solid

	// Age is the crack stage of the frosted ice.
	Age int
}

// BreakInfo ...
func (FrostedIce) BreakInfo() BreakInfo {
	return newBreakInfo(0.5, neverHarvestable, pickaxeEffective, simpleDrops())
}

// RandomTick ...
func (f FrostedIce) RandomTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	f.tick(pos, tx, r)
}

// ScheduledTick ...
func (f FrostedIce) ScheduledTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	f.tick(pos, tx, r)
}

// tick ports FrostedIceBlock.tick: ice with few frosted neighbours, or one
// in three, melts a stage once it is bright enough, and its frosted
// neighbours with it.
func (f FrostedIce) tick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	if r.IntN(3) == 0 || fewerFrostedIceNeighbours(pos, tx, 4) {
		brightness := tx.RawBrightness(pos)
		if tx.World().Dimension() == world.End {
			brightness = int(tx.BlockLight(pos))
		}
		// Frosted ice dampens light by one.
		if brightness > 11-f.Age-1 && f.slightlyMelt(pos, tx) {
			for _, face := range cube.Faces() {
				neighbour := pos.Side(face)
				if ice, ok := tx.Block(neighbour).(FrostedIce); ok && !ice.slightlyMelt(neighbour, tx) {
					tx.ScheduleBlockUpdate(neighbour, ice, time.Duration(20+r.IntN(21))*time.Second/20)
				}
			}
			return
		}
	}
	tx.ScheduleBlockUpdate(pos, f, time.Duration(20+r.IntN(21))*time.Second/20)
}

// slightlyMelt ports FrostedIceBlock.slightlyMelt: the ice ages a stage, or
// melts once it is as old as it gets. It reports if the ice melted.
func (f FrostedIce) slightlyMelt(pos cube.Pos, tx *world.Tx) bool {
	if f.Age < 3 {
		f.Age++
		tx.SetBlock(pos, f, &world.SetOpts{DisableBlockUpdates: true})
		return false
	}
	f.melt(pos, tx)
	return true
}

// melt ports IceBlock.melt: frosted ice turns into water, or vanishes where
// water evaporates.
func (FrostedIce) melt(pos cube.Pos, tx *world.Tx) {
	if tx.World().Dimension().WaterEvaporates() {
		tx.SetBlock(pos, nil, nil)
		return
	}
	tx.SetBlock(pos, Water{Depth: 8, Still: true}, nil)
}

// Placed ports FrostedIceBlock.onPlace: placed ice first ticks after three to
// six seconds.
func (f FrostedIce) Placed(pos cube.Pos, tx *world.Tx, _ world.Block) {
	tx.ScheduleBlockUpdate(pos, f, time.Duration(60+rand.IntN(61))*time.Second/20)
}

// NeighbourUpdateTick ports FrostedIceBlock.neighborChanged: once frosted ice
// next to it melts, ice with fewer than two frosted neighbours melts too.
func (f FrostedIce) NeighbourUpdateTick(pos, changedNeighbour cube.Pos, tx *world.Tx) {
	switch b := tx.Block(changedNeighbour).(type) {
	case Water:
		if b.Depth != 8 || b.Falling {
			return
		}
	case Air:
		if !tx.World().Dimension().WaterEvaporates() {
			return
		}
	default:
		return
	}
	if fewerFrostedIceNeighbours(pos, tx, 2) {
		f.melt(pos, tx)
	}
}

func fewerFrostedIceNeighbours(pos cube.Pos, tx *world.Tx, limit int) bool {
	count := 0
	for _, face := range cube.Faces() {
		if _, ok := tx.Block(pos.Side(face)).(FrostedIce); ok {
			count++
			if count >= limit {
				return false
			}
		}
	}
	return true
}

// EncodeBlock ...
func (f FrostedIce) EncodeBlock() (string, map[string]any) {
	return "minecraft:frosted_ice", map[string]any{"age": int32(f.Age)}
}

// allFrostedIce returns all frosted ice states.
func allFrostedIce() (blocks []world.Block) {
	for age := 0; age < 4; age++ {
		blocks = append(blocks, FrostedIce{Age: age})
	}
	return
}

// NetherReactor is a legacy block kept for old worlds.
type NetherReactor struct {
	solid
	bassDrum
}

// BreakInfo ...
func (n NetherReactor) BreakInfo() BreakInfo {
	return newBreakInfo(3, pickaxeHarvestable, pickaxeEffective, oneOf(n)).withBlastResistance(3)
}

// EncodeItem ...
func (NetherReactor) EncodeItem() (name string, meta int16) {
	return "minecraft:netherreactor", 0
}

// EncodeBlock ...
func (NetherReactor) EncodeBlock() (string, map[string]any) {
	return "minecraft:netherreactor", nil
}

// UnderwaterTNT is an Education Edition TNT variant.
type UnderwaterTNT struct {
	solid

	// Explode specifies if the TNT is primed to explode.
	Explode bool
}

// BreakInfo ...
func (t UnderwaterTNT) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(t))
}

// EncodeItem ...
func (UnderwaterTNT) EncodeItem() (name string, meta int16) {
	return "minecraft:underwater_tnt", 0
}

// EncodeBlock ...
func (t UnderwaterTNT) EncodeBlock() (string, map[string]any) {
	return "minecraft:underwater_tnt", map[string]any{"explode_bit": boolByte(t.Explode)}
}

// allUnderwaterTNT returns all underwater TNT states.
func allUnderwaterTNT() []world.Block {
	return []world.Block{UnderwaterTNT{}, UnderwaterTNT{Explode: true}}
}

// Sculk is a block of the deep dark. Sculk charges travel through it, growing
// sculk sensors and shriekers on top of it.
type Sculk struct {
	solid
	bassDrum
}

// BreakInfo ...
func (s Sculk) BreakInfo() BreakInfo {
	return newBreakInfo(0.2, alwaysHarvestable, hoeEffective, silkTouchOnlyDrop(s)).withXPDropRange(1, 1).withBlastResistance(0.2)
}

// EncodeItem ...
func (Sculk) EncodeItem() (name string, meta int16) {
	return "minecraft:sculk", 0
}

// EncodeBlock ...
func (Sculk) EncodeBlock() (string, map[string]any) {
	return "minecraft:sculk", nil
}

// Target is a redstone component block used for ranged practice.
type Target struct {
	solid
	bassDrum

	Signal uint8
}

// BreakInfo ...
func (t Target) BreakInfo() BreakInfo {
	return newBreakInfo(0.5, alwaysHarvestable, hoeEffective, oneOf(t))
}

// EncodeItem ...
func (Target) EncodeItem() (name string, meta int16) {
	return "minecraft:target", 0
}

// EncodeBlock ...
func (Target) EncodeBlock() (string, map[string]any) {
	return "minecraft:target", nil
}

// ProjectileHit powers the target according to how close the projectile hit to the centre.
func (t Target) ProjectileHit(pos cube.Pos, tx *world.Tx, projectile world.Entity, face cube.Face) {
	t.Signal = targetSignal(pos, projectile.Position(), face)
	tx.SetBlock(pos, t, nil)
	tx.DoBlockUpdatesAround(pos)
	tx.ScheduleBlockUpdate(pos, t, targetResetDelay(projectile.H().Type().EncodeEntity()))
}

func targetSignal(pos cube.Pos, hit mgl64.Vec3, face cube.Face) uint8 {
	rel := hit.Sub(pos.Vec3())
	var distance float64
	switch face.Axis() {
	case cube.X:
		distance = max(math.Abs(rel.Y()-0.5), math.Abs(rel.Z()-0.5))
	case cube.Y:
		distance = max(math.Abs(rel.X()-0.5), math.Abs(rel.Z()-0.5))
	case cube.Z:
		distance = max(math.Abs(rel.X()-0.5), math.Abs(rel.Y()-0.5))
	}
	return uint8(max(1, math.Ceil(15*(1-min(distance*2, 1)))))
}

func targetResetDelay(identifier string) time.Duration {
	if identifier == "minecraft:arrow" || identifier == "minecraft:thrown_trident" {
		return time.Second
	}
	return 400 * time.Millisecond
}

// ScheduledTick resets the target's signal.
func (t Target) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if t.Signal == 0 {
		return
	}
	t.Signal = 0
	tx.SetBlock(pos, t, nil)
	tx.DoBlockUpdatesAround(pos)
}

// RedstonePower returns the target's current hit signal.
func (t Target) RedstonePower(cube.Pos, *world.Tx, cube.Face) int { return int(t.Signal) }

// EncodeNBT saves the output signal, which Java keeps in the block's POWER
// state and Bedrock's target block has no state for.
func (t Target) EncodeNBT() map[string]any {
	return map[string]any{"id": "Target", "OutputSignal": int32(t.Signal)}
}

// DecodeNBT ...
func (t Target) DecodeNBT(data map[string]any) any {
	t.Signal = uint8(min(max(nbtconv.Int32(data, "OutputSignal"), 0), 15))
	return t
}

// ServerOnlyNBT keeps the output signal from clients: BDS sends no block
// actor for target blocks.
func (Target) ServerOnlyNBT() {}

// Fungus is a Nether fungus plant.
type Fungus struct {
	transparent
	empty

	// Warped specifies if the fungus is warped instead of crimson.
	Warped bool
}

// BreakInfo ...
func (f Fungus) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(f))
}

// EncodeItem ...
func (f Fungus) EncodeItem() (name string, meta int16) {
	if f.Warped {
		return "minecraft:warped_fungus", 0
	}
	return "minecraft:crimson_fungus", 0
}

// EncodeBlock ...
func (f Fungus) EncodeBlock() (string, map[string]any) {
	if f.Warped {
		return "minecraft:warped_fungus", nil
	}
	return "minecraft:crimson_fungus", nil
}

// allFungus returns all fungus variants.
func allFungus() []world.Block {
	return []world.Block{Fungus{}, Fungus{Warped: true}}
}

// LegacyStonecutter is the old stonecutter block kept for compatibility with old worlds.
type LegacyStonecutter struct {
	solid
	bassDrum
}

// BreakInfo ...
func (s LegacyStonecutter) BreakInfo() BreakInfo {
	return newBreakInfo(3.5, pickaxeHarvestable, pickaxeEffective, oneOf(s))
}

// EncodeItem ...
func (LegacyStonecutter) EncodeItem() (name string, meta int16) {
	return "minecraft:stonecutter", 0
}

// EncodeBlock ...
func (LegacyStonecutter) EncodeBlock() (string, map[string]any) {
	return "minecraft:stonecutter", nil
}
