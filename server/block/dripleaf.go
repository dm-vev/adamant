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

// DripleafTilt is how far a big dripleaf leaf has tipped under the weight of
// an entity.
type DripleafTilt uint8

const (
	// DripleafTiltNone is a level leaf.
	DripleafTiltNone DripleafTilt = iota
	// DripleafTiltUnstable is a leaf about to tip after an entity stood on it.
	DripleafTiltUnstable
	// DripleafTiltPartial is a half tipped leaf.
	DripleafTiltPartial
	// DripleafTiltFull is a fully tipped leaf, which no longer holds entities.
	DripleafTiltFull
)

// Uint8 returns the tilt as a uint8.
func (t DripleafTilt) Uint8() uint8 { return uint8(t) }

// String returns the Bedrock big_dripleaf_tilt value.
func (t DripleafTilt) String() string {
	switch t {
	case DripleafTiltUnstable:
		return "unstable"
	case DripleafTiltPartial:
		return "partial_tilt"
	case DripleafTiltFull:
		return "full_tilt"
	default:
		return "none"
	}
}

// BigDripleaf is a stem or leaf of a big dripleaf plant. Its leaf tips over
// shortly after an entity stands on it, unless powered by redstone.
type BigDripleaf struct {
	transparent
	sourceWaterDisplacer

	// Facing is the direction the leaf points.
	Facing cube.Direction
	// Head is set for the leaf on top of the plant; other parts are stem.
	Head bool
	// Tilt is how far the leaf has tipped. Stems never tilt.
	Tilt DripleafTilt
}

// Model ...
func (d BigDripleaf) Model() world.BlockModel { return bigDripleafModel{d} }

// SideClosed lets water flow around the plant.
func (BigDripleaf) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool { return false }

// BreakInfo ...
func (d BigDripleaf) BreakInfo() BreakInfo {
	return newBreakInfo(0.1, alwaysHarvestable, axeEffective, oneOf(BigDripleaf{Head: true}))
}

// FlammabilityInfo ...
func (BigDripleaf) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(60, 100, false)
}

// CompostChance ...
func (BigDripleaf) CompostChance() float64 { return 0.65 }

// UseOnBlock ports BigDripleafBlock.getStateForPlacement: a leaf placed on
// another dripleaf takes its facing, otherwise it faces the placer.
func (d BigDripleaf) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, d)
	if !used || !bigDripleafHeadSurvives(tx, pos) {
		return false
	}
	d = BigDripleaf{Head: true, Facing: user.Rotation().Direction().Opposite()}
	if below, ok := tx.Block(pos.Side(cube.FaceDown)).(BigDripleaf); ok {
		d.Facing = below.Facing
	}
	place(tx, pos, d, user, ctx)
	return placed(ctx)
}

// bigDripleafHeadSurvives ports BigDripleafBlock.canSurvive.
func bigDripleafHeadSurvives(tx *world.Tx, pos cube.Pos) bool {
	below := pos.Side(cube.FaceDown)
	if _, ok := tx.Block(below).(BigDripleaf); ok {
		return true
	}
	return javaBlockTag(tx, below, "supports_big_dripleaf")
}

// bigDripleafStemSurvives ports BigDripleafStemBlock.canSurvive: a stem needs
// stem or ground below and stem or leaf above.
func bigDripleafStemSurvives(tx *world.Tx, pos cube.Pos) bool {
	if _, ok := tx.Block(pos.Side(cube.FaceUp)).(BigDripleaf); !ok {
		return false
	}
	return bigDripleafHeadSurvives(tx, pos)
}

