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

type fallenTreeSourceActor struct {
	Pos                [3]int32       `json:"pos"`
	NBT                map[string]any `json:"nbt"`
	FirstEligibleTicks []int          `json:"firstEligibleTicks"`
}

type fallenTreeSourceCase struct {
	Name           string                     `json:"name"`
	Algorithm      string                     `json:"algorithm"`
	Seed           int64                      `json:"seed"`
	Origin         [3]int32                   `json:"origin"`
	Config         map[string]any             `json:"config"`
	Initial        []blockColumnSourceInitial `json:"initial"`
	RejectWrites   bool                       `json:"rejectWrites"`
	Placed         bool                       `json:"placed"`
	NextLong       string                     `json:"nextLong"`
	States         []replaceBlobSourceBlock   `json:"states"`
	Actors         []fallenTreeSourceActor    `json:"actors"`
	PostProcessing [][3]int32                 `json:"postProcessing"`
}

type fallenTreeSourceFixture struct {
	Oracle string                 `json:"oracle"`
	MinY   int                    `json:"minY"`
	MaxY   int                    `json:"maxY"`
	Cases  []fallenTreeSourceCase `json:"cases"`
}

func TestFallenTreeFeatureMatchesJava263Source(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_fallen_tree_source.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture fallenTreeSourceFixture
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
				if featureInsideTarget(target, pos) && pos.y >= int32(fixture.MinY) && pos.y <= int32(fixture.MaxY) {
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
			if placed := placeModernFallenTree(region, tc.Config, origin, &random); placed != tc.Placed {
				t.Errorf("placed=%v Source=%v", placed, tc.Placed)
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
			if len(result.BlockEntities) != len(tc.Actors) {
				t.Fatalf("actor payloads=%d Source=%d", len(result.BlockEntities), len(tc.Actors))
			}
			actors := make(map[cube.Pos]map[string]any, len(result.BlockEntities))
			for _, actor := range result.BlockEntities {
				actors[actor.Pos] = actor.Data
			}
			for _, actor := range tc.Actors {
				pos := cube.Pos{int(actor.Pos[0]), int(actor.Pos[1]), int(actor.Pos[2])}
				data, exists := actors[pos]
				if !exists {
					t.Errorf("missing Source actor at %v", pos)
					continue
				}
				bees, hive := actor.NBT["bees"].([]any)
				if !hive {
					continue
				}
				occupants, _ := data["Occupants"].([]any)
				if len(occupants) != len(bees) {
					t.Fatalf("bees at %v=%d Source=%d", pos, len(occupants), len(bees))
				}
				for i, raw := range bees {
					source := raw.(map[string]any)
					occupant := occupants[i].(map[string]any)
					java := occupant["adamant:java"].(map[string]any)
					for _, key := range [...]string{"ticks_in_hive", "min_ticks_in_hive"} {
						if java[key] != int32(featureNumber(source[key])) {
							t.Errorf("bee[%d] %s=%v Source=%v", i, key, java[key], source[key])
						}
					}
					if occupant["TicksLeftToStay"] != int32(actor.FirstEligibleTicks[i]) {
						t.Errorf("bee[%d] eligible tick=%v Source=%d", i, occupant["TicksLeftToStay"], actor.FirstEligibleTicks[i])
					}
					wantEntity, _ := json.Marshal(source["entity_data"])
					gotEntity, _ := json.Marshal(occupant["SaveData"])
					if string(gotEntity) != string(wantEntity) {
						t.Errorf("bee[%d] entity data=%s Source=%s", i, gotEntity, wantEntity)
					}
				}
			}
			var gotPost [][3]int32
			for section, packedPositions := range result.PostProcessing {
				for _, packed := range packedPositions {
					gotPost = append(gotPost, [3]int32{
						target[0]*16 + int32(packed&15),
						int32(fixture.MinY + section*16 + int((packed>>4)&15)),
						target[1]*16 + int32((packed>>8)&15),
					})
				}
			}
			if len(gotPost) != len(tc.PostProcessing) {
				t.Errorf("post-processing=%v Source=%v", gotPost, tc.PostProcessing)
			} else {
				for i := range gotPost {
					if gotPost[i] != tc.PostProcessing[i] {
						t.Errorf("post-processing[%d]=%v Source=%v", i, gotPost[i], tc.PostProcessing[i])
					}
				}
			}
		})
	}
}
