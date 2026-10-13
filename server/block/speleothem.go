package block

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
)

// SpeleothemThickness is the segment of a pointed dripstone or sulfur spike
// column.
type SpeleothemThickness uint8

const (
	// SpeleothemTip is the pointed end of a column.
	SpeleothemTip SpeleothemThickness = iota
	// SpeleothemTipMerge is a tip touching the opposing tip of another column.
	SpeleothemTipMerge
	// SpeleothemFrustum is the segment behind a tip.
	SpeleothemFrustum
	// SpeleothemMiddle is a segment between the frustum and the base.
	SpeleothemMiddle
	// SpeleothemBase is the thick segment at the root of a column.
	SpeleothemBase
)

// Uint8 returns the thickness as a uint8.
func (t SpeleothemThickness) Uint8() uint8 { return uint8(t) }

// String returns the Bedrock dripstone_thickness value.
func (t SpeleothemThickness) String() string {
	switch t {
	case SpeleothemTipMerge:
		return "merge"
	case SpeleothemFrustum:
		return "frustum"
	case SpeleothemMiddle:
		return "middle"
	case SpeleothemBase:
		return "base"
	default:
		return "tip"
	}
}

// speleothem is a block of Java's SpeleothemBlock family: a column of
// segments pointing up (stalagmite) or down (stalactite).
type speleothem interface {
	world.Block
	// speleothemState returns whether the tip points down and the thickness.
	speleothemState() (hanging bool, thickness SpeleothemThickness)
	// withSpeleothemState returns the same kind of block in another state.
	withSpeleothemState(hanging bool, thickness SpeleothemThickness) speleothem
	// sameSpeleothem reports whether b is the same kind of speleothem.
	sameSpeleothem(b world.Block) bool
	// grows reports whether a stalactite starting at pos grows.
	grows(tx *world.Tx, pos cube.Pos) bool
	// maxGrowthLength is the longest stalactite that keeps growing.
	maxGrowthLength() int
	// blocksStalagmiteScan reports whether b stops the search for the floor a
	// stalagmite grows on.
	blocksStalagmiteScan(tx *world.Tx, pos cube.Pos, b world.Block) bool
}

// speleothemWithDirection ports isSpeleothemWithDirection over the
// #speleothems tag.
func speleothemWithDirection(b world.Block, hanging bool) bool {
	s, ok := b.(speleothem)
	if !ok {
		return false
	}
	h, _ := s.speleothemState()
	return h == hanging
}

// speleothemTip reports whether b is a tip, optionally counting merged tips.
func speleothemTip(b world.Block, includeMerged bool) bool {
	s, ok := b.(speleothem)
	if !ok {
		return false
	}
	_, t := s.speleothemState()
	return t == SpeleothemTip || includeMerged && t == SpeleothemTipMerge
}

// tipFace returns the face a speleothem points toward.
func tipFace(hanging bool) cube.Face {
	if hanging {
		return cube.FaceDown
	}
	return cube.FaceUp
}

// speleothemPlacementValid ports isValidSpeleothemPlacement: the block behind
// the tip needs a sturdy face, or must be the same kind pointing the same way.
func speleothemPlacementValid(s speleothem, tx *world.Tx, pos cube.Pos, hanging bool) bool {
	behind := pos.Side(tipFace(hanging).Opposite())
	b := tx.Block(behind)
	return b.Model().FaceSolid(behind, tipFace(hanging), tx) || speleothemWithDirection(b, hanging) && s.sameSpeleothem(b)
}

// speleothemThickness ports calculateSpeleothemThickness.
func speleothemThickness(s speleothem, tx *world.Tx, pos cube.Pos, hanging, mergeOpposingTips bool) SpeleothemThickness {
	inFront := tx.Block(pos.Side(tipFace(hanging)))
	if speleothemWithDirection(inFront, !hanging) && s.sameSpeleothem(inFront) {
		if _, t := inFront.(speleothem).speleothemState(); !mergeOpposingTips && t != SpeleothemTipMerge {
			return SpeleothemTip
		}
		return SpeleothemTipMerge
	}
	if !speleothemWithDirection(inFront, hanging) {
		return SpeleothemTip
	}
	if _, t := inFront.(speleothem).speleothemState(); t == SpeleothemTip || t == SpeleothemTipMerge {
		return SpeleothemFrustum
	}
	if !speleothemWithDirection(tx.Block(pos.Side(tipFace(hanging).Opposite())), hanging) {
		return SpeleothemBase
	}
	return SpeleothemMiddle
}

