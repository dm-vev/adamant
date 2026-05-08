package nether

import (
	"math"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/mc112"
)

func (g *Nether) populate(chunkX, chunkZ int, c *chunk.Chunk, r *mc112.Rand) {
	preview := make(map[world.ChunkPos]*netherPreview, 9)

	for dx := 0; dx >= -1; dx-- {
		for dz := 0; dz >= -1; dz-- {
			originChunkX := chunkX + dx
			originChunkZ := chunkZ + dz

			var randCopy mc112.Rand
			if originChunkX == chunkX && originChunkZ == chunkZ {
				randCopy = *r
			} else {
				p := g.previewChunkCached(preview, originChunkX, originChunkZ)
				randCopy = p.randAfterSurface
			}

			g.populateOrigin(c, preview, chunkX, chunkZ, originChunkX, originChunkZ, &randCopy)
		}
	}
}

func (g *Nether) populateOrigin(
	c *chunk.Chunk,
	preview map[world.ChunkPos]*netherPreview,
	chunkX, chunkZ int,
	originChunkX, originChunkZ int,
	r *mc112.Rand,
) {
	originX := originChunkX * 16
	originZ := originChunkZ * 16

	target := c
	simulate := false
	if originChunkX != chunkX || originChunkZ != chunkZ {
		target = chunk.New(world.DefaultBlockRegistry, c.Range())
		simulate = true
	}
	g.applyNetherBridge(originChunkX, originChunkZ, target, r, simulate)

	for i := 0; i < 8; i++ {
		x := originX + int(r.Intn(16)) + 8
		y := int(r.Intn(120)) + 4
		z := originZ + int(r.Intn(16)) + 8
		g.genHellLava(c, preview, chunkX, chunkZ, x, y, z, false, r)
	}

	fireCount := int(r.Intn(int32(r.Intn(10)+1)) + 1)
	for i := 0; i < fireCount; i++ {
		x := originX + int(r.Intn(16)) + 8
		y := int(r.Intn(120)) + 4
		z := originZ + int(r.Intn(16)) + 8
		g.genFire(c, preview, chunkX, chunkZ, x, y, z, r)
	}

	glowCount := int(r.Intn(int32(r.Intn(10) + 1)))
	for i := 0; i < glowCount; i++ {
		x := originX + int(r.Intn(16)) + 8
		y := int(r.Intn(120)) + 4
		z := originZ + int(r.Intn(16)) + 8
		g.genGlowstone(c, preview, chunkX, chunkZ, x, y, z, r)
	}

	for i := 0; i < 10; i++ {
		x := originX + int(r.Intn(16)) + 8
		y := int(r.Intn(128))
		z := originZ + int(r.Intn(16)) + 8
		g.genGlowstone(c, preview, chunkX, chunkZ, x, y, z, r)
	}

	if r.Bool() {
		x := originX + int(r.Intn(16)) + 8
		y := int(r.Intn(128))
		z := originZ + int(r.Intn(16)) + 8
		g.genBush(c, preview, chunkX, chunkZ, x, y, z, r, g.brownMushroomRID)
	}

	if r.Bool() {
		x := originX + int(r.Intn(16)) + 8
		y := int(r.Intn(128))
		z := originZ + int(r.Intn(16)) + 8
		g.genBush(c, preview, chunkX, chunkZ, x, y, z, r, g.redMushroomRID)
	}

	for i := 0; i < 16; i++ {
		x := originX + int(r.Intn(16))
		y := int(r.Intn(108)) + 10
		z := originZ + int(r.Intn(16))
		g.genMinable(c, preview, chunkX, chunkZ, x, y, z, 14, g.quartzOreRID, r)
	}

	i2 := netherSeaLevel/2 + 1
	for i := 0; i < 4; i++ {
		x := originX + int(r.Intn(16))
		y := i2 - 5 + int(r.Intn(10))
		z := originZ + int(r.Intn(16))
		if g.magmaRID != 0 {
			g.genMinable(c, preview, chunkX, chunkZ, x, y, z, 33, g.magmaRID, r)
		}
	}

	for i := 0; i < 16; i++ {
		x := originX + int(r.Intn(16))
		y := int(r.Intn(108)) + 10
		z := originZ + int(r.Intn(16))
		g.genHellLava(c, preview, chunkX, chunkZ, x, y, z, true, r)
	}
}

