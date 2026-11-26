package overworld

import (
	"math"

	"github.com/dm-vev/adamant/server/internal/vanilla"
	"github.com/dm-vev/adamant/server/world"
	"github.com/dm-vev/adamant/server/world/chunk"
)

const surfaceNone uint8 = 255

const (
	sbOther uint8 = iota
	sbBadlands
	sbErodedBadlands
	sbWoodedBadlands
	sbSwamp
	sbMangroveSwamp
	sbFrozenOcean
	sbDeepFrozenOcean
	sbWarmOcean
	sbLukewarmOcean
	sbDeepLukewarmOcean
	sbBeach
	sbSnowyBeach
	sbDesert
	sbFrozenPeaks
	sbJaggedPeaks
	sbSnowySlopes
	sbGrove
	sbStonyPeaks
	sbStonyShore
	sbWindsweptHills
	sbDripstoneCaves
	sbSulfurCaves
	sbWindsweptSavanna
	sbWindsweptGravellyHills
	sbOldGrowthTaiga
	sbIceSpikes
	sbMushroomFields
	sbDappledForest
)

var surfaceBiomeTags = func() (tags [vanilla.OverworldBiomeCount]uint8) {
	for i := range tags {
		tags[i] = surfaceBiomeTagOf(biomeName(vanilla.OverworldBiomeName(uint8(i))))
	}
	return
}()

func surfaceBiomeTagOf(name string) uint8 {
	switch name {
	case "badlands":
		return sbBadlands
	case "eroded_badlands":
		return sbErodedBadlands
	case "wooded_badlands":
		return sbWoodedBadlands
	case "swamp":
		return sbSwamp
	case "mangrove_swamp":
		return sbMangroveSwamp
	case "frozen_ocean":
		return sbFrozenOcean
	case "deep_frozen_ocean":
		return sbDeepFrozenOcean
	case "warm_ocean":
		return sbWarmOcean
	case "lukewarm_ocean":
		return sbLukewarmOcean
	case "deep_lukewarm_ocean":
		return sbDeepLukewarmOcean
	case "beach":
		return sbBeach
	case "snowy_beach":
		return sbSnowyBeach
	case "desert":
		return sbDesert
	case "frozen_peaks":
		return sbFrozenPeaks
	case "jagged_peaks":
		return sbJaggedPeaks
	case "snowy_slopes":
		return sbSnowySlopes
	case "grove":
		return sbGrove
	case "stony_peaks":
		return sbStonyPeaks
	case "stony_shore":
		return sbStonyShore
	case "windswept_hills":
		return sbWindsweptHills
	case "dripstone_caves":
		return sbDripstoneCaves
	case "sulfur_caves":
		return sbSulfurCaves
	case "windswept_savanna":
		return sbWindsweptSavanna
	case "windswept_gravelly_hills":
		return sbWindsweptGravellyHills
	case "old_growth_pine_taiga", "old_growth_spruce_taiga":
		return sbOldGrowthTaiga
	case "ice_spikes":
		return sbIceSpikes
	case "mushroom_fields":
		return sbMushroomFields
	case "dappled_forest":
		return sbDappledForest
	default:
		return sbOther
	}
}

func (s *modernSurface) blocksFor(registry chunk.BlockRegistry) surfaceBlocks {
	if registry == world.DefaultBlockRegistry {
		s.defaultBlocksOnce.Do(func() {
			s.defaultBlocks = resolveSurfaceBlocks(registry)
		})
		return s.defaultBlocks
	}
	return resolveSurfaceBlocks(registry)
}

func (s *modernSurface) bedrockAt(x, y, z int32) bool {
	if y <= modernMinY {
		return true
	}
	if y >= modernMinY+5 {
		return false
	}
	random := s.bedrockRandom.At(x, y, z)
	probability := float64(modernMinY+5-int(y)) / float64(5)
	return float64(random.NextFloat()) < probability
}

func (s *modernSurface) deepslateAt(x, y, z int32) bool {
	if y <= 0 {
		return true
	}
	if y >= 8 {
		return false
	}
	random := s.deepslateRandom.At(x, y, z)
	probability := float64(8-int(y)) / float64(8)
	return float64(random.NextFloat()) < probability
}

