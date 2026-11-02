package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// Native resources belong to the lower half. PlayerWillDestroy removes the
// partner before the clicked cell is destroyed; raw API removal instead leaves
// the partner to its scheduled survival callback.
func doorDrops(b world.Block) func(item.Tool, []item.Enchantment) []item.Stack {
	if b.Model().(model.Door).Top {
		return oneOf()
	}
	return oneOf(b.(world.Item))
}

func (d WoodDoor) PlayerWillDestroy(pos cube.Pos, tx *world.Tx, source world.DamageSource) {
	doorPlayerWillDestroy(d, pos, tx, source)
}
func (d IronDoor) PlayerWillDestroy(pos cube.Pos, tx *world.Tx, source world.DamageSource) {
	doorPlayerWillDestroy(d, pos, tx, source)
}
func (d CopperDoor) PlayerWillDestroy(pos cube.Pos, tx *world.Tx, source world.DamageSource) {
	doorPlayerWillDestroy(d, pos, tx, source)
}

func doorPlayerWillDestroy(b world.Block, pos cube.Pos, tx *world.Tx, source world.DamageSource) {
	damage, ok := source.(world.EntityDamageSource)
	if !ok {
		return
	}
	player, ok := damage.SourceEntity().(interface {
		item.Carrier
		GameMode() world.GameMode
	})
	if !ok {
		return
	}
	d := b.Model().(model.Door)
	other := pos.Side(cube.FaceUp)
	if d.Top {
		other = pos.Side(cube.FaceDown)
	}
	partner := tx.Block(other)
	p, ok := partner.Model().(model.Door)
	if !ok || p.Kind != d.Kind || p.Top == d.Top {
		return
	}
	breakBlockNoDrops(partner, other, tx)
	if p.Top || player.GameMode().CreativeInventory() || !tx.World().GameRuleBool("block_drops") {
		return
	}
	held, _ := player.HeldItems()
	tool, ok := held.Item().(item.Tool)
	if !ok {
		tool = item.ToolNone{}
	}
	info := partner.(Breakable).BreakInfo()
	if info.Harvestable(tool) {
		for _, stack := range info.Drops(tool, held.Enchantments()) {
			tx.DropBlockResource(other, stack)
		}
	}
}
