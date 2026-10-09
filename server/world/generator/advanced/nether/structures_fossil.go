package nether

import (
	"fmt"
	"sync"

	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

const netherFossilSalt int32 = 14357921

type netherFossilStart struct {
	box      javastructure.Box
	origin   [3]int32
	rotation javastructure.Rotation
	name     string
	template *javastructure.Template
}
type netherFossilResult struct {
	start *netherFossilStart
	err   error
}
type netherFossilFlight struct {
	done   chan struct{}
	result netherFossilResult
}
type netherFossilCache struct {
	mu      sync.Mutex
	starts  map[world.ChunkPos]netherFossilResult
	flights map[world.ChunkPos]*netherFossilFlight
}

// fossilStart returns the immutable start for an exact random-spread candidate.
func (g *Nether) fossilStart(cx, cz int32) (*netherFossilStart, error) {
	candidate, err := javastructure.PotentialChunk(g.seed, cx, cz, netherFossilSalt, 2, 1, javastructure.Linear)
	if err != nil {
		return nil, err
	}
	if candidate != [2]int32{cx, cz} {
		return nil, nil
	}
	key := world.ChunkPos{cx, cz}
	cache := &g.fossils
	cache.mu.Lock()
	if cached, ok := cache.starts[key]; ok {
		cache.mu.Unlock()
		return cached.start, cached.err
	}
	if flight, ok := cache.flights[key]; ok {
		cache.mu.Unlock()
		<-flight.done
		return flight.result.start, flight.result.err
	}
	if cache.starts == nil {
		cache.starts = make(map[world.ChunkPos]netherFossilResult)
	}
	if cache.flights == nil {
		cache.flights = make(map[world.ChunkPos]*netherFossilFlight)
	}
	flight := &netherFossilFlight{done: make(chan struct{})}
	cache.flights[key] = flight
	cache.mu.Unlock()

	start, err := g.buildNetherFossilStart(cx, cz)
	result := netherFossilResult{start: start, err: err}
	cache.mu.Lock()
	if len(cache.starts) >= 256 {
		for old := range cache.starts {
			delete(cache.starts, old)
			break
		}
	}
	cache.starts[key] = result
	delete(cache.flights, key)
	flight.result = result
	close(flight.done)
	cache.mu.Unlock()
	return start, err
}

func (g *Nether) buildNetherFossilStart(cx, cz int32) (start *netherFossilStart, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			start = nil
			err = fmt.Errorf("nether: Java Nether fossil start at %d,%d failed: %v", cx, cz, recovered)
		}
	}()
	random := largeFeatureRandom(g.seed, int(cx), int(cz))
	x := cx*16 + random.NextInt(16)
	z := cz*16 + random.NextInt(16)
	// Embedded minecraft:nether_fossil uses uniform [absolute 32, below_top 2].
	// In the Nether's minY=0,height=128 generation context this is [32,125].
	y := int32(32 + random.NextInt(94))
	var column [128]uint32
	g.javaBaseColumn(x, z, &column)
	for y > netherSeaLevel {
		current := column[y]
		y--
		below := column[y]
		sturdy := below != g.airRID && below != g.lavaStillRID && below != g.lavaFlowRID
		if current == g.airRID && (below == g.soulSandRID || sturdy) {
			break
		}
	}
	if y <= netherSeaLevel {
		return nil, nil
	}
	biome := g.featureSource.JavaNoiseBiomeAt(x>>2, y>>2, z>>2)
	allowed, err := javadata.BiomeTagContains("#minecraft:has_structure/nether_fossil", biome)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, nil
	}
	rotation := javastructure.Rotation(random.NextInt(4))
	templateNumber := random.NextInt(14) + 1
	name := fmt.Sprintf("nether_fossils/fossil_%d", templateNumber)
	template := loadNetherTemplate(name)
	origin := [3]int32{x, y, z}
	min, max := netherTemplateBounds(template, origin, rotation)
	return &netherFossilStart{
		box: javastructure.Box{Min: min, Max: max}, origin: origin, rotation: rotation, name: name, template: template,
	}, nil
}