// placeSpeleothem ports getStateForPlacement: the tip points away from the
// vertical direction the user looks in where it is supported, otherwise the
// other way. Sneaking places a tip without merging it into an opposing one.
func placeSpeleothem(s speleothem, pos cube.Pos, face cube.Face, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, s)
	if !used {
		return false
	}
	hanging := user.Rotation().Pitch() < 0
	if !speleothemPlacementValid(s, tx, pos, hanging) {
		if hanging = !hanging; !speleothemPlacementValid(s, tx, pos, hanging) {
			return false
		}
	}
	merge := true
	if sn, ok := user.(interface{ Sneaking() bool }); ok && sn.Sneaking() {
		merge = false
	}
	place(tx, pos, s.withSpeleothemState(hanging, speleothemThickness(s, tx, pos, hanging, merge)), user, ctx)
	return placed(ctx)
}

// speleothemNeighbourUpdate ports updateShape for vertical neighbours: an
// unsupported column schedules its fall or break, otherwise its thickness
// follows the neighbours.
func speleothemNeighbourUpdate(s speleothem, pos, changed cube.Pos, tx *world.Tx) {
	if changed.X() != pos.X() || changed.Z() != pos.Z() {
		return
	}
	hanging, thickness := s.speleothemState()
	if hanging && tx.HasScheduledBlockUpdate(pos, s) {
		return
	}
	if changed == pos.Side(tipFace(hanging).Opposite()) && !speleothemPlacementValid(s, tx, pos, hanging) {
		delay := time.Second / 20
		if hanging {
			delay *= 2
		}
		tx.ScheduleBlockUpdate(pos, s, delay)
		return
	}
	if next := speleothemThickness(s, tx, pos, hanging, thickness == SpeleothemTipMerge); next != thickness {
		tx.SetBlock(pos, s.withSpeleothemState(hanging, next), nil)
	}
}

// speleothemScheduledTick ports SpeleothemBlock.tick: an unsupported
// stalagmite breaks, and a stalactite falls with every segment below it. The
// falling tip hurts entities it lands on by the column length, at least six,
// per block fallen, up to 40.
func speleothemScheduledTick(s speleothem, pos cube.Pos, tx *world.Tx) {
	if hanging, _ := s.speleothemState(); !hanging {
		if !speleothemPlacementValid(s, tx, pos, false) {
			breakBlock(s, pos, tx)
		}
		return
	}
	create := tx.World().EntityRegistry().Config().FallingBlock
	for fall := pos; ; fall = fall.Side(cube.FaceDown) {
		b := tx.Block(fall)
		if !speleothemWithDirection(b, true) {
			return
		}
		tx.SetBlock(fall, nil, nil)
		e := tx.AddEntity(create(world.EntitySpawnOpts{Position: fall.Vec3Middle()}, b))
		if speleothemTip(b, true) {
			if h, ok := e.(interface {
				HurtEntities(perBlock, maxDamage float64)
			}); ok {
				h.HurtEntities(float64(max(1+pos.Y()-fall.Y(), 6)), 40)
			}
			return
		}
	}
}

// speleothemRandomTick ports SpeleothemBlock.randomTick: the top block of a
// stalactite occasionally grows the column or a stalagmite below it.
func speleothemRandomTick(s speleothem, pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	if r.Float32() < 0.011377778 && speleothemStalactiteStart(s, pos, tx) {
		speleothemGrowIfPossible(s, pos, tx, r)
	}
}

// speleothemGrowIfPossible ports growStalactiteOrStalagmiteIfPossible.
func speleothemGrowIfPossible(s speleothem, pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	if !s.grows(tx, pos) {
		return
	}
	tip, ok := speleothemFindTip(s, pos, tx, s.maxGrowthLength(), false)
	if !ok {
		return
	}
	tipBlock := tx.Block(tip)
	if !speleothemFreeHanging(tipBlock, tx, tip) || !speleothemTipCanGrow(s, tipBlock, tx, tip) {
		return
	}
	if r.IntN(2) == 0 {
		speleothemGrow(s, tx, tip, true)
		return
	}
	speleothemGrowStalagmiteBelow(s, tx, tip)
}