func (g *Nether) genHellLava(c *chunk.Chunk, preview map[world.ChunkPos]*netherPreview, chunkX, chunkZ, x, y, z int, insideRock bool, r *mc112.Rand) bool {
	if g.blockRIDAt(c, preview, chunkX, chunkZ, x, y+1, z) != g.netherrackRID {
		return false
	}
	rid := g.blockRIDAt(c, preview, chunkX, chunkZ, x, y, z)
	if rid != g.airRID && rid != g.netherrackRID {
		return false
	}

	i := 0
	if g.blockRIDAt(c, preview, chunkX, chunkZ, x-1, y, z) == g.netherrackRID {
		i++
	}
	if g.blockRIDAt(c, preview, chunkX, chunkZ, x+1, y, z) == g.netherrackRID {
		i++
	}
	if g.blockRIDAt(c, preview, chunkX, chunkZ, x, y, z-1) == g.netherrackRID {
		i++
	}
	if g.blockRIDAt(c, preview, chunkX, chunkZ, x, y, z+1) == g.netherrackRID {
		i++
	}
	if g.blockRIDAt(c, preview, chunkX, chunkZ, x, y-1, z) == g.netherrackRID {
		i++
	}

	j := 0
	if g.blockRIDAt(c, preview, chunkX, chunkZ, x-1, y, z) == g.airRID {
		j++
	}
	if g.blockRIDAt(c, preview, chunkX, chunkZ, x+1, y, z) == g.airRID {
		j++
	}
	if g.blockRIDAt(c, preview, chunkX, chunkZ, x, y, z-1) == g.airRID {
		j++
	}
	if g.blockRIDAt(c, preview, chunkX, chunkZ, x, y, z+1) == g.airRID {
		j++
	}
	if g.blockRIDAt(c, preview, chunkX, chunkZ, x, y-1, z) == g.airRID {
		j++
	}

	if (!insideRock && i == 4 && j == 1) || i == 5 {
		g.setRIDIfInChunk(c, chunkX, chunkZ, x, y, z, g.lavaFlowRID)
	}

	return true
}

func (g *Nether) genFire(c *chunk.Chunk, preview map[world.ChunkPos]*netherPreview, chunkX, chunkZ, x, y, z int, r *mc112.Rand) {
	for i := 0; i < 64; i++ {
		wx := x + int(r.Intn(8)) - int(r.Intn(8))
		wy := y + int(r.Intn(4)) - int(r.Intn(4))
		wz := z + int(r.Intn(8)) - int(r.Intn(8))

		if g.blockRIDAt(c, preview, chunkX, chunkZ, wx, wy, wz) != g.airRID {
			continue
		}
		if g.blockRIDAt(c, preview, chunkX, chunkZ, wx, wy-1, wz) != g.netherrackRID {
			continue
		}
		g.setRIDIfInChunk(c, chunkX, chunkZ, wx, wy, wz, g.fireRID)
	}
}

func (g *Nether) genGlowstone(c *chunk.Chunk, preview map[world.ChunkPos]*netherPreview, chunkX, chunkZ, x, y, z int, r *mc112.Rand) bool {
	if g.glowstoneRID == 0 {
		return false
	}
	if g.blockRIDAt(c, preview, chunkX, chunkZ, x, y, z) != g.airRID {
		return false
	}
	if g.blockRIDAt(c, preview, chunkX, chunkZ, x, y+1, z) != g.netherrackRID {
		return false
	}

	g.setRIDIfInChunk(c, chunkX, chunkZ, x, y, z, g.glowstoneRID)

	for i := 0; i < 1500; i++ {
		wx := x + int(r.Intn(8)) - int(r.Intn(8))
		wy := y - int(r.Intn(12))
		wz := z + int(r.Intn(8)) - int(r.Intn(8))

		if g.blockRIDAt(c, preview, chunkX, chunkZ, wx, wy, wz) != g.airRID {
			continue
		}

		count := 0
		if g.blockRIDAt(c, preview, chunkX, chunkZ, wx-1, wy, wz) == g.glowstoneRID {
			count++
		}
		if g.blockRIDAt(c, preview, chunkX, chunkZ, wx+1, wy, wz) == g.glowstoneRID {
			count++
		}
		if g.blockRIDAt(c, preview, chunkX, chunkZ, wx, wy, wz-1) == g.glowstoneRID {
			count++
		}
		if g.blockRIDAt(c, preview, chunkX, chunkZ, wx, wy, wz+1) == g.glowstoneRID {
			count++
		}
		if g.blockRIDAt(c, preview, chunkX, chunkZ, wx, wy-1, wz) == g.glowstoneRID {
			count++
		}
		if g.blockRIDAt(c, preview, chunkX, chunkZ, wx, wy+1, wz) == g.glowstoneRID {
			count++
		}

		if count == 1 {
			g.setRIDIfInChunk(c, chunkX, chunkZ, wx, wy, wz, g.glowstoneRID)
		}
	}

	return true
}

