package overworld

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/mc112"
	mcbiome "github.com/df-mc/dragonfly/server/world/generator/advanced/overworld/internal/biome"
)

type vanillaTreeGen uint8

const (
	vanillaTreeOakSmall vanillaTreeGen = iota
	vanillaTreeOakBig
	vanillaTreeBirch
	vanillaTreeTallBirch
	vanillaTreeTaiga1
	vanillaTreeTaiga2
	vanillaTreeMegaPine
	vanillaTreeMegaSpruce
	vanillaTreeCanopy
	vanillaTreeSavanna
	vanillaTreeJungleSmall
	vanillaTreeJungleMega
	vanillaTreeShrub
)

type vanillaTreeSpec struct {
	kind vanillaTreeGen
	// For WorldGenTrees-like generators.
	minHeight int
	// For WorldGenTrees-like generators.
	vinesGrow bool
}

func (g *Overworld) pickVanillaTree(biomeID int, r *mc112.Rand) vanillaTreeSpec {
	switch mcbiome.ID(biomeID) {
	case mcbiome.Taiga, mcbiome.TaigaHills, mcbiome.TaigaMountains,
		mcbiome.SnowyTaiga, mcbiome.SnowyTaigaHills, mcbiome.SnowyTaigaMountains:
		if r.Intn(3) == 0 {
			return vanillaTreeSpec{kind: vanillaTreeTaiga1}
		}
		return vanillaTreeSpec{kind: vanillaTreeTaiga2}

	case mcbiome.GiantTreeTaiga, mcbiome.GiantTreeTaigaHills:
		if r.Intn(3) == 0 {
			if r.Intn(13) != 0 {
				return vanillaTreeSpec{kind: vanillaTreeMegaPine}
			}
			return vanillaTreeSpec{kind: vanillaTreeMegaSpruce}
		}
		if r.Intn(3) == 0 {
			return vanillaTreeSpec{kind: vanillaTreeTaiga1}
		}
		return vanillaTreeSpec{kind: vanillaTreeTaiga2}

	case mcbiome.GiantSpruceTaiga, mcbiome.GiantSpruceTaigaHills:
		if r.Intn(3) == 0 {
			return vanillaTreeSpec{kind: vanillaTreeMegaSpruce}
		}
		if r.Intn(3) == 0 {
			return vanillaTreeSpec{kind: vanillaTreeTaiga1}
		}
		return vanillaTreeSpec{kind: vanillaTreeTaiga2}

	case mcbiome.BirchForest, mcbiome.BirchForestHills:
		return vanillaTreeSpec{kind: vanillaTreeBirch}
	case mcbiome.TallBirchForest, mcbiome.TallBirchHills:
		return vanillaTreeSpec{kind: vanillaTreeTallBirch}

	case mcbiome.DarkForest, mcbiome.DarkForestHills:
		// BiomeForest.Type.ROOFED.
		if r.Intn(3) > 0 {
			return vanillaTreeSpec{kind: vanillaTreeCanopy}
		}
		if r.Intn(5) != 0 {
			if r.Intn(10) == 0 {
				return vanillaTreeSpec{kind: vanillaTreeOakBig}
			}
			return vanillaTreeSpec{kind: vanillaTreeOakSmall, minHeight: 4, vinesGrow: false}
		}
		return vanillaTreeSpec{kind: vanillaTreeBirch}

	case mcbiome.Forest, mcbiome.FlowerForest:
		// BiomeForest.Type.NORMAL/FLOWER.
		if r.Intn(5) != 0 {
			if r.Intn(10) == 0 {
				return vanillaTreeSpec{kind: vanillaTreeOakBig}
			}
			return vanillaTreeSpec{kind: vanillaTreeOakSmall, minHeight: 4, vinesGrow: false}
		}
		return vanillaTreeSpec{kind: vanillaTreeBirch}

	case mcbiome.Plains, mcbiome.SunflowerPlains:
		// BiomePlains.genBigTreeChance.
		if r.Intn(3) == 0 {
			return vanillaTreeSpec{kind: vanillaTreeOakBig}
		}
		return vanillaTreeSpec{kind: vanillaTreeOakSmall, minHeight: 4, vinesGrow: false}

	case mcbiome.Savanna, mcbiome.SavannaPlateau, mcbiome.ShatteredSavanna, mcbiome.ShatteredSavannaPlateau:
		// BiomeSavanna.genBigTreeChance.
		if r.Intn(5) > 0 {
			return vanillaTreeSpec{kind: vanillaTreeSavanna}
		}
		return vanillaTreeSpec{kind: vanillaTreeOakSmall, minHeight: 4, vinesGrow: false}

	case mcbiome.Jungle, mcbiome.JungleHills, mcbiome.ModifiedJungle:
		// BiomeJungle (not edge).
		if r.Intn(10) == 0 {
			return vanillaTreeSpec{kind: vanillaTreeOakBig}
		}
		if r.Intn(2) == 0 {
			return vanillaTreeSpec{kind: vanillaTreeShrub}
		}
		if r.Intn(3) == 0 {
			return vanillaTreeSpec{kind: vanillaTreeJungleMega}
		}
		return vanillaTreeSpec{
			kind:      vanillaTreeJungleSmall,
			minHeight: 4 + int(r.Intn(7)),
			vinesGrow: true,
		}

	case mcbiome.JungleEdge, mcbiome.ModifiedJungleEdge:
		// BiomeJungle edge.
		if r.Intn(10) == 0 {
			return vanillaTreeSpec{kind: vanillaTreeOakBig}
		}
		if r.Intn(2) == 0 {
			return vanillaTreeSpec{kind: vanillaTreeShrub}
		}
		return vanillaTreeSpec{
			kind:      vanillaTreeJungleSmall,
			minHeight: 4 + int(r.Intn(7)),
			vinesGrow: true,
		}
	}

	return vanillaTreeSpec{kind: vanillaTreeOakSmall, minHeight: 4, vinesGrow: false}
}

