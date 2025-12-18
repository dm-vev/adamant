package overworld

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/mc112"
	mcbiome "github.com/df-mc/dragonfly/server/world/generator/advanced/overworld/internal/biome"
)

func (g *Overworld) populateOresInChunk(chunkX, chunkZ int, c *chunk.Chunk) {
	r := g.chunkPopulationRand(chunkX, chunkZ)
	biomeID := g.biomeProvider.biomes(chunkX*16+16, chunkZ*16+16, 1, 1)[0]
	g.populateOresChunk(c, r, chunkX, chunkZ, biomeID)
}

func (g *Overworld) populateOresChunk(c *chunk.Chunk, r *mc112.Rand, chunkX, chunkZ int, biomeID int) {
	originX, originZ := chunkX*16, chunkZ*16

	// Default Java 1.12 ore generation settings (ChunkGeneratorSettings defaults).
	g.genStandardOre1Chunk(c, r, chunkX, chunkZ, originX, originZ, 20, 33, 0, 256, world.BlockRuntimeID(block.Dirt{}))
	g.genStandardOre1Chunk(c, r, chunkX, chunkZ, originX, originZ, 10, 33, 0, 256, world.BlockRuntimeID(block.Gravel{}))
	g.genStandardOre1Chunk(c, r, chunkX, chunkZ, originX, originZ, 10, 33, 0, 80, world.BlockRuntimeID(block.Granite{}))
	g.genStandardOre1Chunk(c, r, chunkX, chunkZ, originX, originZ, 10, 33, 0, 80, world.BlockRuntimeID(block.Diorite{}))
	g.genStandardOre1Chunk(c, r, chunkX, chunkZ, originX, originZ, 10, 33, 0, 80, world.BlockRuntimeID(block.Andesite{}))

	g.genStandardOre1Chunk(c, r, chunkX, chunkZ, originX, originZ, 20, 17, 0, 128, world.BlockRuntimeID(block.CoalOre{Type: block.StoneOre()}))
	g.genStandardOre1Chunk(c, r, chunkX, chunkZ, originX, originZ, 20, 9, 0, 64, world.BlockRuntimeID(block.IronOre{Type: block.StoneOre()}))
	g.genStandardOre1Chunk(c, r, chunkX, chunkZ, originX, originZ, 2, 9, 0, 32, world.BlockRuntimeID(block.GoldOre{Type: block.StoneOre()}))

	// Redstone ore isn't implemented as a block type yet, but we can still place its state.
	if b, ok := world.BlockByName("minecraft:redstone_ore", nil); ok {
		g.genStandardOre1Chunk(c, r, chunkX, chunkZ, originX, originZ, 8, 8, 0, 16, world.BlockRuntimeID(b))
	}

	g.genStandardOre1Chunk(c, r, chunkX, chunkZ, originX, originZ, 1, 8, 0, 16, world.BlockRuntimeID(block.DiamondOre{Type: block.StoneOre()}))
	g.genStandardOre2Chunk(c, r, chunkX, chunkZ, originX, originZ, 1, 7, 16, 16, world.BlockRuntimeID(block.LapisOre{Type: block.StoneOre()}))

	switch mcbiome.CategoryOf(biomeID) {
	case mcbiome.CategoryMountains:
		g.generateEmeraldOreChunk(c, r, chunkX, chunkZ, originX, originZ)
	case mcbiome.CategoryMesa:
		g.generateExtraGoldChunk(c, r, chunkX, chunkZ, originX, originZ)
	}
}

func (g *Overworld) genStandardOre1Chunk(c *chunk.Chunk, r *mc112.Rand, chunkX, chunkZ, originX, originZ int, count, size, minY, maxY int, oreRID uint32) {
	if maxY <= minY {
		return
	}
	for i := 0; i < count; i++ {
		x := originX + int(r.Intn(16))
		y := minY + int(r.Intn(int32(maxY-minY)))
		z := originZ + int(r.Intn(16))
		g.generateMinableChunk(c, r, chunkX, chunkZ, x, y, z, size, oreRID)
	}
}

func (g *Overworld) genStandardOre2Chunk(c *chunk.Chunk, r *mc112.Rand, chunkX, chunkZ, originX, originZ int, count, size, centerY, spread int, oreRID uint32) {
	if spread <= 0 {
		return
	}
	for i := 0; i < count; i++ {
		x := originX + int(r.Intn(16))
		y := int(r.Intn(int32(spread))) + int(r.Intn(int32(spread))) + centerY - spread
		z := originZ + int(r.Intn(16))
		g.generateMinableChunk(c, r, chunkX, chunkZ, x, y, z, size, oreRID)
	}
}

