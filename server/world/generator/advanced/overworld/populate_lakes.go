package overworld

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/mc112"
	mcbiome "github.com/df-mc/dragonfly/server/world/generator/advanced/overworld/internal/biome"
)

const (
	javaWaterLakeChance = 4
	javaLavaLakeChance  = 80
)

func (g *Overworld) populateLakes(chunkX, chunkZ int, c *chunk.Chunk, villageGenerated bool) {
	// Lakes in vanilla spill over chunk borders because the origin position is picked with rand.nextInt(16)+8.
	// We simulate the same 2x2 origin-chunk area used for decoration and only apply edits that land inside this chunk.
	chunkMinX, chunkMinZ := chunkX<<4, chunkZ<<4
	chunkMaxX, chunkMaxZ := chunkMinX+15, chunkMinZ+15

	preview := make(map[world.ChunkPos]*chunk.Chunk, 9)

	for dx := 0; dx >= -1; dx-- {
		for dz := 0; dz >= -1; dz-- {
			originChunkX, originChunkZ := chunkX+dx, chunkZ+dz
			r := g.chunkPopulationRand(originChunkX, originChunkZ)

			// Vanilla: biome = world.getBiome(blockpos.add(16, 0, 16)).
			biomeID := g.biomeProvider.biomes(originChunkX*16+16, originChunkZ*16+16, 1, 1)[0]
			flagVillage := villageGenerated && originChunkX == chunkX && originChunkZ == chunkZ

			if mcbiome.ID(biomeID) != mcbiome.Desert && mcbiome.ID(biomeID) != mcbiome.DesertHills &&
				!flagVillage && r.Intn(javaWaterLakeChance) == 0 {
				i1 := int(r.Intn(16)) + 8
				j1 := int(r.Intn(256))
				k1 := int(r.Intn(16)) + 8
				g.genLake(c, preview, chunkX, chunkZ, chunkMinX, chunkMinZ, chunkMaxX, chunkMaxZ, originChunkX*16+i1, j1, originChunkZ*16+k1, g.waterRID, biomeID, r)
			}

			if !flagVillage && r.Intn(javaLavaLakeChance/10) == 0 {
				i2 := int(r.Intn(16)) + 8
				l2 := int(r.Intn(int32(int(r.Intn(248)) + 8)))
				k3 := int(r.Intn(16)) + 8

				if l2 < javaSeaLevel || r.Intn(javaLavaLakeChance/8) == 0 {
					g.genLake(c, preview, chunkX, chunkZ, chunkMinX, chunkMinZ, chunkMaxX, chunkMaxZ, originChunkX*16+i2, l2, originChunkZ*16+k3, g.lavaRID, biomeID, r)
				}
			}
		}
	}
}