func (s *modernSurface) oreVeinMaterialIdx(x, y, z int32, copper, iron, richness, gap float32) uint8 {
	if copper > 0 {
		random := s.oreRandom.At(x, y, z)
		if random.NextFloat() <= copper {
			if random.NextFloat() < richness && gap < 0 {
				if random.NextFloat() < 0.02 {
					return surfaceRawCopperBlock
				}
				return surfaceCopperOre
			}
			return surfaceGranite
		}
	}
	if iron > 0 {
		random := s.oreRandom.At(x, y, z)
		if random.NextFloat() <= iron {
			if random.NextFloat() < richness && gap < 0 {
				if random.NextFloat() < 0.02 {
					return surfaceRawIronBlock
				}
				return surfaceDeepslateIronOre
			}
			return surfaceTuff
		}
	}
	return surfaceNone
}

func (s *modernSurface) sulfurMaterialIdx(x, y, z int) uint8 {
	n := s.sulfurGradient.Sample64(float64(x), float64(y), float64(z))
	if n >= -0.4 && n <= -0.1 {
		return surfaceCinnabar
	}
	if n >= 0 && n <= 0.4 {
		return surfaceSulfur
	}
	if n >= 0.4 {
		return surfaceCinnabar
	}
	return surfaceNone
}

func (s *modernSurface) bandIdx(y, x, z int) uint8 {
	offset := int(math.Round(s.clayBandsOffset.Sample64(float64(x), 0, float64(z)) * 4))
	return s.clayBandIDs[(y+offset+192)%192]
}

func (s *modernSurface) surfaceMaterialIdx(b uint8, x, y, z, stoneAbove, stoneBelow, surfaceDepth int, notUnderwater, notUnderDeepWater, onFloor, underFloor, deepUnderFloor, veryDeepUnderFloor bool, gx, gz int, bandNoise float64) uint8 {
	clayNoise := bandNoise >= -0.909 && bandNoise <= -0.5454 || bandNoise >= -0.1818 && bandNoise <= 0.1818 || bandNoise >= 0.5454 && bandNoise <= 0.909
	ceiling := stoneBelow <= 1
	if onFloor {
		if b == sbWoodedBadlands && y >= 97+surfaceDepth*2 {
			if clayNoise {
				return surfaceCoarseDirt
			}
			if notUnderwater {
				return surfaceGrass
			}
			return surfaceDirt
		}
		if (b == sbSwamp && y >= 62 && y < modernSeaLevel || b == sbMangroveSwamp && y >= 60 && y < modernSeaLevel) && s.swamp.Sample64(float64(x), 0, float64(z)) >= 0 {
			return surfaceWater
		}
	}
	if b == sbBadlands || b == sbErodedBadlands || b == sbWoodedBadlands {
		if mat := s.badlandsMaterialIdx(x, y, z, stoneAbove, surfaceDepth, clayNoise, ceiling, notUnderwater, notUnderDeepWater, onFloor, underFloor); mat != surfaceNone {
			return mat
		}
	}
	if onFloor && notUnderwater {
		if (b == sbFrozenOcean || b == sbDeepFrozenOcean) && surfaceDepth <= 0 {
			return surfaceAir
		}
		if mat := s.biomeSurfaceIdx(b, x, y, z, gx, gz, ceiling, notUnderwater, bandNoise); mat != surfaceNone {
			return mat
		}
	}
	if notUnderDeepWater {
		if onFloor && (b == sbFrozenOcean || b == sbDeepFrozenOcean) && surfaceDepth <= 0 {
			return surfaceWater
		}
		if underFloor {
			if mat := s.underBiomeSurfaceIdx(b, x, y, z, gx, gz, ceiling, notUnderwater, bandNoise); mat != surfaceNone {
				return mat
			}
		}
		if deepUnderFloor && (b == sbWarmOcean || b == sbBeach || b == sbSnowyBeach) {
			return surfaceSandstone
		}
		if veryDeepUnderFloor && b == sbDesert {
			return surfaceSandstone
		}
	}
	if onFloor {
		if b == sbFrozenPeaks || b == sbJaggedPeaks {
			return surfaceStone
		}
		if b == sbWarmOcean || b == sbLukewarmOcean || b == sbDeepLukewarmOcean {
			if ceiling {
				return surfaceSandstone
			}
			return surfaceSand
		}
		if ceiling {
			return surfaceStone
		}
		return surfaceGravel
	}
	return surfaceNone
}

