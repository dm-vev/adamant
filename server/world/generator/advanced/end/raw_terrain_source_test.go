package end

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

func TestEndRawTerrainMatchesJava263(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	data, err := os.ReadFile("testdata/java263_raw_terrain.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Chunks []struct {
			Seed    int64  `json:"seed"`
			X       int    `json:"x"`
			Z       int    `json:"z"`
			Min     int    `json:"min"`
			Max     int    `json:"max"`
			Hash    string `json:"hash"`
			Heights []int  `json:"heights"`
		} `json:"chunks"`
		Biomes []struct {
			Seed   int64  `json:"seed"`
			QuartX int32  `json:"quartX"`
			QuartY int32  `json:"quartY"`
			QuartZ int32  `json:"quartZ"`
			Biome  string `json:"biome"`
		} `json:"biomes"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	generators := make(map[int64]*End)
	for _, row := range fixture.Chunks {
		g := generators[row.Seed]
		if g == nil {
			g = NewEnd(row.Seed)
			generators[row.Seed] = g
		}
		c := chunk.New(world.DefaultBlockRegistry, world.End.Range())
		var scratch endScratch
		g.generateTerrain(row.X, row.Z, c, &scratch)
		fnv := uint64(0xcbf29ce484222325)
		minY, maxY := 128, -1
		var tops [4]int
		columns := [4][2]int{{0, 0}, {8, 8}, {15, 15}, {0, 15}}
		for y := range 128 {
			for x := range 16 {
				for z := range 16 {
					id := c.Block(uint8(x), int16(y), uint8(z), 0)
					material := byte(0)
					if id != g.airRID {
						material = 1
						if y < minY {
							minY = y
						}
						if y > maxY {
							maxY = y
						}
					}
					fnv ^= uint64(material)
					fnv *= 0x100000001b3
					for i, pos := range columns {
						if pos[0] == x && pos[1] == z && material != 0 {
							tops[i] = y + 1
						}
					}
				}
			}
		}
		if fmt.Sprintf("%016x", fnv) != row.Hash || minY != row.Min || maxY != row.Max {
			t.Fatalf("seed=%d chunk=(%d,%d): range/hash got=(%d,%d,%016x) want=(%d,%d,%s)", row.Seed, row.X, row.Z, minY, maxY, fnv, row.Min, row.Max, row.Hash)
		}
		if len(row.Heights) != len(tops) {
			t.Fatalf("seed=%d chunk=(%d,%d): source has %d heights, want %d", row.Seed, row.X, row.Z, len(row.Heights), len(tops))
		}
		for i := range tops {
			if tops[i] != row.Heights[i] {
				t.Fatalf("seed=%d chunk=(%d,%d) column=%v: top=%d want=%d", row.Seed, row.X, row.Z, columns[i], tops[i], row.Heights[i])
			}
		}
	}
	for _, row := range fixture.Biomes {
		g := generators[row.Seed]
		if g == nil {
			g = NewEnd(row.Seed)
			generators[row.Seed] = g
		}
		if got := g.javaNoiseBiomeAt(row.QuartX, row.QuartY, row.QuartZ); got != row.Biome {
			t.Fatalf("seed=%d quart=(%d,%d,%d): biome=%s want=%s", row.Seed, row.QuartX, row.QuartY, row.QuartZ, got, row.Biome)
		}
	}
}
