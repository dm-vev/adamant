package world

import (
	"github.com/df-mc/dragonfly/server/block/cube"
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
	cache := &w.javaHeightmapTags[cacheIndex]
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
			matches, known := cache.load(rid)
			if !known {
				matches = javaBlocksMotion(tx, registry, rid, tag)
				cache.store(rid, matches)
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

// javaBlocksMotion reports whether the native block rid is in the heightmap
// tag, as game logic reads its Java block (gameJavaState). A block of no
// Java block blocks motion if anything collides with it.
func javaBlocksMotion(tx *Tx, registry BlockRegistry, rid uint32, tag string) bool {
	id, ok := gameJavaState(registry, NativeState{Block: rid, Liquid: registry.AirRuntimeID()})
	if !ok {
		b, _ := registry.BlockByRuntimeID(rid)
		return b != nil && len(b.Model().BBox(cube.Pos{}, tx)) != 0
	}
	name, _ := gamedata.BlockStateName(id)
	matches, err := gamedata.BlockTagContains(tag, name)
	if err != nil {
		panic(err)
	}
	return matches
}