// speleothemStalactiteStart ports isStalactiteStartPos: the top block of a
// stalactite.
func speleothemStalactiteStart(s speleothem, pos cube.Pos, tx *world.Tx) bool {
	hanging, _ := s.speleothemState()
	return hanging && !s.sameSpeleothem(tx.Block(pos.Side(cube.FaceUp)))
}

// speleothemFreeHanging ports isFreeHangingStalactite: a dry, unmerged
// stalactite tip.
func speleothemFreeHanging(b world.Block, tx *world.Tx, pos cube.Pos) bool {
	s, ok := b.(speleothem)
	if !ok {
		return false
	}
	if hanging, thickness := s.speleothemState(); !hanging || thickness != SpeleothemTip {
		return false
	}
	_, waterlogged := tx.Liquid(pos)
	return !waterlogged
}

// speleothemFindTip ports findTip: the tip of the column pos belongs to,
// searching fewer than maxLength blocks.
func speleothemFindTip(s speleothem, pos cube.Pos, tx *world.Tx, maxLength int, includeMerged bool) (cube.Pos, bool) {
	if speleothemTip(s, includeMerged) {
		return pos, true
	}
	hanging, _ := s.speleothemState()
	return speleothemFindVertical(tx, pos, tipFace(hanging), maxLength, func(_ cube.Pos, b world.Block) bool {
		return s.sameSpeleothem(b) && speleothemWithDirection(b, hanging)
	}, func(_ cube.Pos, b world.Block) bool {
		return speleothemTip(b, includeMerged)
	})
}

// speleothemFindVertical ports findBlockVertical: the first block within
// maxSteps-1 blocks toward face matching target, following blocks matching
// path.
func speleothemFindVertical(tx *world.Tx, pos cube.Pos, face cube.Face, maxSteps int, path func(cube.Pos, world.Block) bool, target func(cube.Pos, world.Block) bool) (cube.Pos, bool) {
	for i := 1; i < maxSteps; i++ {
		pos = pos.Side(face)
		b := tx.Block(pos)
		if target(pos, b) {
			return pos, true
		}
		if pos.OutOfBounds(tx.Range()) || !path(pos, b) {
			return cube.Pos{}, false
		}
	}
	return cube.Pos{}, false
}

// speleothemTipCanGrow ports canTipGrow: the space ahead must be dry air or
// an opposing unmerged tip.
func speleothemTipCanGrow(s speleothem, tip world.Block, tx *world.Tx, pos cube.Pos) bool {
	hanging, _ := tip.(speleothem).speleothemState()
	ahead := pos.Side(tipFace(hanging))
	if _, wet := tx.Liquid(ahead); wet {
		return false
	}
	b := tx.Block(ahead)
	if _, air := b.(Air); air {
		return true
	}
	return speleothemUnmergedTip(s, b, !hanging)
}

// speleothemUnmergedTip ports isUnmergedTipWithDirection.
func speleothemUnmergedTip(s speleothem, b world.Block, hanging bool) bool {
	return speleothemTip(b, false) && speleothemWithDirection(b, hanging) && s.sameSpeleothem(b)
}

// speleothemGrow ports grow: the column extends from pos, merging with an
// opposing tip it meets.
func speleothemGrow(s speleothem, tx *world.Tx, from cube.Pos, downward bool) {
	target := from.Side(tipFace(downward))
	existing := tx.Block(target)
	if speleothemUnmergedTip(s, existing, !downward) {
		stalactite, stalagmite := target, target.Side(cube.FaceDown)
		if !downward {
			stalactite, stalagmite = target.Side(cube.FaceUp), target
		}
		tx.SetBlock(stalactite, s.withSpeleothemState(true, SpeleothemTipMerge), nil)
		tx.SetBlock(stalagmite, s.withSpeleothemState(false, SpeleothemTipMerge), nil)
		return
	}
	_, air := existing.(Air)
	_, water := existing.(Water)
	if air || water {
		tx.SetBlock(target, s.withSpeleothemState(downward, SpeleothemTip), nil)
	}
}

