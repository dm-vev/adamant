package overworld

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

type iceFeatureSourceCase struct {
	blockColumnSourceCase
	FeatureType string `json:"featureType"`
	SeaLevel    int32  `json:"seaLevel"`
}

type iceFeatureSourceFixture struct {
	MinY  int                    `json:"minY"`
	MaxY  int                    `json:"maxY"`
	Cases []iceFeatureSourceCase `json:"cases"`
}

type iceFeatureSourceTerrain struct {
	replaceBlobSourceTerrain
	seaLevel int32
}

func (s iceFeatureSourceTerrain) SeaLevel() int32 { return s.seaLevel }

func iceFeatureTerrain(t *testing.T, tc iceFeatureSourceCase, minY, maxY int) iceFeatureSourceTerrain {
	t.Helper()
	return iceFeatureSourceTerrain{replaceBlobSourceTerrain: blockColumnSourceTerrain(t, tc.blockColumnSourceCase, minY, maxY), seaLevel: tc.SeaLevel}
}

func TestIceFeaturesMatchJava263Source(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_ice_feature_source.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture iceFeatureSourceFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			terrain := iceFeatureTerrain(t, tc, fixture.MinY, fixture.MaxY)
			origin := featurePos{tc.Origin[0], tc.Origin[1], tc.Origin[2]}
			target := world.ChunkPos{origin.x >> 4, origin.z >> 4}
			c := chunk.New(world.DefaultBlockRegistry, cube.Range{fixture.MinY, fixture.MaxY})
			for pos, state := range terrain.initial {
				if featureInsideTarget(target, pos) && pos.y >= int32(fixture.MinY) && pos.y <= int32(fixture.MaxY) {
					world.SetNativeState(c, uint8(pos.x&15), int16(pos.y), uint8(pos.z&15), state)
				}
			}
			region := newFeatureRegion(&ModernFeatures{source: terrain}, c, target, &world.GenerationResult{})
			region.SetOrigin(target[0], target[1])
			if tc.RejectWrites {
				region.SetOrigin(target[0]+3, target[1]+3)
			}
			random := java26.NewLegacyRandom(tc.Seed)
			if tc.Algorithm == "xoroshiro" {
				random = java26.NewXoroshiroRandom(tc.Seed)
			}
			handled, placed := placeModernIceFamily(region, tc.FeatureType, tc.Config, origin, &random)
			if !handled || placed != tc.Placed {
				t.Errorf("handled=%v placed=%v Source=%v", handled, placed, tc.Placed)
			}
			want := make(map[featurePos]uint32, len(tc.States))
			for _, row := range tc.States {
				want[featurePos{row.Pos[0], row.Pos[1], row.Pos[2]}] = replaceBlobSourceNative(t, row).CanonicalJavaID(c.BlockRegistry())
			}
			for pos, state := range region.overlayByPos() {
				got := state.CanonicalJavaID(c.BlockRegistry())
				if got == terrain.BaseState(pos.x, pos.y, pos.z).CanonicalJavaID(c.BlockRegistry()) {
					continue
				}
				expected, exists := want[pos]
				if !exists {
					t.Errorf("unexpected state %d at %v", got, pos)
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
