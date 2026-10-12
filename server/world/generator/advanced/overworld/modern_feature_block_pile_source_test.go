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

type blockPileSourceCase struct {
	Name         string                   `json:"name"`
	Algorithm    string                   `json:"algorithm"`
	Seed         int64                    `json:"seed"`
	Origin       [3]int32                 `json:"origin"`
	Config       map[string]any           `json:"config"`
	RejectWrites bool                     `json:"rejectWrites"`
	Initial      []replaceBlobSourceBlock `json:"initial"`
	Placed       bool                     `json:"placed"`
	NextLong     string                   `json:"nextLong"`
	States       []replaceBlobSourceBlock `json:"states"`
}

type blockPileSourceFixture struct {
	Oracle string                `json:"oracle"`
	MinY   int                   `json:"minY"`
	MaxY   int                   `json:"maxY"`
	Cases  []blockPileSourceCase `json:"cases"`
}

func blockPileSourceTerrain(t *testing.T, tc blockPileSourceCase, minY, maxY int) replaceBlobSourceTerrain {
	t.Helper()
	terrain := replaceBlobSourceTerrain{
		minY: int32(minY), maxY: int32(maxY),
		air:     replaceBlobSourceNative(t, replaceBlobSourceBlock{State: "minecraft:air"}),
		voidAir: replaceBlobSourceNative(t, replaceBlobSourceBlock{State: "minecraft:void_air"}),
		initial: make(map[featurePos]world.NativeState, len(tc.Initial)),
	}
	for _, row := range tc.Initial {
		pos := featurePos{row.Pos[0], row.Pos[1], row.Pos[2]}
		if pos.y < terrain.minY || pos.y > terrain.maxY {
			t.Fatalf("initial pile position %v outside build bounds", pos)
		}
		terrain.initial[pos] = replaceBlobSourceNative(t, row)
	}
	return terrain
}

func TestBlockPileFeatureMatchesJava263Source(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_block_pile_source.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture blockPileSourceFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.MinY >= fixture.MaxY || len(fixture.Cases) == 0 {
		t.Fatal("Java block pile fixture has invalid build bounds or no cases")
	}
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			terrain := blockPileSourceTerrain(t, tc, fixture.MinY, fixture.MaxY)
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
			} else if tc.Algorithm != "legacy" {
				t.Fatalf("unsupported Source RNG algorithm %q", tc.Algorithm)
			}
			if got := placeModernBlockPile(region, tc.Config, origin, &random); got != tc.Placed {
				t.Errorf("placement=%v, Source=%v", got, tc.Placed)
			}
			want := make(map[featurePos]uint32, len(tc.States))
			for _, row := range tc.States {
				want[featurePos{row.Pos[0], row.Pos[1], row.Pos[2]}] = replaceBlobSourceNative(t, row).CanonicalJavaID(c.BlockRegistry())
			}
			for pos, state := range region.overlay {
				gotID := state.CanonicalJavaID(c.BlockRegistry())
				initialID := terrain.BaseState(pos.x, pos.y, pos.z).CanonicalJavaID(c.BlockRegistry())
				if gotID == initialID {
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
			for pos, state := range want {
				name, _ := javadata.BlockStateName(state)
				t.Errorf("missing Source state %s at %v", name, pos)
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