// speleothemGrowStalagmiteBelow ports growStalagmiteBelow: a stalagmite
// grows on the first floor within ten blocks below the tip.
func speleothemGrowStalagmiteBelow(s speleothem, tx *world.Tx, above cube.Pos) {
	pos := above
	for range 10 {
		pos = pos.Side(cube.FaceDown)
		if _, wet := tx.Liquid(pos); wet {
			return
		}
		b := tx.Block(pos)
		if speleothemUnmergedTip(s, b, false) && speleothemTipCanGrow(s, b, tx, pos) {
			speleothemGrow(s, tx, pos, false)
			return
		}
		if speleothemPlacementValid(s, tx, pos, false) && !waterAt(tx, pos.Side(cube.FaceDown)) {
			speleothemGrow(s, tx, pos.Side(cube.FaceDown), false)
			return
		}
		if s.blocksStalagmiteScan(tx, pos, b) {
			return
		}
	}
}

// waterAt reports whether pos holds water, as a block or waterlogged.
func waterAt(tx *world.Tx, pos cube.Pos) bool {
	l, ok := tx.Liquid(pos)
	return ok && l.LiquidType() == "water"
}

// dripThrough ports PointedDripstoneBlock.canDripThrough: drops pass air and
// dry blocks leaving a four pixel wide column free.
func dripThrough(tx *world.Tx, pos cube.Pos, b world.Block) bool {
	if _, air := b.(Air); air {
		return true
	}
	if _, wet := tx.Liquid(pos); wet {
		return false
	}
	column := cube.Box(6.0/16, 0, 6.0/16, 10.0/16, 1, 10.0/16)
	for _, box := range b.Model().BBox(pos, tx) {
		if box.IntersectsWith(column) {
			return false
		}
	}
	return true
}

// speleothemModel is the column shape of a speleothem segment.
type speleothemModel struct {
	hanging   bool
	thickness SpeleothemThickness
}

// BBox ...
func (m speleothemModel) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	column := func(width, minY, maxY float64) []cube.BBox {
		half := width / 32
		return []cube.BBox{cube.Box(0.5-half, minY/16, 0.5-half, 0.5+half, maxY/16, 0.5+half)}
	}
	switch m.thickness {
	case SpeleothemTipMerge:
		return column(6, 0, 16)
	case SpeleothemTip:
		if m.hanging {
			return column(6, 5, 16)
		}
		return column(6, 0, 11)
	case SpeleothemFrustum:
		return column(8, 0, 16)
	case SpeleothemMiddle:
		return column(10, 0, 16)
	default:
		return column(12, 0, 16)
	}
}

// FaceSolid ...
func (speleothemModel) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool { return false }

// speleothemProjectileHit ports onProjectileHit: a trident faster than 0.6
// blocks per tick shatters the segment.
func speleothemProjectileHit(s speleothem, pos cube.Pos, tx *world.Tx, projectile world.Entity) {
	if projectile.H().Type().EncodeEntity() != "minecraft:thrown_trident" || !projectileMayInteract(tx, projectile) || !projectileMayBreak(tx, projectile) {
		return
	}
	if v, ok := projectile.(velocityEntity); ok && v.Velocity().Len() > 0.6 {
		breakBlock(s, pos, tx)
	}
}

// PointedDripstone is a stalactite or stalagmite segment of dripstone. It
// grows under dripstone blocks with water above, drips fluids into cauldrons,
// falls when unsupported and impales entities landing on its upward tip.
type PointedDripstone struct {
	transparent
	sourceWaterDisplacer

	// Hanging is set for stalactites, whose tip points down.
	Hanging bool
	// Thickness is the segment of the column.
	Thickness SpeleothemThickness
}

