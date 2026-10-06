package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCompareBiomePrecipitation(t *testing.T) {
	reference := snapshot{Biomes: []biome{{Identifier: "minecraft:the_end", BiomeID: -1, Downfall: 0.5, Rain: false}}}
	candidate := snapshot{Biomes: []biome{{Identifier: "minecraft:the_end", BiomeID: -1, Downfall: 0.5, Rain: true}}}
	got := compare(reference, candidate)
	if got.Equal || len(got.Differences.Biomes) != 1 {
		t.Fatalf("precipitation mismatch was not reported: %+v", got)
	}
	difference := got.Differences.Biomes[0]
	if difference.Identifier != "minecraft:the_end" || len(difference.Changed) != 1 || difference.Changed[0] != "rain" {
		t.Fatalf("wrong precipitation difference: %+v", difference)
	}
}

func TestCompareBiomeAdditionAndRemoval(t *testing.T) {
	got := compare(snapshot{Biomes: []biome{{Identifier: "removed"}}}, snapshot{Biomes: []biome{{Identifier: "added"}}})
	if got.Equal || len(got.Differences.Biomes) != 2 {
		t.Fatalf("biome additions/removals were not reported: %+v", got)
	}
	added, removed := got.Differences.Biomes[0], got.Differences.Biomes[1]
	if added.Identifier != "added" || added.Reference != nil || added.Candidate == nil ||
		removed.Identifier != "removed" || removed.Reference == nil || removed.Candidate != nil {
		t.Fatalf("wrong addition/removal direction: %+v", got.Differences.Biomes)
	}
}

func TestLoadRejectsAmbiguousBiomeMappings(t *testing.T) {
	cases := map[string][]biome{
		"missing_registry":    nil,
		"duplicate_name":      {{Identifier: "same", Index: 0}, {Identifier: "same", Index: 1}},
		"duplicate_index":     {{Identifier: "a", Index: 0}, {Identifier: "b", Index: 0}},
		"negative_index":      {{Identifier: "a", Index: -1}},
		"index_past_registry": {{Identifier: "a", Index: 1}},
	}
	for name, biomes := range cases {
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal(snapshot{ProtocolVersion: 2193, Items: []item{}, Biomes: biomes})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "snapshot.json")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := load(path); err == nil {
				t.Fatal("ambiguous biome mapping was accepted")
			}
		})
	}
}