func (g *Overworld) genLake(
	c *chunk.Chunk,
	preview map[world.ChunkPos]*chunk.Chunk,
	chunkX, chunkZ int,
	chunkMinX, chunkMinZ, chunkMaxX, chunkMaxZ int,
	x, y, z int,
	liquidRID uint32,
	biomeID int,
	r *mc112.Rand,
) bool {
	// WorldGenLakes.generate: position = position.add(-8, 0, -8)
	px, py, pz := x-8, y, z-8

	// Move down while air.
	for py > 5 && g.blockRIDAt(c, preview, chunkX, chunkZ, px, py, pz) == g.airRID {
		py--
	}
	if py <= 4 {
		return false
	}
	py -= 4

	// Early out if the affected 16x16 square cannot intersect this chunk.
	if !intersectsXZ(chunkMinX, chunkMinZ, chunkMaxX, chunkMaxZ, px+8, pz+8, 16) {
		// Still consume RNG by running the shape generation.
		// We don't validate or place blocks since it can't touch this chunk.
		i := int(r.Intn(4)) + 4
		for j := 0; j < i; j++ {
			_ = r.Float64()
			_ = r.Float64()
			_ = r.Float64()
			_ = r.Float64()
			_ = r.Float64()
			_ = r.Float64()
		}
		return false
	}

	shape := make([]bool, 2048)
	ellipses := int(r.Intn(4)) + 4
	for j := 0; j < ellipses; j++ {
		d0 := r.Float64()*6.0 + 3.0
		d1 := r.Float64()*4.0 + 2.0
		d2 := r.Float64()*6.0 + 3.0
		d3 := r.Float64()*(16.0-d0-2.0) + 1.0 + d0/2.0
		d4 := r.Float64()*(8.0-d1-4.0) + 2.0 + d1/2.0
		d5 := r.Float64()*(16.0-d2-2.0) + 1.0 + d2/2.0

		for lx := 1; lx < 15; lx++ {
			for lz := 1; lz < 15; lz++ {
				for ly := 1; ly < 7; ly++ {
					d6 := (float64(lx) - d3) / (d0 / 2.0)
					d7 := (float64(ly) - d4) / (d1 / 2.0)
					d8 := (float64(lz) - d5) / (d2 / 2.0)
					if d6*d6+d7*d7+d8*d8 < 1.0 {
						shape[(lx*16+lz)*8+ly] = true
					}
				}
			}
		}
	}

	// Validate boundary.
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			for ly := 0; ly < 8; ly++ {
				idx := (lx*16 + lz) * 8 + ly
				if shape[idx] {
					continue
				}
				adjacent := (lx < 15 && shape[((lx+1)*16+lz)*8+ly]) ||
					(lx > 0 && shape[((lx-1)*16+lz)*8+ly]) ||
					(lz < 15 && shape[(lx*16+lz+1)*8+ly]) ||
					(lz > 0 && shape[(lx*16+(lz-1))*8+ly]) ||
					(ly < 7 && shape[(lx*16+lz)*8+ly+1]) ||
					(ly > 0 && shape[(lx*16+lz)*8+ly-1])
				if !adjacent {
					continue
				}

				wx, wy, wz := px+lx, py+ly, pz+lz
				mat := g.blockRIDAt(c, preview, chunkX, chunkZ, wx, wy, wz)

				if ly >= 4 && (mat == g.waterRID || mat == g.lavaRID) {
					return false
				}
				if ly < 4 && (mat == g.airRID || mat == g.waterRID || mat == g.lavaRID || g.isLeaves(mat)) && mat != liquidRID {
					return false
				}
			}
		}
	}

	// Place blocks.
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			for ly := 0; ly < 8; ly++ {
				if !shape[(lx*16+lz)*8+ly] {
					continue
				}
				wx, wy, wz := px+lx, py+ly, pz+lz
				if ly >= 4 {
					g.setRIDIfInChunk(c, chunkX, chunkZ, wx, wy, wz, g.airRID)
				} else {
					g.setRIDIfInChunk(c, chunkX, chunkZ, wx, wy, wz, liquidRID)
				}
			}
		}
	}

	// Dirt->grass/mycelium at shore (best-effort, no skylight simulation).
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			for ly := 4; ly < 8; ly++ {
				if !shape[(lx*16+lz)*8+ly] {
					continue
				}
				wx, wy, wz := px+lx, py+ly-1, pz+lz
				if wx>>4 != chunkX || wz>>4 != chunkZ {
					continue
				}
				if c.Block(uint8(wx&15), int16(wy), uint8(wz&15), 0) != g.dirtRID {
					continue
				}
				top := g.grassRID
				if mcbiome.ID(biomeID) == mcbiome.MushroomFields || mcbiome.ID(biomeID) == mcbiome.MushroomFieldShore {
					top = g.myceliumRID
				}
				if wy+1 <= 255 && c.Block(uint8(wx&15), int16(wy+1), uint8(wz&15), 0) == g.airRID {
					c.SetBlock(uint8(wx&15), int16(wy), uint8(wz&15), 0, top)
				}
			}
		}
	}

	// Lava solidifies some boundary blocks to stone.
	if liquidRID == g.lavaRID {
		for lx := 0; lx < 16; lx++ {
			for lz := 0; lz < 16; lz++ {
				for ly := 0; ly < 8; ly++ {
					if shape[(lx*16+lz)*8+ly] {
						continue
					}
					adjacent := (lx < 15 && shape[((lx+1)*16+lz)*8+ly]) ||
						(lx > 0 && shape[((lx-1)*16+lz)*8+ly]) ||
						(lz < 15 && shape[(lx*16+lz+1)*8+ly]) ||
						(lz > 0 && shape[(lx*16+(lz-1))*8+ly]) ||
						(ly < 7 && shape[(lx*16+lz)*8+ly+1]) ||
						(ly > 0 && shape[(lx*16+lz)*8+ly-1])
					if !adjacent {
						continue
					}
					wx, wy, wz := px+lx, py+ly, pz+lz
					if wx>>4 != chunkX || wz>>4 != chunkZ {
						continue
					}
					if ly >= 4 && r.Intn(2) == 0 {
						continue
					}
					if g.isSolidForLakeRID(c.Block(uint8(wx&15), int16(wy), uint8(wz&15), 0)) {
						c.SetBlock(uint8(wx&15), int16(wy), uint8(wz&15), 0, g.stoneRID)
					}
				}
			}
		}
	}
	return true
}

func (g *Overworld) isSolidForLakeRID(rid uint32) bool {
	return rid != g.airRID && rid != g.waterRID && rid != g.lavaRID && !g.isLeaves(rid)
}
