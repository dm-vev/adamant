package nether

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

// netherComplexStart is shared by generation and lookup. Rendering must not
// mutate either the selected graph or the templates retained by its pieces.
type netherComplexStart struct {
	fortress *netherBridgeStart
	bastion  javastructure.JigsawResult
	startPos [3]int32 // First-piece bottom centre, the source placement reference.
	name     string
	box      javastructure.Box
}
type netherComplexResult struct {
	start *netherComplexStart
	err   error
}
type netherComplexFlight struct {
	done   chan struct{}
	result netherComplexResult
}

func (g *Nether) complexStart(cx, cz int32) (*netherComplexStart, error) {
	candidate, err := javastructure.PotentialChunk(g.seed, cx, cz, 30084232, 27, 4, javastructure.Linear)
	if err != nil {
		return nil, err
	}
	if candidate != [2]int32{cx, cz} {
		return nil, nil
	}
	return g.cachedComplexStart(cx, cz)
}

// cachedComplexStart requires a previously validated placement candidate.
func (g *Nether) cachedComplexStart(cx, cz int32) (*netherComplexStart, error) {
	key := world.ChunkPos{cx, cz}
	g.complexMu.Lock()
	if cached, ok := g.complexStarts[key]; ok {
		g.complexMu.Unlock()
		return cached.start, cached.err
	}
	if flight, ok := g.complexFlights[key]; ok {
		g.complexMu.Unlock()
		<-flight.done
		return flight.result.start, flight.result.err
	}
	flight := &netherComplexFlight{done: make(chan struct{})}
	g.complexFlights[key] = flight
	g.complexMu.Unlock()
	start, err := g.buildComplexStart(cx, cz)
	result := netherComplexResult{start: start, err: err}
	g.complexMu.Lock()
	// Bound retained decoded graphs; eviction never changes deterministic starts
	// or the immutable result already held by waiting generators.
	if len(g.complexStarts) >= 256 {
		for old := range g.complexStarts {
			delete(g.complexStarts, old)
			break
		}
	}
	g.complexStarts[key] = result
	delete(g.complexFlights, key)
	flight.result = result
	close(flight.done)
	g.complexMu.Unlock()
	return start, err
}

func (g *Nether) buildComplexStart(cx, cz int32) (start *netherComplexStart, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			start = nil
			err = fmt.Errorf("nether: Java complex build at %d,%d failed: %v", cx, cz, recovered)
		}
	}()
	// nether_complexes orders fortress(weight 2), bastion(weight 3). Selection
	// and each attempted structure have independent Legacy large-feature streams.
	selection := largeFeatureRandom(g.seed, int(cx), int(cz))
	if selection.NextInt(5) >= 2 {
		random := largeFeatureRandom(g.seed, int(cx), int(cz))
		prepared, valid, err := javastructure.PrepareJigsaw(javastructure.JigsawOptions{
			Pool: "bastion/starts", Origin: [3]int32{cx * 16, 33, cz * 16}, MaxDepth: 6,
			MaxDistance: 80, MaxVerticalDistance: 80, HeightRange: cube.Range{0, 255}, Seed: g.seed,
			HeightAt: func(x, z int32) int32 { return g.featureSource.BaseHeight(x, z, false) },
		}, &random)
		if err != nil {
			return nil, err
		}
		if valid {
			position := prepared.Position()
			biome := g.featureSource.JavaNoiseBiomeAt(position[0]>>2, position[1]>>2, position[2]>>2)
			allowed, err := javadata.BiomeTagContains("#minecraft:has_structure/bastion_remnant", biome)
			if err != nil {
				return nil, err
			}
			if allowed {
				graph, err := prepared.Build(&random)
				if err != nil {
					return nil, err
				}
				if len(graph.Pieces) != 0 {
					first := graph.Pieces[0].Bounds
					box := first
					for i := 1; i < len(graph.Pieces); i++ {
						for axis := range 3 {
							box.Min[axis] = min(box.Min[axis], graph.Pieces[i].Bounds.Min[axis])
							box.Max[axis] = max(box.Max[axis], graph.Pieces[i].Bounds.Max[axis])
						}
					}
					return &netherComplexStart{name: "minecraft:bastion_remnant", bastion: graph, box: box, startPos: [3]int32{
						first.Min[0] + (first.Max[0]-first.Min[0]+1)/2, first.Min[1], first.Min[2] + (first.Max[2]-first.Min[2]+1)/2,
					}}, nil
				}
			}
		}
		// Java still draws a weighted choice when only fortress remains after a
		// rejected bastion. That draw must not become part of fortress generation.
		selection.NextInt(2)
	}
	random := largeFeatureRandom(g.seed, int(cx), int(cz))
	fortress := newNetherBridgeStart(&random, int(cx), int(cz), g)
	box := fortress.bb
	return &netherComplexStart{name: "minecraft:fortress", fortress: fortress, box: javastructure.Box{
		Min: [3]int32{int32(box.minX), int32(box.minY), int32(box.minZ)},
		Max: [3]int32{int32(box.maxX), int32(box.maxY), int32(box.maxZ)},
	}}, nil
}
