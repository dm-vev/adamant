package player

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
)

func (p *Player) frostWalkerLevel() int {
	level, _ := p.Armour().Boots().Enchantment(enchantment.FrostWalker)
	return level.Level()
}

func (p *Player) frostWalkerProtectsFrom(src world.DamageSource) bool {
	if p.frostWalkerLevel() == 0 {
		return false
	}
	switch src.(type) {
	case block.MagmaDamageSource:
		return true
	case block.FireDamageSource:
		pos := cube.PosFromVec3(p.Position()).Side(cube.FaceDown)
		b, loaded := p.tx.BlockLoaded(pos)
		if !loaded {
			return false
		}
		_, campfire := b.(block.Campfire)
		return campfire
	default:
		return false
	}
}

// tickFrostWalker runs the LOCATION_CHANGED effect of Frost Walker once the
// player moves into another block while on the ground and riding nothing.
func (p *Player) tickFrostWalker() {
	pos := cube.PosFromVec3(p.Position())
	if p.frostWalkPosSet && pos == p.frostWalkPos {
		return
	}
	p.frostWalkPos, p.frostWalkPosSet = pos, true
	if !p.onGround || p.Riding() != nil || !p.movementChunkReady(pos.Side(cube.FaceDown)) {
		return
	}
	entity.FrostWalk(p.tx, p, p.Position())
}
