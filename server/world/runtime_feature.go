package world

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
)

// RuntimeFeatures places Java configured and placed features into a live
// world, as saplings growing into trees, mushrooms growing huge and bone meal
// spreading vegetation do with ConfiguredFeature.place.
type RuntimeFeatures interface {
	// PlaceConfigured places the configured feature name at pos.
	PlaceConfigured(tx *Tx, name string, pos cube.Pos, random *java26.Random) bool
	// PlacePlaced places the placed feature name at pos.
	PlacePlaced(tx *Tx, name string, pos cube.Pos, random *java26.Random) bool
	// BoneMealFeatures returns the configured features of the biome at pos
	// that bone meal may spawn on grass, as
	// BiomeGenerationSettings.getBoneMealFeatures does.
	BoneMealFeatures(tx *Tx, pos cube.Pos) []string
	// FeatureConfig returns the type and data of the configured feature
	// name, such as a tree's trunk_placer, or false if there is none.
	FeatureConfig(name string) (typeName string, config map[string]any, ok bool)
}

var runtimeFeatures RuntimeFeatures

// RegisterRuntimeFeatures sets the features worlds place at runtime. The Java
// feature data lives with the world generator, which registers it.
func RegisterRuntimeFeatures(f RuntimeFeatures) { runtimeFeatures = f }

// Features returns the features worlds place at runtime, or false if none
// were registered.
func (tx *Tx) Features() (RuntimeFeatures, bool) {
	return runtimeFeatures, runtimeFeatures != nil
}

// FeatureRandom returns a new random for a feature placed at runtime, as
// ServerLevel's random, which Java seeds anew for every level.
func FeatureRandom() *java26.Random {
	r := java26.NewLegacyRandom(rand.Int64())
	return &r
}

// NativeState returns both native layers of the block at pos, with the Java
// state they retain.
func (tx *Tx) NativeState(pos cube.Pos) NativeState {
	registry := tx.World().conf.Blocks
	if pos.OutOfBounds(tx.Range()) {
		return NativeState{Block: registry.AirRuntimeID(), Liquid: registry.AirRuntimeID()}
	}
	c := tx.chunk(chunkPosFromBlockPos(pos))
	return NativeStateAt(c.Chunk, uint8(pos[0]&15), int16(pos[1]), uint8(pos[2]&15))
}

// SetFeatureState sets both native layers of the block at pos as a feature
// placed at runtime leaves them, notifying the neighbours. actor is the block
// entity of a block that has one; without one, the block gets a new one.
func (tx *Tx) SetFeatureState(pos cube.Pos, state NativeState, actor Block) {
	if pos.OutOfBounds(tx.Range()) {
		return
	}
	registry := tx.World().conf.Blocks
	b := actor
	if b == nil {
		b = registry.BlockByRuntimeIDOrAir(state.Block)
		if prototype, ok := b.(NBTer); ok {
			b = DecodeNBT(prototype, map[string]any{}, registry).(Block)
		}
	}
	tx.setBlock(pos, b, &SetOpts{DisableLiquidDisplacement: true, nativeState: &state})
}
