package overworld

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"testing"

	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
)

func TestNamedStateProvidersMatchJava263(t *testing.T) {
	// Captured from the six source providers that do not need world queries,
	// decoded from the pinned client resources by Java's actual provider codec.
	var cases []struct {
		Provider   string
		Xoroshiro  bool
		Seed, Next int64
		Samples    []struct {
			Position   [3]int32
			Name       string
			Properties map[string]any
		}
	}
	data, err := os.ReadFile("testdata/java263_state_providers.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/xoro=%v", tc.Provider, tc.Xoroshiro), func(t *testing.T) {
			random := java26.NewLegacyRandom(tc.Seed)
			if tc.Xoroshiro {
				random = java26.NewXoroshiroRandom(tc.Seed)
			}
			for _, sample := range tc.Samples {
				p := featurePos{sample.Position[0], sample.Position[1], sample.Position[2]}
				state, ok := sampleJavaBlockState(nil, p, "minecraft:"+tc.Provider, &random)
				if !ok {
					t.Fatalf("provider returned no state at %v", p)
				}
				properties := javadata.BlockPropertiesWithDefaults(state.State.Name, state.State.Properties)
				want := world.DecodeStateProperties(sample.Properties)
				if state.State.Name != sample.Name || !maps.Equal(properties, want) {
					t.Errorf("at %v: got %s %v, Java wants %s %v", p, state.State.Name, properties, sample.Name, want)
				}
			}
			if next := random.NextLong(); next != tc.Next {
				t.Errorf("following stream=%d, Java wants %d", next, tc.Next)
			}
		})
	}
}

func TestNamedOptionalTreeProviders(t *testing.T) {
	r := countLayerTestRegion()
	grass, stone := featurePos{1, 5, 1}, featurePos{2, 5, 1}
	r.Set(grass, countLayerNativeState(t, "grass_block"))
	r.Set(stone, countLayerNativeState(t, "stone"))
	dirt := featurePos{3, 5, 1}
	r.Set(dirt, countLayerNativeState(t, "dirt"))
	for _, tc := range []struct {
		provider string
		pos      featurePos
		want     string
	}{
		{"podzol_beneath_tree", grass, "minecraft:podzol"},
		{"podzol_beneath_tree", stone, ""},
		{"soil_beneath_tree", dirt, ""},
		{"soil_beneath_tree", stone, "minecraft:dirt"},
	} {
		random := java26.NewLegacyRandom(123)
		state, ok := sampleOptionalJavaBlockState(r, tc.pos, tc.provider, &random)
		if ok != (tc.want != "") || ok && state.State.Name != tc.want {
			t.Errorf("%s at %v: state=%+v present=%v, want %q", tc.provider, tc.pos, state, ok, tc.want)
		}
		if next := random.NextLong(); next != -5106534569952410475 {
			t.Errorf("deterministic tree provider consumed random stream: %d", next)
		}
	}
}

func TestRandomizedProviderMissingIntegerPropertyDoesNotDraw(t *testing.T) {
	random := java26.NewLegacyRandom(123)
	provider := map[string]any{
		"type": "randomized_int", "source": map[string]any{"id": "minecraft:stone"}, "property": "age",
		"values": map[string]any{"type": "uniform", "min_inclusive": float64(1), "max_inclusive": float64(3)},
	}
	state, ok := sampleJavaBlockState(nil, featurePos{}, provider, &random)
	if !ok || state.State.Name != "minecraft:stone" || state.State.Properties["age"] != nil {
		t.Fatalf("absent property changed state: %+v", state)
	}
	if next := random.NextLong(); next != -5106534569952410475 {
		t.Fatalf("absent integer property consumed a draw: %d", next)
	}
}
