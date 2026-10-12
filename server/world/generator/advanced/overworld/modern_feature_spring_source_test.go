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

type springSourceTick struct {
	Pos   [3]int32 `json:"pos"`
	Type  string   `json:"type"`
	Delay int32    `json:"delay"`
}

type springSourceCase struct {
	blockColumnSourceCase
	Ticks []springSourceTick `json:"ticks"`
}

type springSourceFixture struct {
	MinY  int                `json:"minY"`
	MaxY  int                `json:"maxY"`
	Cases []springSourceCase `json:"cases"`
}

func TestSpringFeatureMatchesJava263Source(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_spring_source.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture springSourceFixture
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			terrain := blockColumnSourceTerrain(t, tc.blockColumnSourceCase, fixture.MinY, fixture.MaxY)
			origin := featurePos{tc.Origin[0], tc.Origin[1], tc.Origin[2]}
			target := world.ChunkPos{origin.x >> 4, origin.z >> 4}
			c := chunk.New(world.DefaultBlockRegistry, cube.Range{fixture.MinY, fixture.MaxY})
			for pos, state := range terrain.initial {
				if pos.y >= terrain.minY && pos.y <= terrain.maxY && featureInsideTarget(target, pos) {
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
			if got := placeModernSpringRegion(region, tc.Config, origin, &random); got != tc.Placed {
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
			if len(result.ScheduledTicks) != len(tc.Ticks) {
				t.Errorf("scheduled fluid ticks=%v Source=%v", result.ScheduledTicks, tc.Ticks)
			} else {
				for i, want := range tc.Ticks {
					got := result.ScheduledTicks[i]
					pos := cube.Pos{int(want.Pos[0]), int(want.Pos[1]), int(want.Pos[2])}
					if want.Delay != 0 || got.Pos != pos || got.Kind != chunk.FluidTick || got.Type != want.Type || got.Priority != 0 {
						t.Errorf("ordered tick %d=%+v Source=%+v", i, got, want)
					}
				}
			}
			if got := strconv.FormatInt(random.NextLong(), 10); got != tc.NextLong {
				t.Errorf("nextLong=%s Source=%s", got, tc.NextLong)
			}
		})
	}
}
