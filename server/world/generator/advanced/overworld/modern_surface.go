package overworld

import (
	"fmt"
	"math"
	"strings"

	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

const (
	modernSeaLevel = 63
	modernMinY     = -64
	modernMaxY     = 319
)

type modernSurfaceSource interface {
	PreliminarySurfaceHeight(x, z int32) int32
	JavaBiomeAt(x, y, z int32) string
	OreVein(x, y, z int32) (copperDensity, ironDensity, richness, gap float32)
}

type modernSurface struct {
	random, oreRandom                                           java26.PositionalFactory
	surface, surfaceSecondary, clayBandsOffset                  *java26.NormalNoise
	badlandsPillar, badlandsPillarRoof, badlandsSurface          *java26.NormalNoise
	icebergPillar, icebergPillarRoof, icebergSurface             *java26.NormalNoise
	sulfurGradient, swamp, calcite, gravel, smallPatch            *java26.NormalNoise
	powderSnow, packedIce, ice                                   *java26.NormalNoise
	biomeInfo                                                    *java26.SimplexNoise
	frozenTemperatureLayers                                      [3]*java26.SimplexNoise
	clayBands                                                    [192]string
}

func newModernSurface(seed int64) *modernSurface {
	n := java26.NewOverworldMaterialNoises(seed)
	oreRandom := n.Random.FromHashOf("minecraft:ore")
	s := &modernSurface{
		random: n.Random,
		oreRandom: oreRandom.ForkPositional(),
		surface: n.Surface,
		surfaceSecondary: n.SurfaceSecondary,
		clayBandsOffset: n.ClayBandsOffset,
		badlandsPillar: n.BadlandsPillar,
		badlandsPillarRoof: n.BadlandsPillarRoof,
		badlandsSurface: n.BadlandsSurface,
		icebergPillar: n.IcebergPillar,
		icebergPillarRoof: n.IcebergPillarRoof,
		icebergSurface: n.IcebergSurface,
		sulfurGradient: n.SulfurCaveGradient,
		swamp: n.Swamp,
		calcite: n.Calcite,
		gravel: n.Gravel,
		powderSnow: n.PowderSnow,
		packedIce: n.PackedIce,
		ice: n.Ice,
		smallPatch: n.SmallPatch,
		biomeInfo: n.BiomeInfo,
		frozenTemperatureLayers: n.FrozenTemperatureLayers,
	}
	s.makeClayBands()
	return s
}

func (s *modernSurface) Apply(pos world.ChunkPos, c *chunk.Chunk, source modernSurfaceSource) {
	registry := c.BlockRegistry()
	blocks := resolveSurfaceBlocks(registry)
	air := registry.AirRuntimeID()
	minY, maxY := int(c.Range()[0]), int(c.Range()[1])
	if minY < modernMinY {
		minY = modernMinY
	}
	if maxY > modernMaxY {
		maxY = modernMaxY
	}
	chunkX, chunkZ := int(pos[0])<<4, int(pos[1])<<4
	var heights [18][18]int
	for x := range 16 {
		for z := range 16 {
			heights[x+1][z+1] = surfaceHeight(c, x, z, minY, maxY, air)
		}
	}
	for x := range 16 {
		for z := range 16 {
			worldX, worldZ := chunkX+x, chunkZ+z
			height := heights[x+1][z+1]
			surfaceBiome := biomeName(source.JavaBiomeAt(int32(worldX), int32(height), int32(worldZ)))
			if surfaceBiome == "eroded_badlands" {
				s.erodedBadlands(c, uint8(x), uint8(z), int32(worldX), int32(worldZ), height, minY, maxY, blocks, air)
				height = surfaceHeight(c, x, z, minY, maxY, air)
				heights[x+1][z+1] = height
			}
			gradientX := heights[min(x+2, 16)][z+1] - heights[max(x, 1)][z+1]
			gradientZ := heights[x+1][min(z+2, 16)] - heights[x+1][max(z, 1)]
			preliminary := int(source.PreliminarySurfaceHeight(int32(worldX), int32(worldZ)))
			columnRandom := s.random.At(int32(worldX), 0, int32(worldZ))
			surfaceDepth := int(s.surface.Sample64(float64(worldX), 0, float64(worldZ))*2.75 + 3 + columnRandom.NextDouble()*0.25)
			waterHeight := math.MinInt
			stoneAbove, nextCeiling := 0, math.MaxInt
			for y := min(height, maxY); y >= minY; y-- {
				y16 := int16(y)
				rid := c.Block(uint8(x), y16, uint8(z), 0)
				if rid == air {
					stoneAbove = 0
					waterHeight = math.MinInt
					continue
				}
				if registry.LiquidBlock(rid) {
					if waterHeight == math.MinInt {
						waterHeight = y + 1
					}
					continue
				}
				if nextCeiling >= y {
					nextCeiling = minY
					for lookaheadY := y - 1; lookaheadY >= minY; lookaheadY-- {
						lower := c.Block(uint8(x), int16(lookaheadY), uint8(z), 0)
						if lower == air || registry.LiquidBlock(lower) {
							nextCeiling = lookaheadY + 1
							break
						}
					}
				}
				stoneAbove++
				stoneBelow := y - nextCeiling + 1
				biome := biomeName(source.JavaBiomeAt(int32(worldX), int32(y), int32(worldZ)))
				if s.verticalGradient("bedrock_floor", int32(worldX), int32(y), int32(worldZ), minY, minY+5) {
					c.SetBlock(uint8(x), y16, uint8(z), 0, blocks.id("bedrock"))
					continue
				}
				copper, iron, richness, gap := source.OreVein(int32(worldX), int32(y), int32(worldZ))
				if ore, ok := s.oreVeinMaterial(int32(worldX), int32(y), int32(worldZ), copper, iron, richness, gap); ok {
					c.SetBlock(uint8(x), y16, uint8(z), 0, blocks.id(ore))
					continue
				}
				if y < preliminary+surfaceDepth-8 {
					if biome == "sulfur_caves" {
						if material := s.sulfurMaterial(worldX, y, worldZ); material != "" {
							c.SetBlock(uint8(x), y16, uint8(z), 0, blocks.id(material))
							continue
						}
					}
					if s.verticalGradient("deepslate", int32(worldX), int32(y), int32(worldZ), 0, 8) {
						c.SetBlock(uint8(x), y16, uint8(z), 0, blocks.id("deepslate"))
					}
					continue
				}
				notUnderwater := waterHeight == math.MinInt || y >= waterHeight-1
				notUnderDeepWater := waterHeight == math.MinInt || y+stoneAbove >= waterHeight-6-surfaceDepth
				onFloor := stoneAbove <= 1
				underFloor := stoneAbove <= 1+surfaceDepth
				deepUnderFloor := stoneAbove <= 1+surfaceDepth+surfaceSecondaryDepth(s, worldX, worldZ, 6)
				veryDeepUnderFloor := stoneAbove <= 1+surfaceDepth+surfaceSecondaryDepth(s, worldX, worldZ, 30)
				material := s.surfaceMaterial(biome, worldX, y, worldZ, stoneAbove, stoneBelow, surfaceDepth, notUnderwater, notUnderDeepWater, onFloor, underFloor, deepUnderFloor, veryDeepUnderFloor, gradientX, gradientZ)
				if material != "" {
					c.SetBlock(uint8(x), y16, uint8(z), 0, blocks.id(material))
					continue
				}
				if biome == "sulfur_caves" {
					if material := s.sulfurMaterial(worldX, y, worldZ); material != "" {
						c.SetBlock(uint8(x), y16, uint8(z), 0, blocks.id(material))
						continue
					}
				}
				if s.verticalGradient("deepslate", int32(worldX), int32(y), int32(worldZ), 0, 8) {
					c.SetBlock(uint8(x), y16, uint8(z), 0, blocks.id("deepslate"))
				}
			}
			if surfaceBiome == "frozen_ocean" || surfaceBiome == "deep_frozen_ocean" {
				minSurface := preliminary + surfaceDepth - 8
				s.frozenOcean(c, uint8(x), uint8(z), int32(worldX), int32(worldZ), height, minY, minSurface, surfaceBiome, blocks)
			}
			heights[x+1][z+1] = surfaceHeight(c, x, z, minY, maxY, air)
		}
	}
}
// TopMaterial evaluates the same Overworld material rule at a single post-carver
// position, using the depth and fluid context from Java MaterialSystem.topMaterial.
func (s *modernSurface) TopMaterial(pos world.ChunkPos, c *chunk.Chunk, source modernSurfaceSource, x, y, z int32, hasFluid bool) (uint32, bool) {
	registry := c.BlockRegistry()
	minY, maxY := int(c.Range()[0]), int(c.Range()[1])
	if minY < modernMinY {
		minY = modernMinY
	}
	if maxY > modernMaxY {
		maxY = modernMaxY
	}
	localX := int(x - (int32(pos[0]) << 4))
	localZ := int(z - (int32(pos[1]) << 4))
	air := registry.AirRuntimeID()
	gradientX := surfaceHeight(c, min(localX+1, 15), localZ, minY, maxY, air) - surfaceHeight(c, max(localX-1, 0), localZ, minY, maxY, air)
	gradientZ := surfaceHeight(c, localX, min(localZ+1, 15), minY, maxY, air) - surfaceHeight(c, localX, max(localZ-1, 0), minY, maxY, air)
	columnRandom := s.random.At(x, 0, z)
	surfaceDepth := int(s.surface.Sample64(float64(x), 0, float64(z))*2.75 + 3 + columnRandom.NextDouble()*0.25)
	waterHeight := math.MinInt
	if hasFluid {
		waterHeight = int(y) + 1
	}
	stoneAbove, stoneBelow := 1, 1
	biome := biomeName(source.JavaBiomeAt(x, y, z))
	if s.verticalGradient("bedrock_floor", x, y, z, minY, minY+5) {
		return resolveSurfaceBlockID(registry, "bedrock"), true
	}
	copper, iron, richness, gap := source.OreVein(x, y, z)
	if ore, ok := s.oreVeinMaterial(x, y, z, copper, iron, richness, gap); ok {
		return resolveSurfaceBlockID(registry, ore), true
	}
	if int(y) < int(source.PreliminarySurfaceHeight(x, z))+surfaceDepth-8 {
		if biome == "sulfur_caves" {
			if material := s.sulfurMaterial(int(x), int(y), int(z)); material != "" {
				return resolveSurfaceBlockID(registry, material), true
			}
		}
		if s.verticalGradient("deepslate", x, y, z, 0, 8) {
			return resolveSurfaceBlockID(registry, "deepslate"), true
		}
		return 0, false
	}
	notUnderwater := waterHeight == math.MinInt || int(y) >= waterHeight-1
	notUnderDeepWater := waterHeight == math.MinInt || int(y)+stoneAbove >= waterHeight-6-surfaceDepth
	underFloor := stoneAbove <= 1+surfaceDepth
	deepUnderFloor := stoneAbove <= 1+surfaceDepth+surfaceSecondaryDepth(s, int(x), int(z), 6)
	veryDeepUnderFloor := stoneAbove <= 1+surfaceDepth+surfaceSecondaryDepth(s, int(x), int(z), 30)
	material := s.surfaceMaterial(biome, int(x), int(y), int(z), stoneAbove, stoneBelow, surfaceDepth, notUnderwater, notUnderDeepWater, true, underFloor, deepUnderFloor, veryDeepUnderFloor, gradientX, gradientZ)
	if material != "" {
		return resolveSurfaceBlockID(registry, material), true
	}
	if biome == "sulfur_caves" {
		if material := s.sulfurMaterial(int(x), int(y), int(z)); material != "" {
			return resolveSurfaceBlockID(registry, material), true
		}
	}
	if s.verticalGradient("deepslate", x, y, z, 0, 8) {
		return resolveSurfaceBlockID(registry, "deepslate"), true
	}
	return 0, false
}


func biomeName(name string) string { return strings.TrimPrefix(name, "minecraft:") }

const (
	surfaceAir = iota
	surfaceBedrock
	surfaceDeepslate
	surfaceStone
	surfaceDirt
	surfaceGrass
	surfacePodzol
	surfaceCoarseDirt
	surfaceMycelium
	surfaceCalcite
	surfaceGravel
	surfaceSand
	surfaceSandstone
	surfaceRedSand
	surfaceRedSandstone
	surfaceTerracotta
	surfaceOrangeTerracotta
	surfaceWhiteTerracotta
	surfaceYellowTerracotta
	surfaceBrownTerracotta
	surfaceRedTerracotta
	surfaceLightGrayTerracotta
	surfacePackedIce
	surfaceSnowBlock
	surfaceMud
	surfacePowderSnow
	surfaceIce
	surfaceWater
	surfaceCinnabar
	surfaceSulfur
	surfaceCopperOre
	surfaceRawCopperBlock
	surfaceGranite
	surfaceDeepslateIronOre
	surfaceRawIronBlock
	surfaceTuff
	surfaceBlockCount
)

var surfaceBlockNames = [surfaceBlockCount]string{
	"minecraft:air", "minecraft:bedrock", "minecraft:deepslate", "minecraft:stone", "minecraft:dirt", "minecraft:grass_block", "minecraft:podzol", "minecraft:coarse_dirt", "minecraft:mycelium", "minecraft:calcite",
	"minecraft:gravel", "minecraft:sand", "minecraft:sandstone", "minecraft:red_sand", "minecraft:red_sandstone", "minecraft:terracotta", "minecraft:orange_terracotta", "minecraft:white_terracotta",
	"minecraft:yellow_terracotta", "minecraft:brown_terracotta", "minecraft:red_terracotta", "minecraft:light_gray_terracotta", "minecraft:packed_ice", "minecraft:snow_block",
	"minecraft:mud", "minecraft:powder_snow", "minecraft:ice", "minecraft:water", "minecraft:cinnabar", "minecraft:sulfur", "minecraft:copper_ore", "minecraft:raw_copper_block", "minecraft:granite",
	"minecraft:deepslate_iron_ore", "minecraft:raw_iron_block", "minecraft:tuff",
}

type surfaceBlocks [surfaceBlockCount]uint32

func resolveSurfaceBlocks(registry chunk.BlockRegistry) surfaceBlocks {
	var blocks surfaceBlocks
	for i, name := range surfaceBlockNames {
		blocks[i] = resolveSurfaceBlock(registry, name)
	}
	return blocks
}

func surfaceBlockIndex(name string) int {
	switch name {
	case "air": return surfaceAir
	case "bedrock": return surfaceBedrock
	case "deepslate": return surfaceDeepslate
	case "stone": return surfaceStone
	case "dirt": return surfaceDirt
	case "grass_block": return surfaceGrass
	case "podzol": return surfacePodzol
	case "coarse_dirt": return surfaceCoarseDirt
	case "mycelium": return surfaceMycelium
	case "calcite": return surfaceCalcite
	case "gravel": return surfaceGravel
	case "sand": return surfaceSand
	case "sandstone": return surfaceSandstone
	case "red_sand": return surfaceRedSand
	case "red_sandstone": return surfaceRedSandstone
	case "terracotta": return surfaceTerracotta
	case "orange_terracotta": return surfaceOrangeTerracotta
	case "white_terracotta": return surfaceWhiteTerracotta
	case "yellow_terracotta": return surfaceYellowTerracotta
	case "brown_terracotta": return surfaceBrownTerracotta
	case "red_terracotta": return surfaceRedTerracotta
	case "light_gray_terracotta": return surfaceLightGrayTerracotta
	case "packed_ice": return surfacePackedIce
	case "snow_block": return surfaceSnowBlock
	case "mud": return surfaceMud
	case "powder_snow": return surfacePowderSnow
	case "ice": return surfaceIce
	case "water": return surfaceWater
	case "cinnabar": return surfaceCinnabar
	case "sulfur": return surfaceSulfur
	case "copper_ore": return surfaceCopperOre
	case "raw_copper_block": return surfaceRawCopperBlock
	case "granite": return surfaceGranite
	case "deepslate_iron_ore": return surfaceDeepslateIronOre
	case "raw_iron_block": return surfaceRawIronBlock
	case "tuff": return surfaceTuff
	default: panic("unknown modern surface material " + name)
	}
}

func (b surfaceBlocks) id(name string) uint32 { return b[surfaceBlockIndex(name)] }

func resolveSurfaceBlockID(registry chunk.BlockRegistry, name string) uint32 {
	return resolveSurfaceBlock(registry, surfaceBlockNames[surfaceBlockIndex(name)])
}

func resolveSurfaceBlock(registry chunk.BlockRegistry, name string) uint32 {
	state, err := world.ResolveState(registry, world.BlockState{Name: name})
	if err != nil {
		panic(fmt.Sprintf("modern surface material %s: %v", name, err))
	}
	return state.Block
}

func (s *modernSurface) oreVeinMaterial(x, y, z int32, copper, iron, richness, gap float32) (string, bool) {
	for _, vein := range [...]struct {
		density       float32
		ore, raw, fill string
	}{
		{copper, "copper_ore", "raw_copper_block", "granite"},
		{iron, "deepslate_iron_ore", "raw_iron_block", "tuff"},
	} {
		if vein.density <= 0 {
			continue
		}
		random := s.oreRandom.At(x, y, z)
		if random.NextFloat() > vein.density {
			continue
		}
		if random.NextFloat() < richness && gap < 0 {
			if random.NextFloat() < 0.02 {
				return vein.raw, true
			}
			return vein.ore, true
		}
		return vein.fill, true
	}
	return "", false
}

func surfaceHeight(c *chunk.Chunk, x, z, minY, maxY int, air uint32) int {
	for y := maxY; y >= minY; y-- {
		if c.Block(uint8(x), int16(y), uint8(z), 0) != air {
			return y + 1
		}
	}
	return minY
}

func surfaceSecondaryDepth(s *modernSurface, x, z, span int) int {
	if span == 0 {
		return 0
	}
	return int((s.surfaceSecondary.Sample64(float64(x), 0, float64(z)) + 1) * 0.5 * float64(span))
}

func (s *modernSurface) surfaceMaterial(b string, x, y, z, stoneAbove, stoneBelow, surfaceDepth int, notUnderwater, notUnderDeepWater, onFloor, underFloor, deepUnderFloor, veryDeepUnderFloor bool, gx, gz int) string {
	bandNoise := s.surface.Sample64(float64(x), 0, float64(z))
	clayNoise := bandNoise >= -0.909 && bandNoise <= -0.5454 || bandNoise >= -0.1818 && bandNoise <= 0.1818 || bandNoise >= 0.5454 && bandNoise <= 0.909
	ceiling := stoneBelow <= 1
	if onFloor {
		if b == "wooded_badlands" && y >= 97+surfaceDepth*2 {
			if clayNoise {
				return "coarse_dirt"
			}
			if notUnderwater {
				return "grass_block"
			}
			return "dirt"
		}
		if (b == "swamp" && y >= 62 && y < modernSeaLevel || b == "mangrove_swamp" && y >= 60 && y < modernSeaLevel) && s.swamp.Sample64(float64(x), 0, float64(z)) >= 0 {
			return "water"
		}
	}
	if b == "badlands" || b == "eroded_badlands" || b == "wooded_badlands" {
		if onFloor {
			if y >= 256 {
				return "orange_terracotta"
			}
			if y+stoneAbove >= 74+surfaceDepth {
				if clayNoise {
					return "terracotta"
				}
				return s.band(y, x, z)
			}
			if notUnderwater {
				if ceiling {
					return "red_sandstone"
				}
				return "red_sand"
			}
			if surfaceDepth > 0 {
				return "orange_terracotta"
			}
			if !notUnderDeepWater {
				return "white_terracotta"
			}
			if ceiling {
				return "stone"
			}
			return "gravel"
		}
		if y+stoneAbove >= 63-surfaceDepth {
			if y+stoneAbove >= 63 && y+stoneAbove < 74+surfaceDepth {
				return "orange_terracotta"
			}
			return s.band(y, x, z)
		}
		if underFloor && notUnderDeepWater {
			return "white_terracotta"
		}
	}
	if onFloor && notUnderwater {
		if b == "frozen_ocean" || b == "deep_frozen_ocean" {
			if surfaceDepth <= 0 {
				return "air"
			}
		}
		if material := s.biomeSurface(b, x, y, z, gx, gz, ceiling, notUnderwater); material != "" {
			return material
		}
	}
	if notUnderDeepWater {
		if onFloor && (b == "frozen_ocean" || b == "deep_frozen_ocean") && surfaceDepth <= 0 {
			return "water"
		}
		if underFloor {
			if material := s.underBiomeSurface(b, x, y, z, gx, gz, ceiling, notUnderwater); material != "" {
				return material
			}
		}
		if deepUnderFloor && (b == "warm_ocean" || b == "beach" || b == "snowy_beach") {
			return "sandstone"
		}
		if veryDeepUnderFloor && b == "desert" {
			return "sandstone"
		}
	}
	if onFloor {
		if b == "frozen_peaks" || b == "jagged_peaks" {
			return "stone"
		}
		if b == "warm_ocean" || b == "lukewarm_ocean" || b == "deep_lukewarm_ocean" {
			if ceiling {
				return "sandstone"
			}
			return "sand"
		}
		if ceiling {
			return "stone"
		}
		return "gravel"
	}
	return ""
}

func (s *modernSurface) biomeSurface(b string, x, y, z, gx, gz int, ceiling, notUnderwater bool) string {
	surface := s.surface.Sample64(float64(x), 0, float64(z)) / 8.25
	switch b {
	case "frozen_peaks":
		if surfaceSteep(gx, gz) { return "packed_ice" }
		if n := s.packedIce.Sample64(float64(x), 0, float64(z)); n >= 0 && n <= 0.2 { return "packed_ice" }
		if n := s.ice.Sample64(float64(x), 0, float64(z)); n >= 0 && n <= 0.025 { return "ice" }
		return "snow_block"
	case "snowy_slopes":
		if surfaceSteep(gx, gz) { return "stone" }
		if n := s.powderSnow.Sample64(float64(x), 0, float64(z)); n >= 0.35 && n <= 0.6 { return "powder_snow" }
		return "snow_block"
	case "jagged_peaks":
		if surfaceSteep(gx, gz) { return "stone" }
		return "snow_block"
	case "grove":
		if n := s.powderSnow.Sample64(float64(x), 0, float64(z)); n >= 0.35 && n <= 0.6 { return "powder_snow" }
		return "snow_block"
	case "stony_peaks":
		if n := s.calcite.Sample64(float64(x), 0, float64(z)); n >= -0.0125 && n <= 0.0125 { return "calcite" }
		return "stone"
	case "stony_shore":
		if n := s.gravel.Sample64(float64(x), 0, float64(z)); n >= -0.05 && n <= 0.05 {
			if ceiling { return "stone" }
			return "gravel"
		}
		return "stone"
	case "windswept_hills":
		if surface >= 1 { return "stone" }
	case "beach", "snowy_beach", "warm_ocean", "desert":
		if ceiling { return "sandstone" }
		return "sand"
	case "dripstone_caves":
		return "stone"
	case "sulfur_caves":
		if material := s.sulfurMaterial(x, y, z); material != "" { return material }
		return "stone"
	case "windswept_savanna":
		if surface >= 1.75 { return "stone" }
		if surface >= -0.5 { return "coarse_dirt" }
	case "windswept_gravelly_hills":
		if surface >= 2 {
			if ceiling { return "stone" }
			return "gravel"
		}
		if surface >= 1 { return "stone" }
		if surface >= -1 { return "grass_block" }
	case "old_growth_pine_taiga", "old_growth_spruce_taiga":
		if surface >= 1.75 { return "coarse_dirt" }
		if surface >= -0.95 { return "podzol" }
	case "ice_spikes":
		return "snow_block"
	case "mushroom_fields":
		return "mycelium"
	case "dappled_forest":
		if s.smallPatch.Sample64(float64(x), 0, float64(z)) >= 1.2 { return "coarse_dirt" }
	}
	if notUnderwater { return "grass_block" }
	return "dirt"
}

func (s *modernSurface) underBiomeSurface(b string, x, y, z, gx, gz int, ceiling, notUnderwater bool) string {
	surface := s.surface.Sample64(float64(x), 0, float64(z)) / 8.25
	switch b {
	case "frozen_peaks":
		if surfaceSteep(gx, gz) { return "packed_ice" }
		if n := s.packedIce.Sample64(float64(x), 0, float64(z)); n >= -0.5 && n <= 0.2 { return "packed_ice" }
		if n := s.ice.Sample64(float64(x), 0, float64(z)); n >= -0.0625 && n <= 0.025 { return "ice" }
		if notUnderwater { return "snow_block" }
	case "snowy_slopes":
		if surfaceSteep(gx, gz) { return "stone" }
		if n := s.powderSnow.Sample64(float64(x), 0, float64(z)); n >= 0.45 && n <= 0.58 && notUnderwater { return "powder_snow" }
		if notUnderwater { return "snow_block" }
	case "jagged_peaks":
		return "stone"
	case "grove":
		if n := s.powderSnow.Sample64(float64(x), 0, float64(z)); n >= 0.45 && n <= 0.58 && notUnderwater { return "powder_snow" }
	case "stony_peaks":
		if n := s.calcite.Sample64(float64(x), 0, float64(z)); n >= -0.0125 && n <= 0.0125 { return "calcite" }
		return "stone"
	case "stony_shore":
		if n := s.gravel.Sample64(float64(x), 0, float64(z)); n >= -0.05 && n <= 0.05 {
			if ceiling { return "stone" }
			return "gravel"
		}
		return "stone"
	case "windswept_hills":
		if surface >= 1 { return "stone" }
	case "beach", "snowy_beach", "warm_ocean":
		if ceiling { return "sandstone" }
		return "sand"
	case "dripstone_caves":
		return "stone"
	case "sulfur_caves":
		if material := s.sulfurMaterial(x, y, z); material != "" { return material }
		return "stone"
	case "mangrove_swamp":
		return "mud"
	case "windswept_savanna":
		if surface >= 1.75 { return "stone" }
	case "windswept_gravelly_hills":
		if surface >= 2 {
			if ceiling { return "stone" }
			return "gravel"
		}
		if surface >= 1 { return "stone" }
		if surface >= -1 { return "dirt" }
		if ceiling { return "stone" }
		return "gravel"
	}
	return "dirt"
}

func surfaceSteep(gx, gz int) bool { return gx <= -4 || gz >= 4 }

func (s *modernSurface) sulfurMaterial(x, y, z int) string {
	n := s.sulfurGradient.Sample64(float64(x), float64(y), float64(z))
	if n >= -0.4 && n <= -0.1 { return "cinnabar" }
	if n >= 0 && n <= 0.4 { return "sulfur" }
	if n >= 0.4 { return "cinnabar" }
	return ""
}

func (s *modernSurface) band(y, x, z int) string {
	offset := int(math.Round(s.clayBandsOffset.Sample64(float64(x), 0, float64(z)) * 4))
	return s.clayBands[(y+offset+192)%192]
}

func (s *modernSurface) makeClayBands() {
	for i := range s.clayBands { s.clayBands[i] = "terracotta" }
	r := s.random.FromHashOf("minecraft:clay_bands")
	for i := 0; i < len(s.clayBands); i++ {
		i += int(r.NextInt(5)) + 1
		if i < len(s.clayBands) { s.clayBands[i] = "orange_terracotta" }
	}
	s.makeBands(&r, 1, "yellow_terracotta")
	s.makeBands(&r, 2, "brown_terracotta")
	s.makeBands(&r, 1, "red_terracotta")
	count := int(r.NextInt(7)) + 9
	for start, n := 0, 0; n < count && start < len(s.clayBands); start, n = start+int(r.NextInt(16))+4, n+1 {
		s.clayBands[start] = "white_terracotta"
		if start-1 > 0 && r.NextBoolean() { s.clayBands[start-1] = "light_gray_terracotta" }
		if start+1 < len(s.clayBands) && r.NextBoolean() { s.clayBands[start+1] = "light_gray_terracotta" }
	}
}

func (s *modernSurface) makeBands(r *java26.Random, width int, name string) {
	count := int(r.NextInt(10)) + 6
	for range count {
		bandWidth := width + int(r.NextInt(3))
		start := int(r.NextInt(192))
		for p := range min(bandWidth, len(s.clayBands)-start) { s.clayBands[start+p] = name }
	}
}

func (s *modernSurface) verticalGradient(name string, x, y, z int32, minY, maxY int) bool {
	if int(y) <= minY { return true }
	if int(y) >= maxY { return false }
	named := s.random.FromHashOf("minecraft:" + name)
	random := named.ForkPositional().At(x, y, z)
	probability := float64(maxY-int(y)) / float64(maxY-minY)
	return float64(random.NextFloat()) < probability
}

func (s *modernSurface) erodedBadlands(c *chunk.Chunk, x, z uint8, wx, wz int32, height, minY, maxY int, b surfaceBlocks, air uint32) {
	pillar := math.Min(math.Abs(s.badlandsSurface.Sample64(float64(wx), 0, float64(wz))*8.25), s.badlandsPillar.Sample64(float64(wx)*0.2, 0, float64(wz)*0.2)*15)
	if pillar <= 0 { return }
	roof := math.Abs(s.badlandsPillarRoof.Sample64(float64(wx)*0.75, 0, float64(wz)*0.75) * 1.5)
	top := 64 + math.Min(pillar*pillar*2.5, math.Ceil(roof*50)+24)
	start := int(math.Floor(top))
	if height > start { return }
	for y := min(start, maxY); y >= minY; y-- {
		rid := c.Block(x, int16(y), z, 0)
		if rid == b.id("stone") { break }
		if rid == b.id("water") { return }
	}
	for y := min(start, maxY); y >= minY && c.Block(x, int16(y), z, 0) == air; y-- {
		c.SetBlock(x, int16(y), z, 0, b.id("stone"))
	}
}

func (s *modernSurface) frozenOcean(c *chunk.Chunk, x, z uint8, wx, wz int32, height, minY, minSurface int, biome string, b surfaceBlocks) {
	iceberg := math.Min(math.Abs(s.icebergSurface.Sample64(float64(wx), 0, float64(wz))*8.25), s.icebergPillar.Sample64(float64(wx)*1.28, 0, float64(wz)*1.28)*15)
	if iceberg <= 1.8 { return }
	roof := math.Abs(s.icebergPillarRoof.Sample64(float64(wx)*1.17, 0, float64(wz)*1.17) * 1.5)
	top := math.Min(iceberg*iceberg*1.2, math.Ceil(roof*40)+14)
	if s.icebergTemperature(wx, wz, biome) > 0.1 { top -= 2 }
	if top <= 2 { return }
	bottom := float64(modernSeaLevel) - top - 7
	upper := float64(modernSeaLevel) + top
	r := s.random.At(wx, 0, wz)
	maxSnow := 2 + int(r.NextInt(4))
	minSnow := modernSeaLevel + 18 + int(r.NextInt(10))
	snow := 0
	start := max(height, int(upper)+1)
	if start > modernMaxY { start = modernMaxY }
	endY := max(minSurface, minY)
	for y := start; y >= endY; y-- {
		rid := c.Block(x, int16(y), z, 0)
		replace := rid == c.BlockRegistry().AirRuntimeID() && y < int(upper) && r.NextDouble() > 0.01 || rid == b.id("water") && y > int(bottom) && y < modernSeaLevel && r.NextDouble() > 0.15
		if !replace { continue }
		if snow <= maxSnow && y > minSnow {
			c.SetBlock(x, int16(y), z, 0, b.id("snow_block"))
			snow++
		} else {
			c.SetBlock(x, int16(y), z, 0, b.id("packed_ice"))
		}
	}
}

func (s *modernSurface) icebergTemperature(x, z int32, biome string) float32 {
	base := float32(0)
	if biome == "deep_frozen_ocean" { base = 0.5 }
	large := s.frozenTemperatureLayers[0].Sample2D(float64(x)*0.05, float64(z)*0.05)*float64(float32(0.14285715))
	large += s.frozenTemperatureLayers[1].Sample2D(float64(x)*0.025, float64(z)*0.025)*float64(float32(0.2857143))
	large += s.frozenTemperatureLayers[2].Sample2D(float64(x)*0.0125, float64(z)*0.0125)*float64(float32(0.5714286))
	icePatches := large*7 + s.biomeInfo.Sample2D(float64(x)*0.2, float64(z)*0.2)
	if icePatches < 0.3 && s.biomeInfo.Sample2D(float64(x)*0.09, float64(z)*0.09) < 0.8 { return 0.2 }
	return base
}