func (s *modernSurface) badlandsMaterialIdx(x, y, z, stoneAbove, surfaceDepth int, clayNoise, ceiling, notUnderwater, notUnderDeepWater, onFloor, underFloor bool) uint8 {
	if onFloor {
		if y >= 256 {
			return surfaceOrangeTerracotta
		}
		if y+stoneAbove >= 74+surfaceDepth {
			if clayNoise {
				return surfaceTerracotta
			}
			return s.bandIdx(y, x, z)
		}
		if notUnderwater {
			if ceiling {
				return surfaceRedSandstone
			}
			return surfaceRedSand
		}
		if surfaceDepth > 0 {
			return surfaceOrangeTerracotta
		}
		if !notUnderDeepWater {
			return surfaceWhiteTerracotta
		}
		if ceiling {
			return surfaceStone
		}
		return surfaceGravel
	}
	if y+stoneAbove >= 63-surfaceDepth {
		if y+stoneAbove >= 63 && y+stoneAbove < 74+surfaceDepth {
			return surfaceOrangeTerracotta
		}
		return s.bandIdx(y, x, z)
	}
	if underFloor && notUnderDeepWater {
		return surfaceWhiteTerracotta
	}
	return surfaceNone
}

func surfaceCeilingOr(ceiling bool, ifCeil, ifNot uint8) uint8 {
	if ceiling {
		return ifCeil
	}
	return ifNot
}

func (s *modernSurface) biomeSurfaceIdx(b uint8, x, y, z, gx, gz int, ceiling, notUnderwater bool, bandNoise float64) uint8 {
	surface := bandNoise / 8.25
	switch b {
	case sbFrozenPeaks:
		if surfaceSteep(gx, gz) {
			return surfacePackedIce
		}
		if n := s.packedIce.Sample64(float64(x), 0, float64(z)); n >= 0 && n <= 0.2 {
			return surfacePackedIce
		}
		if n := s.ice.Sample64(float64(x), 0, float64(z)); n >= 0 && n <= 0.025 {
			return surfaceIce
		}
		return surfaceSnowBlock
	case sbSnowySlopes:
		if surfaceSteep(gx, gz) {
			return surfaceStone
		}
		if n := s.powderSnow.Sample64(float64(x), 0, float64(z)); n >= 0.35 && n <= 0.6 {
			return surfacePowderSnow
		}
		return surfaceSnowBlock
	case sbJaggedPeaks:
		return surfaceCeilingOr(surfaceSteep(gx, gz), surfaceStone, surfaceSnowBlock)
	case sbGrove:
		if n := s.powderSnow.Sample64(float64(x), 0, float64(z)); n >= 0.35 && n <= 0.6 {
			return surfacePowderSnow
		}
		return surfaceSnowBlock
	case sbStonyPeaks:
		if n := s.calcite.Sample64(float64(x), 0, float64(z)); n >= -0.0125 && n <= 0.0125 {
			return surfaceCalcite
		}
		return surfaceStone
	case sbStonyShore:
		if n := s.gravel.Sample64(float64(x), 0, float64(z)); n >= -0.05 && n <= 0.05 {
			return surfaceCeilingOr(ceiling, surfaceStone, surfaceGravel)
		}
		return surfaceStone
	case sbWindsweptHills:
		if surface >= 1 {
			return surfaceStone
		}
	case sbBeach, sbSnowyBeach, sbWarmOcean, sbDesert:
		return surfaceCeilingOr(ceiling, surfaceSandstone, surfaceSand)
	case sbDripstoneCaves:
		return surfaceStone
	case sbSulfurCaves:
		if mat := s.sulfurMaterialIdx(x, y, z); mat != surfaceNone {
			return mat
		}
		return surfaceStone
	case sbWindsweptSavanna:
		if surface >= 1.75 {
			return surfaceStone
		}
		if surface >= -0.5 {
			return surfaceCoarseDirt
		}
	case sbWindsweptGravellyHills:
		if surface >= 2 {
			return surfaceCeilingOr(ceiling, surfaceStone, surfaceGravel)
		}
		if surface >= 1 {
			return surfaceStone
		}
		if surface >= -1 {
			return surfaceGrass
		}
	case sbOldGrowthTaiga:
		if surface >= 1.75 {
			return surfaceCoarseDirt
		}
		if surface >= -0.95 {
			return surfacePodzol
		}
	case sbIceSpikes:
		return surfaceSnowBlock
	case sbMushroomFields:
		return surfaceMycelium
	case sbDappledForest:
		if s.smallPatch.Sample64(float64(x), 0, float64(z)) >= 1.2 {
			return surfaceCoarseDirt
		}
	}
	return surfaceCeilingOr(notUnderwater, surfaceGrass, surfaceDirt)
}