// NeighbourUpdateTick ports updateShape and neighborChanged: a leaf without
// support breaks, a leaf grown over turns into stem and a powered leaf
// levels. A stem without support breaks a tick later.
func (d BigDripleaf) NeighbourUpdateTick(pos, changed cube.Pos, tx *world.Tx) {
	if !d.Head {
		if (changed == pos.Side(cube.FaceDown) || changed == pos.Side(cube.FaceUp)) && !bigDripleafStemSurvives(tx, pos) {
			tx.ScheduleBlockUpdate(pos, d, time.Second/20)
		}
		return
	}
	if changed == pos.Side(cube.FaceDown) && !bigDripleafHeadSurvives(tx, pos) {
		breakBlock(d, pos, tx)
		return
	}
	if above, ok := tx.Block(pos.Side(cube.FaceUp)).(BigDripleaf); ok && above.Head && changed == pos.Side(cube.FaceUp) {
		tx.SetBlock(pos, BigDripleaf{Facing: d.Facing}, nil)
		return
	}
	if redstonePowered(pos, tx) {
		d.resetTilt(pos, tx)
	}
}

// EntityInside ports BigDripleafBlock.entityInside: an entity standing on a
// level, unpowered leaf makes it unstable.
func (d BigDripleaf) EntityInside(pos cube.Pos, tx *world.Tx, e world.Entity) {
	if !d.Head || d.Tilt != DripleafTiltNone || redstonePowered(pos, tx) {
		return
	}
	if g, ok := e.(interface{ OnGround() bool }); ok && g.OnGround() && e.Position().Y() > float64(pos.Y())+0.6875 {
		d.setTiltAndSchedule(pos, tx, DripleafTiltUnstable, false)
	}
}

// ScheduledTick ports BigDripleafBlock.tick, advancing the tilt of a leaf
// from unstable to partial after 10 ticks, to full after 10 more and back to
// level after 100, and BigDripleafStemBlock.tick, breaking an unsupported
// stem.
func (d BigDripleaf) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if !d.Head {
		if !bigDripleafStemSurvives(tx, pos) {
			breakBlock(d, pos, tx)
		}
		return
	}
	if redstonePowered(pos, tx) {
		d.resetTilt(pos, tx)
		return
	}
	switch d.Tilt {
	case DripleafTiltUnstable:
		d.setTiltAndSchedule(pos, tx, DripleafTiltPartial, true)
	case DripleafTiltPartial:
		d.setTiltAndSchedule(pos, tx, DripleafTiltFull, true)
	case DripleafTiltFull:
		d.resetTilt(pos, tx)
	}
}

// ProjectileHit tips the leaf fully.
func (d BigDripleaf) ProjectileHit(pos cube.Pos, tx *world.Tx, _ world.Entity, _ cube.Face) {
	if d.Head {
		d.setTiltAndSchedule(pos, tx, DripleafTiltFull, true)
	}
}

// setTiltAndSchedule ports setTiltAndScheduleTick.
func (d BigDripleaf) setTiltAndSchedule(pos cube.Pos, tx *world.Tx, tilt DripleafTilt, playSound bool) {
	d.setTilt(pos, tx, tilt)
	if playSound {
		tx.PlaySound(pos.Vec3Centre(), sound.BigDripleafTilt{})
	}
	delay := 10
	if tilt == DripleafTiltFull {
		delay = 100
	}
	tx.ScheduleBlockUpdate(pos, BigDripleaf{Head: true, Facing: d.Facing, Tilt: tilt}, time.Duration(delay)*time.Second/20)
}

// resetTilt levels the leaf, with a sound if it was tipped.
func (d BigDripleaf) resetTilt(pos cube.Pos, tx *world.Tx) {
	d.setTilt(pos, tx, DripleafTiltNone)
	if d.Tilt != DripleafTiltNone {
		tx.PlaySound(pos.Vec3Centre(), sound.BigDripleafTilt{Up: true})
	}
}

// setTilt stores the tilt, emitting a block change for every tilt but the
// unstable one.
func (d BigDripleaf) setTilt(pos cube.Pos, tx *world.Tx, tilt DripleafTilt) {
	previous := d.Tilt
	d.Tilt = tilt
	tx.SetBlock(pos, d, &world.SetOpts{DisableBlockUpdates: true})
	if tilt != DripleafTiltUnstable && tilt != previous {
		tx.EmitGameEvent(world.GameEventBlockChange, pos.Vec3Centre(), world.GameEventContext{AffectedState: d})
	}
}

