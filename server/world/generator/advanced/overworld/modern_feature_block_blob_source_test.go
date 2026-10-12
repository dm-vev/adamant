package overworld

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

type blockBlobSourceInitial struct {
	Pos   [3]int32 `json:"pos"`
	State any      `json:"state"`
}

type blockBlobSourceCase struct {
	Name         string                   `json:"name"`
	Algorithm    string                   `json:"algorithm"`
	Seed         int64                    `json:"seed"`
	Origin       [3]int32                 `json:"origin"`
	MinY         int                      `json:"minY"`
	MaxY         int                      `json:"maxY"`
	Config       map[string]any           `json:"config"`
	RejectWrites bool                     `json:"rejectWrites"`
	Initial      []blockBlobSourceInitial `json:"initial"`
	Placed       bool                     `json:"placed"`
	NextLong     string                   `json:"nextLong"`
	States       []replaceBlobSourceBlock `json:"states"`
}

type blockBlobSourceFixture struct {
	Oracle string                `json:"oracle"`
	Cases  []blockBlobSourceCase `json:"cases"`
}

// blockBlobSourceTerrain builds the sparse live world sampled by BlockBlobFeature.
func blockBlobSourceTerrain(t *testing.T, tc blockBlobSourceCase) replaceBlobSourceTerrain {
	t.Helper()
	terrain := replaceBlobSourceTerrain{
		minY: int32(tc.MinY), maxY: int32(tc.MaxY),
		air:     replaceBlobSourceNative(t, replaceBlobSourceBlock{State: "minecraft:air"}),
		voidAir: replaceBlobSourceNative(t, replaceBlobSourceBlock{State: "minecraft:void_air"}),
		initial: make(map[featurePos]world.NativeState, len(tc.Initial)),
	}
	for _, row := range tc.Initial {
		var name string
		var properties map[string]any
		switch value := row.State.(type) {
		case string:
			name = value
		case map[string]any:
			name, _ = value["id"].(string)
			properties, _ = value["properties"].(map[string]any)
		default:
			t.Fatalf("invalid Java Source initial block state %v", row.State)
		}
		id, err := javadata.BlockStateID(name, properties)
		if err != nil {
			t.Fatalf("invalid Java Source initial state %s: %v", name, err)
		}
		terrain.initial[featurePos{row.Pos[0], row.Pos[1], row.Pos[2]}] = world.ResolveStateID(world.DefaultBlockRegistry, id)
	}
	return terrain
}

func TestBlockBlobFeatureMatchesJava263Source(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_block_blob_source.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture blockBlobSourceFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Oracle == "" || len(fixture.Cases) == 0 {
		t.Fatal("Java BlockBlob Source fixture has no oracle record or cases")
	}
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			if tc.MinY >= tc.MaxY {
				t.Fatalf("invalid build range [%d,%d]", tc.MinY, tc.MaxY)
			}
			terrain := blockBlobSourceTerrain(t, tc)
			origin := featurePos{tc.Origin[0], tc.Origin[1], tc.Origin[2]}
			target := world.ChunkPos{origin.x >> 4, origin.z >> 4}
			c := chunk.New(world.DefaultBlockRegistry, cube.Range{tc.MinY, tc.MaxY})
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
			var random java26.Random
			switch tc.Algorithm {
			case "legacy":
				random = java26.NewLegacyRandom(tc.Seed)
			case "xoroshiro":
				random = java26.NewXoroshiroRandom(tc.Seed)
			default:
				t.Fatalf("unsupported Java Source RNG algorithm %q", tc.Algorithm)
			}
			if got := placeModernBlockBlob(region, tc.Config, origin, &random); got != tc.Placed {
				t.Errorf("placement=%v, Source=%v", got, tc.Placed)
			}
			want := make(map[featurePos]uint32, len(tc.States))
			for _, row := range tc.States {
				want[featurePos{row.Pos[0], row.Pos[1], row.Pos[2]}] = replaceBlobSourceNative(t, row).CanonicalJavaID(c.BlockRegistry())
			}
			for pos, state := range region.overlay {
				gotID := state.CanonicalJavaID(c.BlockRegistry())
				if gotID == terrain.BaseState(pos.x, pos.y, pos.z).CanonicalJavaID(c.BlockRegistry()) {
					continue
				}
				expected, exists := want[pos]
				if !exists {
					name, _ := javadata.BlockStateName(gotID)
					t.Errorf("unexpected Java state %s at %v", name, pos)
					continue
				}
				if gotID != expected {
					name, _ := javadata.BlockStateName(gotID)
					wantName, _ := javadata.BlockStateName(expected)
					t.Errorf("state at %v=%s, Source=%s", pos, name, wantName)
				}
				delete(want, pos)
			}
			for pos, id := range want {
				name, _ := javadata.BlockStateName(id)
				t.Errorf("missing Java Source state %s at %v", name, pos)
			}
			next, err := strconv.ParseInt(tc.NextLong, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			if got := random.NextLong(); got != next {
				t.Errorf("following RNG=%d, Source=%d", got, next)
			}
		})
	}
}
