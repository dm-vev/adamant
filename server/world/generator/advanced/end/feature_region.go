package end

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

const endReplayRadius = 1
// Each replayed origin needs a one-chunk read/write halo, making a five-chunk raw region.
const endFeatureRadius = 4*endReplayRadius + 1
const endFeatureChunkCount = endFeatureRadius * endFeatureRadius
type endFeatureRegion struct {
	chunks   [endFeatureChunkCount]*chunk.Chunk
	overlays [endFeatureChunkCount]map[uint32]world.NativeState
	minChunkX, minChunkZ int
	minY, maxY int
	airRID uint32
}

type endFeatureOrigin struct {
	region *endFeatureRegion
	chunkX, chunkZ int
}

func newEndFeatureRegion(g *End, targetX, targetZ int, target *chunk.Chunk, scratch *endScratch) *endFeatureRegion {
	region := &endFeatureRegion{
		minChunkX: targetX - endFeatureRadius/2,
		minChunkZ: targetZ - endFeatureRadius/2,
		minY: target.Range().Min(),
		maxY: target.Range().Max(),
		airRID: g.airRID,
	}
	for chunkX := region.minChunkX; chunkX < region.minChunkX+endFeatureRadius; chunkX++ {
		for chunkZ := region.minChunkZ; chunkZ < region.minChunkZ+endFeatureRadius; chunkZ++ {
			c := target
			if chunkX != targetX || chunkZ != targetZ {
				c = chunk.New(target.BlockRegistry(), target.Range())
			}
			if chunkX == targetX && chunkZ == targetZ {
				g.fillBiomes(c)
			}
			g.generateTerrain(chunkX, chunkZ, c, scratch)
			region.chunks[region.index(chunkX, chunkZ)] = c
		}
	}
	return region
}

func (r *endFeatureRegion) index(chunkX, chunkZ int) int {
	return (chunkX-r.minChunkX)*endFeatureRadius + chunkZ - r.minChunkZ
}

func (r *endFeatureRegion) containsChunk(chunkX, chunkZ int) bool {
	return chunkX >= r.minChunkX && chunkX < r.minChunkX+endFeatureRadius && chunkZ >= r.minChunkZ && chunkZ < r.minChunkZ+endFeatureRadius
}

func (r *endFeatureRegion) block(worldX, worldY, worldZ int) uint32 {
	if worldY < r.minY || worldY > r.maxY {
		return r.airRID
	}
	chunkX, chunkZ := floorDiv(worldX, 16), floorDiv(worldZ, 16)
	if !r.containsChunk(chunkX, chunkZ) {
		return r.airRID
	}
	i := r.index(chunkX, chunkZ)
	local, localZ := uint8(worldX&15), uint8(worldZ&15)
	key := uint32(worldY-r.minY)<<8 | uint32(localZ)<<4 | uint32(local)
	if overlay := r.overlays[i]; overlay != nil {
		if state, ok := overlay[key]; ok {
			return state.Block
		}
	}
	return r.chunks[i].Block(local, int16(worldY), localZ, 0)
}

func (r *endFeatureRegion) setBlock(worldX, worldY, worldZ int, rid uint32) bool {
	return r.setLayers(worldX, worldY, worldZ, world.NativeState{Block: rid, Liquid: r.airRID})
}

func (r *endFeatureRegion) setLayers(worldX, worldY, worldZ int, state world.NativeState) bool {
	if worldY < r.minY || worldY > r.maxY {
		return false
	}
	chunkX, chunkZ := floorDiv(worldX, 16), floorDiv(worldZ, 16)
	if !r.containsChunk(chunkX, chunkZ) {
		return false
	}
	i := r.index(chunkX, chunkZ)
	if r.overlays[i] == nil {
		r.overlays[i] = make(map[uint32]world.NativeState)
	}
	key := uint32(worldY-r.minY)<<8 | uint32(worldZ&15)<<4 | uint32(worldX&15)
	r.overlays[i][key] = state
	return true
}

func (r *endFeatureRegion) highestBlock(worldX, worldZ int) int {
	for y := r.maxY; y >= r.minY; y-- {
		if r.block(worldX, y, worldZ) != r.airRID {
			return y
		}
	}
	return 0
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
	chunkX, chunkZ := floorDiv(worldX, 16), floorDiv(worldZ, 16)
	return chunkX >= r.chunkX-endReplayRadius && chunkX <= r.chunkX+endReplayRadius && chunkZ >= r.chunkZ-endReplayRadius && chunkZ <= r.chunkZ+endReplayRadius
}

func (o endFeatureOrigin) block(x, y, z int) uint32 { return o.region.block(x, y, z) }

func (o endFeatureOrigin) setBlock(x, y, z int, rid uint32) bool {
	if !o.inWriteRadius(x, z) {
		return false
	}
	return o.region.setBlock(x, y, z, rid)
}

func (o endFeatureOrigin) highestBlock(x, z int) int { return o.region.highestBlock(x, z) }

func (o endFeatureOrigin) isTarget(targetX, targetZ int) bool {
	return o.chunkX == targetX && o.chunkZ == targetZ
}

