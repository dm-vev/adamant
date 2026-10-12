package nether

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/overworld"
)

// Java WorldOptions disables structure placement in this controlled comparison.
// Source structure references and beard adaptation remain part of real terrain.
type decorationSourceWithoutStructures struct{ *netherFeatureSource }

func (*decorationSourceWithoutStructures) PlaceStructureStep(world.ChunkPos, int, javastructure.ProcessorEnvironment, func(javastructure.Placement)) {
}

func TestNetherPlacedFeaturesMatchJava263(t *testing.T) {
	// Captured by executing mapped Java createReferences/createBiomes/buildTerrain
	// and applyBiomeDecoration for the target's 3x3 origins, z-major. Digests cover
	// all 32768 states including their full properties, in y,x,z order. Tick lists
	// are the target ProtoChunkTicks snapshots (source delay zero, priority NORMAL).
	cases := []struct {
		seed      int64
		pos       world.ChunkPos
		digest    string
		lavaTicks []cube.Pos
	}{
		{seed: 0, pos: world.ChunkPos{0, 0}, digest: "6a778e2b3d8f49f6da647abafbdf845ae77dee7dd61b0a081a495e979d40a586", lavaTicks: []cube.Pos{{14, 60, 9}, {12, 37, 6}, {1, 42, 14}, {15, 67, 11}, {12, 66, 11}, {1, 21, 7}, {11, 37, 1}, {9, 47, 7}, {9, 58, 2}, {7, 75, 11}, {4, 107, 10}}},
		{seed: 0, pos: world.ChunkPos{-35, -9}, digest: "aba0cf05cf71301cbcd8153d3d97e0d9495653c18d1d2df9ea532ba108132046", lavaTicks: []cube.Pos{{-546, 14, -139}, {-560, 110, -130}, {-559, 10, -138}, {-560, 117, -135}}},
		{seed: 0, pos: world.ChunkPos{-1024, 64}, digest: "4f171eb85b0b18c79babccdd8bd5be7f259ebe81655cea4ba878e6303616c738", lavaTicks: []cube.Pos{{-16373, 85, 1024}, {-16369, 53, 1035}, {-16382, 56, 1031}, {-16371, 10, 1024}, {-16369, 39, 1025}}},
		{seed: 1, pos: world.ChunkPos{1, -41}, digest: "b460c4ba676950968b51d4f9dc65e0f59a5dcf314a96df7f55410b82e73ef288", lavaTicks: []cube.Pos{{22, 71, -653}, {23, 70, -641}, {19, 84, -655}, {21, 87, -645}, {29, 34, -647}, {29, 67, -647}, {31, 103, -643}, {22, 36, -649}, {27, 115, -649}, {30, 83, -649}, {22, 85, -651}, {21, 116, -648}}},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%d_%d_%d", tc.seed, tc.pos[0], tc.pos[1]), func(t *testing.T) {
			g := NewNether(tc.seed)
			c := chunk.New(world.DefaultBlockRegistry, world.Nether.Range())
			g.prepareTerrain(tc.pos, c)
			g.fillBiomes(int(tc.pos[0]), int(tc.pos[1]), c)
			g.featureSource.remember(tc.pos, c)
			features := overworld.NewModernFeatures(tc.seed, &decorationSourceWithoutStructures{g.featureSource})
			result := features.Decorate(tc.pos, c, world.GenerationContext{})
			if got := netherSourceStateHash(t, c); got != tc.digest {
				t.Errorf("Java placed-feature state digest=%s, native=%s", tc.digest, got)
			}
			want := make([]chunk.ScheduledTick, len(tc.lavaTicks))
			for i, pos := range tc.lavaTicks {
				want[i] = chunk.ScheduledTick{Pos: pos, Kind: chunk.FluidTick, Type: "minecraft:lava"}
			}
			if !reflect.DeepEqual(result.ScheduledTicks, want) {
				t.Errorf("Java proto-chunk ticks=%v, native=%v", want, result.ScheduledTicks)
			}
		})
	}
}

func netherSourceStateHash(t *testing.T, c *chunk.Chunk) string {
	t.Helper()
	hash := sha256.New()
	states := make(map[world.NativeState]string)
	for y := int16(0); y < 128; y++ {
		for x := uint8(0); x < 16; x++ {
			for z := uint8(0); z < 16; z++ {
				native := world.NativeStateAt(c, x, y, z)
				text, found := states[native]
				if !found {
					state, err := world.JavaState(c.BlockRegistry(), native)
					if err != nil {
						t.Fatal(err)
					}
					var out strings.Builder
					out.WriteString(state.Name)
					if len(state.Properties) != 0 {
						keys := make([]string, 0, len(state.Properties))
						for key := range state.Properties {
							keys = append(keys, key)
						}
						sort.Strings(keys)
						out.WriteByte('[')
						for i, key := range keys {
							if i != 0 {
								out.WriteByte(',')
							}
							fmt.Fprintf(&out, "%s=%v", key, state.Properties[key])
						}
						out.WriteByte(']')
					}
					out.WriteByte(',')
					text = out.String()
					states[native] = text
				}
				_, _ = hash.Write([]byte(text))
			}
		}
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}