func (s *modernSurface) underBiomeSurfaceIdx(b uint8, x, y, z, gx, gz int, ceiling, notUnderwater bool, bandNoise float64) uint8 {
	surface := bandNoise / 8.25
	switch b {
	case sbFrozenPeaks:
		if surfaceSteep(gx, gz) {
			return surfacePackedIce
		}
		if n := s.packedIce.Sample64(float64(x), 0, float64(z)); n >= -0.5 && n <= 0.2 {
			return surfacePackedIce
		}
		if n := s.ice.Sample64(float64(x), 0, float64(z)); n >= -0.0625 && n <= 0.025 {
			return surfaceIce
		}
		if notUnderwater {
			return surfaceSnowBlock
		}
	case sbSnowySlopes:
		if surfaceSteep(gx, gz) {
			return surfaceStone
		}
		if n := s.powderSnow.Sample64(float64(x), 0, float64(z)); n >= 0.45 && n <= 0.58 && notUnderwater {
			return surfacePowderSnow
		}
		if notUnderwater {
			return surfaceSnowBlock
		}
	case sbJaggedPeaks:
		return surfaceStone
	case sbGrove:
		if n := s.powderSnow.Sample64(float64(x), 0, float64(z)); n >= 0.45 && n <= 0.58 && notUnderwater {
			return surfacePowderSnow
		}
	case sbStonyPeaks:
		if n := s.calcite.Sample64(float64(x), 0, float64(z)); n >= -0.0125 && n <= 0.0125 {
			return surfaceCalcite
		}
		return surfaceStone
	case sbStonyShore:
		if n := s.gravel.Sample64(float64(x), 0, float64(z)); n >= -0.05 && n <= 0.05 {
			if ceiling {
				return surfaceStone
			}
			return surfaceGravel
		}
		return surfaceStone
	case sbWindsweptHills:
		if surface >= 1 {
			return surfaceStone
		}
	case sbBeach, sbSnowyBeach, sbWarmOcean, sbDesert:
		if ceiling {
			return surfaceSandstone
		}
		return surfaceSand
	case sbDripstoneCaves:
		return surfaceStone
	case sbSulfurCaves:
		if mat := s.sulfurMaterialIdx(x, y, z); mat != surfaceNone {
			return mat
		}
		return surfaceStone
	case sbMangroveSwamp:
		return surfaceMud
	case sbWindsweptSavanna:
		if surface >= 1.75 {
			return surfaceStone
		}
	case sbWindsweptGravellyHills:
		if surface >= 2 {
			if ceiling {
				return surfaceStone
			}
			return surfaceGravel
		}
		if surface >= 1 {
			return surfaceStone
		}
		if surface >= -1 {
			return surfaceDirt
		}
		if ceiling {
			return surfaceStone
		}
		return surfaceGravel
	}
	return surfaceDirt
}

