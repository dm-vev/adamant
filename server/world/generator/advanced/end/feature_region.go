package end

import (
	"math/bits"

	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/internal/vanilla"
	"github.com/dm-vev/adamant/server/world"
	"github.com/dm-vev/adamant/server/world/chunk"
)

const endReplayRadius = 1

// Each replayed origin needs a one-chunk read/write halo, making a five-chunk raw region.
const endFeatureRadius = 4*endReplayRadius + 1
const endFeatureChunkCount = endFeatureRadius * endFeatureRadius

type endFeatureRegion struct {
	chunks               [endFeatureChunkCount]*chunk.Chunk
	terrains             [endFeatureChunkCount]*endTerrainChunk
	overlays             [endFeatureChunkCount]map[uint32]world.NativeState
	overlayMaxY          [endFeatureChunkCount]int
	dirtyOverlays        uint32
	sourceBiomes         [endFeatureChunkCount]endGenerationBiomes
	minChunkX, minChunkZ int
	targetX, targetZ     int
	minY, maxY           int
	airRID               uint32
	endStoneRID          uint32
	airStateID           uint32
	endStoneStateID      uint32
	registry             chunk.BlockRegistry
	shapeRandoms         map[[2]int]*vanilla.Random
	scheduledTicks       map[chunk.ScheduledTick]struct{}
	blockActors          map[cube.Pos]endBlockActor
	logicalStates        map[cube.Pos]world.NativeState
}

type endFeatureOrigin struct {
	region         *endFeatureRegion
	chunkX, chunkZ int
}

func newEndFeatureRegion(g *End, targetX, targetZ int, target *chunk.Chunk, scratch *endScratch) *endFeatureRegion {
	var region *endFeatureRegion
	if scratch != nil && scratch.region != nil {
		region = scratch.region
		region.resetForPool()
	} else {
		region = &endFeatureRegion{}
	}
	region.minChunkX = targetX - endFeatureRadius/2
	region.minChunkZ = targetZ - endFeatureRadius/2
	region.targetX = targetX
	region.targetZ = targetZ
	region.minY = target.Range().Min()
	region.maxY = target.Range().Max()
	st := g.state()
	region.airRID = g.airRID
	region.endStoneRID = g.endStoneRID
	region.airStateID = st.airStateID
	region.endStoneStateID = st.endStoneStateID
	region.registry = target.BlockRegistry()

	g.fillBiomes(target)
	for chunkX := region.minChunkX; chunkX < region.minChunkX+endFeatureRadius; chunkX++ {
		for chunkZ := region.minChunkZ; chunkZ < region.minChunkZ+endFeatureRadius; chunkZ++ {
			idx := region.index(chunkX, chunkZ)
			terrain := g.terrainChunk(chunkX, chunkZ, scratch)
			region.terrains[idx] = terrain
			region.sourceBiomes[idx] = terrain.sourceBiome
			if chunkX == targetX && chunkZ == targetZ {
				region.chunks[idx] = target
				terrain.writeToChunk(target, g.endStoneRID)
			}
		}
	}
	return region
}

func (r *endFeatureRegion) resetForPool() {
	for mask := r.dirtyOverlays; mask != 0; {
		i := bits.TrailingZeros32(mask)
		mask &= mask - 1
		clear(r.overlays[i])
	}
	r.dirtyOverlays = 0
	r.chunks = [endFeatureChunkCount]*chunk.Chunk{}
	r.terrains = [endFeatureChunkCount]*endTerrainChunk{}
	if len(r.shapeRandoms) != 0 {
		clear(r.shapeRandoms)
	}
	if len(r.scheduledTicks) != 0 {
		clear(r.scheduledTicks)
	}
	if len(r.blockActors) != 0 {
		clear(r.blockActors)
	}
	if len(r.logicalStates) != 0 {
		clear(r.logicalStates)
	}
}

func (r *endFeatureRegion) blockRegistry() chunk.BlockRegistry {
	if r.registry != nil {
		return r.registry
	}
	if c := r.chunks[r.index(r.targetX, r.targetZ)]; c != nil {
		return c.BlockRegistry()
	}
	for _, c := range r.chunks {
		if c != nil {
			return c.BlockRegistry()
		}
	}
	return world.DefaultBlockRegistry
}

func (r *endFeatureRegion) blockRange() cube.Range {
	return cube.Range{r.minY, r.maxY}
}

func (r *endFeatureRegion) index(chunkX, chunkZ int) int {
	return (chunkX-r.minChunkX)*endFeatureRadius + chunkZ - r.minChunkZ
}

func (r *endFeatureRegion) containsChunk(chunkX, chunkZ int) bool {
	return chunkX >= r.minChunkX && chunkX < r.minChunkX+endFeatureRadius && chunkZ >= r.minChunkZ && chunkZ < r.minChunkZ+endFeatureRadius
}

func (r *endFeatureRegion) sourceShapeRandom(seed int64, chunkX, chunkZ int) *vanilla.Random {
	if r.shapeRandoms == nil {
		r.shapeRandoms = make(map[[2]int]*vanilla.Random)
	}
	key := [2]int{chunkX, chunkZ}
	if random := r.shapeRandoms[key]; random != nil {
		return random
	}
	random := endWorldgenRegionRandom(seed, chunkX, chunkZ)
	r.shapeRandoms[key] = &random
	return &random
}

