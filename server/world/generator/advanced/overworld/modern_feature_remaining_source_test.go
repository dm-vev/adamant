package overworld

import (
	"cmp"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

type remainingFeatureSourceTick struct {
	smallFeatureSourceTick
	Kind string `json:"kind"`
}

type remainingFeatureSourceCase struct {
	smallFeatureSourceCase
	ScheduledTicks []remainingFeatureSourceTick `json:"scheduledTicks"`
}

type remainingFeatureSourceTerrain struct{ smallFeatureSourceTerrain }

func (remainingFeatureSourceTerrain) UseLegacyRandomSource() bool { return false }
func (remainingFeatureSourceTerrain) HasSkyLight() bool           { return true }
func (remainingFeatureSourceTerrain) GenerationDepth() int32      { return 384 }
func (remainingFeatureSourceTerrain) FluidTickDelay(name string) int32 {
	if name == "minecraft:water" {
		return 5
	}
	if name == "minecraft:lava" {
		return 30
	}
	panic("unexpected Source fluid delay query: " + name)
}

func TestRemainingFeaturesMatchSource(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_remaining_feature_source.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []remainingFeatureSourceCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	features := NewModernFeatures(0, remainingFeatureSourceTerrain{smallFeatureSourceTerrain{blockColumnSourceTerrain(t, blockColumnSourceCase{}, -64, 319)}})
	for _, tc := range fixture.Cases {
		t.Run(tc.FeatureType+"/"+tc.Name, func(t *testing.T) {
			terrain := remainingFeatureSourceTerrain{smallFeatureSourceTerrain{blockColumnSourceTerrain(t, tc.blockColumnSourceCase, tc.MinY, tc.MaxY)}}
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
			regionRandom := vanilla.NewLegacyRandom(tc.RegionSeed)
			if tc.FeatureType == "tree" {
				features.seed = tc.RegionSeed
				regionRandom = *region.RegionRandom()
			}
			if tc.RejectWrites {
				region.SetOrigin(target[0]+3, target[1]+3)
			}
			region.regionRandoms[[2]int32{region.writeOriginX, region.writeOriginZ}] = &regionRandom
			random := vanilla.NewLegacyRandom(tc.Seed)
			if tc.Algorithm == "xoroshiro" {
				random = vanilla.NewXoroshiroRandom(tc.Seed)
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
				want[featurePos{row.Pos[0], row.Pos[1], row.Pos[2]}] = replaceBlobSourceNative(t, row).CanonicalID(c.BlockRegistry())
			}
			for pos, state := range region.overlayByPos() {
				got := state.CanonicalID(c.BlockRegistry())
				if got == terrain.BaseState(pos.x, pos.y, pos.z).CanonicalID(c.BlockRegistry()) {
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
				kind := chunk.BlockTick
				if tick.Kind == "fluid" {
					kind = chunk.FluidTick
				}
				wantTicks = append(wantTicks, chunk.ScheduledTick{Pos: cube.Pos{int(p.x), int(p.y), int(p.z)}, Kind: kind, Type: name})
			}
			canonicalTicks := func(ticks []chunk.ScheduledTick) {
				slices.SortFunc(ticks, func(a, b chunk.ScheduledTick) int {
					if n := cmp.Compare(a.Kind, b.Kind); n != 0 {
						return n
					}
					for axis := range 3 {
						if n := cmp.Compare(a.Pos[axis], b.Pos[axis]); n != 0 {
							return n
						}
					}
					return cmp.Compare(a.Type, b.Type)
				})
			}
			canonicalTicks(result.ScheduledTicks)
			canonicalTicks(wantTicks)
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
