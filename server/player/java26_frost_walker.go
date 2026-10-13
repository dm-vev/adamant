package player

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
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
		_, campfire := p.tx.Block(cube.PosFromVec3(p.Position()).Side(cube.FaceDown)).(block.Campfire)
		return campfire
	default:
		return false
	}
}

func (p *Player) tickFrostWalker() {
	if !p.onGround {
		return
	}
	level := p.frostWalkerLevel()
	if level == 0 {
		return
	}
	centre := cube.PosFromVec3(p.Position())
	radius := min(2+level, 4)
	for x := -radius; x <= radius; x++ {
		for z := -radius; z <= radius; z++ {
			pos := cube.Pos{centre[0] + x, centre[1] - 1, centre[2] + z}
			liquid, ok := p.tx.Liquid(pos)
			if !ok || liquid.LiquidType() != "water" || liquid.LiquidDepth() != 8 {
				continue
			}
			if _, air := p.tx.Block(pos.Side(cube.FaceUp)).(block.Air); !air {
				continue
			}
			if math.Hypot(float64(x), float64(z)) > float64(radius)+0.5 {
				continue
			}
			p.tx.SetLiquid(pos, nil)
			p.tx.SetBlock(pos, block.FrostedIce{}, nil)
		}
	}
}
