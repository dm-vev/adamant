package overworld

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

type hugeFungusSourceCase struct {
	Name         string                     `json:"name"`
	Algorithm    string                     `json:"algorithm"`
	Seed         int64                      `json:"seed"`
	Origin       [3]int32                   `json:"origin"`
	MinY         int                        `json:"minY"`
	MaxY         int                        `json:"maxY"`
	GenDepth     int                        `json:"genDepth"`
	Config       map[string]any             `json:"config"`
	RejectWrites bool                       `json:"rejectWrites"`
	Initial      []blockColumnSourceInitial `json:"initial"`
	Placed       bool                       `json:"placed"`
	NextLong     string                     `json:"nextLong"`
	States       []replaceBlobSourceBlock   `json:"states"`
}

type hugeFungusSourceFixture struct {
	MinY  int                    `json:"minY"`
	MaxY  int                    `json:"maxY"`
	Cases []hugeFungusSourceCase `json:"cases"`
}

type hugeFungusSourceTerrain struct {
	replaceBlobSourceTerrain
	genDepth int32
}

func (s hugeFungusSourceTerrain) GenerationDepth() int32 { return s.genDepth }

func TestHugeFungusFeatureMatchesSource(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_huge_fungus_source.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture hugeFungusSourceFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			terrain := hugeFungusSourceTerrain{
				replaceBlobSourceTerrain: blockColumnSourceTerrain(t, blockColumnSourceCase{Initial: tc.Initial}, tc.MinY, tc.MaxY),
				genDepth:                 int32(tc.GenDepth),
			}
			origin := featurePos{tc.Origin[0], tc.Origin[1], tc.Origin[2]}
			target := world.ChunkPos{origin.x >> 4, origin.z >> 4}
			c := chunk.New(world.DefaultBlockRegistry, cube.Range{tc.MinY, tc.MaxY})
			for pos, state := range terrain.initial {
				if featureInsideTarget(target, pos) && pos.y >= int32(tc.MinY) && pos.y <= int32(tc.MaxY) {
					world.SetNativeState(c, uint8(pos.x&15), int16(pos.y), uint8(pos.z&15), state)
				}
			}
			result := &world.GenerationResult{}
			region := newFeatureRegion(&ModernFeatures{source: terrain}, c, target, result)
			region.SetOrigin(target[0], target[1])
			if tc.RejectWrites {
				region.SetOrigin(target[0]+3, target[1]+3)
			}
			random := vanilla.NewLegacyRandom(tc.Seed)
			if tc.Algorithm == "xoroshiro" {
				random = vanilla.NewXoroshiroRandom(tc.Seed)
			}
			placed := placeModernHugeFungus(region, tc.Config, origin, &random)
			if placed != tc.Placed {
				t.Errorf("placed=%v Source=%v", placed, tc.Placed)
			}
			want := make(map[featurePos]uint32, len(tc.States))
			for _, row := range tc.States {
				want[featurePos{row.Pos[0], row.Pos[1], row.Pos[2]}] = replaceBlobSourceNative(t, row).CanonicalID(c.BlockRegistry())
			}
			for pos, state := range region.overlayByPos() {
				got := state.CanonicalID(c.BlockRegistry())
				if got == terrain.BaseState(pos.x, pos.y, pos.z).CanonicalID(c.BlockRegistry()) {
					continue
				}
				expected, exists := want[pos]
				if !exists {
					t.Errorf("unexpected mutation %d at %v", got, pos)
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
		})
	}
}
