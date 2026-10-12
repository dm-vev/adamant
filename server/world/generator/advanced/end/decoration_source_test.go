package end

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/go-gl/mathgl/mgl64"
)

type endSourceCrystal struct {
	Position   mgl64.Vec3 `json:"position"`
	Rotation   [2]float32 `json:"rotation"`
	ShowBase   bool       `json:"showBase"`
	BeamTarget *cube.Pos  `json:"beamTarget"`
}

func assertEndSourceCrystals(t *testing.T, result world.GenerationResult, expected []endSourceCrystal) {
	t.Helper()
	if len(result.Entities) != len(expected) {
		t.Fatalf("crystals=%d source=%d", len(result.Entities), len(expected))
	}
	actual := make(map[mgl64.Vec3]*entity.EndCrystal, len(result.Entities))
	for _, handle := range result.Entities {
		crystal, ok := handle.WorldlessEntity(nil).(*entity.EndCrystal)
		if !ok {
			t.Fatalf("unexpected generated entity %q", handle.Type().EncodeEntity())
		}
		actual[crystal.Position()] = crystal
	}
	for _, source := range expected {
		crystal := actual[source.Position]
		if crystal == nil {
			t.Errorf("missing crystal at %v", source.Position)
			continue
		}
		rotation := crystal.Rotation()
		beam, hasBeam := crystal.BeamTarget()
		if rotation[0] != float64(source.Rotation[0]) || rotation[1] != float64(source.Rotation[1]) || crystal.ShowBase() != source.ShowBase || hasBeam != (source.BeamTarget != nil) || (hasBeam && beam != source.BeamTarget.Vec3()) {
			t.Errorf("crystal at %v: rotation=%v base=%v beam=%v/%v source=%+v", source.Position, rotation, crystal.ShowBase(), beam, hasBeam, source)
		}
	}
}

// The oracle hashes complete Java states in Y/X/Z order, including every
// default property. All captured source fluids are empty. Source structures
// were disabled; these targets are also clear of source End-city pieces.
func endSourceBlockHash(t *testing.T, c *chunk.Chunk) string {
	t.Helper()
	cache := make(map[world.NativeState][]byte)
	hash := sha256.New()
	registry := c.BlockRegistry()
	for y := int16(c.Range().Min()); y <= int16(c.Range().Max()); y++ {
		for x := range uint8(16) {
			for z := range uint8(16) {
				state := world.NativeStateAt(c, x, y, z)
				if state.Liquid != registry.AirRuntimeID() {
					t.Fatalf("unexpected fluid at (%d,%d,%d)", x, y, z)
				}
				encoded, ok := cache[state]
				if !ok {
					source, err := world.JavaState(registry, state)
					if err != nil {
						t.Fatal(err)
					}
					var text strings.Builder
					text.WriteString(source.Name)
					if len(source.Properties) != 0 {
						keys := make([]string, 0, len(source.Properties))
						for key := range source.Properties {
							keys = append(keys, key)
						}
						slices.Sort(keys)
						text.WriteByte('[')
						for i, key := range keys {
							if i != 0 {
								text.WriteByte(',')
							}
							fmt.Fprintf(&text, "%s=%v", key, source.Properties[key])
						}
						text.WriteByte(']')
					}
					text.WriteByte('\n')
					encoded = []byte(text.String())
					cache[state] = encoded
				}
				hash.Write(encoded)
			}
		}
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func TestEndDecorationMatchesJava263(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	data, err := os.ReadFile("testdata/java263_decoration.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Seed     int64 `json:"seed"`
		X, Z     int
		Hash     string             `json:"hash"`
		Gateways []endSourceGateway `json:"gateways"`
		Crystals []endSourceCrystal `json:"crystals"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	generators := make(map[int64]*End)
	for _, row := range rows {
		t.Run(fmt.Sprintf("seed=%d/chunk=%d,%d", row.Seed, row.X, row.Z), func(t *testing.T) {
			g := generators[row.Seed]
			if g == nil {
				g = NewEnd(row.Seed)
				generators[row.Seed] = g
			}
			c := chunk.New(world.DefaultBlockRegistry, world.End.Range())
			result := g.GenerateChunk(world.ChunkPos{int32(row.X), int32(row.Z)}, c, world.GenerationContext{})
			t.Cleanup(func() {
				for _, handle := range result.Entities {
					if err := handle.Close(); err != nil {
						t.Error(err)
					}
				}
			})
			if actual := endSourceBlockHash(t, c); actual != row.Hash {
				t.Errorf("complete block hash=%s source=%s", actual, row.Hash)
			}
			assertEndSourceGateways(t, result, row.Gateways)
			assertEndSourceCrystals(t, result, row.Crystals)
		})
	}
}

func TestEndSpikeOnlyWritesFromItsOwningOrigin(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	data, err := os.ReadFile("testdata/java263_spike_ownership.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Seed       int64
		SpikeIndex int
		Origin     [2]int
		Position   cube.Pos
		Block      string
		Entities   int
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		t.Run(fmt.Sprintf("index=%d/origin=%v", row.SpikeIndex, row.Origin), func(t *testing.T) {
			g := NewEnd(row.Seed)
			c := chunk.New(world.DefaultBlockRegistry, world.End.Range())
			var scratch endScratch
			x, y, z := row.Position[0], row.Position[1], row.Position[2]
			region := newEndFeatureRegion(g, x>>4, z>>4, c, &scratch)
			state, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: "minecraft:diamond_block"})
			if err != nil {
				t.Fatal(err)
			}
			region.setBlock(x, y, z, state.Block)
			origin := endFeatureOrigin{region: region, chunkX: row.Origin[0], chunkZ: row.Origin[1]}
			var result world.GenerationResult
			g.generateSpike(origin, x>>4, z>>4, &g.spikes[row.SpikeIndex], &result, newEndRand(0))
			t.Cleanup(func() {
				for _, handle := range result.Entities {
					if err := handle.Close(); err != nil {
						t.Error(err)
					}
				}
			})
			actual, _, ok := world.DefaultBlockRegistry.RuntimeIDToState(region.block(x, y, z))
			if !ok || actual != row.Block {
				t.Errorf("neighbour origin changed the retained block at %v: got=%s source=%s", row.Position, actual, row.Block)
			}
			if len(result.Entities) != row.Entities {
				t.Errorf("neighbour origin emitted %d crystals, source=%d", len(result.Entities), row.Entities)
			}
		})
	}
}
