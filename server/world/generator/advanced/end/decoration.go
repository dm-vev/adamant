package end

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
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
		panic("end: unknown Java generation biome " + name)
	}
}

func (g *End) javaBiomeAt(x, y, z int) string {
	qx, qy, qz := java26.FiddledBiomeQuart(g.biomeZoomSeed, int32(x), int32(y), int32(z))
	return g.javaNoiseBiomeAt(qx, qy, qz)
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
			if g.javaBiomeAt(x, y, z) == "minecraft:small_end_islands" {
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
				if g.javaBiomeAt(x, y, z) == "minecraft:end_highlands" {
					g.generateEndGatewayStructure(origin, x, y, z, result, targetX, targetZ)
				}
			}
		}
	}
	if biomes&endCentralBiome != 0 && g.javaBiomeAt(chunkX*16, origin.region.minY, chunkZ*16) == "minecraft:the_end" {
		g.generateCentralStructures(origin, targetX, targetZ, result, g.endFeatureRandom(chunkX, chunkZ, 1, 4))
	}
	if biomes&endHighlandsBiome != 0 {
		random := g.endFeatureRandom(chunkX, chunkZ, 0, 9)
		for range int(random.Intn(5)) {
			x := chunkX*16 + int(random.Intn(16))
			z := chunkZ*16 + int(random.Intn(16))
			y := g.motionBlockingHeight(origin.region, x, z)
			if y <= origin.region.minY || g.javaBiomeAt(x, y, z) != "minecraft:end_highlands" || origin.block(x, y, z) != g.airRID {
				continue
			}
			state := origin.region.nativeState(x, y-1, z)
			name, ok := javadata.BlockStateName(state.CanonicalJavaID(world.DefaultBlockRegistry))
			if !ok {
				panic("end: chorus support has no Java state")
			}
			supports, err := javadata.BlockTagContains("minecraft:supports_chorus_plant", name)
			if err != nil {
				panic(err)
			}
			if supports {
				world.GenerateJavaChorusPlant(endJavaShapeAccess{endFeatureOrigin: origin, result: result},
					cube.Pos{x, y, z}, &random.random, g.chorusMaxDistance)
			}
		}
	}
	if biomes&endCentralBiome != 0 && g.javaBiomeAt(100, 49, 0) == "minecraft:the_end" {
		g.generateEndPlatform(origin)
	}
}

func (g *End) motionBlockingHeight(region *endFeatureRegion, x, z int) int {
	registry := world.DefaultBlockRegistry
	for y := region.maxY; y >= region.minY; y-- {
		state := region.nativeState(x, y, z)
		for _, rid := range [...]uint32{state.Block, state.Liquid} {
			if rid != g.airRID && (registry.LiquidBlock(rid) || g.blocksMotionInHeightmap(rid)) {
				return y + 1
			}
		}
	}
	return region.minY
}

func (g *End) blocksMotionInHeightmap(rid uint32) bool {
	if rid == g.airRID {
		return false
	}
	if value, ok := g.motionBlocking.Load(rid); ok {
		return value.(bool)
	}
	state, err := world.JavaState(world.DefaultBlockRegistry, world.NativeState{Block: rid, Liquid: g.airRID})
	if err != nil {
		panic(err)
	}
	matches, err := javadata.BlockTagContains("minecraft:blocks_motion_in_heightmap", state.Name)
	if err != nil {
		panic(err)
	}
	g.motionBlocking.Store(rid, matches)
	return matches
}
