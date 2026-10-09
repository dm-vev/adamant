package nether

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

func TestNetherBeardMatchesJava263(t *testing.T) {
	// Captured from actual Java fossil starts, adjusted reference bounds and
	// LongOpenHashSet ordering. Final-density volumes use the source Beardifier
	// and disabled aquifer, before material rules, carvers or decoration.
	type reference struct {
		X, Z     int32
		Min, Max [3]int32
	}
	var fixtures []struct {
		Seed          int64
		X, Z          int32
		References    []reference
		BlocksSHA256  string `json:"blocks_sha256"`
		SamplesSHA256 string `json:"samples_sha256"`
	}
	data, err := os.ReadFile("testdata/java263_beard.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	generators := make(map[int64]*Nether)
	for _, fixture := range fixtures {
		t.Run(fmt.Sprintf("%d/%d,%d", fixture.Seed, fixture.X, fixture.Z), func(t *testing.T) {
			g := generators[fixture.Seed]
			if g == nil {
				g = NewNether(fixture.Seed)
				generators[fixture.Seed] = g
			}
			pos := world.ChunkPos{fixture.X, fixture.Z}
			var refs []reference
			g.forFossilReferences(pos, func(start *netherFossilStart) {
				refs = append(refs, reference{X: start.origin[0] >> 4, Z: start.origin[2] >> 4, Min: start.box.Min, Max: start.box.Max})
			})
			if !slices.Equal(refs, fixture.References) {
				t.Fatalf("ordered fossil references = %+v, want Java %+v", refs, fixture.References)
			}
			beard := g.fossilBeard(pos)
			sampleHash := sha256.New()
			var bits [4]byte
			for _, z := range [...]int32{0, 7, 15} {
				for _, x := range [...]int32{0, 7, 15} {
					for y := int32(0); y < 128; y++ {
						value := beard.AdjustDensity(fixture.X*16+x, y, fixture.Z*16+z)
						binary.LittleEndian.PutUint32(bits[:], math.Float32bits(value))
						sampleHash.Write(bits[:])
					}
				}
			}
			if got := fmt.Sprintf("%x", sampleHash.Sum(nil)); got != fixture.SamplesSHA256 {
				t.Fatalf("beard sample bits SHA256 = %s, want Java %s", got, fixture.SamplesSHA256)
			}
			terrain := chunk.New(world.DefaultBlockRegistry, world.Nether.Range())
			var scratch netherScratch
			g.prepareHeights(int(pos[0]), int(pos[1]), terrain, &scratch, beard)
			var materials [16 * 16 * 128]byte
			index := 0
			for z := uint8(0); z < 16; z++ {
				for x := uint8(0); x < 16; x++ {
					for y := int16(0); y < 128; y++ {
						switch rid := terrain.Block(x, y, z, 0); rid {
						case g.airRID:
							materials[index] = 0
						case g.lavaStillRID:
							materials[index] = 1
						case g.netherrackRID:
							materials[index] = 2
						default:
							t.Fatalf("unexpected pre-material runtime ID %d at %d,%d,%d", rid, x, y, z)
						}
						index++
					}
				}
			}
			if got := fmt.Sprintf("%x", sha256.Sum256(materials[:])); got != fixture.BlocksSHA256 {
				t.Fatalf("adapted terrain SHA256 = %s, want Java %s", got, fixture.BlocksSHA256)
			}
		})
	}
}
