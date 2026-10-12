package overworld

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"
)

func TestOceanRuinGraphMatchesJava263(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_ocean_ruin_source.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Graphs []struct {
			Seed     string `json:"seed"`
			Warm     bool   `json:"warm"`
			Cluster  bool   `json:"cluster"`
			Rotation int    `json:"rotation"`
			NextLong string `json:"next_long"`
			Pieces   []struct {
				Template  string   `json:"template"`
				Rotation  int      `json:"rotation"`
				Position  [3]int32 `json:"position"`
				Box       [6]int32 `json:"bbox"`
				Integrity string   `json:"integrity"`
				Large     string   `json:"large"`
				BiomeType string   `json:"biomeType"`
			} `json:"pieces"`
		} `json:"graphs"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, source := range fixture.Graphs {
		seed, err := strconv.ParseInt(source.Seed, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(source.Seed+"/"+strconv.Itoa(source.Rotation)+"/"+strconv.FormatBool(source.Warm)+"/"+strconv.FormatBool(source.Cluster), func(t *testing.T) {
			random := largeFeatureRandom(seed, 3, 7)
			clusterProbability := float64(0)
			if source.Cluster {
				clusterProbability = 1
			}
			start, valid, err := buildModernOceanRuin(modernStructureBuildInput{
				seed: seed, name: "minecraft:ocean_ruin", chunkX: 3, chunkZ: 7,
				config: map[string]any{"biome_temp": map[bool]string{true: "warm", false: "cold"}[source.Warm], "large_probability": float64(1), "cluster_probability": clusterProbability},
				source: configuredStartSource{biome: "minecraft:warm_ocean", height: 64}, random: &random,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !valid {
				t.Fatal("Source-valid ocean ruin graph was rejected")
			}
			if len(start.pieces) != len(source.Pieces) {
				t.Fatalf("piece count=%d, Source=%d", len(start.pieces), len(source.Pieces))
			}
			for i, want := range source.Pieces {
				got, ok := start.pieces[i].(modernOceanRuinPiece)
				if !ok {
					t.Fatalf("piece %d has type %T", i, start.pieces[i])
				}
				box := got.Bounds()
				bounds := [6]int32{box.Min[0], box.Min[1], box.Min[2], box.Max[0], box.Max[1], box.Max[2]}
				integrity, wantErr := strconv.ParseFloat(want.Integrity, 32)
				if wantErr != nil {
					t.Fatal(wantErr)
				}
				if got.templateName != want.Template || got.origin != want.Position || int(got.rotation) != want.Rotation || bounds != want.Box || float64(got.integrity) != integrity || got.large != (want.Large == "true") || got.warm != (want.BiomeType == "WARM") {
					t.Errorf("piece %d=%s at %v rot%d integrity %.2f large=%t warm=%t box=%v; Source=%+v", i, got.templateName, got.origin, got.rotation, got.integrity, got.large, got.warm, bounds, want)
				}
			}
			wantNext, err := strconv.ParseInt(source.NextLong, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			if got := random.NextLong(); got != wantNext {
				t.Errorf("graph random continuation=%d, Source=%d", got, wantNext)
			}
		})
	}
}