func (g *Nether) genBush(c *chunk.Chunk, preview map[world.ChunkPos]*netherPreview, chunkX, chunkZ, x, y, z int, r *mc112.Rand, rid uint32) {
	if rid == 0 {
		return
	}
	for i := 0; i < 64; i++ {
		wx := x + int(r.Intn(8)) - int(r.Intn(8))
		wy := y + int(r.Intn(4)) - int(r.Intn(4))
		wz := z + int(r.Intn(8)) - int(r.Intn(8))

		if g.blockRIDAt(c, preview, chunkX, chunkZ, wx, wy, wz) != g.airRID {
			continue
		}
		if !g.canMushroomStay(c, preview, chunkX, chunkZ, wx, wy, wz) {
			continue
		}
		g.setRIDIfInChunk(c, chunkX, chunkZ, wx, wy, wz, rid)
	}
}

func (g *Nether) canMushroomStay(c *chunk.Chunk, preview map[world.ChunkPos]*netherPreview, chunkX, chunkZ, x, y, z int) bool {
	if y < 0 || y > 127 {
		return false
	}
	below := g.blockRIDAt(c, preview, chunkX, chunkZ, x, y-1, z)
	return below != g.airRID && below != g.lavaStillRID && below != g.lavaFlowRID
}

func (g *Nether) genMinable(c *chunk.Chunk, preview map[world.ChunkPos]*netherPreview, chunkX, chunkZ, startX, startY, startZ, size int, oreRID uint32, r *mc112.Rand) {
	if oreRID == 0 || size <= 0 {
		return
	}
	if startY < 0 || startY > 127 {
		return
	}

	angle := r.Float32() * float32(math.Pi)
	sin, cos := mc112.Sin(angle), mc112.Cos(angle)

	fSize := float32(size)
	x0 := float64(float32(startX+8) + sin*fSize/8.0)
	x1 := float64(float32(startX+8) - sin*fSize/8.0)
	z0 := float64(float32(startZ+8) + cos*fSize/8.0)
	z1 := float64(float32(startZ+8) - cos*fSize/8.0)

	y0 := float64(startY + int(r.Intn(3)) - 2)
	y1 := float64(startY + int(r.Intn(3)) - 2)

	for i := 0; i < size; i++ {
		t := float32(i) / float32(size)

		px := x0 + (x1-x0)*float64(t)
		py := y0 + (y1-y0)*float64(t)
		pz := z0 + (z1-z0)*float64(t)

		spread := r.Float64() * float64(size) / 16.0
		s := float32(math.Pi) * t
		rx := float64(mc112.Sin(s)+1.0)*spread + 1.0
		ry := float64(mc112.Sin(s)+1.0)*spread + 1.0

		minX := mc112.Floor(px - rx/2.0)
		minY := mc112.Floor(py - ry/2.0)
		minZ := mc112.Floor(pz - rx/2.0)
		maxX := mc112.Floor(px + rx/2.0)
		maxY := mc112.Floor(py + ry/2.0)
		maxZ := mc112.Floor(pz + rx/2.0)

		invRX := 2.0 / rx
		invRY := 2.0 / ry

		for x := minX; x <= maxX; x++ {
			dx := (float64(x) + 0.5 - px) * invRX
			dx2 := dx * dx
			if dx2 >= 1.0 {
				continue
			}
			for y := minY; y <= maxY; y++ {
				if y < 0 || y > 127 {
					continue
				}
				dy := (float64(y) + 0.5 - py) * invRY
				dy2 := dy * dy
				if dx2+dy2 >= 1.0 {
					continue
				}
				for z := minZ; z <= maxZ; z++ {
					dz := (float64(z) + 0.5 - pz) * invRX
					if dx2+dy2+dz*dz >= 1.0 {
						continue
					}
					if g.blockRIDAt(c, preview, chunkX, chunkZ, x, y, z) != g.netherrackRID {
						continue
					}
					g.setRIDIfInChunk(c, chunkX, chunkZ, x, y, z, oreRID)
				}
			}
		}
	}
}
