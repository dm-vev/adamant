package overworld

import (
	"encoding/json"
	"os"
	"slices"
	"strconv"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

type diskSourceCase struct {
	Name         string                     `json:"name"`
	Algorithm    string                     `json:"algorithm"`
	Seed         int64                      `json:"seed"`
	Origin       [3]int32                   `json:"origin"`
	Config       map[string]any             `json:"config"`
	RejectWrites bool                       `json:"rejectWrites"`
	Initial      []blockColumnSourceInitial `json:"initial"`
	Placed       bool                       `json:"placed"`
	NextLong     string                     `json:"nextLong"`
	States       []replaceBlobSourceBlock   `json:"states"`
	MarkerChunks []diskSourceMarkerChunk    `json:"markerChunks"`
}
type diskSourceMarkerChunk struct {
	ChunkX   int                       `json:"chunkX"`
	ChunkZ   int                       `json:"chunkZ"`
	Sections []diskSourceMarkerSection `json:"sections"`
}

type diskSourceMarkerSection struct {
	Section int      `json:"section"`
	Packed  []uint16 `json:"packed"`
}

type diskSourceFixture struct {
	MinY  int              `json:"minY"`
	MaxY  int              `json:"maxY"`
	Cases []diskSourceCase `json:"cases"`
}

func TestDiskFeatureMatchesJava263Source(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_disk_source.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture diskSourceFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			terrain := blockColumnSourceTerrain(t, blockColumnSourceCase{
				Name: tc.Name, Algorithm: tc.Algorithm, Seed: tc.Seed, Origin: tc.Origin,
				Config: tc.Config, RejectWrites: tc.RejectWrites, Initial: tc.Initial,
				Placed: tc.Placed, NextLong: tc.NextLong, States: tc.States,
			}, fixture.MinY, fixture.MaxY)
			origin := featurePos{tc.Origin[0], tc.Origin[1], tc.Origin[2]}
			target := world.ChunkPos{origin.x >> 4, origin.z >> 4}
			c := chunk.New(world.DefaultBlockRegistry, cube.Range{fixture.MinY, fixture.MaxY})
			for pos, state := range terrain.initial {
				if featureInsideTarget(target, pos) {
					world.SetNativeState(c, uint8(pos.x&15), int16(pos.y), uint8(pos.z&15), state)
				}
			}
			result := &world.GenerationResult{}
			region := newFeatureRegion(&ModernFeatures{source: terrain}, c, target, result)
			region.SetOrigin(target[0], target[1])
			if tc.RejectWrites {
				region.SetOrigin(target[0]+3, target[1]+3)
			}
			random := java26.NewLegacyRandom(tc.Seed)
			if tc.Algorithm == "xoroshiro" {
				random = java26.NewXoroshiroRandom(tc.Seed)
			}
			if got := placeModernDiskRegion(region, tc.Config, origin, &random); got != tc.Placed {
				t.Errorf("placed=%v Source=%v", got, tc.Placed)
			}
			want := make(map[featurePos]uint32, len(tc.States))
			for _, row := range tc.States {
				want[featurePos{row.Pos[0], row.Pos[1], row.Pos[2]}] = replaceBlobSourceNative(t, row).CanonicalJavaID(c.BlockRegistry())
			}
			for pos, state := range region.overlay {
				got := state.CanonicalJavaID(c.BlockRegistry())
				if got == terrain.BaseState(pos.x, pos.y, pos.z).CanonicalJavaID(c.BlockRegistry()) {
					continue
				}
				expected, exists := want[pos]
				if !exists {
					t.Errorf("unexpected write at %v: %d", pos, got)
					continue
				}
				if got != expected {
					t.Errorf("state at %v=%d Source=%d", pos, got, expected)
				}
				delete(want, pos)
			}
			for pos, id := range want {
				t.Errorf("missing Source state %d at %v", id, pos)
			}
			if got := strconv.FormatInt(random.NextLong(), 10); got != tc.NextLong {
				t.Errorf("nextLong=%s Source=%s", got, tc.NextLong)
			}
			if tc.MarkerChunks != nil {
				sectionCount := (fixture.MaxY >> 4) - (fixture.MinY >> 4) + 1
				wantMarkers := make([][]uint16, sectionCount)
				for _, markerChunk := range tc.MarkerChunks {
					if markerChunk.ChunkX != int(target[0]) || markerChunk.ChunkZ != int(target[1]) {
						continue
					}
					for _, section := range markerChunk.Sections {
						if section.Section < 0 || section.Section >= len(wantMarkers) {
							t.Errorf("Source marker section %d outside expected range", section.Section)
							continue
						}
						wantMarkers[section.Section] = append(wantMarkers[section.Section], section.Packed...)
					}
				}
				for section := range max(len(wantMarkers), len(result.PostProcessing)) {
					var got []uint16
					if section < len(result.PostProcessing) {
						got = result.PostProcessing[section]
					}
					var want []uint16
					if section < len(wantMarkers) {
						want = wantMarkers[section]
					}
					if !slices.Equal(got, want) {
						t.Errorf("post-processing section %d=%v Source=%v", section, got, want)
					}
				}
			}
		})
	}
}
