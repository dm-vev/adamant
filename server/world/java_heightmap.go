package world

import (
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// JavaMotionBlockingHeight returns the first free Y coordinate above the highest
// native state in either layer that Java's motion-blocking heightmap includes.
func (tx *Tx) JavaMotionBlockingHeight(x, z int, noLeaves bool) int {
	tag := "minecraft:blocks_motion_in_heightmap"
	cacheIndex := 0
	if noLeaves {
		tag = "minecraft:blocks_motion_in_heightmap_no_leaves"
		cacheIndex = 1
	}
	w := tx.World()
	cache := w.javaHeightmapTags[cacheIndex]
	if cache == nil {
		cache = make(map[uint32]bool)
		w.javaHeightmapTags[cacheIndex] = cache
	}
	rng := tx.Range()
	column := tx.chunk(ChunkPos{int32(x >> 4), int32(z >> 4)})
	registry := tx.World().BlockRegistry()
	air := registry.AirRuntimeID()
	for y := rng.Max(); y >= rng.Min(); y-- {
		for layer := uint8(0); layer < 2; layer++ {
			rid := column.Block(uint8(x&15), int16(y), uint8(z&15), layer)
			if rid == air {
				continue
			}
			if registry.LiquidBlock(rid) {
				return y + 1
			}
			matches, known := cache[rid]
			if !known {
				state, err := CanonicalState(registry, NativeState{Block: rid, Liquid: air})
				if err != nil {
					panic(err)
				}
				matches, err = gamedata.BlockTagContains(tag, state.Name)
				if err != nil {
					panic(err)
				}
				cache[rid] = matches
			}
			if matches {
				return y + 1
			}
		}
	}
	return rng.Min()
}

// WaterSourceRuntimeID returns the native runtime ID for Java source water.
func WaterSourceRuntimeID(registry chunk.BlockRegistry) (uint32, bool) {
	if registry == nil {
		return 0, false
	}
	return registry.StateToRuntimeID("minecraft:water", waterSourceProperties)
}
