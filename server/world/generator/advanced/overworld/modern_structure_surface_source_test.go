package overworld

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"testing"

	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

type surfaceSourceCase struct {
	Structure string     `json:"structure"`
	Biome     string     `json:"biome"`
	Scenario  string     `json:"scenario"`
	Height    int32      `json:"firstFreeHeight"`
	Chunk     [2]int32   `json:"chunk"`
	Seed      string     `json:"seed"`
	Accepted  bool       `json:"accepted"`
	Pieces    [][6]int32 `json:"pieces"`
}

type surfaceSourceTerrain struct {
	modernTerrainSource
	fixture surfaceSourceCase
}

func (surfaceSourceTerrain) SeaLevel() int32 { return 63 }
func (s surfaceSourceTerrain) BaseHeight(x, z int32, _ bool) int32 {
	width, depth := int32(21), int32(21)
	if s.fixture.Structure == "jungle_pyramid" {
		width, depth = 12, 15
	}
	minX, minZ := s.fixture.Chunk[0]*16, s.fixture.Chunk[1]*16
	if s.fixture.Scenario == "low_far_corner" && x == minX+width && z == minZ+depth {
		return 63
	}
	if s.fixture.Scenario == "low_interior" && x == minX+1 && z == minZ+1 {
		return 12
	}
	return s.fixture.Height
}
func (s surfaceSourceTerrain) JavaNoiseBiomeAt(_, y, _ int32) string {
	valid := true
	switch s.fixture.Scenario {
	case "invalid_column":
		valid = false
	case "invalid_stub":
		valid = y >= 20
	case "lower_boundary":
		valid = y == -17
	case "upper_boundary":
		valid = y == 79
	case "stub_quart_boundary":
		valid = y == 16
	}
	if valid {
		return "minecraft:" + s.fixture.Biome
	}
	return "minecraft:plains"
}

// The fixtures execute Structure.findValidGenerationPoint and the source's raw
// builders. The acceptance boundaries are independent of postProcess HPos.
func TestSurfaceStructureGenerationMatchesJavaSource(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_surface_acceptance.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []surfaceSourceCase
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fmt.Sprintf("%s/%s/%d", fixture.Structure, fixture.Scenario, fixture.Height), func(t *testing.T) {
			seed, err := strconv.ParseInt(fixture.Seed, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			structures := modernStructures{seed: seed, source: surfaceSourceTerrain{fixture: fixture}}
			start, accepted, err := structures.buildStructure("minecraft:"+fixture.Structure, fixture.Chunk[0], fixture.Chunk[1])
			if err != nil {
				t.Fatal(err)
			}
			if accepted != fixture.Accepted {
				t.Fatalf("accepted=%t, Java=%t", accepted, fixture.Accepted)
			}
			if !accepted {
				return
			}
			boxes := make([][6]int32, len(start.pieces))
			for i, piece := range start.pieces {
				boxes[i] = surfaceSourceBox(piece.Bounds())
			}
			if !reflect.DeepEqual(boxes, fixture.Pieces) {
				t.Fatalf("constructor boxes=%v, Java=%v", boxes, fixture.Pieces)
			}
		})
	}
}

func surfaceSourceBox(box javastructure.Box) [6]int32 {
	return [6]int32{box.Min[0], box.Min[1], box.Min[2], box.Max[0], box.Max[1], box.Max[2]}
}
