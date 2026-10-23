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

type hugeMushroomSourceCase struct {
	Name         string                     `json:"name"`
	Algorithm    string                     `json:"algorithm"`
	Seed         int64                      `json:"seed"`
	Type         string                     `json:"type"`
	Origin       [3]int32                   `json:"origin"`
	Config       map[string]any             `json:"config"`
	RejectWrites bool                       `json:"rejectWrites"`
	Initial      []blockColumnSourceInitial `json:"initial"`
	Placed       bool                       `json:"placed"`
	NextLong     string                     `json:"nextLong"`
	States       []replaceBlobSourceBlock   `json:"states"`
}

type hugeMushroomSourceFixture struct {
	MinY  int                      `json:"minY"`
	MaxY  int                      `json:"maxY"`
	Cases []hugeMushroomSourceCase `json:"cases"`
}

func TestHugeMushroomFeatureMatchesSource(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_huge_mushroom_source.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture hugeMushroomSourceFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			terrain := blockColumnSourceTerrain(t, blockColumnSourceCase{Initial: tc.Initial}, fixture.MinY, fixture.MaxY)
			origin := featurePos{tc.Origin[0], tc.Origin[1], tc.Origin[2]}
			target := world.ChunkPos{origin.x >> 4, origin.z >> 4}
			c := chunk.New(world.DefaultBlockRegistry, cube.Range{fixture.MinY, fixture.MaxY})
			for pos, state := range terrain.initial {
				if featureInsideTarget(target, pos) {
					world.SetNativeState(c, uint8(pos.x&15), int16(pos.y), uint8(pos.z&15), state)
				}
			}
			region := newFeatureRegion(&ModernFeatures{source: terrain}, c, target, &world.GenerationResult{})
			region.SetOrigin(target[0], target[1])
			if tc.RejectWrites {
				region.SetOrigin(target[0]+3, target[1]+3)
			}
			random := vanilla.NewLegacyRandom(tc.Seed)
			if tc.Algorithm == "xoroshiro" {
				random = vanilla.NewXoroshiroRandom(tc.Seed)
			}
			handled, placed := placeModernTreeFamily(region, tc.Type, tc.Config, origin, &random)
			if !handled || placed != tc.Placed {
				t.Errorf("handled=%v placed=%v Source=%v", handled, placed, tc.Placed)
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
