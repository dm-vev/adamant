package overworld

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"os"
	"slices"
	"testing"
)

func TestVoidPlatformMatchesJava263(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_void_platform.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Origin [3]int32
		Placed bool
		Next   int64
		Count  int
		Digest string
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(fmt.Sprint(tc.Origin), func(t *testing.T) {
			origin := featurePos{tc.Origin[0], tc.Origin[1], tc.Origin[2]}
			target := world.ChunkPos{origin.x >> 4, origin.z >> 4}
			region := newFeatureRegion(&ModernFeatures{}, chunk.New(world.DefaultBlockRegistry, world.Overworld.Range()), target, &world.GenerationResult{})
			region.SetOrigin(target[0], target[1])
			random := java26.NewLegacyRandom(123)
			if got := region.f.placeConfiguredData(region, "void_start_platform", nil, origin, &random); got != tc.Placed {
				t.Errorf("placed=%v, Java wants %v", got, tc.Placed)
			}
			if next := random.NextLong(); next != tc.Next {
				t.Errorf("following stream=%d, Java wants %d", next, tc.Next)
			}
			positions := make([]featurePos, 0, region.overlay.len())
			for p := range region.overlayByPos() {
				positions = append(positions, p)
			}
			slices.SortFunc(positions, func(a, b featurePos) int {
				if a.z != b.z {
					return int(a.z - b.z)
				}
				return int(a.x - b.x)
			})
			digest := sha256.New()
			for _, p := range positions {
				name, _ := region.State(p)
				fmt.Fprintf(digest, "%d,%d,%d=%s\n", p.x, p.y, p.z, name)
			}
			if got := hex.EncodeToString(digest.Sum(nil)); got != tc.Digest || len(positions) != tc.Count {
				t.Errorf("writes=%d digest=%s; source wants %d %s", len(positions), got, tc.Count, tc.Digest)
			}
		})
	}
}

func TestNoOpFeaturePreservesWorldAndRandom(t *testing.T) {
	region := countLayerTestRegion()
	random := java26.NewLegacyRandom(123)
	wantRandom := java26.NewLegacyRandom(123)
	if !region.f.placeConfiguredData(region, "no_op", nil, featurePos{8, 10, 8}, &random) {
		t.Fatal("source no_op must succeed")
	}
	if region.overlay.len() != 0 {
		t.Fatal("no_op changed blocks")
	}
	if got, want := random.NextLong(), wantRandom.NextLong(); got != want {
		t.Fatalf("no_op consumed randomness: got %d want %d", got, want)
	}
}
