package overworld

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// The controlled flat-stone aquifer isolates geometry: source CarverOutput
// records the mask before replacement, fluids and surface material decisions.
type sourceGeometryAquifer struct{}

func (sourceGeometryAquifer) CarveSubstance(_, _, _ int32, density float32) (world.Block, bool) {
	if density != 0 {
		panic("unexpected geometry density")
	}
	return block.Air{}, false
}

func (sourceGeometryAquifer) CarveTopMaterial(_, _, _ int32, _ bool) (world.Block, bool) {
	panic("unexpected surface replacement in flat-stone geometry")
}

func TestCarvingGeometryMatchesJava263(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	data, err := os.ReadFile("testdata/java263_carving_geometry.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Kind                   string
		Seed                   int64
		X, Z, SourceX, SourceZ int32
		Starts                 int
		Hash                   string
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		t.Run(fmt.Sprintf("%s/seed=%d/chunk=%d,%d", row.Kind, row.Seed, row.X, row.Z), func(t *testing.T) {
			g := NewOverworld(row.Seed)
			f := g.modernFeatures
			pos := world.ChunkPos{row.X, row.Z}
			c := chunk.New(world.DefaultBlockRegistry, cube.Range{-64, 319})
			for y := int16(-64); y < 320; y++ {
				for x := range uint8(16) {
					for z := range uint8(16) {
						c.SetBlock(x, y, z, 0, g.stoneRID)
					}
				}
			}
			if row.Kind == "source" {
				biome := f.biomes[normalizeJavaResource(f.source.JavaNoiseBiomeAt(row.SourceX<<2, 0, row.SourceZ<<2))]
				cfg := f.carvers[biome.carvers[0]]
				random := largeFeatureRandom(row.Seed, row.SourceX, row.SourceZ)
				starts := 0
				if float64(random.NextFloat()) <= cfg.config["probability"].(float64) {
					starts = 1
					mask := newCarvingMask(-63, 312)
					f.carveCave(c, pos, cfg.config, row.SourceX, row.SourceZ, &random, -63, 312, mask)
					var result world.GenerationResult
					mask.apply(pos, c, sourceGeometryAquifer{}, &result)
				}
				if starts != row.Starts {
					t.Fatalf("started sources=%d Java=%d", starts, row.Starts)
				}
			} else {
				f.Carve(pos, c, sourceGeometryAquifer{})
			}
			hash := sha256.New()
			var value [1]byte
			for y := int16(-64); y < 320; y++ {
				for x := range uint8(16) {
					for z := range uint8(16) {
						value[0] = 0
						if c.Block(x, y, z, 0) == g.airRID {
							value[0] = 1
						}
						hash.Write(value[:])
					}
				}
			}
			if actual := fmt.Sprintf("%x", hash.Sum(nil)); actual != row.Hash {
				t.Errorf("carved footprint=%s source=%s", actual, row.Hash)
			}
		})
	}
}