func (s *modernSurface) applyFlat(pos world.ChunkPos, work *modernTerrainScratch, registry chunk.BlockRegistry, air, water, lava uint32) {
	blocks := s.blocksFor(registry)
	chunkX, chunkZ := int(pos[0])<<4, int(pos[1])<<4
	for x := range 16 {
		for z := range 16 {
			worldX, worldZ := chunkX+x, chunkZ+z
			height := int(work.heights[x+1][z+1])
			surfaceBiomeIdx := work.biomeAt(x, height, z, worldX, worldZ)
			surfaceBiome := surfaceBiomeTags[surfaceBiomeIdx]
			if surfaceBiome == sbErodedBadlands {
				if newH := s.erodedBadlandsFlat(work, x, z, int32(worldX), int32(worldZ), height, blocks, air); newH != height {
					height = newH
					work.heights[x+1][z+1] = int16(height)
				}
			}
			gradientX := int(work.heights[min(x+2, 16)][z+1]) - int(work.heights[max(x, 1)][z+1])
			gradientZ := int(work.heights[x+1][min(z+2, 16)]) - int(work.heights[x+1][max(z, 1)])
			preliminary := int(work.lattice.PreliminarySurfaceHeight(int32(worldX), int32(worldZ)))
			columnRandom := s.random.At(int32(worldX), 0, int32(worldZ))
			bandNoise := s.surface.Sample64(float64(worldX), 0, float64(worldZ))
			surfaceDepth := int(bandNoise*2.75 + 3 + columnRandom.NextDouble()*0.25)
			secNoise := s.surfaceSecondary.Sample64(float64(worldX), 0, float64(worldZ))
			secondaryDepth := int((secNoise + 1) * 0.5 * 6)
			deepSecondaryDepth := int((secNoise + 1) * 0.5 * 30)
			minSurface := preliminary + surfaceDepth - 8
			s.applyFlatColumn(work, x, z, worldX, worldZ, height, minSurface, surfaceDepth, secondaryDepth, deepSecondaryDepth, gradientX, gradientZ, bandNoise, blocks, air, water, lava)
			if surfaceBiome == sbFrozenOcean || surfaceBiome == sbDeepFrozenOcean {
				s.frozenOceanFlat(work, x, z, int32(worldX), int32(worldZ), height, minSurface, surfaceBiome, blocks, air)
				work.heights[x+1][z+1] = int16(work.columnHeight(x, z, air))
			} else if height > modernMinY {
				topY := height - 1
				if work.blocks[(topY-modernMinY)>>4][x<<8|z<<4|((topY-modernMinY)&15)] == air {
					work.heights[x+1][z+1] = int16(work.columnHeight(x, z, air))
				}
			}
		}
	}
}

