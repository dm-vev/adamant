package overworld

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"testing"
)

type seaSourceCase struct {
	Structure     string     `json:"structure"`
	Biome         string     `json:"biome"`
	Scenario      string     `json:"scenario"`
	SurfaceHeight int32      `json:"surfaceFirstFree"`
	FloorHeight   int32      `json:"floorFirstFree"`
	Chunk         [2]int32   `json:"chunk"`
	Seed          string     `json:"seed"`
	Accepted      bool       `json:"accepted"`
	Pieces        [][6]int32 `json:"pieces"`
}

type seaSourceTerrain struct {
	modernTerrainSource
	fixture seaSourceCase
}

func (seaSourceTerrain) SeaLevel() int32 { return 63 }
func (s seaSourceTerrain) BaseHeight(_, _ int32, oceanFloor bool) int32 {
	if oceanFloor {
		return s.fixture.FloorHeight
	}
	return s.fixture.SurfaceHeight
}
func (s seaSourceTerrain) NoiseBiomeAt(_, y, _ int32) string {
	valid := true
	switch s.fixture.Scenario {
	case "invalid_column":
		valid = false
	case "invalid_stub":
		valid = y >= 20
	case "floor_only":
		valid = y == 7
	case "surface_only":
		valid = y == 15
	case "first_free_not_occupied":
		valid = y == 8
	case "upper_boundary":
		valid = y == 79
	case "lower_boundary":
		valid = y == -17
	}
	if valid {
		return "minecraft:" + s.fixture.Biome
	}
	return "minecraft:plains"
}

// Fixtures execute the actual Source findValidGenerationPoint and raw builders.
// Ocean-floor and surface stubs must not sample the first-free height instead.
func TestSeaStructureGenerationMatchesSource(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_sea_acceptance.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []seaSourceCase
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fmt.Sprintf("%s/%s/%s", fixture.Structure, fixture.Scenario, fixture.Seed), func(t *testing.T) {
			seed, err := strconv.ParseInt(fixture.Seed, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			structures := modernStructures{seed: seed, source: seaSourceTerrain{fixture: fixture}}
			start, accepted, err := structures.buildStructure("minecraft:"+fixture.Structure, fixture.Chunk[0], fixture.Chunk[1])
			if err != nil {
				t.Fatal(err)
			}
			if accepted != fixture.Accepted {
				t.Fatalf("accepted=%t, Source=%t", accepted, fixture.Accepted)
			}
			if !accepted {
				return
			}
			boxes := make([][6]int32, len(start.pieces))
			for i, piece := range start.pieces {
				box := piece.Bounds()
				boxes[i] = [6]int32{box.Min[0], box.Min[1], box.Min[2], box.Max[0], box.Max[1], box.Max[2]}
			}
			if !reflect.DeepEqual(boxes, fixture.Pieces) {
				t.Fatalf("raw pieces=%v, Source=%v", boxes, fixture.Pieces)
			}
		})
	}
}
