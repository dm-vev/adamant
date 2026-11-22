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

type netherColumnNoises struct {
	patch    netherColumnNoise
	selector netherColumnNoise
	forest   netherColumnNoise
	wart     netherColumnNoise
	soul     netherColumnNoise
	gravel   netherColumnNoise
}

func (g *Nether) selectSurfaceKind(biomeIdx, y int, fd int16, surfaceDepth int, onFloor, underFloor, underCeiling bool, worldX, worldZ int32, n *netherColumnNoises) uint8 {
	switch biomeIdx {
	case 4: // minecraft:basalt_deltas
		if underCeiling {
			return nbBasalt
		}
		if underFloor {
			switch {
			case y+int(fd) >= 30 && y+int(fd) < 35 && n.patch.sample(worldX, worldZ, g.material.Patch) >= -0.012:
				return nbGravel
			case n.selector.sample(worldX, worldZ, g.material.Selector) >= 0:
				return nbBasalt
			default:
				return nbBlackstone
			}
		}
	case 1: // minecraft:soul_sand_valley
		if underCeiling {
			if n.selector.sample(worldX, worldZ, g.material.Selector) >= 0 {
				return nbSoulSand
			}
			return nbSoulSoil
		}
		if underFloor {
			switch {
			case y+int(fd) >= 30 && y+int(fd) < 35 && n.patch.sample(worldX, worldZ, g.material.Patch) >= -0.012:
				return nbGravel
			case n.selector.sample(worldX, worldZ, g.material.Selector) >= 0:
				return nbSoulSand
			default:
				return nbSoulSoil
			}
		}
	case 2, 3: // minecraft:crimson_forest, minecraft:warped_forest
		if onFloor && y >= 31 && n.forest.sample(worldX, worldZ, g.material.Netherrack) < 0.54 {
			switch {
			case n.wart.sample(worldX, worldZ, g.material.NetherWart) >= 1.17:
				if biomeIdx == 3 {
					return nbWarpedWartBlock
				}
				return nbNetherWartBlock
			case biomeIdx == 3:
				return nbWarpedNylium
			default:
				return nbCrimsonNylium
			}
		}
	case 0: // minecraft:nether_wastes
		switch {
		case underFloor && surfaceDepth > 0 && y+int(fd) >= 30 && y+int(fd) < 35 &&
			n.soul.sample(worldX, worldZ, g.material.SoulSandLayer) >= -0.012:
			return nbSoulSand
		case onFloor && y >= 31 && y+int(fd) < 35 && (y >= 32 || surfaceDepth > 0) &&
			n.gravel.sample(worldX, worldZ, g.material.GravelLayer) >= -0.012:
			return nbGravel
		}
	}
	return nbNetherrack
}

func (g *Nether) applyNetherMaterialGrid(chunkX, chunkZ int, s *netherScratch) {
	s.biomes.init(g, chunkX, chunkZ)
	baseX, baseZ := int32(chunkX*16), int32(chunkZ*16)
	uniformChunk := s.biomes.uniform
	uniformBiome := int(s.biomes.uniformIndex)
	for x := 0; x < 16; x++ {
		worldX := baseX + int32(x)
		px := int(((worldX - 2) >> 2) - s.biomes.minQuartX)
		xBase := x << 11
		for z := 0; z < 16; z++ {
			worldZ := baseZ + int32(z)
			colBase := xBase | (z << 7)
			col := s.blocks[colBase : colBase+128]
			surfaceDepth := g.material.SurfaceDepth(worldX, worldZ)
			var noises netherColumnNoises
			var floorDepth [128]int16
			depth := int16(0)
			for y := 127; y >= 0; y-- {
				k := col[y]
				if k == nbAir {
					depth = 0
				} else if k != nbLavaStill {
					depth++
					floorDepth[y] = depth
				}
			}

			colUniform := uniformChunk
			colBiome := uniformBiome
			if !uniformChunk {
				pz := int(((worldZ - 2) >> 2) - s.biomes.minQuartZ)
				if s.biomes.cellUniform[px][pz] {
					colUniform = true
					colBiome = int(s.biomes.indices[px][pz])
				}
			}

			ceilDepth := int16(0)
			for y := 0; y < 128; y++ {
				k := col[y]
				if k == nbAir || k == nbLavaStill {
					ceilDepth = 0
					continue
				}
				ceilDepth++
				if (y < 5 && g.material.BedrockFloor(worldX, int32(y), worldZ)) || (y > 122 && g.material.BedrockRoof(worldX, int32(y), worldZ)) {
					col[y] = nbBedrock
					continue
				}
				if y >= 122 {
					continue
				}
				fd := floorDepth[y]
				onFloor := fd == 1
				underFloor := fd > 0 && int(fd) <= 1+surfaceDepth
				underCeiling := ceilDepth > 0 && int(ceilDepth) <= 1+surfaceDepth
				if onFloor && y < 32 && surfaceDepth <= 0 {
					col[y] = nbLavaStill
					continue
				}
				if k != nbNetherrack || (!onFloor && !underFloor && !underCeiling) {
					continue
				}
				biomeIdx := colBiome
				if !colUniform {
					biomeIdx = s.biomes.fiddledIndex(worldX, int32(y), worldZ)
				}
				col[y] = g.selectSurfaceKind(biomeIdx, y, fd, surfaceDepth, onFloor, underFloor, underCeiling, worldX, worldZ, &noises)
			}
		}
	}
}

