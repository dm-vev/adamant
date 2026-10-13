package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// FlowerPot is a decorative pot that holds a single small plant: a flower,
// sapling, fern, mushroom, fungus, roots, cactus, bamboo or azalea.
type FlowerPot struct {
	transparent

	// Plant is the potted plant, or nil when the pot is empty.
	//blockhash:ignore
	Plant world.Block
}

// Model ports FlowerPotBlock.SHAPE, a six pixel column six pixels tall.
func (FlowerPot) Model() world.BlockModel { return flowerPotModel{} }

// BreakInfo ...
func (p FlowerPot) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, func(item.Tool, []item.Enchantment) []item.Stack {
		drops := []item.Stack{item.NewStack(FlowerPot{}, 1)}
		if plant, ok := p.plantItem(); ok {
			drops = append(drops, item.NewStack(plant, 1))
		}
		return drops
	})
}

// Activate ports FlowerPotBlock.useItemOn and useWithoutItem: a pottable plant
// is put into an empty pot, and anything else takes the plant out.
func (p FlowerPot) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	held, _ := u.HeldItems()
	if plant, ok := pottedPlant(held); ok {
		if p.Plant == nil {
			tx.SetBlock(pos, FlowerPot{Plant: plant}, nil)
			tx.EmitGameEvent(world.GameEventBlockChange, pos.Vec3Centre(), world.GameEventContext{Source: u})
			ctx.SubtractFromCount(1)
		}
		return true
	}
	plant, ok := p.plantItem()
	if !ok {
		return false
	}
	tx.SetBlock(pos, FlowerPot{}, nil)
	tx.EmitGameEvent(world.GameEventBlockChange, pos.Vec3Centre(), world.GameEventContext{Source: u})
	ctx.NewItem = item.NewStack(plant, 1)
	return true
}

// plantItem returns the item of the potted plant.
func (p FlowerPot) plantItem() (world.Item, bool) {
	if p.Plant == nil {
		return nil, false
	}
	it, ok := p.Plant.(world.Item)
	return it, ok
}

// pottedPlant returns the plant state a held item puts into a pot, ported
// from the POTTED_BY_CONTENT blocks.
func pottedPlant(s item.Stack) (world.Block, bool) {
	if s.Empty() {
		return nil, false
	}
	switch b := s.Item().(type) {
	case Flower, GoldenDandelion, Torchflower, Fern, DeadBush, RedMushroom, BrownMushroom, Fungus, CrimsonRoots, WarpedRoots, Azalea, FloweringAzalea:
		return b.(world.Block), true
	case Eyeblossom:
		return b, true
	case Cactus:
		return Cactus{}, true
	case TreeSapling:
		return TreeSapling{Type: b.Type}, true
	case Bamboo:
		return Bamboo{}, true
	}
	return nil, false
}

// RandomTick ports FlowerPotBlock.randomTick: a potted eyeblossom opens and
// closes with the time of day.
func (p FlowerPot) RandomTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	e, ok := p.Plant.(Eyeblossom)
	if !ok {
		return
	}
	open, defined := eyeblossomOpen(tx)
	if !defined || open == e.Open {
		return
	}
	tx.SetBlock(pos, FlowerPot{Plant: Eyeblossom{Open: open}}, nil)
	tx.AddParticle(pos.Vec3Centre(), particle.EyeblossomTransform{Open: open})
	tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: eyeblossomSound(open, true), Volume: 1, Pitch: 1})
}

// UseOnBlock places an empty pot. FlowerPotBlock keeps the default canSurvive,
// so pots need no support.
func (p FlowerPot) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, p)
	if !used {
		return false
	}
	place(tx, pos, FlowerPot{}, user, ctx)
	return placed(ctx)
}

// EncodeItem ...
func (FlowerPot) EncodeItem() (name string, meta int16) {
	return "minecraft:flower_pot", 0
}

// EncodeBlock ...
func (FlowerPot) EncodeBlock() (string, map[string]any) {
	return "minecraft:flower_pot", map[string]any{"update_bit": false}
}

// EncodeNBT encodes the native FlowerPot block actor.
func (p FlowerPot) EncodeNBT() map[string]any {
	m := map[string]any{"id": "FlowerPot"}
	if p.Plant != nil {
		m["PlantBlock"] = nbtconv.WriteBlock(p.Plant)
	}
	return m
}

// DecodeNBT ...
func (p FlowerPot) DecodeNBT(m map[string]any) any {
	p.Plant = nil
	if _, ok := m["PlantBlock"]; ok {
		if plant := nbtconv.Block(m, "PlantBlock"); plant != nil {
			if _, air := plant.(Air); !air {
				p.Plant = plant
			}
		}
	}
	return p
}

// flowerPotModel is the collision of a flower pot.
type flowerPotModel struct{}

func (flowerPotModel) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	return []cube.BBox{cube.Box(5.0/16, 0, 5.0/16, 11.0/16, 6.0/16, 11.0/16)}
}
func (flowerPotModel) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool { return false }