func (g *Overworld) vanillaTreeMaxRadius(spec vanillaTreeSpec) int {
	// Conservative bounds so the 2x2 decoration simulation catches cross-chunk trees.
	switch spec.kind {
	case vanillaTreeOakSmall, vanillaTreeBirch, vanillaTreeTallBirch:
		return 5
	case vanillaTreeTaiga1, vanillaTreeTaiga2:
		return 6
	case vanillaTreeOakBig:
		return 9
	case vanillaTreeSavanna:
		return 8
	case vanillaTreeCanopy:
		return 10
	case vanillaTreeJungleSmall:
		return 8
	case vanillaTreeJungleMega:
		return 10
	case vanillaTreeShrub:
		return 3
	case vanillaTreeMegaPine, vanillaTreeMegaSpruce:
		return 11
	default:
		return 11
	}
}

func (g *Overworld) generateVanillaTree(
	c *chunk.Chunk,
	preview map[world.ChunkPos]*chunk.Chunk,
	chunkX, chunkZ int,
	x, y, z int,
	r *mc112.Rand,
	spec vanillaTreeSpec,
	apply bool,
) bool {
	switch spec.kind {
	case vanillaTreeOakSmall:
		return g.genWorldGenTrees(c, preview, chunkX, chunkZ, x, y, z, r, spec.minHeight, g.oakLogRID, g.oakLeavesRID, spec.vinesGrow, apply)
	case vanillaTreeJungleSmall:
		return g.genWorldGenTrees(c, preview, chunkX, chunkZ, x, y, z, r, spec.minHeight, g.jungleLogRID, g.jungleLeavesRID, spec.vinesGrow, apply)
	case vanillaTreeOakBig:
		gen := worldGenBigTree{
			g:                 g,
			c:                 c,
			preview:           preview,
			chunkX:            chunkX,
			chunkZ:            chunkZ,
			apply:             apply,
			heightAttenuation: 0.618,
			branchSlope:       0.381,
			scaleWidth:        1.0,
			leafDensity:       1.0,
			trunkSize:         1,
			heightLimitLimit:  12,
			leafDistanceLimit: 4,
		}
		return gen.generate(x, y, z, r)
	case vanillaTreeBirch:
		return g.genWorldGenBirch(c, preview, chunkX, chunkZ, x, y, z, r, false, apply)
	case vanillaTreeTallBirch:
		return g.genWorldGenBirch(c, preview, chunkX, chunkZ, x, y, z, r, true, apply)
	case vanillaTreeTaiga1:
		return g.genWorldGenTaiga1(c, preview, chunkX, chunkZ, x, y, z, r, apply)
	case vanillaTreeTaiga2:
		return g.genWorldGenTaiga2(c, preview, chunkX, chunkZ, x, y, z, r, apply)
	case vanillaTreeMegaPine:
		return g.genWorldGenMegaPine(c, preview, chunkX, chunkZ, x, y, z, r, false, apply)
	case vanillaTreeMegaSpruce:
		return g.genWorldGenMegaPine(c, preview, chunkX, chunkZ, x, y, z, r, true, apply)
	case vanillaTreeCanopy:
		return g.genWorldGenCanopy(c, preview, chunkX, chunkZ, x, y, z, r, apply)
	case vanillaTreeSavanna:
		return g.genWorldGenSavanna(c, preview, chunkX, chunkZ, x, y, z, r, apply)
	case vanillaTreeJungleMega:
		return g.genWorldGenMegaJungle(c, preview, chunkX, chunkZ, x, y, z, r, apply)
	case vanillaTreeShrub:
		return g.genWorldGenShrub(c, preview, chunkX, chunkZ, x, y, z, r, apply)
	default:
		return false
	}
}

func floorToInt(x float64) int {
	return int(math.Floor(x))
}

func (g *Overworld) isFullBlockRID(rid uint32) bool {
	b, ok := world.BlockByRuntimeID(rid)
	if !ok {
		return false
	}
	_, ok = b.Model().(model.Solid)
	return ok
}

func (g *Overworld) canGrowIntoRID(rid uint32) bool {
	if rid == g.airRID || g.isLeaves(rid) || rid == g.grassRID || rid == g.dirtRID || rid == g.coarseDirtRID || rid == g.podzolRID || rid == g.farmlandRID {
		return true
	}
	switch rid {
	case g.oakLogRID, g.spruceLogRID, g.birchLogRID, g.jungleLogRID, g.acaciaLogRID, g.darkOakLogRID:
		return true
	}
	name, _, ok := chunk.RuntimeIDToState(rid)
	if !ok {
		return false
	}
	switch name {
	case "minecraft:sapling", "minecraft:vine":
		return true
	default:
		return false
	}
}

func (g *Overworld) setDirtAt(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, apply bool) {
	if !apply {
		return
	}
	if g.blockRIDAt(c, preview, chunkX, chunkZ, x, y, z) != g.dirtRID {
		g.setRIDIfInChunk(c, chunkX, chunkZ, x, y, z, g.dirtRID)
	}
}