func (s *netherFeatureSource) placeNetherFossilStep(pos world.ChunkPos, env javastructure.ProcessorEnvironment, emit func(javastructure.Placement)) {
	if env.Random == nil {
		panic("nether: Java fossil placement has no step random source")
	}
	targetX, targetZ := int32(pos[0]), int32(pos[1])
	chunkClip := javastructure.Bounds{Min: [3]int32{targetX * 16, 0, targetZ * 16}, Max: [3]int32{targetX*16 + 15, 127, targetZ*16 + 15}}
	s.generator.forFossilReferences(pos, func(start *netherFossilStart) {
		if !fossilBoxIntersects(start.box, chunkClip) {
			return
		}
		// NetherFossilPiece.postProcess expands the supplied chunkBB to the complete
		// template bounding box before delegating to TemplateStructurePiece.
		clip := chunkClip
		for axis := range 3 {
			clip.Min[axis] = min32(clip.Min[axis], start.box.Min[axis])
			clip.Max[axis] = max32(clip.Max[axis], start.box.Max[axis])
		}
		// Fossil metadata may exceed the native dimension; only block placement
		// is restricted to the generated Nether height.
		clip.Min[1] = 0
		clip.Max[1] = 127
		placement, err := start.template.Place(0, start.origin, start.rotation, javastructure.NoMirror, clip, s.generator.seed)
		if err != nil {
			panic(fmt.Errorf("place Java Nether fossil %s: %w", start.name, err))
		}
		filtered := javastructure.Placement{
			Entities: placement.Entities, PostProcessing: placement.PostProcessing,
			ScheduledTicks: placement.ScheduledTicks, StructurePieceWrites: placement.StructurePieceWrites,
		}
		for _, pieceBlock := range placement.Blocks {
			switch pieceBlock.State.Name {
			case "minecraft:air", "minecraft:structure_block", "minecraft:structure_void":
				continue
			}
			javastructure.SeedRandomizableContainerLoot(&pieceBlock, env.Random)
			filtered.Blocks = append(filtered.Blocks, pieceBlock)
		}
		if len(filtered.Blocks) != 0 || len(filtered.Entities) != 0 {
			filtered, err = filtered.AdaptItemFrameEntities()
			if err != nil {
				panic(fmt.Errorf("adapt Java Nether fossil: %w", err))
			}
			emit(filtered)
		}
		random := java26.NewLegacyRandom(s.generator.seed)
		base := random.ForkPositional().At(start.box.Min[0]+(start.box.Max[0]-start.box.Min[0]+1)/2, start.box.Min[1]+(start.box.Max[1]-start.box.Min[1]+1)/2, start.box.Min[2]+(start.box.Max[2]-start.box.Min[2]+1)/2)
		if base.NextFloat() >= 0.5 {
			return
		}
		ghastPos := [3]int32{
			start.box.Min[0] + base.NextInt(start.box.Max[0]-start.box.Min[0]+1),
			start.box.Min[1],
			start.box.Min[2] + base.NextInt(start.box.Max[2]-start.box.Min[2]+1),
		}
		if ghastPos[1] < clip.Min[1] || ghastPos[1] > clip.Max[1] {
			return
		}
		occupied := false
		for _, b := range filtered.Blocks {
			if b.Pos == ghastPos {
				occupied = true
				break
			}
		}
		if occupied {
			return
		}
		name, _ := env.BaseBlock(ghastPos[0], ghastPos[1], ghastPos[2]).EncodeBlock()
		if name != "minecraft:air" {
			return
		}
		facing := []string{"north", "east", "south", "west"}[base.NextInt(4)]
		emit(javastructure.Placement{Blocks: []javastructure.Block{{
			Pos:   ghastPos,
			State: world.BlockState{Name: "minecraft:dried_ghast", Properties: map[string]any{"facing": facing, "hydration": int32(0), "waterlogged": false}},
		}}})
	})
}

func fossilBoxIntersects(box javastructure.Box, clip javastructure.Bounds) bool {
	for axis := range 3 {
		if box.Max[axis] < clip.Min[axis] || box.Min[axis] > clip.Max[axis] {
			return false
		}
	}
	return true
}
