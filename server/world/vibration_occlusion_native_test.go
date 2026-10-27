package world

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

func TestVibrationOcclusionMatchesExecutedClient(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_vibration_occlusion.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Origin, Listener mgl64.Vec3
			Occluders        []cube.Pos
			Occluded         bool
		}
		Paths []struct {
			Start, End mgl64.Vec3
			Cells      []cube.Pos
			Hit        bool
		}
		UnitIntersections []struct {
			Pos        cube.Pos
			Start, End mgl64.Vec3
			Hit        bool
		} `json:"unit_intersections"`
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 208 || len(native.Paths) != 133 || len(native.UnitIntersections) != 202 {
		t.Fatal("incomplete native clip/occlusion capture")
	}
	w := Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	wool, ok := w.conf.Blocks.BlockByName("minecraft:white_wool", nil)
	if !ok {
		t.Fatal("missing native wool")
	}
	for i, c := range native.Scenarios {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			<-w.Exec(func(tx *Tx) {
				for _, p := range c.Occluders {
					tx.SetBlock(p, wool, nil)
				}
				if got := vibrationOccluded(tx, c.Origin, c.Listener); got != c.Occluded {
					t.Errorf("%v -> %v wool %v: occluded=%t, native=%t", c.Origin, c.Listener, c.Occluders, got, c.Occluded)
				}
				for _, p := range c.Occluders {
					tx.SetBlock(p, nil, nil)
				}
			})
		})
	}
	for i, c := range native.Paths {
		t.Run(fmt.Sprintf("path%d", i), func(t *testing.T) {
			var visited []cube.Pos
			hit := walkVibrationRay(c.Start, c.End, func(p cube.Pos) bool {
				visited = append(visited, p)
				return false
			})
			if hit != c.Hit || !slices.Equal(visited, c.Cells) {
				t.Errorf("%v -> %v visited=%v, native=%v", c.Start, c.End, visited, c.Cells)
			}
		})
	}
	for i, c := range native.UnitIntersections {
		if hit := vibrationRayHitsCube(c.Pos, c.Start, c.End); hit != c.Hit {
			t.Errorf("unit intersection %d at %v: %v -> %v hit=%t, native=%t", i, c.Pos, c.Start, c.End, hit, c.Hit)
		}
	}
}