func (s *modernSurface) applyFlatColumn(work *modernTerrainScratch, x, z, worldX, worldZ, height, minSurface, surfaceDepth, secondaryDepth, deepSecondaryDepth, gradientX, gradientZ int, bandNoise float64, blocks surfaceBlocks, air, water, lava uint32) {
	colOff := x<<8 | z<<4
	startY := min(height, modernMaxY)
	upperEnd := max(minSurface, modernMinY)
	waterHeight := math.MinInt
	stoneAbove, nextCeiling := 0, math.MaxInt
	for y := startY; y >= upperEnd; y-- {
		subIdx := (y - modernMinY) >> 4
		ly := (y - modernMinY) & 15
		rid := work.blocks[subIdx][colOff|ly]
		if rid == air {
			stoneAbove = 0
			waterHeight = math.MinInt
			continue
		}
		if rid == water || rid == lava {
			if waterHeight == math.MinInt {
				waterHeight = y + 1
			}
			continue
		}
		if nextCeiling >= y {
			nextCeiling = flatNextCeiling(work, colOff, y, air, water, lava)
		}
		stoneAbove++
		stoneBelow := y - nextCeiling + 1
		biome := surfaceBiomeTags[work.biomeInChunk(subIdx, colOff|ly)]
		if y < modernMinY+5 && s.bedrockAt(int32(worldX), int32(y), int32(worldZ)) {
			work.setFlatBlock(subIdx, colOff|ly, blocks[surfaceBedrock], air)
			continue
		}
		if y >= -60 && y < 50 && (y < -8 || y >= 0) {
			copper, iron, richness, gap := work.lattice.OreVein(int32(worldX), int32(y), int32(worldZ))
			if mat := s.oreVeinMaterialIdx(int32(worldX), int32(y), int32(worldZ), copper, iron, richness, gap); mat != surfaceNone {
				work.setFlatBlock(subIdx, colOff|ly, blocks[mat], air)
				continue
			}
		}
		notUnderwater := waterHeight == math.MinInt || y >= waterHeight-1
		notUnderDeepWater := waterHeight == math.MinInt || y+stoneAbove >= waterHeight-6-surfaceDepth
		onFloor := stoneAbove <= 1
		underFloor := stoneAbove <= 1+surfaceDepth
		deepUnderFloor := stoneAbove <= 1+surfaceDepth+secondaryDepth
		veryDeepUnderFloor := stoneAbove <= 1+surfaceDepth+deepSecondaryDepth
		if mat := s.surfaceMaterialIdx(biome, worldX, y, worldZ, stoneAbove, stoneBelow, surfaceDepth, notUnderwater, notUnderDeepWater, onFloor, underFloor, deepUnderFloor, veryDeepUnderFloor, gradientX, gradientZ, bandNoise); mat != surfaceNone {
			work.setFlatBlock(subIdx, colOff|ly, blocks[mat], air)
			continue
		}
		if biome == sbSulfurCaves {
			if mat := s.sulfurMaterialIdx(worldX, y, worldZ); mat != surfaceNone {
				work.setFlatBlock(subIdx, colOff|ly, blocks[mat], air)
				continue
			}
		}
		if y < 8 && s.deepslateAt(int32(worldX), int32(y), int32(worldZ)) {
			work.setFlatBlock(subIdx, colOff|ly, blocks[surfaceDeepslate], air)
		}
	}
	lowerStart := min(startY, minSurface-1)
	if lowerStart >= modernMinY {
		s.applyFlatColumnLower(work, colOff, worldX, worldZ, lowerStart, blocks, air, water, lava)
	}
}

func (s *modernSurface) applyFlatColumnLower(work *modernTerrainScratch, colOff, worldX, worldZ, lowerStart int, blocks surfaceBlocks, air, water, lava uint32) {
	bedrockRID := blocks[surfaceBedrock]
	deepslateRID := blocks[surfaceDeepslate]
	for y := lowerStart; y >= modernMinY; y-- {
		subIdx := (y - modernMinY) >> 4
		ly := (y - modernMinY) & 15
		rid := work.blocks[subIdx][colOff|ly]
		if rid == air || rid == water || rid == lava {
			continue
		}
		if y < modernMinY+5 && s.bedrockAt(int32(worldX), int32(y), int32(worldZ)) {
			work.setFlatBlock(subIdx, colOff|ly, bedrockRID, air)
			continue
		}
		if y >= -60 && y < 50 && (y < -8 || y >= 0) {
			copper, iron, richness, gap := work.lattice.OreVein(int32(worldX), int32(y), int32(worldZ))
			if mat := s.oreVeinMaterialIdx(int32(worldX), int32(y), int32(worldZ), copper, iron, richness, gap); mat != surfaceNone {
				work.setFlatBlock(subIdx, colOff|ly, blocks[mat], air)
				continue
			}
		}
		if work.biomeInChunk(subIdx, colOff|ly) == sulfurCavesBiomeIndex {
			if mat := s.sulfurMaterialIdx(worldX, y, worldZ); mat != surfaceNone {
				work.setFlatBlock(subIdx, colOff|ly, blocks[mat], air)
				continue
			}
		}
		if y < 8 && s.deepslateAt(int32(worldX), int32(y), int32(worldZ)) {
			work.setFlatBlock(subIdx, colOff|ly, deepslateRID, air)
		}
	}
}

var sulfurCavesBiomeIndex = func() uint8 {
	for i := range vanilla.OverworldBiomeCount {
		if biomeName(vanilla.OverworldBiomeName(uint8(i))) == "sulfur_caves" {
			return uint8(i)
		}
	}
	return 255
}()

func flatNextCeiling(work *modernTerrainScratch, colOff, y int, air, water, lava uint32) int {
	for lookaheadY := y - 1; lookaheadY >= modernMinY; lookaheadY-- {
		subIdx := (lookaheadY - modernMinY) >> 4
		ly := (lookaheadY - modernMinY) & 15
		lower := work.blocks[subIdx][colOff|ly]
		if lower == air || lower == water || lower == lava {
			return lookaheadY + 1
		}
	}
	return modernMinY
}

