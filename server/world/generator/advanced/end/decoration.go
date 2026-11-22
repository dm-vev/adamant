package end

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
)

type endGenerationBiomes uint8

const (
	endCentralBiome endGenerationBiomes = 1 << iota
	endHighlandsBiome
	endMidlandsBiome
	endSmallIslandsBiome
	endBarrensBiome
)

func endGenerationBiome(name string) endGenerationBiomes {
	switch name {
	case "minecraft:the_end":
		return endCentralBiome
	case "minecraft:end_highlands":
		return endHighlandsBiome
	case "minecraft:end_midlands":
		return endMidlandsBiome
	case "minecraft:small_end_islands":
		return endSmallIslandsBiome
	case "minecraft:end_barrens":
		return endBarrensBiome
	default:
		panic("end: unknown generation biome " + name)
	}
}

func (g *End) biomeAt(x, y, z int) string {
	qx, qy, qz := vanilla.FiddledBiomeQuart(g.biomeZoomSeed, int32(x), int32(y), int32(z))
	return g.noiseBiomeAt(qx, qy, qz)
}

func (r *endFeatureRegion) featureBiomes(origin endFeatureOrigin) endGenerationBiomes {
	var biomes endGenerationBiomes
	for z := origin.chunkZ - 1; z <= origin.chunkZ+1; z++ {
		for x := origin.chunkX - 1; x <= origin.chunkX+1; x++ {
			biomes |= r.sourceBiomes[r.index(x, z)]
		}
	}
	return biomes
}

func (r *endFeatureRegion) zoomedBiomeMatches(g *End, x, y, z int, candidateBiomes, target endGenerationBiomes) bool {
	if candidateBiomes == target {
		return true
	}
	qx, _, qz := vanilla.FiddledBiomeQuart(g.biomeZoomSeed, int32(x), int32(y), int32(z))
	cx, cz := int((qx*4)>>4), int((qz*4)>>4)
	if r.containsChunk(cx, cz) {
		return r.sourceBiomes[r.index(cx, cz)] == target
	}
	return endGenerationBiome(g.noiseBiomeAt(qx, 0, qz)) == target
}

func (g *End) populateOrigin(targetX, targetZ int, origin endFeatureOrigin, result *world.GenerationResult) {
	chunkX, chunkZ := origin.chunkX, origin.chunkZ
	biomes := origin.region.featureBiomes(origin)
	// The source feature sorter yields island=0/0, gateway=4/0, spike=4/1,
	// chorus=9/0 and platform=10/0. Availability comes from the 3x3 raw-biome
	// palette, while each final placement runs BiomeFilter at its zoomed position.
	if biomes&endSmallIslandsBiome != 0 {
		random := g.endFeatureRandom(chunkX, chunkZ, 0, 0)
		for range random.islandCount() {
			x := chunkX*16 + int(random.Intn(16))
			z := chunkZ*16 + int(random.Intn(16))
			y := 55 + int(random.Intn(16))
			if origin.region.zoomedBiomeMatches(g, x, y, z, biomes, endSmallIslandsBiome) {
				g.generateEndIsland(origin, x, y, z, random)
			}
		}
	}
	// Structures precede placed features within their source generation step.
	g.applyEndCityStructures(chunkX, chunkZ, origin.region, origin.isTarget(targetX, targetZ), result)
	if biomes&endHighlandsBiome != 0 {
		random := g.endFeatureRandom(chunkX, chunkZ, 0, 4)
		if random.OneIn700() {
			x := chunkX*16 + int(random.Intn(16))
			z := chunkZ*16 + int(random.Intn(16))
			height := g.motionBlockingHeight(origin.region, x, z)
			if height > origin.region.minY {
				y := height + 3 + int(random.Intn(7))
				if origin.region.zoomedBiomeMatches(g, x, y, z, biomes, endHighlandsBiome) {
					g.generateEndGatewayStructure(origin, x, y, z, result, targetX, targetZ)
				}
			}
		}
	}
	if biomes&endCentralBiome != 0 && origin.region.zoomedBiomeMatches(g, chunkX*16, origin.region.minY, chunkZ*16, biomes, endCentralBiome) {
		g.generateCentralStructures(origin, targetX, targetZ, result, g.endFeatureRandom(chunkX, chunkZ, 1, 4))
	}
	if biomes&endHighlandsBiome != 0 {
		random := g.endFeatureRandom(chunkX, chunkZ, 0, 9)
		for range int(random.Intn(5)) {
			x := chunkX*16 + int(random.Intn(16))
			z := chunkZ*16 + int(random.Intn(16))
			y := g.motionBlockingHeight(origin.region, x, z)
			if y <= origin.region.minY || !origin.region.zoomedBiomeMatches(g, x, y, z, biomes, endHighlandsBiome) || origin.block(x, y, z) != g.airRID {
				continue
			}
			state := origin.region.nativeState(x, y-1, z)
			supports := g.state().endStoneSupportsChorus
			if state.Block != g.endStoneRID || state.Liquid != g.airRID || state.StateID != 0 {
				name, ok := gamedata.BlockStateName(state.CanonicalID(world.DefaultBlockRegistry))
				if !ok {
					panic("end: chorus support has no state")
				}
				var err error
				supports, err = gamedata.BlockTagContains("minecraft:supports_chorus_plant", name)
				if err != nil {
					panic(err)
				}
			}
			if supports {
				world.GenerateChorusPlant(endShapeAccess{endFeatureOrigin: origin, result: result},
					cube.Pos{x, y, z}, &random.random, g.chorusMaxDistance)
			}
		}
	}
	if chunkX == 6 && chunkZ == 0 && biomes&endCentralBiome != 0 && g.biomeAt(100, 49, 0) == "minecraft:the_end" {
		g.generateEndPlatform(origin)
	}
}

