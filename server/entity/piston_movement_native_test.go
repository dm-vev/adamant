package entity

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl32"
)

func TestPistonSweepMatchesExecutedClient(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_piston_sweep.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Body, Shape [6]float32
			Moves       []mgl32.Vec3
			Obstacles   [][6]float32
			Limits      mgl32.Vec3
			Move        mgl32.Vec3
			Penetration float32
			Clips       []struct {
				Penetration            float32
				Clipped, Depenetrating mgl32.Vec3
				Axis                   int
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 2072 {
		t.Fatal("incomplete original-client sweep fixture")
	}
	box := func(b [6]float32) cube.BBox32 { return cube.Box32(b[0], b[1], b[2], b[3], b[4], b[5]) }
	for i, c := range native.Scenarios {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			var obstacles []cube.BBox32
			for n, b := range c.Obstacles {
				obstacles = append(obstacles, box(b))
				if len(c.Moves) != 0 {
					if len(c.Clips) != len(c.Obstacles) {
						t.Fatal("missing original clipping results")
					}
					got := pistonClipCollide(box(b), box(c.Body), c.Moves[0])
					w := c.Clips[n]
					want := pistonClipResult{penetration: w.Penetration, clipped: w.Clipped, depenetrating: w.Depenetrating, axis: w.Axis}
					if got != want {
						t.Errorf("clip=%+v, original=%+v", got, want)
					}
				}
			}
			got := pistonSweep(c.Moves, box(c.Body), obstacles, c.Limits)
			if got.move != c.Move || got.penetration != c.Penetration || got.shape != box(c.Shape) {
				t.Errorf("sweep=%+v, original move=%v penetration=%g shape=%v", got, c.Move, c.Penetration, c.Shape)
			}
		})
	}
}

func TestDepenetrationLimitsMatchExecutedClient(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_piston_sweep.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete bool
		Policies []struct {
			Bits            uint8
			OneWayBlocks    bool `json:"one_way_blocks"`
			Minimum, Limits mgl32.Vec3
			Override        *mgl32.Vec3
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Policies) != 576 {
		t.Fatal("incomplete original-client depenetration policy fixture")
	}
	for n, c := range native.Policies {
		if got := nativeDepenetrationLimits(c.Bits, c.OneWayBlocks, c.Minimum, c.Override); got != c.Limits {
			t.Errorf("case %d: limits=%v, native=%v", n, got, c.Limits)
		}
	}
}
