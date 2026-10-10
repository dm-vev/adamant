package overworld

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

type modernTerrainScratch struct {
	lattice java26.OverworldChunk
	aquifer java26.OverworldAquifer
}

// terrainColumn has two acyclic stages: structure height queries use raw noise;
// decoration reads surfaced/carved terrain with structure beardification. Neither
// cache contains placed features or live entity handles.
func (g *Overworld) terrainColumn(pos world.ChunkPos, raw bool) *previewCacheEntry {
	cache := g.previewCache
	if raw {
		cache = g.rawCache
	}
	return cache.load(pos, func() *previewCacheEntry {
		return g.buildTerrainColumn(pos, raw)
	})
}

func (g *Overworld) buildTerrainColumn(pos world.ChunkPos, raw bool) *previewCacheEntry {
	work := g.modernPool.Get().(*modernTerrainScratch)
	defer g.modernPool.Put(work)
	baseX, baseZ := pos[0]*16, pos[1]*16
	work.lattice.Prepare(g.noise, g.zoomSeed, baseX, baseZ)
	work.aquifer.Prepare(g.noise, baseX, baseZ)
	c := chunk.New(world.DefaultBlockRegistry, cube.Range{int(modernMinY), int(modernMaxY)})
	entry := &previewCacheEntry{pos: pos, c: c}
	var terrainResult world.GenerationResult
	if !raw {
		entry.structures = g.modernStructures.Prepare(pos)
	}
	materials := [...]uint32{g.stoneRID, g.airRID, g.waterRID, g.lavaRID}
	for z := range int32(16) {
		worldZ := baseZ + z
		for x := range int32(16) {
			worldX := baseX + x
			for y := int32(modernMaxY); y >= modernMinY; y-- {
				density := work.lattice.Sample(x, y, z)
				if entry.structures != nil {
					density += entry.structures.AdjustDensity(worldX, y, worldZ)
				}
				material, schedule := work.aquifer.Substance(worldX, y, worldZ, float64(density))
				rid := materials[material]
				if rid != g.airRID {
					c.SetBlock(uint8(x), int16(y), uint8(z), 0, rid)
				}
				if raw {
					continue
				}
				c.SetBiome(uint8(x), int16(y), uint8(z), modernBiomeIDs[work.lattice.BiomeIndexAt(worldX, y, worldZ)])
				if schedule && (material == java26.AquiferWater || material == java26.AquiferLava) {
					terrainResult.MarkForPostProcessing(cube.Pos{int(worldX), int(y), int(worldZ)}, c.Range())
				}
			}
		}
	}
	if !raw {
		g.modernSurface.Apply(pos, c, &work.lattice)
		carved := g.modernFeatures.Carve(pos, c, modernCarverFluid{
			aquifer: &work.aquifer,
			surface: g.modernSurface,
			lattice: &work.lattice,
			chunk:   c,
			pos:     pos,
		})
		appendPostProcessing(&terrainResult, carved.PostProcessing)
		entry.postProcessing = terrainResult.PostProcessing
	}
	c.Compact()
	return entry
}

func appendPostProcessing(result *world.GenerationResult, markers [][]uint16) {
	if len(result.PostProcessing) < len(markers) {
		result.PostProcessing = append(result.PostProcessing, make([][]uint16, len(markers)-len(result.PostProcessing))...)
	}
	for section := range len(markers) {
		if result.PostProcessing[section] == nil {
			result.PostProcessing[section] = append([]uint16{}, markers[section]...)
			continue
		}
		result.PostProcessing[section] = append(result.PostProcessing[section], markers[section]...)
	}
}

// modernTerrainSource separates generator base-column queries from the mutable
// WorldGenRegion used while replaying decoration stages.
type modernTerrainSource struct {
	generator *Overworld
	raw       bool
}

func (modernTerrainSource) UseLegacyRandomSource() bool { return false }

func (modernTerrainSource) JavaHasSkyLight() bool { return true }

func (modernTerrainSource) JavaFluidTickDelay(name string) int32 {
	switch name {
	case "minecraft:water":
		return 5
	case "minecraft:lava":
		return 30
	default:
		panic("overworld: unsupported generation fluid tick " + name)
	}
}

