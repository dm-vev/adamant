package nether

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

func TestNetherRawTerrainJava263(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_raw_terrain.json")
	if err != nil {
		t.Fatal(err)
	}
	var source struct {
		Columns []struct {
			Seed     int64  `json:"seed"`
			X        int32  `json:"x"`
			Z        int32  `json:"z"`
			Material string `json:"material"`
		} `json:"columns"`
	}
	if err = json.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	generators := map[int64]*Nether{}
	var scratch netherScratch
	for _, row := range source.Columns {
		g := generators[row.Seed]
		if g == nil {
			g = NewNether(row.Seed)
			generators[row.Seed] = g
		}
		terrain := chunk.New(world.DefaultBlockRegistry, world.Nether.Range())
		g.prepareHeights(int(row.X>>4), int(row.Z>>4), terrain, &scratch, nil)
		var column [128]uint32
		g.javaBaseColumn(row.X, row.Z, &column)
		if len(row.Material) != len(column) {
			t.Fatalf("invalid source column length %d", len(row.Material))
		}
		for y, material := range []byte(row.Material) {
			var expected uint32
			switch material {
			case 'a':
				expected = g.airRID
			case 'l':
				expected = g.lavaStillRID
			case 'n':
				expected = g.netherrackRID
			default:
				t.Fatalf("invalid source material %q", material)
			}
			actual := terrain.Block(uint8(row.X&15), int16(y), uint8(row.Z&15), 0)
			if actual != expected || column[y] != expected {
				t.Fatalf("seed=%d xyz=(%d,%d,%d): terrain=%d column=%d source=%d", row.Seed, row.X, y, row.Z, actual, column[y], expected)
			}
		}
	}
}

func TestNetherTerrainPipelineJava263(t *testing.T) {
	// Actual ChunkGenerator.createReferences and NoiseBasedChunkGenerator.buildTerrain,
	// including source beard, MaterialSystem, configured carvers and loaded block tags.
	// Source BlockState.CODEC encoded every palette entry as its default-state name.
	var fixtures []struct {
		Seed    int64
		X, Z    int32
		Palette map[string]string
		SHA256  string
	}
	data, err := os.ReadFile("testdata/java263_terrain_pipeline.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fmt.Sprintf("%d/%d,%d", fixture.Seed, fixture.X, fixture.Z), func(t *testing.T) {
			g := NewNether(fixture.Seed)
			terrain := chunk.New(world.DefaultBlockRegistry, world.Nether.Range())
			g.prepareTerrain(world.ChunkPos{fixture.X, fixture.Z}, terrain)
			palette := make(map[uint64]byte, len(fixture.Palette))
			for symbol, name := range fixture.Palette {
				state, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: name})
				if err != nil {
					t.Fatal(err)
				}
				palette[uint64(state.Block)<<32|uint64(state.Liquid)] = symbol[0]
			}
			var output [128 * 16 * 16]byte
			index := 0
			for y := int16(0); y < 128; y++ {
				for x := uint8(0); x < 16; x++ {
					for z := uint8(0); z < 16; z++ {
						state := world.NativeStateAt(terrain, x, y, z)
						symbol, ok := palette[uint64(state.Block)<<32|uint64(state.Liquid)]
						if !ok {
							decoded, err := world.JavaState(world.DefaultBlockRegistry, state)
							t.Fatalf("state outside Java palette at %d,%d,%d: native=%+v Java=%+v error=%v", x, y, z, state, decoded, err)
						}
						output[index] = symbol
						index++
					}
				}
			}
			if got := fmt.Sprintf("%x", sha256.Sum256(output[:])); got != fixture.SHA256 {
				t.Fatalf("terrain pipeline SHA256 = %s, want Java %s", got, fixture.SHA256)
			}
		})
	}
}
