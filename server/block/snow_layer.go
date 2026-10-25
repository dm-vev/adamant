package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

var hashSnowLayer = NextHash()

// SnowLayer is a non-solid block that can have up to 8 layers.
type SnowLayer struct {
	transparent
	sourceWaterDisplacer

	// Layers is the amount of snow layers, in the range 1..8.
	Layers uint8
	// Covered is the covered_bit Bedrock keeps for snow lying over a plant.
	Covered bool
}

// allSnowLayers returns every state of snow layers.
func allSnowLayers() (layers []world.Block) {
	for _, covered := range []bool{false, true} {
		for i := uint8(1); i <= 8; i++ {
			layers = append(layers, SnowLayer{Layers: i, Covered: covered})
		}
	}
	return layers
}

// layers returns the layers of the snow, in the range 1..8.
func (s SnowLayer) layers() uint8 {
	return min(max(s.Layers, 1), 8)
}

// Model ...
func (s SnowLayer) Model() world.BlockModel {
	return model.SnowLayer{Layers: s.layers()}
}

// SideClosed ...
func (SnowLayer) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// ReplaceableBy ports SnowLayerBlock.canBeReplaced: a single layer gives way
// to any block, and snow is stacked on snow of fewer than 8 layers.
func (s SnowLayer) ReplaceableBy(b world.Block) bool {
	if _, snow := b.(SnowLayer); snow && s.layers() < 8 {
		return true
	}
	return s.layers() == 1
}

// BreakInfo ports the loot table of snow: a shovel digs up a snowball a
// layer, or with Silk Touch the layers themselves, or a snow block for 8.
func (s SnowLayer) BreakInfo() BreakInfo {
	return newBreakInfo(0.1, shovelEffective, shovelEffective, func(_ item.Tool, enchantments []item.Enchantment) []item.Stack {
		n := int(s.layers())
		if hasSilkTouch(enchantments) {
			if n == 8 {
				return []item.Stack{item.NewStack(Snow{}, 1)}
			}
			return []item.Stack{item.NewStack(SnowLayer{Layers: 1}, n)}
		}
		return []item.Stack{item.NewStack(item.Snowball{}, n)}
	})
}

// RandomTick ports SnowLayerBlock.randomTick: snow melts away where block
// light is above 11. Melting drops nothing, as the loot table of snow needs
// an entity to break it.
func (s SnowLayer) RandomTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if tx.BlockLight(pos) > 11 {
		tx.SetBlock(pos, nil, nil)
		tx.EmitGameEvent(world.GameEventBlockDestroy, pos.Vec3Centre(), world.GameEventContext{AffectedState: s})
	}
}

// NeighbourUpdateTick ports SnowLayerBlock.updateShape: snow that can no
// longer stay turns into air, dropping nothing.
func (s SnowLayer) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !snowLayerSurvives(tx, pos) {
		tx.SetBlock(pos, nil, nil)
	}
}

// snowLayerSurvives ports SnowLayerBlock.canSurvive: snow does not lie on
// ice and barriers, always lies on honey, mud and soul sand, and otherwise
// needs a full face below.
func snowLayerSurvives(tx *world.Tx, pos cube.Pos) bool {
	below := pos.Side(cube.FaceDown)
	if javaBlockTag(tx, below, "minecraft:cannot_support_snow_layer") {
		return false
	}
	if javaBlockTag(tx, below, "minecraft:support_override_snow_layer") {
		return true
	}
	return tx.Block(below).Model().FaceSolid(below, cube.FaceUp, tx)
}

// EncodeItem ...
func (SnowLayer) EncodeItem() (name string, meta int16) {
	return "minecraft:snow_layer", 0
}

// EncodeBlock ...
func (s SnowLayer) EncodeBlock() (name string, properties map[string]any) {
	return "minecraft:snow_layer", map[string]any{"height": int32(s.layers() - 1), "covered_bit": boolByte(s.Covered)}
}

// Hash ...
func (s SnowLayer) Hash() (uint64, uint64) {
	return hashSnowLayer, uint64(s.layers()-1) | uint64(boolByte(s.Covered))<<3
}

// UseOnBlock ports SnowLayerBlock.getStateForPlacement and canBeReplaced:
// snow clicked on its top, or placed where snow already lies, gets a layer
// more; otherwise a single layer is placed where it can lie.
func (s SnowLayer) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	if clicked, ok := tx.Block(pos).(SnowLayer); ok {
		if face == cube.FaceUp && clicked.layers() < 8 {
			clicked.Layers = clicked.layers() + 1
			place(tx, pos, clicked, user, ctx)
			return placed(ctx)
		}
		// Snow clicked on its side is not replaced: the layer goes next to
		// it.
		pos = pos.Side(face)
	} else if !replaceableWith(tx, pos, s) {
		pos = pos.Side(face)
	}
	if existing, ok := tx.Block(pos).(SnowLayer); ok {
		if existing.layers() >= 8 {
			return false
		}
		existing.Layers = existing.layers() + 1
		place(tx, pos, existing, user, ctx)
		return placed(ctx)
	}
	if !replaceableWith(tx, pos, s) || !snowLayerSurvives(tx, pos) {
		return false
	}
	place(tx, pos, SnowLayer{Layers: 1}, user, ctx)
	return placed(ctx)
}
