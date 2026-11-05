package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// dispenseHoneycomb ports the behaviour of Items.HONEYCOMB: a block that
// HoneycombItem.getWaxed waxes is replaced by its waxed form, with level event
// 3003 and the sound of waxing; any other block leaves the honeycomb to be
// ejected. Signs are waxed by the item through their block entities, not by
// this behaviour.
func dispenseHoneycomb(src dispenseSource) dispensed {
	tx, front := src.tx, src.front()
	b := tx.Block(front)
	switch b.(type) {
	case Sign, HangingSign:
		return dispenseDefault(src)
	}
	wa, ok := b.(interface {
		Wax(cube.Pos, mgl64.Vec3) (world.Block, bool)
	})
	if !ok {
		return dispenseDefault(src)
	}
	waxed, ok := wa.Wax(front, mgl64.Vec3{})
	if !ok {
		return dispenseDefault(src)
	}
	tx.SetBlock(front, waxed, nil)
	// BDS plays the click at the corner of the block that was waxed.
	tx.PlaySound(front.Vec3Centre(), sound.SignWaxed{})
	res := src.done()
	corner := front.Vec3()
	res.effects.clickAt = &corner
	return res
}

// dispenseGlowstone ports the behaviour of Items.GLOWSTONE: a respawn anchor in
// front that is not fully charged takes it (RespawnAnchorBlock.charge); a
// charged one leaves it, and the dispenser clicks. Any other block leaves the
// glowstone to be ejected.
func dispenseGlowstone(src dispenseSource) dispensed {
	tx, front := src.tx, src.front()
	anchor, ok := tx.Block(front).(RespawnAnchor)
	if !ok {
		return dispenseDefault(src)
	}
	if anchor.Charge >= 4 {
		return src.failed()
	}
	anchor.Charge++
	tx.SetBlock(front, anchor, nil)
	notifyComparatorUpdate(front, tx)
	tx.PlaySound(front.Vec3Centre(), sound.RespawnAnchorCharge{Charge: anchor.Charge})
	return src.done()
}

// convertibleToMud reports whether a water bottle turns the block into mud: the
// blocks of the tag convertible_to_mud.
func convertibleToMud(b world.Block) bool {
	_, dirt := b.(Dirt)
	return dirt
}

// dispensePotion ports the behaviour of Items.POTION: a water bottle turns the
// dirt in front into mud, the bottle it leaves replacing it or going into the
// dispenser, and any other potion is ejected.
func dispensePotion(src dispenseSource) dispensed {
	tx, front := src.tx, src.front()
	p := src.stack.Item().(item.Potion)
	if p.Type != potion.Water() || !convertibleToMud(tx.Block(front)) {
		return dispenseDefault(src)
	}
	tx.PlaySound(src.pos.Vec3Centre(), sound.Custom{Name: "item.bottle.empty", Volume: 1, Pitch: 1})
	tx.EmitGameEvent(world.GameEventFluidPlace, src.pos.Vec3Centre(), world.GameEventContext{})
	tx.SetBlock(front, Mud{}, nil)
	return src.withRemainder(item.NewStack(item.GlassBottle{}, 1))
}

// dispenseShulkerBox ports ShulkerBoxDispenseBehavior: BlockItem.place with a
// DirectionalPlaceContext, which puts the shulker box in the block in front, or
// beyond it if that is not replaceable, facing the side clicked: the direction of
// the dispenser, or up if there is a block below. If it cannot be placed, the
// dispenser clicks and keeps the shulker box. BDS plays no sound for it.
func dispenseShulkerBox(src dispenseSource) dispensed {
	tx, front := src.tx, src.front()
	clicked := cube.FaceUp
	if _, air := tx.Block(front.Side(cube.FaceDown)).(Air); air {
		clicked = src.d.Facing
	}
	s := src.stack.Item().(ShulkerBox)
	pos, _, ok := firstReplaceable(tx, front, clicked, s)
	if !ok {
		return dispensed{left: src.stack, unchanged: true, effects: dispenseEffects{click: clickSilent}}
	}
	if s.inventory == nil {
		typ, customName := s.Type, s.CustomName
		s = NewShulkerBox()
		s.Type, s.CustomName = typ, customName
	}
	s.Facing = clicked
	tx.SetBlock(pos, s, nil)
	res := src.done()
	res.effects.click = clickSilent
	return res
}

// patternOrientations calls visit for each of the 24 ways a block pattern can
// lie in the world, as BlockPattern.find tries them, with the offsets of its
// right and up, until visit returns true.
func patternOrientations(visit func(right, up cube.Pos) bool) bool {
	for _, up := range cube.Faces() {
		for _, right := range cube.Faces() {
			if right.Axis() == up.Axis() {
				continue
			}
			if visit(cube.Pos{}.Side(right), cube.Pos{}.Side(up)) {
				return true
			}
		}
	}
	return false
}