func (modernTerrainSource) SeaLevel() int32 { return modernSeaLevel }

func (s modernTerrainSource) JavaFeatureBiomeNames() []string {
	return modernJavaBiomeNames[:]
}

func (s modernTerrainSource) JavaNoiseBiomeAt(quartX, quartY, quartZ int32) string {
	column := s.generator.noise.SampleBiomeColumn(quartX*4, quartZ*4)
	return column.Biome(max(-16, min(quartY, 79)) * 4)
}

func (s modernTerrainSource) JavaBiomeAt(x, y, z int32) string {
	qx, qy, qz := java26.FiddledBiomeQuart(s.generator.zoomSeed, x, y, z)
	return s.JavaNoiseBiomeAt(qx, qy, qz)
}

func (s modernTerrainSource) BaseState(x, y, z int32) world.NativeState {
	if y < modernMinY || y > modernMaxY {
		return world.NativeState{Block: s.generator.airRID, Liquid: s.generator.airRID}
	}
	entry := s.generator.terrainColumn(world.ChunkPos{x >> 4, z >> 4}, s.raw)
	return world.NativeStateAt(entry.c, uint8(x&15), int16(y), uint8(z&15))
}

func (s modernTerrainSource) BaseBlock(x, y, z int32) world.Block {
	rid := s.BaseState(x, y, z).Block
	b, ok := world.BlockByRuntimeID(rid)
	if !ok {
		panic(fmt.Sprintf("overworld: invalid base runtime ID %d at %d,%d,%d", rid, x, y, z))
	}
	return b
}

func (s modernTerrainSource) BaseHeight(x, z int32, oceanFloor bool) int32 {
	entry := s.generator.terrainColumn(world.ChunkPos{x >> 4, z >> 4}, s.raw)
	lx, lz := uint8(x&15), uint8(z&15)
	for y := int32(entry.c.HighestBlock(lx, lz)); y >= modernMinY; y-- {
		rid := entry.c.Block(lx, int16(y), lz, 0)
		if rid == s.generator.airRID {
			continue
		}
		if !oceanFloor || s.generator.blocksMotionInHeightmap(rid) {
			return y + 1
		}
	}
	return modernMinY
}

func (s modernTerrainSource) PlaceStructureStep(pos world.ChunkPos, step int, env javastructure.ProcessorEnvironment, emit func(javastructure.Placement)) {
	entry := s.generator.terrainColumn(pos, false)
	entry.structures.PlaceStep(pos, entry.c.Range(), step, env, emit)
}

func (g *Overworld) blocksMotionInHeightmap(rid uint32) bool {
	if rid == g.stoneRID {
		return true
	}
	if rid == g.airRID || rid == g.waterRID || rid == g.lavaRID {
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

type modernCarverFluid struct {
	aquifer *java26.OverworldAquifer
	surface *modernSurface
	lattice *java26.OverworldChunk
	chunk   *chunk.Chunk
	pos     world.ChunkPos
}

func (a modernCarverFluid) CarveSubstance(x, y, z int32, density float32) (world.Block, bool) {
	material, schedule := a.aquifer.Substance(x, y, z, float64(density))
	switch material {
	case java26.AquiferSolid:
		return nil, false
	case java26.AquiferAir:
		return block.Air{}, false
	case java26.AquiferWater:
		return block.Water{Depth: 8, Still: true}, schedule
	case java26.AquiferLava:
		return block.Lava{Depth: 8, Still: true}, schedule
	default:
		panic("overworld: invalid aquifer material")
	}
}

func (a modernCarverFluid) CarveTopMaterial(x, y, z int32, hasFluid bool) (world.Block, bool) {
	rid, matched := a.surface.TopMaterial(a.pos, a.chunk, a.lattice, x, y, z, hasFluid)
	if !matched {
		return nil, false
	}
	material, ok := world.BlockByRuntimeID(rid)
	if !ok {
		panic(fmt.Sprintf("overworld: invalid carver top material runtime ID %d", rid))
	}
	return material, true
}