func (g *Nether) applyNetherMaterial(chunkX, chunkZ int, c *chunk.Chunk) {
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
				if (y < 5 && g.material.BedrockFloor(worldX, y32, worldZ)) || (y > 122 && g.material.BedrockRoof(worldX, y32, worldZ)) {
					c.SetBlock(x, int16(y), z, 0, g.bedrockRID)
					continue
				}
				if y >= 122 {
					continue
				}
				fd, cd := floorDepth[y], ceilingDepth[y]
				onFloor, underFloor, underCeiling := fd == 1, fd > 0 && int(fd) <= 1+surfaceDepth, cd > 0 && int(cd) <= 1+surfaceDepth
				if onFloor && y < 32 && surfaceDepth <= 0 {
					c.SetBlock(x, int16(y), z, 0, g.lavaStillRID)
					continue
				}
				if rid != g.netherrackRID || (!onFloor && !underFloor && !underCeiling) {
					continue
				}
				nextRID := rid
				switch biomeIdx := biomes.index(worldX, y32, worldZ); biomeIdx {
				case 4:
					if underCeiling {
						nextRID = g.basaltRID
					} else if underFloor {
						switch {
						case y+int(fd) >= 30 && y+int(fd) < 35 && patchNoise.sample(worldX, worldZ, g.material.Patch) >= -0.012:
							nextRID = g.gravelRID
						case selectorNoise.sample(worldX, worldZ, g.material.Selector) >= 0:
							nextRID = g.basaltRID
						default:
							nextRID = g.blackstoneRID
						}
					}
				case 1:
					if underCeiling {
						if selectorNoise.sample(worldX, worldZ, g.material.Selector) >= 0 {
							nextRID = g.soulSandRID
						} else {
							nextRID = g.soulSoilRID
						}
					} else if underFloor {
						switch {
						case y+int(fd) >= 30 && y+int(fd) < 35 && patchNoise.sample(worldX, worldZ, g.material.Patch) >= -0.012:
							nextRID = g.gravelRID
						case selectorNoise.sample(worldX, worldZ, g.material.Selector) >= 0:
							nextRID = g.soulSandRID
						default:
							nextRID = g.soulSoilRID
						}
					}
				case 2, 3:
					if onFloor && y >= 31 && forestNoise.sample(worldX, worldZ, g.material.Netherrack) < 0.54 {
						switch {
						case wartNoise.sample(worldX, worldZ, g.material.NetherWart) >= 1.17:
							if biomeIdx == 3 {
								nextRID = g.warpedWartBlockRID
							} else {
								nextRID = g.netherWartBlockRID
							}
						case biomeIdx == 3:
							nextRID = g.warpedNyliumRID
						default:
							nextRID = g.crimsonNyliumRID
						}
					}
				case 0:
					switch {
					case underFloor && surfaceDepth > 0 && y+int(fd) >= 30 && y+int(fd) < 35 &&
						soulNoise.sample(worldX, worldZ, g.material.SoulSandLayer) >= -0.012:
						nextRID = g.soulSandRID
					case onFloor && y >= 31 && y+int(fd) < 35 && (y >= 32 || surfaceDepth > 0) &&
						gravelNoise.sample(worldX, worldZ, g.material.GravelLayer) >= -0.012:
						nextRID = g.gravelRID
					}
				}
				if nextRID != rid {
					c.SetBlock(x, int16(y), z, 0, nextRID)
				}
			}
		}
	}
}

func (g *Nether) isSurfaceSolid(rid uint32) bool {
	return rid != g.airRID && rid != g.lavaStillRID && rid != g.lavaFlowRID
}
