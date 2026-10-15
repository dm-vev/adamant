package nether

import (
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// Material noise conditions are column-constant. Sample only when a rule needs
// a value, and never resample the same noise for deeper blocks in that column.
// Java promotes the float noise result before comparing double thresholds.
type netherColumnNoise struct {
	value float64
	ready bool
}

func (n *netherColumnNoise) sample(x, z int32, source func(int32, int32) float32) float64 {
	if !n.ready {
		n.value = float64(source(x, z))
		n.ready = true
	}
	return n.value
}

func (g *Nether) applyJavaNetherMaterial(chunkX, chunkZ int, c *chunk.Chunk) {
	minY, maxY := c.Range().Min(), c.Range().Max()
	if minY < 0 {
		minY = 0
	}
	if maxY > 127 {
		maxY = 127
	}
	biomes := newNetherChunkBiomes(g, chunkX, chunkZ)
	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			worldX, worldZ := int32(chunkX*16)+int32(x), int32(chunkZ*16)+int32(z)
			surfaceDepth := g.material.SurfaceDepth(worldX, worldZ)
			var patchNoise, selectorNoise, forestNoise, wartNoise, soulNoise, gravelNoise netherColumnNoise
			var floorDepth, ceilingDepth [128]int16
			depth := int16(0)
			for y := maxY; y >= minY; y-- {
				rid := c.Block(x, int16(y), z, 0)
				if rid == g.airRID {
					depth = 0
				} else if rid != g.lavaStillRID && rid != g.lavaFlowRID {
					depth++
					floorDepth[y] = depth
				}
			}
			depth = 0
			for y := minY; y <= maxY; y++ {
				if g.isSurfaceSolid(c.Block(x, int16(y), z, 0)) {
					depth++
					ceilingDepth[y] = depth
				} else {
					depth = 0
				}
			}

			for y := minY; y <= maxY; y++ {
				rid := c.Block(x, int16(y), z, 0)
				if !g.isSurfaceSolid(rid) {
					continue
				}
				y32 := int32(y)
				if g.material.BedrockFloor(worldX, y32, worldZ) || (y >= 122 && g.material.BedrockRoof(worldX, y32, worldZ)) {
					c.SetBlock(x, int16(y), z, 0, g.bedrockRID)
					continue
				}
				if y >= 122 {
					continue
				}
				fd, cd := floorDepth[y], ceilingDepth[y]
				onFloor := fd > 0 && fd <= 1
				underFloor := fd > 0 && int(fd) <= 1+surfaceDepth
				underCeiling := cd > 0 && int(cd) <= 1+surfaceDepth
				if onFloor && y < 32 && surfaceDepth <= 0 {
					c.SetBlock(x, int16(y), z, 0, g.lavaStillRID)
					continue
				}
				if rid != g.netherrackRID {
					continue
				}
				name := [...]string{
					"minecraft:nether_wastes",
					"minecraft:soul_sand_valley",
					"minecraft:crimson_forest",
					"minecraft:warped_forest",
					"minecraft:basalt_deltas",
				}[biomes.index(worldX, y32, worldZ)]
				switch name {
				case "minecraft:basalt_deltas":
					if underCeiling {
						rid = g.basaltRID
					} else if underFloor {
						if int(y)+int(fd) >= 30 && int(y)+int(fd) < 35 && patchNoise.sample(worldX, worldZ, g.material.Patch) >= -0.012 {
							rid = g.gravelRID
						} else if selectorNoise.sample(worldX, worldZ, g.material.Selector) >= 0 {
							rid = g.basaltRID
						} else {
							rid = g.blackstoneRID
						}
					}
				case "minecraft:soul_sand_valley":
					if underCeiling {
						if selectorNoise.sample(worldX, worldZ, g.material.Selector) >= 0 {
							rid = g.soulSandRID
						} else {
							rid = g.soulSoilRID
						}
					} else if underFloor {
						if int(y)+int(fd) >= 30 && int(y)+int(fd) < 35 && patchNoise.sample(worldX, worldZ, g.material.Patch) >= -0.012 {
							rid = g.gravelRID
						} else if selectorNoise.sample(worldX, worldZ, g.material.Selector) >= 0 {
							rid = g.soulSandRID
						} else {
							rid = g.soulSoilRID
						}
					}
				case "minecraft:warped_forest", "minecraft:crimson_forest":
					if onFloor && y >= 31 && forestNoise.sample(worldX, worldZ, g.material.Netherrack) < 0.54 {
						if wartNoise.sample(worldX, worldZ, g.material.NetherWart) >= 1.17 {
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
					if underFloor && surfaceDepth > 0 && int(y)+int(fd) >= 30 && int(y)+int(fd) < 35 &&
						soulNoise.sample(worldX, worldZ, g.material.SoulSandLayer) >= -0.012 {
						rid = g.soulSandRID
					} else if onFloor && y >= 31 && int(y)+int(fd) < 35 && (y >= 32 || surfaceDepth > 0) &&
						gravelNoise.sample(worldX, worldZ, g.material.GravelLayer) >= -0.012 {
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