func (d PointedDripstone) speleothemState() (bool, SpeleothemThickness) {
	return d.Hanging, d.Thickness
}
func (d PointedDripstone) withSpeleothemState(h bool, t SpeleothemThickness) speleothem {
	d.Hanging, d.Thickness = h, t
	return d
}
func (PointedDripstone) sameSpeleothem(b world.Block) bool { _, ok := b.(PointedDripstone); return ok }
func (PointedDripstone) maxGrowthLength() int              { return 7 }
func (PointedDripstone) blocksStalagmiteScan(tx *world.Tx, pos cube.Pos, b world.Block) bool {
	return !dripThrough(tx, pos, b)
}

// grows ports PointedDripstoneBlock.canGrow: a dripstone block above with a
// water source above that.
func (PointedDripstone) grows(tx *world.Tx, pos cube.Pos) bool {
	if _, ok := tx.Block(pos.Side(cube.FaceUp)).(Dripstone); !ok {
		return false
	}
	water, ok := tx.Liquid(pos.Add(cube.Pos{0, 2, 0}))
	return ok && water.LiquidType() == "water" && water.LiquidDepth() == 8 && !water.LiquidFalling()
}

// Model ...
func (d PointedDripstone) Model() world.BlockModel { return speleothemModel{d.Hanging, d.Thickness} }

// SideClosed lets water flow around the thin column.
func (PointedDripstone) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool { return false }

// BreakInfo ...
func (d PointedDripstone) BreakInfo() BreakInfo {
	return newBreakInfo(1.5, alwaysHarvestable, pickaxeEffective, oneOf(PointedDripstone{})).withBlastResistance(3)
}

// UseOnBlock ...
func (d PointedDripstone) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	return placeSpeleothem(d, pos, face, tx, user, ctx)
}

// NeighbourUpdateTick ...
func (d PointedDripstone) NeighbourUpdateTick(pos, changed cube.Pos, tx *world.Tx) {
	speleothemNeighbourUpdate(d, pos, changed, tx)
}

// ScheduledTick ...
func (d PointedDripstone) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	speleothemScheduledTick(d, pos, tx)
}

// RandomTick ports PointedDripstoneBlock.randomTick: fluid above the
// stalactite drips into a cauldron below or dries mud into clay, then the
// column may grow.
func (d PointedDripstone) RandomTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	dripstoneTransferFluid(d, pos, tx, r.Float32())
	speleothemRandomTick(d, pos, tx, r)
}

// ProjectileHit ...
func (d PointedDripstone) ProjectileHit(pos cube.Pos, tx *world.Tx, projectile world.Entity, _ cube.Face) {
	speleothemProjectileHit(d, pos, tx, projectile)
}

// SurvivesFall reports whether a falling segment can be placed where it
// lands, which needs the same support as placing it.
func (d PointedDripstone) SurvivesFall(pos cube.Pos, tx *world.Tx) bool {
	return speleothemPlacementValid(d, tx, pos, d.Hanging)
}

// BrokenAfterFall plays the shattering of a falling segment.
func (PointedDripstone) BrokenAfterFall(tx *world.Tx, pos cube.Pos) {
	tx.AddParticle(pos.Vec3Centre(), particle.PointedDripstoneLand{})
}

// EntityLand ports PointedDripstoneBlock.fallOn: landing on a stalagmite tip
// counts 2.5 extra blocks and doubles the fall damage.
func (d PointedDripstone) EntityLand(_ cube.Pos, _ *world.Tx, e world.Entity, distance *float64) {
	if d.Hanging || d.Thickness != SpeleothemTip {
		return
	}
	if f, ok := e.(fallDamageTaker); ok {
		f.CauseFallDamage(*distance+2.5, 2, StalagmiteDamageSource{})
	}
	*distance = 0
}

// fallDamageTaker is an entity taking fall damage. A nil source is plain
// fall damage.
type fallDamageTaker interface {
	CauseFallDamage(distance, multiplier float64, src world.DamageSource)
}

// EncodeItem ...
func (PointedDripstone) EncodeItem() (name string, meta int16) {
	return "minecraft:pointed_dripstone", 0
}

// EncodeBlock ...
func (d PointedDripstone) EncodeBlock() (string, map[string]any) {
	return "minecraft:pointed_dripstone", map[string]any{"hanging": d.Hanging, "dripstone_thickness": d.Thickness.String()}
}

