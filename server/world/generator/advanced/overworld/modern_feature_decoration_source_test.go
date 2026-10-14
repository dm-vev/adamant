package overworld

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// These source chunks execute NoiseBasedChunkGenerator.buildTerrain followed
// by all nine applyBiomeDecoration origins in one WorldGenRegion, the
// composition DecorateInOneRegion runs. Structures are disabled on both
// sides; noise, surfaces, carvers and every available placed feature remain on.
func TestOverworldDecorationMatchesJava263(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	data, err := os.ReadFile("testdata/java263_decoration.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Seed           int64                 `json:"seed"`
		Chunk          world.ChunkPos        `json:"chunk"`
		SHA256         string                `json:"sha256"`
		PostProcessing []int                 `json:"postProcessing"`
		ScheduledTicks []chunk.ScheduledTick `json:"scheduledTicks"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fmt.Sprintf("seed%d/%d,%d", fixture.Seed, fixture.Chunk[0], fixture.Chunk[1]), func(t *testing.T) {
			g := NewOverworld(fixture.Seed)
			g.modernStructures.sets = nil
			c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
			base := g.terrainColumn(fixture.Chunk, false)
			*c = *base.c.Clone()
			result := g.modernFeatures.DecorateInOneRegion(fixture.Chunk, c, world.GenerationContext{})
			decorationMarkers := result.PostProcessing
			result.PostProcessing = nil
			appendPostProcessing(&result, base.postProcessing)
			appendPostProcessing(&result, decorationMarkers)
			for _, handle := range result.Entities {
				t.Cleanup(func() {
					if err := handle.Close(); err != nil {
						t.Error(err)
					}
				})
			}
			if hash := overworldSourceStateHash(t, c); hash != fixture.SHA256 {
				t.Errorf("complete block-state hash=%s, source=%s", hash, fixture.SHA256)
			}
			var markers []int
			for section, values := range result.PostProcessing {
				for _, local := range values {
					markers = append(markers, section*4096+int(local))
				}
			}
			if !slices.Equal(markers, fixture.PostProcessing) {
				t.Errorf("ordered post-processing markers=%v, source=%v", markers, fixture.PostProcessing)
			}
			if !slices.Equal(result.ScheduledTicks, fixture.ScheduledTicks) {
				t.Errorf("ordered scheduled ticks=%v, source=%v", result.ScheduledTicks, fixture.ScheduledTicks)
			}
		})
	}
}

// Canonical Java states include the independent native fluid layer. Property
// keys are sorted, and complete states are hashed in source Y/X/Z order.
func overworldSourceStateHash(t *testing.T, c *chunk.Chunk) string {
	t.Helper()
	hash := sha256.New()
	cache := make(map[world.NativeState][]byte)
	for y := int16(c.Range().Min()); y <= int16(c.Range().Max()); y++ {
		for x := range uint8(16) {
			for z := range uint8(16) {
				native := world.NativeStateAt(c, x, y, z)
				encoded, ok := cache[native]
				if !ok {
					state, err := world.JavaState(c.BlockRegistry(), native)
					if err != nil {
						t.Fatal(err)
					}
					var text strings.Builder
					text.WriteString(state.Name)
					if len(state.Properties) != 0 {
						keys := make([]string, 0, len(state.Properties))
						for key := range state.Properties {
							keys = append(keys, key)
						}
						slices.Sort(keys)
						text.WriteByte('[')
						for i, key := range keys {
							if i != 0 {
								text.WriteByte(',')
							}
							fmt.Fprintf(&text, "%s=%v", key, state.Properties[key])
						}
						text.WriteByte(']')
					}
					text.WriteByte('\n')
					encoded = []byte(text.String())
					cache[native] = encoded
				}
				hash.Write(encoded)
			}
		}
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}
