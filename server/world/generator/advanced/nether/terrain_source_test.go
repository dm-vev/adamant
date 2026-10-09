package nether

import (
	"encoding/json"
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
		g.prepareHeights(int(row.X>>4), int(row.Z>>4), terrain, &scratch)
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
