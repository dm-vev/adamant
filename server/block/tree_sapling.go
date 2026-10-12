package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

var treeSaplingHash = NextHash()

// TreeSapling is a placeable Java tree sapling or mangrove propagule.
type TreeSapling struct {
	empty
	transparent
	replaceable
	bass

	// Type is the exact Java item and block identifier suffix.
	Type    string
	Stage   int
	Age     int
	Hanging bool
}

// UseOnBlock places the matching sapling or propagule on a supported surface.
func (s TreeSapling) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, s)
	if !used {
		return false
	}
	s.Stage, s.Age, s.Hanging = 0, 0, false
	if s.Type == "mangrove_propagule" {
		s.Age = 4
	}
	if !s.canSurvive(pos, tx) {
		return false
	}
	liquid, waterlogged := tx.Liquid(pos)
	waterlogged = waterlogged && s.CanDisplace(liquid)
	place(tx, pos, s, user, ctx)
	if placed(ctx) && waterlogged {
		tx.SetLiquid(pos, Water{Depth: 8, Still: true})
	}
	return placed(ctx)
}

// NeighbourUpdateTick breaks saplings that lose their supporting block.
func (s TreeSapling) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !s.canSurvive(pos, tx) {
		breakBlock(s, pos, tx)
	}
}

func (s TreeSapling) canSurvive(pos cube.Pos, tx *world.Tx) bool {
	if s.Type == "mangrove_propagule" {
		if s.Hanging {
			leaves, ok := tx.Block(pos.Side(cube.FaceUp)).(Leaves)
			return ok && leaves.Type == MangroveLeaves()
		}
		if _, clay := tx.Block(pos.Side(cube.FaceDown)).(Clay); clay {
			return true
		}
	}
	return supportsVegetation(s, tx.Block(pos.Side(cube.FaceDown)))
}

// BreakInfo ...
func (s TreeSapling) BreakInfo() BreakInfo {
	if s.Type == "mangrove_propagule" && s.Age < 4 {
		return newBreakInfo(0, alwaysHarvestable, nothingEffective, simpleDrops()).withBlastResistance(0)
	}
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(s)).withBlastResistance(0)
}

// HasLiquidDrops ...
func (TreeSapling) HasLiquidDrops() bool { return true }

// CanDisplace ...
func (s TreeSapling) CanDisplace(liquid world.Liquid) bool {
	return s.Type == "mangrove_propagule" && (flowingWaterDisplacer{}).CanDisplace(liquid)
}

// SideClosed ...
func (TreeSapling) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool { return false }

// EncodeItem ...
func (s TreeSapling) EncodeItem() (string, int16) { return "minecraft:" + s.Type, 0 }

// EncodeBlock ...
func (s TreeSapling) EncodeBlock() (string, map[string]any) {
	if s.Type == "mangrove_propagule" {
		return "minecraft:" + s.Type, map[string]any{"hanging": s.Hanging, "propagule_stage": int32(s.Age)}
	}
	return "minecraft:" + s.Type, map[string]any{"age_bit": boolByte(s.Stage != 0)}
}

// Hash ...
func (s TreeSapling) Hash() (uint64, uint64) {
	var kind uint64
	switch s.Type {
	case "oak_sapling":
		kind = 0
	case "spruce_sapling":
		kind = 1
	case "birch_sapling":
		kind = 2
	case "jungle_sapling":
		kind = 3
	case "acacia_sapling":
		kind = 4
	case "dark_oak_sapling":
		kind = 5
	case "cherry_sapling":
		kind = 6
	case "pale_oak_sapling":
		kind = 7
	case "poplar_sapling":
		kind = 8
	case "mangrove_propagule":
		kind = 9
	}
	return treeSaplingHash, kind | uint64(s.Stage)<<4 | uint64(s.Age)<<5 | uint64(boolByte(s.Hanging))<<8
}

// FlammabilityInfo ...
func (TreeSapling) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(60, 100, false)
}

// allTreeSaplings returns every registered Java tree sapling state.
func allTreeSaplings() []world.Block {
	names := [...]string{"oak_sapling", "spruce_sapling", "birch_sapling", "jungle_sapling", "acacia_sapling", "dark_oak_sapling", "cherry_sapling", "pale_oak_sapling", "poplar_sapling", "mangrove_propagule"}
	blocks := make([]world.Block, 0, len(names)*2+10)
	for _, name := range names {
		if name != "mangrove_propagule" {
			blocks = append(blocks, TreeSapling{Type: name}, TreeSapling{Type: name, Stage: 1})
			continue
		}
		for age := range 5 {
			for _, hanging := range []bool{false, true} {
				blocks = append(blocks, TreeSapling{Type: name, Age: age, Hanging: hanging})
			}
		}
	}
	return blocks
}