// canSpawnGolem ports CarvedPumpkinBlock.canSpawnGolem: whether a carved
// pumpkin at pos would have the base of a snow, iron or copper golem below it.
func canSpawnGolem(tx *world.Tx, head cube.Pos) bool {
	return patternOrientations(func(right, up cube.Pos) bool {
		below, base := head.Sub(up), head.Sub(up).Sub(up)
		isAir := func(p cube.Pos) bool { _, air := tx.Block(p).(Air); return air }
		_, snow1 := tx.Block(below).(Snow)
		_, snow2 := tx.Block(base).(Snow)
		if snow1 && snow2 {
			return true
		}
		if c, ok := tx.Block(below).(Copper); ok && c.Type == NormalCopper() {
			return true
		}
		_, centre := tx.Block(below).(Iron)
		_, left := tx.Block(below.Sub(right)).(Iron)
		_, rightIron := tx.Block(below.Add(right)).(Iron)
		_, bottom := tx.Block(base).(Iron)
		return centre && left && rightIron && bottom && isAir(head.Sub(right)) && isAir(head.Add(right)) &&
			isAir(base.Sub(right)) && isAir(base.Add(right))
	})
}

// canSpawnWither ports WitherSkullBlock.canSpawnMob for a wither skeleton skull:
// the skull is placed at least two blocks above the bottom of the world, not in
// peaceful, and with the T of soul sand or soul soil below it.
func canSpawnWither(tx *world.Tx, head cube.Pos) bool {
	if head.Y() < tx.Range().Min()+2 || tx.World().Difficulty() == world.DifficultyPeaceful {
		return false
	}
	return patternOrientations(func(right, up cube.Pos) bool {
		below, base := head.Sub(up), head.Sub(up).Sub(up)
		soul := func(p cube.Pos) bool {
			switch tx.Block(p).(type) {
			case SoulSand, SoulSoil:
				return true
			}
			return false
		}
		air := func(p cube.Pos) bool { _, ok := tx.Block(p).(Air); return ok }
		return soul(below) && soul(below.Sub(right)) && soul(below.Add(right)) && soul(base) &&
			air(base.Sub(right)) && air(base.Add(right))
	})
}

// dispensePumpkin ports the behaviour of Blocks.CARVED_PUMPKIN: it is placed in
// the air in front if that makes the base of a golem a golem (the golem is
// summoned as the pumpkin is placed), and otherwise worn by a mob in front;
// without one the dispenser clicks and keeps it. A pumpkin that is not carved
// is ejected.
func dispensePumpkin(src dispenseSource) dispensed {
	tx, front := src.tx, src.front()
	if !src.stack.Item().(Pumpkin).Carved {
		return dispenseDefault(src)
	}
	if _, air := tx.Block(front).(Air); air && canSpawnGolem(tx, front) {
		tx.SetBlock(front, Pumpkin{Carved: true}, nil)
		tx.EmitGameEvent(world.GameEventBlockPlace, front.Vec3Centre(), world.GameEventContext{})
		return src.done()
	}
	return equipped(src)
}

// dispenseSkull ports the behaviour of Items.WITHER_SKELETON_SKULL: it is placed
// in the air in front if the base of a wither is there (the wither is summoned as
// the skull completes the pattern), and otherwise worn by a mob in front;
// without one the dispenser clicks and keeps it. Other skulls are worn.
func dispenseSkull(src dispenseSource) dispensed {
	skull := src.stack.Item().(Skull)
	if skull.Type != WitherSkeletonSkull() {
		return dispenseEquip(src)
	}
	tx, front := src.tx, src.front()
	if _, air := tx.Block(front).(Air); air && canSpawnWither(tx, front) {
		skull = Skull{Type: skull.Type, Attach: StandingAttachment(skullOrientation(src.d.Facing))}
		tx.SetBlock(front, skull, nil)
		tx.EmitGameEvent(world.GameEventBlockPlace, front.Vec3Centre(), world.GameEventContext{})
		return src.done()
	}
	return equipped(src)
}

// skullOrientation returns the orientation of a skull that faces the way a
// dispenser faces, as RotationSegment.convertToSegment(direction): a skull of
// a vertical dispenser faces south.
func skullOrientation(facing cube.Face) cube.Orientation {
	switch facing {
	case cube.FaceNorth:
		return 0
	case cube.FaceEast:
		return 4
	case cube.FaceWest:
		return 12
	}
	return 8
}
