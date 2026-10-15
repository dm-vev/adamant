package overworld

import (
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// featureColumn holds what a region knows of a column: the highest write to
// its overlay and the heights its heightmap scans found.
type featureColumn struct {
	overlayTop int32
	hasOverlay bool
	// known has a bit for every heightmap kind with a height.
	known  uint8
	height [heightmapKinds]int32
}

// invalidatePlacementHeight forgets the heights of the column of p that a
// write at p may change.
func (r *featureRegion) invalidatePlacementHeight(p featurePos) {
	key := featureColumnKey(p.x, p.z)
	// ProtoChunk stops updating the WORLDGEN heightmaps at TERRAIN status.
	if column, ok := r.columns[key]; ok && column.known&^heightmapFrozenKinds != 0 {
		column.known &= heightmapFrozenKinds
		r.columns[key] = column
	}
}

func (r *featureRegion) placementHeight(x, z int32, heightmap string) int32 {
	kind, mask, frozen := heightmapOf(heightmap)
	key := featureColumnKey(x, z)
	column := r.columns[key]
	if column.known&(1<<kind) != 0 {
		return column.height[kind]
	}
	height := r.MinY()
	for y := min(r.MaxY(), r.columnTop(x, z, frozen)); y >= r.MinY(); y-- {
		var state world.NativeState
		if frozen {
			state = r.baseState(featurePos{x, y, z})
		} else {
			state = r.nativeState(featurePos{x, y, z})
		}
		if r.f.heightmapFlags(r.c, state)&mask != 0 {
			height = y + 1
			break
		}
	}
	column.known |= 1 << kind
	column.height[kind] = height
	if r.columns == nil {
		r.columns = make(map[uint64]featureColumn)
	}
	r.columns[key] = column
	return height
}

// columnTop returns a height at or above every block the heightmap scan of the
// column can read, so the scan skips the air above them: the base terrain for
// the WORLDGEN heightmaps, otherwise the target chunk or base terrain and the
// writes of the features.
func (r *featureRegion) columnTop(x, z int32, frozen bool) int32 {
	var top int32
	switch {
	case !frozen && r.inTarget(featurePos{x, r.MinY(), z}):
		top = NativeColumnTop(r.c, uint8(x&15), uint8(z&15))
	case r.base != nil:
		top = NativeColumnTop(r.baseChunk(world.ChunkPos{x >> 4, z >> 4}), uint8(x&15), uint8(z&15))
	default:
		source, ok := r.f.source.(baseTopSource)
		if !ok {
			return r.MaxY()
		}
		top = source.BaseTop(x, z)
	}
	if frozen {
		return top
	}
	if column, ok := r.columns[featureColumnKey(x, z)]; ok && column.hasOverlay {
		top = max(top, column.overlayTop)
	}
	return top
}

// noteOverlay records that the overlay holds a state at p.
func (r *featureRegion) noteOverlay(p featurePos) {
	key := featureColumnKey(p.x, p.z)
	column, ok := r.columns[key]
	if ok && column.hasOverlay && column.overlayTop >= p.y {
		return
	}
	column.overlayTop, column.hasOverlay = p.y, true
	if r.columns == nil {
		r.columns = make(map[uint64]featureColumn)
	}
	r.columns[key] = column
}

// NativeColumnTop returns the highest y of the column holding a non-air
// state in either layer, or one below the chunk if it holds none.
func NativeColumnTop(c *chunk.Chunk, x, z uint8) int32 {
	air := c.BlockRegistry().AirRuntimeID()
	subs := c.Sub()
	for index := len(subs) - 1; index >= 0; index-- {
		sub := subs[index]
		if sub.Empty() {
			continue
		}
		layers := sub.Layers()
		for y := 15; y >= 0; y-- {
			for _, layer := range layers {
				if layer.At(x, uint8(y), z) != air {
					return int32(c.SubY(int16(index))) + int32(y)
				}
			}
		}
	}
	return int32(c.Range()[0]) - 1
}

// Heightmap predicates of a native state.
const (
	heightmapWorldSurface uint8 = 1 << iota
	heightmapMotion
	heightmapMotionNoLeaves
	heightmapFluid
)

// heightmapKinds is the number of Java heightmaps; heightmapFrozenKinds has
// the bits of the WORLDGEN ones among them.
const (
	heightmapKinds             = 6
	heightmapFrozenKinds uint8 = 1<<1 | 1<<3
)

// heightmapOf returns the index of a Java heightmap, the predicates it counts
// a block by and whether it is a WORLDGEN heightmap frozen at the base
// terrain.
func heightmapOf(heightmap string) (kind, mask uint8, frozen bool) {
	switch heightmap {
	case "WORLD_SURFACE":
		return 0, heightmapWorldSurface, false
	case "WORLD_SURFACE_WG":
		return 1, heightmapWorldSurface, true
	case "OCEAN_FLOOR":
		return 2, heightmapMotion, false
	case "OCEAN_FLOOR_WG":
		return 3, heightmapMotion, true
	case "MOTION_BLOCKING":
		return 4, heightmapMotion | heightmapFluid, false
	case "MOTION_BLOCKING_NO_LEAVES":
		return 5, heightmapMotionNoLeaves | heightmapFluid, false
	}
	panic("overworld: unsupported Java heightmap " + heightmap)
}

// heightmapFlags returns the heightmap predicates the native state meets. Its
// Java state and tags are resolved once per state: heightmap scans test every
// block of a column.
func (f *ModernFeatures) heightmapFlags(c *chunk.Chunk, state world.NativeState) uint8 {
	if flags, ok := f.heightmaps.Load(state); ok {
		return flags.(uint8)
	}
	javaState, err := world.JavaState(c.BlockRegistry(), state)
	if err != nil {
		panic(err)
	}
	name := normalizeJavaResource(javaState.Name)
	var flags uint8
	if name != "air" && name != "cave_air" && name != "void_air" {
		flags |= heightmapWorldSurface
	}
	if javaBlockTagContains("minecraft:blocks_motion_in_heightmap", name) {
		flags |= heightmapMotion
	}
	if javaBlockTagContains("minecraft:blocks_motion_in_heightmap_no_leaves", name) {
		flags |= heightmapMotionNoLeaves
	}
	if featureStateHasFluid(c, state) {
		flags |= heightmapFluid
	}
	f.heightmaps.Store(state, flags)
	return flags
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