func (r *endFeatureRegion) block(worldX, worldY, worldZ int) uint32 {
	if worldY < r.minY || worldY > r.maxY {
		return r.airRID
	}
	chunkX, chunkZ := worldX>>4, worldZ>>4
	if !r.containsChunk(chunkX, chunkZ) {
		return r.airRID
	}
	if len(r.logicalStates) != 0 {
		if state, ok := r.logicalStates[cube.Pos{worldX, worldY, worldZ}]; ok {
			return state.Block
		}
	}
	i := r.index(chunkX, chunkZ)
	localX, localZ := worldX&15, worldZ&15
	if r.dirtyOverlays&(1<<uint(i)) != 0 || (r.terrains[i] == nil && len(r.overlays[i]) != 0) {
		key := uint32(worldY-r.minY)<<8 | uint32(localZ)<<4 | uint32(localX)
		if state, ok := r.overlays[i][key]; ok {
			return state.Block
		}
	}
	if terrain := r.terrains[i]; terrain != nil {
		if terrain.hasBlock(localX, worldY-r.minY, localZ) {
			return r.endStoneRID
		}
		return r.airRID
	}
	return r.chunks[i].Block(uint8(localX), int16(worldY), uint8(localZ), 0)
}

func (r *endFeatureRegion) nativeState(worldX, worldY, worldZ int) world.NativeState {
	if worldY < r.minY || worldY > r.maxY {
		return world.NativeState{Block: r.airRID, Liquid: r.airRID}
	}
	if len(r.logicalStates) != 0 {
		if state, ok := r.logicalStates[cube.Pos{worldX, worldY, worldZ}]; ok {
			return state
		}
	}
	chunkX, chunkZ := worldX>>4, worldZ>>4
	if !r.containsChunk(chunkX, chunkZ) {
		return world.NativeState{Block: r.airRID, Liquid: r.airRID}
	}
	i := r.index(chunkX, chunkZ)
	localX, localZ := worldX&15, worldZ&15
	if r.dirtyOverlays&(1<<uint(i)) != 0 || (r.terrains[i] == nil && len(r.overlays[i]) != 0) {
		key := uint32(worldY-r.minY)<<8 | uint32(localZ)<<4 | uint32(localX)
		if state, ok := r.overlays[i][key]; ok {
			return state
		}
	}
	if terrain := r.terrains[i]; terrain != nil {
		if terrain.hasBlock(localX, worldY-r.minY, localZ) {
			return world.NativeState{Block: r.endStoneRID, Liquid: r.airRID}
		}
		return world.NativeState{Block: r.airRID, Liquid: r.airRID}
	}
	return world.NativeStateAt(r.chunks[i], uint8(localX), int16(worldY), uint8(localZ))
}

func (r *endFeatureRegion) setBlock(worldX, worldY, worldZ int, rid uint32) bool {
	return r.setLayers(worldX, worldY, worldZ, world.NativeState{Block: rid, Liquid: r.airRID})
}

func (r *endFeatureRegion) setLayers(worldX, worldY, worldZ int, state world.NativeState) bool {
	if worldY < r.minY || worldY > r.maxY {
		return false
	}
	chunkX, chunkZ := worldX>>4, worldZ>>4
	if !r.containsChunk(chunkX, chunkZ) {
		return false
	}
	i := r.index(chunkX, chunkZ)
	bit := uint32(1) << uint(i)
	if r.overlays[i] == nil {
		r.overlays[i] = make(map[uint32]world.NativeState, 64)
	}
	if r.dirtyOverlays&bit == 0 {
		r.dirtyOverlays |= bit
		r.overlayMaxY[i] = worldY
	} else if worldY > r.overlayMaxY[i] {
		r.overlayMaxY[i] = worldY
	}
	key := uint32(worldY-r.minY)<<8 | uint32(worldZ&15)<<4 | uint32(worldX&15)
	r.overlays[i][key] = state
	if len(r.logicalStates) != 0 {
		delete(r.logicalStates, cube.Pos{worldX, worldY, worldZ})
	}
	return true
}

func (r *endFeatureRegion) publishTarget(targetX, targetZ int, target *chunk.Chunk) {
	i := r.index(targetX, targetZ)
	for pos, state := range r.overlays[i] {
		y := r.minY + int(pos>>8)
		z := int(pos>>4) & 15
		x := int(pos) & 15
		world.SetNativeState(target, uint8(x), int16(y), uint8(z), state)
	}
}

func (r endFeatureOrigin) inWriteRadius(worldX, worldZ int) bool {
	chunkX, chunkZ := worldX>>4, worldZ>>4
	return chunkX >= r.chunkX-endReplayRadius && chunkX <= r.chunkX+endReplayRadius && chunkZ >= r.chunkZ-endReplayRadius && chunkZ <= r.chunkZ+endReplayRadius
}

func (o endFeatureOrigin) block(x, y, z int) uint32 { return o.region.block(x, y, z) }

func (o endFeatureOrigin) setBlock(x, y, z int, rid uint32) bool {
	if !o.inWriteRadius(x, z) {
		return false
	}
	return o.region.setBlock(x, y, z, rid)
}

func (o endFeatureOrigin) isTarget(targetX, targetZ int) bool {
	return o.chunkX == targetX && o.chunkZ == targetZ
}