// BoneMeal ports performBonemeal: the plant grows a block taller if the
// space above its leaf is free.
func (d BigDripleaf) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	head := pos
	for !d.Head {
		head = head.Side(cube.FaceUp)
		above, ok := tx.Block(head).(BigDripleaf)
		if !ok {
			return item.BoneMealResultNone
		}
		d = above
	}
	if !dripleafCanGrowInto(tx, head.Side(cube.FaceUp)) {
		return item.BoneMealResultNone
	}
	tx.SetBlock(head, BigDripleaf{Facing: d.Facing}, nil)
	tx.SetBlock(head.Side(cube.FaceUp), BigDripleaf{Head: true, Facing: d.Facing}, nil)
	return item.BoneMealResultSmall
}

// dripleafCanGrowInto ports BigDripleafBlock.canGrowInto: air, water or a
// small dripleaf within the build height.
func dripleafCanGrowInto(tx *world.Tx, pos cube.Pos) bool {
	if pos.OutOfBounds(tx.Range()) {
		return false
	}
	switch tx.Block(pos).(type) {
	case Air, Water, SmallDripleaf:
		return true
	}
	return false
}

// placeBigDripleafWithRandomHeight ports placeWithRandomHeight: a plant two
// to five blocks tall, as far as there is space, grows up from pos.
func placeBigDripleafWithRandomHeight(tx *world.Tx, pos cube.Pos, facing cube.Direction) {
	desired := 2 + rand.IntN(4)
	height := 0
	for height < desired && dripleafCanGrowInto(tx, pos.Add(cube.Pos{0, height, 0})) {
		height++
	}
	for y := 0; y < height-1; y++ {
		tx.SetBlock(pos.Add(cube.Pos{0, y, 0}), BigDripleaf{Facing: facing}, nil)
	}
	tx.SetBlock(pos.Add(cube.Pos{0, height - 1, 0}), BigDripleaf{Head: true, Facing: facing}, nil)
}

// EncodeItem ...
func (BigDripleaf) EncodeItem() (name string, meta int16) {
	return "minecraft:big_dripleaf", 0
}

// EncodeBlock ...
func (d BigDripleaf) EncodeBlock() (string, map[string]any) {
	return "minecraft:big_dripleaf", map[string]any{
		"big_dripleaf_head":            d.Head,
		"big_dripleaf_tilt":            d.Tilt.String(),
		"minecraft:cardinal_direction": d.Facing.String(),
	}
}

// bigDripleafModel is the collision of a big dripleaf: only the leaf surface
// at its tilt. Stems have no collision.
type bigDripleafModel struct{ d BigDripleaf }

// BBox ...
func (m bigDripleafModel) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	if !m.d.Head {
		return nil
	}
	switch m.d.Tilt {
	case DripleafTiltNone, DripleafTiltUnstable:
		return []cube.BBox{cube.Box(0, 11.0/16, 0, 1, 15.0/16, 1)}
	case DripleafTiltPartial:
		return []cube.BBox{cube.Box(0, 11.0/16, 0, 1, 13.0/16, 1)}
	}
	return nil
}

// FaceSolid ...
func (bigDripleafModel) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool { return false }

// allBigDripleaf returns every big dripleaf state.
func allBigDripleaf() (b []world.Block) {
	for _, facing := range cube.Directions() {
		b = append(b, BigDripleaf{Facing: facing})
		for tilt := DripleafTiltNone; tilt <= DripleafTiltFull; tilt++ {
			b = append(b, BigDripleaf{Head: true, Facing: facing, Tilt: tilt})
		}
	}
	return
}

// SmallDripleaf is a two block tall plant growing underwater on clay, moss or
// soil. Bone meal grows it into a big dripleaf.
type SmallDripleaf struct {
	transparent
	sourceWaterDisplacer

	// Facing is the direction the leaves point.
	Facing cube.Direction
	// UpperPart is set for the top half of the plant.
	UpperPart bool
}

// Model ...
func (SmallDripleaf) Model() world.BlockModel { return smallDripleafModel{} }

// SideClosed lets water flow around the plant.
func (SmallDripleaf) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool { return false }

