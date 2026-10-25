package world

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// TickPrecipitation exposes one chunk precipitation step to world_test.
func (w *World) TickPrecipitation(tx *Tx, pos ChunkPos) { w.tickPrecipitation(tx, pos) }

// FindLightningRod exposes the lightning rod search to world_test.
func (tx *Tx) FindLightningRod(center cube.Pos) (cube.Pos, bool) { return tx.findLightningRod(center) }

// FreshLight computes the light of the chunks within two of the one at pos
// from scratch, filling clones of the chunks within three of it and spreading
// their light. It returns a function to look up the sky and block light of a
// position in them.
func (tx *Tx) FreshLight(pos cube.Pos) func(cube.Pos) (sky, block uint8) {
	centre := chunkPosFromBlockPos(pos)
	clones := map[ChunkPos]*chunk.Chunk{}
	for x := int32(-3); x <= 3; x++ {
		for z := int32(-3); z <= 3; z++ {
			p := ChunkPos{centre[0] + x, centre[1] + z}
			c := tx.chunk(p).Chunk.Clone()
			chunk.LightArea([]*chunk.Chunk{c}, int(p[0]), int(p[1])).Fill()
			clones[p] = c
		}
	}
	for x := int32(-2); x <= 2; x++ {
		for z := int32(-2); z <= 2; z++ {
			area := make([]*chunk.Chunk, 0, 9)
			for dz := int32(-1); dz <= 1; dz++ {
				for dx := int32(-1); dx <= 1; dx++ {
					area = append(area, clones[ChunkPos{centre[0] + x + dx, centre[1] + z + dz}])
				}
			}
			chunk.LightArea(area, int(centre[0]+x)-1, int(centre[1]+z)-1).Spread()
		}
	}
	return func(p cube.Pos) (uint8, uint8) {
		c := clones[chunkPosFromBlockPos(p)]
		return c.SkyLight(uint8(p[0]&15), int16(p[1]), uint8(p[2]&15)), c.SubChunk(int16(p[1])).BlockLight(uint8(p[0]&15), uint8(p[1]&15), uint8(p[2]&15))
	}
}