// SulfurSpike is a stalactite or stalagmite segment of sulfur. It grows under
// sulfur blocks up to two blocks long and falls when unsupported.
type SulfurSpike struct {
	transparent
	sourceWaterDisplacer

	// Hanging is set for stalactites, whose tip points down.
	Hanging bool
	// Thickness is the segment of the column.
	Thickness SpeleothemThickness
}

func (s SulfurSpike) speleothemState() (bool, SpeleothemThickness) { return s.Hanging, s.Thickness }
func (s SulfurSpike) withSpeleothemState(h bool, t SpeleothemThickness) speleothem {
	s.Hanging, s.Thickness = h, t
	return s
}
func (SulfurSpike) sameSpeleothem(b world.Block) bool                          { _, ok := b.(SulfurSpike); return ok }
func (SulfurSpike) maxGrowthLength() int                                       { return 2 }
func (SulfurSpike) blocksStalagmiteScan(*world.Tx, cube.Pos, world.Block) bool { return false }
func (SulfurSpike) grows(tx *world.Tx, pos cube.Pos) bool {
	_, ok := tx.Block(pos.Side(cube.FaceUp)).(Sulfur)
	return ok
}

// Model ...
func (s SulfurSpike) Model() world.BlockModel { return speleothemModel{s.Hanging, s.Thickness} }

// SideClosed lets water flow around the thin column.
func (SulfurSpike) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool { return false }

// BreakInfo ...
func (s SulfurSpike) BreakInfo() BreakInfo {
	return newBreakInfo(1.5, alwaysHarvestable, pickaxeEffective, oneOf(SulfurSpike{})).withBlastResistance(3)
}

// UseOnBlock ...
func (s SulfurSpike) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	return placeSpeleothem(s, pos, face, tx, user, ctx)
}

// NeighbourUpdateTick ...
func (s SulfurSpike) NeighbourUpdateTick(pos, changed cube.Pos, tx *world.Tx) {
	speleothemNeighbourUpdate(s, pos, changed, tx)
}

// ScheduledTick ...
func (s SulfurSpike) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	speleothemScheduledTick(s, pos, tx)
}

// RandomTick ...
func (s SulfurSpike) RandomTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	speleothemRandomTick(s, pos, tx, r)
}

// ProjectileHit ...
func (s SulfurSpike) ProjectileHit(pos cube.Pos, tx *world.Tx, projectile world.Entity, _ cube.Face) {
	speleothemProjectileHit(s, pos, tx, projectile)
}

// SurvivesFall reports whether a falling segment can be placed where it
// lands, which needs the same support as placing it.
func (s SulfurSpike) SurvivesFall(pos cube.Pos, tx *world.Tx) bool {
	return speleothemPlacementValid(s, tx, pos, s.Hanging)
}

// BrokenAfterFall plays the shattering of a falling segment.
func (s SulfurSpike) BrokenAfterFall(tx *world.Tx, pos cube.Pos) {
	tx.AddParticle(pos.Vec3Centre(), particle.BlockBreak{Block: s})
}

// EncodeItem ...
func (SulfurSpike) EncodeItem() (name string, meta int16) {
	return "minecraft:sulfur_spike", 0
}

// EncodeBlock ...
func (s SulfurSpike) EncodeBlock() (string, map[string]any) {
	return "minecraft:sulfur_spike", map[string]any{"hanging": s.Hanging, "dripstone_thickness": s.Thickness.String()}
}

// allSpeleothems returns every pointed dripstone and sulfur spike state.
func allSpeleothems() (b []world.Block) {
	for _, hanging := range []bool{false, true} {
		for t := SpeleothemTip; t <= SpeleothemBase; t++ {
			b = append(b, PointedDripstone{Hanging: hanging, Thickness: t}, SulfurSpike{Hanging: hanging, Thickness: t})
		}
	}
	return
}

// StalagmiteDamageSource is the damage of landing on a stalagmite tip. It is
// fall damage: armour does not reduce it but Feather Falling does.
type StalagmiteDamageSource struct{}

