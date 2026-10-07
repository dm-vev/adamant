package overworld

import (
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
)

type featureHeightKey struct {
	x, z int32
	kind string
}

func (r *featureRegion) invalidatePlacementHeight(p featurePos) {
	if r.placementHeights == nil {
		return
	}
	for _, kind := range [...]string{"WORLD_SURFACE", "WORLD_SURFACE_WG", "OCEAN_FLOOR", "OCEAN_FLOOR_WG", "MOTION_BLOCKING", "MOTION_BLOCKING_NO_LEAVES"} {
		delete(r.placementHeights, featureHeightKey{p.x, p.z, kind})
	}
}
func (r *featureRegion) placementHeight(x, z int32, heightmap string) int32 {
	key := featureHeightKey{x, z, heightmap}
	if height, ok := r.placementHeights[key]; ok {
		return height
	}
	for y := r.MaxY(); y >= r.MinY(); y-- {
		state := r.nativeState(featurePos{x, y, z})
		javaState, err := world.JavaState(r.c.BlockRegistry(), state)
		if err != nil {
			panic(err)
		}
		name := normalizeJavaResource(javaState.Name)
		matches := false
		switch heightmap {
		case "WORLD_SURFACE", "WORLD_SURFACE_WG":
			matches = name != "air" && name != "cave_air" && name != "void_air"
		case "OCEAN_FLOOR", "OCEAN_FLOOR_WG":
			matches, err = javadata.BlockTagContains("minecraft:blocks_motion_in_heightmap", name)
			if err != nil {
				panic(err)
			}
		case "MOTION_BLOCKING":
			matches, err = javadata.BlockTagContains("minecraft:blocks_motion_in_heightmap", name)
			if err != nil {
				panic(err)
			}
			matches = matches || featureStateHasFluid(r.c, state)
		case "MOTION_BLOCKING_NO_LEAVES":
			matches, err = javadata.BlockTagContains("minecraft:blocks_motion_in_heightmap_no_leaves", name)
			if err != nil {
				panic(err)
			}
			matches = matches || featureStateHasFluid(r.c, state)
		default:
			panic("overworld: unsupported Java heightmap " + heightmap)
		}
		if matches {
			if r.placementHeights == nil {
				r.placementHeights = make(map[featureHeightKey]int32)
			}
			r.placementHeights[key] = y + 1
			return y + 1
		}
	}
	if r.placementHeights == nil {
		r.placementHeights = make(map[featureHeightKey]int32)
	}
	r.placementHeights[key] = r.MinY()
	return r.MinY()
}

// countEveryLayerPositions mirrors CountOnEveryLayerPlacement.modify and its
// ground-layer scan. The provider is sampled once per iteration of Java's
// for-loop condition, not once for the whole placement.
func countEveryLayerPositions(region *featureRegion, base featurePos, provider any, random *java26.Random, out []featurePos) []featurePos {
	layer := int32(0)
	for {
		foundAny := false
		for i := int32(0); i < sampleJavaInt(provider, random); i++ {
			x := random.NextInt(16) + base.x
			z := random.NextInt(16) + base.z
			startY := region.placementHeight(x, z, "MOTION_BLOCKING")
			y := findEveryLayerGroundY(region, x, startY, z, layer)
			if y != int32(^uint32(0)>>1) {
				out = append(out, featurePos{x, y, z})
				foundAny = true
			}
		}
		layer++
		if !foundAny {
			return out
		}
	}
}

func findEveryLayerGroundY(region *featureRegion, xStart, yStart, zStart, layerToPlaceOn int32) int32 {
	currentLayer := int32(0)
	current := featurePos{xStart, yStart, zStart}
	currentName, _ := region.State(current)
	currentEmpty := everyLayerEmpty(currentName)
	for y := yStart; y >= region.MinY()+1; y-- {
		below := featurePos{xStart, y - 1, zStart}
		belowName, _ := region.State(below)
		belowEmpty := everyLayerEmpty(belowName)
		if !belowEmpty && currentEmpty && normalizeJavaResource(belowName) != "bedrock" {
			if currentLayer == layerToPlaceOn {
				return y
			}
			currentLayer++
		}
		currentEmpty = belowEmpty
	}
	return int32(^uint32(0) >> 1)
}

func everyLayerEmpty(name string) bool {
	switch normalizeJavaResource(name) {
	case "air", "cave_air", "void_air", "water", "lava":
		return true
	default:
		return false
	}
}