func (g *Overworld) generateEmeraldOreChunk(c *chunk.Chunk, r *mc112.Rand, chunkX, chunkZ, originX, originZ int) {
	emeraldRID := world.BlockRuntimeID(block.EmeraldOre{Type: block.StoneOre()})
	for i := 0; i < 3+int(r.Intn(6)); i++ {
		x := originX + int(r.Intn(16))
		y := 4 + int(r.Intn(28))
		z := originZ + int(r.Intn(16))
		if y < 0 || y > 255 {
			continue
		}
		if x>>4 != chunkX || z>>4 != chunkZ {
			continue
		}
		if c.Block(uint8(x&15), int16(y), uint8(z&15), 0) != g.stoneRID {
			continue
		}
		c.SetBlock(uint8(x&15), int16(y), uint8(z&15), 0, emeraldRID)
	}
}

func (g *Overworld) generateExtraGoldChunk(c *chunk.Chunk, r *mc112.Rand, chunkX, chunkZ, originX, originZ int) {
	goldRID := world.BlockRuntimeID(block.GoldOre{Type: block.StoneOre()})
	for i := 0; i < 20; i++ {
		x := originX + int(r.Intn(16))
		y := 32 + int(r.Intn(48))
		z := originZ + int(r.Intn(16))
		g.generateMinableChunk(c, r, chunkX, chunkZ, x, y, z, 9, goldRID)
	}
}

func (g *Overworld) generateMinableChunk(c *chunk.Chunk, r *mc112.Rand, chunkX, chunkZ, startX, startY, startZ, size int, oreRID uint32) {
	if size <= 0 {
		return
	}
	if startY < 0 || startY > 255 {
		return
	}
	if startX>>4 != chunkX || startZ>>4 != chunkZ {
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
			if x>>4 != chunkX {
				continue
			}
			for y := minY; y <= maxY; y++ {
				if y < 0 || y > 255 {
					continue
				}
				dy := (float64(y) + 0.5 - py) * invRY
				dy2 := dy * dy
				if dx2+dy2 >= 1.0 {
					continue
				}
				for z := minZ; z <= maxZ; z++ {
					if z>>4 != chunkZ {
						continue
					}
					dz := (float64(z) + 0.5 - pz) * invRX
					if dx2+dy2+dz*dz >= 1.0 {
						continue
					}
					if c.Block(uint8(x&15), int16(y), uint8(z&15), 0) != g.stoneRID {
						continue
					}
					c.SetBlock(uint8(x&15), int16(y), uint8(z&15), 0, oreRID)
				}
			}
		}
	}
}

func (g *Overworld) populateOres(tx *world.Tx, r *mc112.Rand, chunkX, chunkZ int, origin cube.Pos, biomeID int) {
	// Default Java 1.12 ore generation settings.
	g.genStandardOre1(tx, r, chunkX, chunkZ, origin, 20, 33, 0, 256, block.Dirt{})
	g.genStandardOre1(tx, r, chunkX, chunkZ, origin, 10, 33, 0, 256, block.Gravel{})
	g.genStandardOre1(tx, r, chunkX, chunkZ, origin, 10, 33, 0, 80, block.Granite{})
	g.genStandardOre1(tx, r, chunkX, chunkZ, origin, 10, 33, 0, 80, block.Diorite{})
	g.genStandardOre1(tx, r, chunkX, chunkZ, origin, 10, 33, 0, 80, block.Andesite{})

	g.genStandardOre1(tx, r, chunkX, chunkZ, origin, 20, 17, 0, 128, block.CoalOre{Type: block.StoneOre()})
	g.genStandardOre1(tx, r, chunkX, chunkZ, origin, 20, 9, 0, 64, block.IronOre{Type: block.StoneOre()})
	g.genStandardOre1(tx, r, chunkX, chunkZ, origin, 2, 9, 0, 32, block.GoldOre{Type: block.StoneOre()})

	// Redstone ore isn't implemented as a block type yet, but we can still place its state.
	if b, ok := world.BlockByName("minecraft:redstone_ore", nil); ok {
		g.genStandardOre1(tx, r, chunkX, chunkZ, origin, 8, 8, 0, 16, b)
	}

	g.genStandardOre1(tx, r, chunkX, chunkZ, origin, 1, 8, 0, 16, block.DiamondOre{Type: block.StoneOre()})
	g.genStandardOre2(tx, r, chunkX, chunkZ, origin, 1, 7, 16, 16, block.LapisOre{Type: block.StoneOre()})

	switch mcbiome.CategoryOf(biomeID) {
	case mcbiome.CategoryMountains:
		g.generateEmeraldOre(tx, r, chunkX, chunkZ, origin)
	case mcbiome.CategoryMesa:
		g.generateExtraGold(tx, r, chunkX, chunkZ, origin)
	}
}

