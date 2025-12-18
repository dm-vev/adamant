package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

var hashSnowLayer = NextHash()

// SnowLayer is a non-solid block that can have up to 8 layers.
type SnowLayer struct {
	replaceable
	transparent
	sourceWaterDisplacer

	// Layers is the amount of snow layers, in the range 1..8.
	Layers uint8
}

// Permutations ...
func (SnowLayer) Permutations() []world.Block {
	perms := make([]world.Block, 0, 8)
	for i := uint8(1); i <= 8; i++ {
		perms = append(perms, SnowLayer{Layers: i})
	}
	return perms
}

// Model ...
func (s SnowLayer) Model() world.BlockModel {
	return model.SnowLayer{Layers: s.Layers}
}

// SideClosed ...
func (SnowLayer) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// BreakInfo ...
func (s SnowLayer) BreakInfo() BreakInfo {
	// Vanilla drops snowballs depending on layers. For now, keep it simple.
	return newBreakInfo(0.1, alwaysHarvestable, shovelEffective, oneOf(s))
}

// EncodeItem ...
func (SnowLayer) EncodeItem() (name string, meta int16) {
	return "minecraft:snow_layer", 0
}

// EncodeBlock ...
func (s SnowLayer) EncodeBlock() (name string, properties map[string]any) {
	layers := s.Layers
	if layers < 1 {
		layers = 1
	}
	if layers > 8 {
		layers = 8
	}
	// Bedrock uses "height" in the range 0..7 (int32 in the palette), plus a covered_bit (uint8).
	return "minecraft:snow_layer", map[string]any{"height": int32(layers - 1), "covered_bit": uint8(0)}
}

// Hash ...
func (s SnowLayer) Hash() (uint64, uint64) {
	layers := s.Layers
	if layers < 1 {
		layers = 1
	}
	if layers > 8 {
		layers = 8
	}
	return hashSnowLayer, uint64(layers-1) | (uint64(0) << 3)
}

// NeighbourUpdateTick ...
func (s SnowLayer) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if _, ok := tx.Block(pos.Side(cube.FaceDown)).(Air); ok {
		breakBlock(s, pos, tx)
	}
}

// UseOnBlock places the snow layer, stacking if possible.
func (s SnowLayer) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, s)
	if !used {
		return false
	}
	if below, ok := tx.Block(pos).(SnowLayer); ok {
		if below.Layers < 8 {
			below.Layers++
			tx.SetBlock(pos, below, nil)
			return placed(ctx)
		}
	}
	if _, ok := tx.Block(pos.Side(cube.FaceDown)).(Air); ok {
		return false
	}
	if s.Layers < 1 || s.Layers > 8 {
		s.Layers = 1
	}
	place(tx, pos, s, user, ctx)
	return placed(ctx)
}