func (s *modernSurface) erodedBadlandsFlat(work *modernTerrainScratch, x, z int, wx, wz int32, height int, b surfaceBlocks, air uint32) int {
	pillar := math.Min(math.Abs(s.badlandsSurface.Sample64(float64(wx), 0, float64(wz))*8.25), s.badlandsPillar.Sample64(float64(wx)*0.2, 0, float64(wz)*0.2)*15)
	if pillar <= 0 {
		return height
	}
	roof := math.Abs(s.badlandsPillarRoof.Sample64(float64(wx)*0.75, 0, float64(wz)*0.75) * 1.5)
	top := 64 + math.Min(pillar*pillar*2.5, math.Ceil(roof*50)+24)
	start := int(math.Floor(top))
	if height > start {
		return height
	}
	colOff := x<<8 | z<<4
	stoneRID, waterRID := b[surfaceStone], b[surfaceWater]
	topY := min(start, modernMaxY)
	for y := topY; y >= modernMinY; y-- {
		subIdx := (y - modernMinY) >> 4
		ly := (y - modernMinY) & 15
		rid := work.blocks[subIdx][colOff|ly]
		if rid == stoneRID {
			break
		}
		if rid == waterRID {
			return height
		}
	}
	changed := false
	for y := topY; y >= modernMinY; y-- {
		subIdx := (y - modernMinY) >> 4
		ly := (y - modernMinY) & 15
		if work.blocks[subIdx][colOff|ly] != air {
			break
		}
		work.setFlatBlock(subIdx, colOff|ly, stoneRID, air)
		changed = true
	}
	if !changed {
		return height
	}
	return work.columnHeight(x, z, air)
}

func (s *modernSurface) frozenOceanFlat(work *modernTerrainScratch, x, z int, wx, wz int32, height, minSurface int, biome uint8, b surfaceBlocks, air uint32) {
	iceberg := math.Min(math.Abs(s.icebergSurface.Sample64(float64(wx), 0, float64(wz))*8.25), s.icebergPillar.Sample64(float64(wx)*1.28, 0, float64(wz)*1.28)*15)
	if iceberg <= 1.8 {
		return
	}
	roof := math.Abs(s.icebergPillarRoof.Sample64(float64(wx)*1.17, 0, float64(wz)*1.17) * 1.5)
	top := math.Min(iceberg*iceberg*1.2, math.Ceil(roof*40)+14)
	biomeStr := "frozen_ocean"
	if biome == sbDeepFrozenOcean {
		biomeStr = "deep_frozen_ocean"
	}
	if s.icebergTemperature(wx, wz, biomeStr) > 0.1 {
		top -= 2
	}
	if top <= 2 {
		return
	}
	bottom := float64(modernSeaLevel) - top - 7
	upper := float64(modernSeaLevel) + top
	r := s.random.At(wx, 0, wz)
	maxSnow := 2 + int(r.NextInt(4))
	minSnow := modernSeaLevel + 18 + int(r.NextInt(10))
	snow := 0
	start := min(max(height, int(upper)+1), modernMaxY)
	endY := max(minSurface, modernMinY)
	colOff := x<<8 | z<<4
	waterRID, snowRID, packedIceRID := b[surfaceWater], b[surfaceSnowBlock], b[surfacePackedIce]
	for y := start; y >= endY; y-- {
		subIdx := (y - modernMinY) >> 4
		ly := (y - modernMinY) & 15
		rid := work.blocks[subIdx][colOff|ly]
		replace := rid == air && y < int(upper) && r.NextDouble() > 0.01 || rid == waterRID && y > int(bottom) && y < modernSeaLevel && r.NextDouble() > 0.15
		if !replace {
			continue
		}
		if snow <= maxSnow && y > minSnow {
			work.setFlatBlock(subIdx, colOff|ly, snowRID, air)
			snow++
		} else {
			work.setFlatBlock(subIdx, colOff|ly, packedIceRID, air)
		}
	}
}