func (g *Overworld) genStandardOre1(tx *world.Tx, r *mc112.Rand, chunkX, chunkZ int, origin cube.Pos, count, size, minY, maxY int, ore world.Block) {
	if maxY <= minY {
		return
	}
	for i := 0; i < count; i++ {
		x := origin[0] + int(r.Intn(16))
		y := minY + int(r.Intn(int32(maxY-minY)))
		z := origin[2] + int(r.Intn(16))
		g.generateMinable(tx, r, chunkX, chunkZ, cube.Pos{x, y, z}, size, ore)
	}
}

func (g *Overworld) genStandardOre2(tx *world.Tx, r *mc112.Rand, chunkX, chunkZ int, origin cube.Pos, count, size, centerY, spread int, ore world.Block) {
	if spread <= 0 {
		return
	}
	for i := 0; i < count; i++ {
		x := origin[0] + int(r.Intn(16))
		y := int(r.Intn(int32(spread))) + int(r.Intn(int32(spread))) + centerY - spread
		z := origin[2] + int(r.Intn(16))
		g.generateMinable(tx, r, chunkX, chunkZ, cube.Pos{x, y, z}, size, ore)
	}
}

func (g *Overworld) generateEmeraldOre(tx *world.Tx, r *mc112.Rand, chunkX, chunkZ int, origin cube.Pos) {
	for i := 0; i < 3+int(r.Intn(6)); i++ {
		x := origin[0] + int(r.Intn(16))
		y := 4 + int(r.Intn(28))
		z := origin[2] + int(r.Intn(16))
		pos := cube.Pos{x, y, z}
		if pos.OutOfBounds(tx.Range()) {
			continue
		}
		if pos[0]>>4 != chunkX || pos[2]>>4 != chunkZ {
			continue
		}
		if world.BlockRuntimeID(tx.Block(pos)) != g.stoneRID {
			continue
		}
		tx.SetBlock(pos, block.EmeraldOre{Type: block.StoneOre()}, populationSetOpts)
	}
}

func (g *Overworld) generateExtraGold(tx *world.Tx, r *mc112.Rand, chunkX, chunkZ int, origin cube.Pos) {
	for i := 0; i < 20; i++ {
		x := origin[0] + int(r.Intn(16))
		y := 32 + int(r.Intn(48))
		z := origin[2] + int(r.Intn(16))
		g.generateMinable(tx, r, chunkX, chunkZ, cube.Pos{x, y, z}, 9, block.GoldOre{Type: block.StoneOre()})
	}
}

func (g *Overworld) generateMinable(tx *world.Tx, r *mc112.Rand, chunkX, chunkZ int, start cube.Pos, size int, ore world.Block) {
	if size <= 0 {
		return
	}
	if start[0]>>4 != chunkX || start[2]>>4 != chunkZ {
		return
	}

	angle := r.Float32() * float32(math.Pi)
	sin, cos := mc112.Sin(angle), mc112.Cos(angle)

	fSize := float32(size)
	x0 := float64(float32(start[0]+8) + sin*fSize/8.0)
	x1 := float64(float32(start[0]+8) - sin*fSize/8.0)
	z0 := float64(float32(start[2]+8) + cos*fSize/8.0)
	z1 := float64(float32(start[2]+8) - cos*fSize/8.0)

	y0 := float64(start[1] + int(r.Intn(3)) - 2)
	y1 := float64(start[1] + int(r.Intn(3)) - 2)

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
				if y < 0 || y > 255 {
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
					pos := cube.Pos{x, y, z}
					if pos.OutOfBounds(tx.Range()) {
						continue
					}
					if pos[0]>>4 != chunkX || pos[2]>>4 != chunkZ {
						continue
					}
					if world.BlockRuntimeID(tx.Block(pos)) != g.stoneRID {
						continue
					}
					tx.SetBlock(pos, ore, populationSetOpts)
				}
			}
		}
	}
}