func (g *End) motionBlockingHeight(region *endFeatureRegion, x, z int) int {
	chunkX, chunkZ := x>>4, z>>4
	if !region.containsChunk(chunkX, chunkZ) {
		return region.minY
	}
	i := region.index(chunkX, chunkZ)
	localX, localZ := x&15, z&15
	terrain := region.terrains[i]
	hasOverlay := region.dirtyOverlays&(1<<uint(i)) != 0 || (terrain == nil && len(region.overlays[i]) != 0)
	hasLogical := len(region.logicalStates) != 0
	if terrain != nil && !hasOverlay && !hasLogical {
		top := terrain.highestY(localX, localZ)
		if top < 0 {
			return region.minY
		}
		if region.minY+top <= region.maxY {
			return region.minY + top + 1
		}
	}
	startY := region.maxY
	if terrain != nil && !hasLogical {
		topY := region.minY + terrain.highestY(localX, localZ)
		if hasOverlay && region.overlayMaxY[i] > topY {
			topY = region.overlayMaxY[i]
		}
		if topY < region.minY {
			return region.minY
		}
		if topY < startY {
			startY = topY
		}
	}
	registry := world.DefaultBlockRegistry
	overlay := region.overlays[i]
	colBits := uint32(localZ)<<4 | uint32(localX)
	for y := startY; y >= region.minY; y-- {
		var state world.NativeState
		found := false
		if hasLogical {
			if s, ok := region.logicalStates[cube.Pos{x, y, z}]; ok {
				state, found = s, true
			}
		}
		if !found && hasOverlay {
			if s, ok := overlay[uint32(y-region.minY)<<8|colBits]; ok {
				state, found = s, true
			}
		}
		if !found {
			if terrain != nil {
				if terrain.hasBlock(localX, y-region.minY, localZ) {
					return y + 1
				}
				continue
			}
			state = world.NativeStateAt(region.chunks[i], uint8(localX), int16(y), uint8(localZ))
		}
		if state.Block != g.airRID && (state.Block == g.endStoneRID || registry.LiquidBlock(state.Block) || g.blocksMotionInHeightmap(state.Block)) {
			return y + 1
		}
		if state.Liquid != g.airRID && (registry.LiquidBlock(state.Liquid) || g.blocksMotionInHeightmap(state.Liquid)) {
			return y + 1
		}
	}
	return region.minY
}

func (g *End) blocksMotionInHeightmap(rid uint32) bool {
	if rid == g.airRID {
		return false
	}
	if rid == g.endStoneRID {
		return true
	}
	if value, ok := g.motionBlocking.Load(rid); ok {
		return value.(bool)
	}
	state, err := world.CanonicalState(world.DefaultBlockRegistry, world.NativeState{Block: rid, Liquid: g.airRID})
	if err != nil {
		panic(err)
	}
	matches, err := gamedata.BlockTagContains("minecraft:blocks_motion_in_heightmap", state.Name)
	if err != nil {
		panic(err)
	}
	g.motionBlocking.Store(rid, matches)
	return matches
}