func (StalagmiteDamageSource) ReducedByArmour() bool     { return false }
func (StalagmiteDamageSource) ReducedByResistance() bool { return true }
func (StalagmiteDamageSource) Fire() bool                { return false }
func (StalagmiteDamageSource) IgnoreTotem() bool         { return false }
func (StalagmiteDamageSource) AffectedByEnchantment(e item.EnchantmentType) bool {
	return e == enchantment.FeatherFalling
}

// dripstoneTransferFluid ports PointedDripstoneBlock.maybeTransferFluid: with
// a chance of 0.176 for water and 0.059 for lava, fluid above a stalactite
// drips: mud above dries into clay, or a cauldron below the tip is scheduled
// to receive the drop.
func dripstoneTransferFluid(d PointedDripstone, pos cube.Pos, tx *world.Tx, chance float32) {
	if chance > 0.17578125 || !speleothemStalactiteStart(d, pos, tx) {
		return
	}
	source, fluid, ok := dripstoneFluidAbove(d, pos, tx)
	if !ok {
		return
	}
	switch fluid {
	case "water":
		if chance >= 0.17578125 {
			return
		}
	case "lava":
		if chance >= 0.05859375 {
			return
		}
	default:
		return
	}
	tip, ok := speleothemFindTip(d, pos, tx, 11, false)
	if !ok {
		return
	}
	if mud, ok := tx.Block(source).(Mud); ok && fluid == "water" {
		tx.SetBlock(source, Clay{}, nil)
		pushEntitiesUp(tx, source, mud)
		tx.EmitGameEvent(world.GameEventBlockChange, source.Vec3Centre(), world.GameEventContext{AffectedState: Clay{}})
		tx.AddParticle(tip.Vec3Centre(), particle.DripstoneDrip{})
		return
	}
	cauldronPos, ok := speleothemFindVertical(tx, tip, cube.FaceDown, 11, func(p cube.Pos, b world.Block) bool {
		return dripThrough(tx, p, b)
	}, func(_ cube.Pos, b world.Block) bool {
		c, ok := b.(Cauldron)
		return ok && c.receivesDrip(fluid)
	})
	if !ok {
		return
	}
	tx.AddParticle(tip.Vec3Centre(), particle.DripstoneDrip{})
	tx.ScheduleBlockUpdate(cauldronPos, tx.Block(cauldronPos), time.Duration(50+tip.Y()-cauldronPos.Y())*time.Second/20)
}

// dripstoneFluidAbove ports getFluidAboveStalactite: the position above the
// block a stalactite hangs from and the fluid there, counting mud as water
// where water does not evaporate.
func dripstoneFluidAbove(s speleothem, pos cube.Pos, tx *world.Tx) (cube.Pos, string, bool) {
	if hanging, _ := s.speleothemState(); !hanging {
		return cube.Pos{}, "", false
	}
	root, ok := speleothemFindVertical(tx, pos, cube.FaceUp, 11, func(_ cube.Pos, b world.Block) bool {
		return s.sameSpeleothem(b) && speleothemWithDirection(b, true)
	}, func(_ cube.Pos, b world.Block) bool {
		return !s.sameSpeleothem(b)
	})
	if !ok {
		return cube.Pos{}, "", false
	}
	above := root.Side(cube.FaceUp)
	if _, mud := tx.Block(above).(Mud); mud && !tx.World().Dimension().WaterEvaporates() {
		return above, "water", true
	}
	if liquid, ok := tx.Liquid(above); ok {
		return above, liquid.LiquidType(), true
	}
	return above, "", true
}

// dripstoneFluidAboveCauldron ports findStalactiteTipAboveCauldron and
// getCauldronFillFluidType: the water or lava a stalactite above the
// cauldron drips.
func dripstoneFluidAboveCauldron(tx *world.Tx, cauldron cube.Pos) (string, bool) {
	tip, ok := speleothemFindVertical(tx, cauldron, cube.FaceUp, 11, func(p cube.Pos, b world.Block) bool {
		return dripThrough(tx, p, b)
	}, func(p cube.Pos, b world.Block) bool {
		return speleothemFreeHanging(b, tx, p)
	})
	if !ok {
		return "", false
	}
	_, fluid, ok := dripstoneFluidAbove(tx.Block(tip).(speleothem), tip, tx)
	return fluid, ok && (fluid == "water" || fluid == "lava")
}
