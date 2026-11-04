package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// ChorusPlant is a block found growing in the End. It drops chorus fruit when broken.
type ChorusPlant struct {
	transparent

	// North, East, South, West, Up and Down are the sides the plant
	// connects to: those of the chorus plants and flowers around it, and
	// below it end stone.
	North, East, South, West, Up, Down bool
}

// chorusPlantAt ports ChorusPlantBlock.getStateWithConnections: the plant
// placed at pos, connected to the blocks around it.
func chorusPlantAt(src world.BlockSource, pos cube.Pos) ChorusPlant {
	if shaped, ok := world.ShapeFromNeighbours(src, pos, ChorusPlant{}); ok {
		if c, ok := shaped.(ChorusPlant); ok {
			return c
		}
	}
	return ChorusPlant{}
}

// Model ...
func (ChorusPlant) Model() world.BlockModel {
	return model.ChorusPlant{}
}

// BreakInfo ...
func (ChorusPlant) BreakInfo() BreakInfo {
	return newBreakInfo(0.4, alwaysHarvestable, nothingEffective, func(item.Tool, []item.Enchantment) []item.Stack {
		if amount := rand.IntN(2); amount != 0 {
			return []item.Stack{item.NewStack(item.ChorusFruit{}, amount)}
		}
		return nil
	})
}

// NeighbourUpdateTick breaks a plant that cannot survive, and connects the
// rest to the block that changed, as ChorusPlantBlock.updateShape does.
func (c ChorusPlant) NeighbourUpdateTick(pos, changedNeighbour cube.Pos, tx *world.Tx) {
	if !c.canSurviveAt(pos, tx) {
		breakBlock(c, pos, tx)
		return
	}
	if shaped, ok := tx.ShapeUpdate(pos, changedNeighbour); ok {
		if p, ok := shaped.(ChorusPlant); ok && p != c {
			tx.SetBlock(pos, p, nil)
		}
	}
}

// HasLiquidDrops ...
func (ChorusPlant) HasLiquidDrops() bool {
	return true
}

// FlammabilityInfo ...
func (ChorusPlant) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(60, 100, false)
}

// UseOnBlock ...
func (c ChorusPlant) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, c)
	if !used || !c.canSurviveAt(pos, tx) {
		return false
	}
	place(tx, pos, chorusPlantAt(tx, pos), user, ctx)
	return placed(ctx)
}

// EncodeItem ...
func (ChorusPlant) EncodeItem() (name string, meta int16) {
	return "minecraft:chorus_plant", 0
}

// EncodeBlock ...
func (c ChorusPlant) EncodeBlock() (name string, properties map[string]any) {
	return "minecraft:chorus_plant", map[string]any{
		"minecraft:connection_north": boolByte(c.North),
		"minecraft:connection_east":  boolByte(c.East),
		"minecraft:connection_south": boolByte(c.South),
		"minecraft:connection_west":  boolByte(c.West),
		"minecraft:connection_up":    boolByte(c.Up),
		"minecraft:connection_down":  boolByte(c.Down),
	}
}

// allChorusPlants returns a chorus plant of every set of sides.
func allChorusPlants() (b []world.Block) {
	for sides := range 64 {
		b = append(b, ChorusPlant{North: sides&1 != 0, East: sides&2 != 0, South: sides&4 != 0, West: sides&8 != 0,
			Up: sides&16 != 0, Down: sides&32 != 0})
	}
	return
}

func (ChorusPlant) canSurviveAt(pos cube.Pos, tx *world.Tx) bool {
	_, upAir := tx.Block(pos.Side(cube.FaceUp)).(Air)
	_, downAir := tx.Block(pos.Side(cube.FaceDown)).(Air)

	for _, face := range cube.HorizontalFaces() {
		side := pos.Side(face)
		if _, ok := tx.Block(side).(ChorusPlant); !ok {
			continue
		}
		if !upAir && !downAir {
			return false
		}
		switch tx.Block(side.Side(cube.FaceDown)).(type) {
		case ChorusPlant, EndStone:
			return true
		}
	}

	switch tx.Block(pos.Side(cube.FaceDown)).(type) {
	case ChorusPlant, EndStone:
		return true
	}
	return false
}
