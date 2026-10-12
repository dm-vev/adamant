package overworld

import (
	"encoding/json"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

type smallFeatureSourceTick struct {
	Pos    [3]int32 `json:"pos"`
	State  string   `json:"state"`
	Object string   `json:"object"`
}

type smallFeatureSourceCase struct {
	blockColumnSourceCase
	FeatureType    string                   `json:"featureType"`
	EncodedConfig  map[string]any           `json:"encodedConfig"`
	MinY           int                      `json:"minY"`
	MaxY           int                      `json:"maxY"`
	RegionSeed     int64                    `json:"regionSeed"`
	RegionNextLong string                   `json:"regionNextLong"`
	PostProcessing [][3]int32               `json:"postProcessing"`
	ScheduledTicks []smallFeatureSourceTick `json:"scheduledTicks"`
}

type smallFeatureSourceTerrain struct{ replaceBlobSourceTerrain }

func (smallFeatureSourceTerrain) JavaFeatureBiomeNames() []string { return []string{"plains"} }
func TestSmallFeaturesMatchJava263Source(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_small_feature_source.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []smallFeatureSourceCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	features := NewModernFeatures(0, smallFeatureSourceTerrain{blockColumnSourceTerrain(t, blockColumnSourceCase{}, -64, 319)})
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("Source-valid feature panicked: %v", p)
				}
			}()
			terrain := smallFeatureSourceTerrain{blockColumnSourceTerrain(t, tc.blockColumnSourceCase, tc.MinY, tc.MaxY)}
			origin := featurePos{tc.Origin[0], tc.Origin[1], tc.Origin[2]}
			target := world.ChunkPos{origin.x >> 4, origin.z >> 4}
			c := chunk.New(world.DefaultBlockRegistry, cube.Range{tc.MinY, tc.MaxY})
			for pos, state := range terrain.initial {
				if featureInsideTarget(target, pos) && pos.y >= int32(tc.MinY) && pos.y <= int32(tc.MaxY) {
					world.SetNativeState(c, uint8(pos.x&15), int16(pos.y), uint8(pos.z&15), state)
				}
			}
			features.source, features.seed = terrain, tc.Seed
			result := &world.GenerationResult{}
			region := newFeatureRegion(features, c, target, result)
			region.SetOrigin(target[0], target[1])
			if tc.RejectWrites {
				region.SetOrigin(target[0]+3, target[1]+3)
			}
			regionRandom := java26.NewLegacyRandom(tc.RegionSeed)
			region.javaRandoms[[2]int32{region.writeOriginX, region.writeOriginZ}] = &regionRandom
			random := java26.NewLegacyRandom(tc.Seed)
			if tc.Algorithm == "xoroshiro" {
				random = java26.NewXoroshiroRandom(tc.Seed)
			}
			feature := make(map[string]any, len(tc.EncodedConfig)+1)
			for key, value := range tc.EncodedConfig {
				feature[key] = value
			}
			feature["type"] = "minecraft:" + tc.FeatureType
			if got := region.PlaceValue(feature, origin, &random); got != tc.Placed {
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
			if got := strconv.FormatInt(random.NextLong(), 10); got != tc.NextLong {
				t.Errorf("nextLong=%s Source=%s", got, tc.NextLong)
			}
			if tc.RegionNextLong != "" {
				if got := strconv.FormatInt(regionRandom.NextLong(), 10); got != tc.RegionNextLong {
					t.Errorf("region nextLong=%s Source=%s", got, tc.RegionNextLong)
				}
			}
			var wantTicks []chunk.ScheduledTick
			for _, tick := range tc.ScheduledTicks {
				p := featurePos{tick.Pos[0], tick.Pos[1], tick.Pos[2]}
				if !featureInsideTarget(target, p) {
					continue
				}
				name := tick.State
				if name == "" {
					name = strings.TrimSuffix(strings.TrimPrefix(tick.Object, "Block{"), "}")
				}
				wantTicks = append(wantTicks, chunk.ScheduledTick{Pos: cube.Pos{int(p.x), int(p.y), int(p.z)}, Kind: chunk.BlockTick, Type: name})
			}
			if !reflect.DeepEqual(result.ScheduledTicks, wantTicks) {
				t.Errorf("scheduled ticks=%v Source=%v", result.ScheduledTicks, wantTicks)
			}
			var gotPost [][3]int32
			for section, positions := range result.PostProcessing {
				for _, packed := range positions {
					gotPost = append(gotPost, [3]int32{target[0]*16 + int32(packed&15), int32(tc.MinY + section*16 + int((packed>>4)&15)), target[1]*16 + int32((packed>>8)&15)})
				}
			}
			if !reflect.DeepEqual(gotPost, tc.PostProcessing) && (len(gotPost) != 0 || len(tc.PostProcessing) != 0) {
				t.Errorf("post-processing=%v Source=%v", gotPost, tc.PostProcessing)
			}
		})
	}
}
