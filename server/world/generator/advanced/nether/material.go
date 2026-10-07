package nether

import (
	"github.com/df-mc/dragonfly/server/world/chunk"
)

func (g *Nether) applyJavaNetherMaterial(chunkX, chunkZ int, c *chunk.Chunk) {
	minY, maxY := c.Range().Min(), c.Range().Max()
	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			worldX, worldZ := int32(chunkX*16)+int32(x), int32(chunkZ*16)+int32(z)
			var floorDepth, ceilingDepth [128]int16
			for y := minY; y <= maxY && y < 128; y++ {
				floorDepth[y], ceilingDepth[y] = -1, -1
			}
			depth := int16(-1)
			for y := maxY; y >= minY && y < 128; y-- {
				if g.isSurfaceSolid(c.Block(x, int16(y), z, 0)) {
					if y == maxY || !g.isSurfaceSolid(c.Block(x, int16(y+1), z, 0)) {
						depth = 0
					} else if depth >= 0 {
						depth++
					}
					floorDepth[y] = depth
				} else {
					depth = -1
				}
			}
			depth = -1
			for y := minY; y <= maxY && y < 128; y++ {
				if g.isSurfaceSolid(c.Block(x, int16(y), z, 0)) {
					if y == minY || !g.isSurfaceSolid(c.Block(x, int16(y-1), z, 0)) {
						depth = 0
					} else if depth >= 0 {
						depth++
					}
					ceilingDepth[y] = depth
				} else {
					depth = -1
				}
			}

			for y := minY; y <= maxY; y++ {
				y32 := int32(y)
				name := [...]string{
					"minecraft:nether_wastes",
					"minecraft:soul_sand_valley",
					"minecraft:crimson_forest",
					"minecraft:warped_forest",
					"minecraft:basalt_deltas",
				}[g.biomeIndexAt(worldX, y32, worldZ)]
				if g.material.BedrockFloor(worldX, y32, worldZ) || (y >= 123 && g.material.BedrockRoof(worldX, y32, worldZ)) {
					c.SetBlock(x, int16(y), z, 0, g.bedrockRID)
					continue
				}
				rid := c.Block(x, int16(y), z, 0)
				if rid != g.netherrackRID {
					continue
				}
				fd, cd := floorDepth[y], ceilingDepth[y]
				patch := g.material.Patch(worldX, worldZ) >= -0.012 && y >= 30 && y < 35
				selector := g.material.Selector(worldX, worldZ) >= 0
				switch name {
				case "minecraft:basalt_deltas":
					if cd > 0 && cd <= 4 {
						rid = g.basaltRID
					} else if fd > 0 && fd <= 4 {
						if patch {
							rid = g.gravelRID
						} else if selector {
							rid = g.basaltRID
						} else {
							rid = g.blackstoneRID
						}
					}
				case "minecraft:soul_sand_valley":
					if cd > 0 && cd <= 4 || fd > 0 && fd <= 4 {
						if patch {
							rid = g.gravelRID
						} else if selector {
							rid = g.soulSandRID
						} else {
							rid = g.soulSoilRID
						}
					}
				case "minecraft:warped_forest", "minecraft:crimson_forest":
					if fd == 0 && y >= 32 && g.material.Netherrack(worldX, worldZ) >= 0.54 {
						if g.material.NetherWart(worldX, worldZ) >= 1.17 {
							if name == "minecraft:warped_forest" {
								rid = g.warpedWartBlockRID
							} else {
								rid = g.netherWartBlockRID
							}
						} else if name == "minecraft:warped_forest" {
							rid = g.warpedNyliumRID
						} else {
							rid = g.crimsonNyliumRID
						}
					}
				case "minecraft:nether_wastes":
					if fd > 0 && fd <= 4 && g.material.SoulSandLayer(worldX, worldZ) >= -0.012 && y >= 30 && y < 35 {
						rid = g.soulSandRID
					} else if fd == 0 && y > 31 && y < 35 && g.material.GravelLayer(worldX, worldZ) >= -0.012 {
						rid = g.gravelRID
					}
				}
				c.SetBlock(x, int16(y), z, 0, rid)
			}
		}
	}
}

func (g *Nether) isSurfaceSolid(rid uint32) bool {
	return rid != g.airRID && rid != g.lavaStillRID && rid != g.lavaFlowRID
}