// BreakInfo ports the shears-only loot table, for either half.
func (d SmallDripleaf) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, shearsEffective, func(t item.Tool, _ []item.Enchantment) []item.Stack {
		if t.ToolType() != item.TypeShears {
			return nil
		}
		return []item.Stack{item.NewStack(SmallDripleaf{}, 1)}
	})
}

// FlammabilityInfo ...
func (SmallDripleaf) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(60, 100, false)
}

// CompostChance ...
func (SmallDripleaf) CompostChance() float64 { return 0.3 }

// smallDripleafSurvives ports SmallDripleafBlock.canSurvive for the lower
// half: clay or moss below, or soil below with a water source at the plant.
func smallDripleafSurvives(tx *world.Tx, pos cube.Pos) bool {
	below := pos.Side(cube.FaceDown)
	if javaBlockTag(tx, below, "supports_small_dripleaf") {
		return true
	}
	water, ok := tx.Liquid(pos)
	return ok && water.LiquidType() == "water" && water.LiquidDepth() == 8 && !water.LiquidFalling() && javaBlockTag(tx, below, "supports_vegetation")
}

// UseOnBlock places both halves facing the placer.
func (d SmallDripleaf) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, d)
	if !used || pos.Side(cube.FaceUp).OutOfBounds(tx.Range()) || !replaceableWith(tx, pos.Side(cube.FaceUp), d) || !smallDripleafSurvives(tx, pos) {
		return false
	}
	facing := user.Rotation().Direction().Opposite()
	place(tx, pos, SmallDripleaf{Facing: facing}, user, ctx)
	place(tx, pos.Side(cube.FaceUp), SmallDripleaf{Facing: facing, UpperPart: true}, user, ctx)
	return placed(ctx)
}

// NeighbourUpdateTick ports DoublePlantBlock.updateShape: a half without its
// other half or a lower half without support breaks.
func (d SmallDripleaf) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if d.UpperPart {
		if lower, ok := tx.Block(pos.Side(cube.FaceDown)).(SmallDripleaf); !ok || lower.UpperPart {
			breakBlockNoDrops(d, pos, tx)
		}
		return
	}
	if upper, ok := tx.Block(pos.Side(cube.FaceUp)).(SmallDripleaf); !ok || !upper.UpperPart {
		breakBlockNoDrops(d, pos, tx)
		return
	}
	if !smallDripleafSurvives(tx, pos) {
		breakBlock(d, pos, tx)
	}
}

// BoneMeal ports performBonemeal: the plant becomes a big dripleaf two to
// five blocks tall.
func (d SmallDripleaf) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	if d.UpperPart {
		pos = pos.Side(cube.FaceDown)
		lower, ok := tx.Block(pos).(SmallDripleaf)
		if !ok {
			return item.BoneMealResultNone
		}
		d = lower
	}
	upper := pos.Side(cube.FaceUp)
	tx.SetBlock(upper, nil, &world.SetOpts{DisableBlockUpdates: true})
	placeBigDripleafWithRandomHeight(tx, pos, d.Facing)
	return item.BoneMealResultSmall
}

// EncodeItem ...
func (SmallDripleaf) EncodeItem() (name string, meta int16) {
	return "minecraft:small_dripleaf_block", 0
}

// EncodeBlock ...
func (d SmallDripleaf) EncodeBlock() (string, map[string]any) {
	return "minecraft:small_dripleaf_block", map[string]any{
		"upper_block_bit":              d.UpperPart,
		"minecraft:cardinal_direction": d.Facing.String(),
	}
}

// smallDripleafModel has no collision.
type smallDripleafModel struct{}

// BBox ...
func (smallDripleafModel) BBox(cube.Pos, world.BlockSource) []cube.BBox { return nil }

// FaceSolid ...
func (smallDripleafModel) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool { return false }

// allSmallDripleaf returns every small dripleaf state.
func allSmallDripleaf() (b []world.Block) {
	for _, facing := range cube.Directions() {
		b = append(b, SmallDripleaf{Facing: facing}, SmallDripleaf{Facing: facing, UpperPart: true})
	}
	return
}
