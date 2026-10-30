package entity

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
)

func TestItemEscapeMatchesExecutedClient(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_piston_item_escape.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			PushesOutItems          bool   `json:"pushes_out_items"`
			SolidNeighbours         uint8  `json:"solid_neighbours"`
			BlockProperties         uint64 `json:"block_properties"`
			Point, Velocity, Motion mgl32.Vec3
			Body                    [6]float32
			SingleShape             [6]float32 `json:"single_shape"`
			Shapes                  [][6]float32
			HeightOffset            float32         `json:"height_offset"`
			DepenetrationBits       map[string]bool `json:"depenetration_bits"`
			StatusFlags             map[string]bool `json:"status_flags"`
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 1600 {
		t.Fatal("incomplete original-client item escape fixture")
	}
	box := func(b [6]float32) cube.BBox32 { return cube.Box32(b[0], b[1], b[2], b[3], b[4], b[5]) }
	for n, c := range native.Scenarios {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			shapes := make([]cube.BBox32, len(c.Shapes))
			for i, shape := range c.Shapes {
				shapes[i] = box(shape)
			}
			got := itemEscape(c.Point, c.Velocity, box(c.Body), box(c.SingleShape), shapes, c.PushesOutItems, c.SolidNeighbours, c.BlockProperties, c.HeightOffset)
			collision, hasCollision := c.StatusFlags["48"]
			stuck, hasStuck := c.DepenetrationBits["3"]
			if !hasCollision || !hasStuck {
				t.Fatal("native flag transition missing")
			}
			if got.velocity != c.Motion || got.stuck != stuck || got.hasCollision != collision {
				t.Errorf("escape=%+v, native motion=%v stuck=%t collision=%t", got, c.Motion, stuck, collision)
			}
		})
	}
}

func TestItemEscapeFlagsMatchBDS(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_entity_physics.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete   bool
		BlockTypes map[string]struct {
			Properties     uint64
			PushesOutItems bool `json:"pushes_out_items"`
		} `json:"block_types"`
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.BlockTypes) != 1477 {
		t.Fatal("incomplete original-BDS block type flags")
	}
	for name, flags := range native.BlockTypes {
		pushes, properties := nativeItemEscapeFlags(name)
		if pushes != flags.PushesOutItems || properties&1 != flags.Properties&1 {
			t.Errorf("%s: flags=(%t,%d), original=(%t,%d)", name, pushes, properties, flags.PushesOutItems, flags.Properties&1)
		}
	}
}

func TestItemSpawnInsideBlockMatchesBDS(t *testing.T) { testItemNativePhysics(t, 1) }

func testItemNativePhysics(t *testing.T, index int) {
	t.Helper()
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_entity_physics.json")
	if err != nil {
		t.Fatal(err)
	}
	type point struct{ X, Y, Z float64 }
	vec := func(p point) mgl64.Vec3 { return mgl64.Vec3{p.X, p.Y, p.Z} }
	var native struct {
		Complete    bool
		Checkpoints []struct {
			Entities []struct{ Position, Velocity point }
			Native   []struct {
				Position, Velocity mgl64.Vec3
				ItemAge            int `json:"item_age"`
				Depenetration      struct{ Bits uint8 }
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Checkpoints) != 4 {
		t.Fatal("incomplete original-BDS item physics trace")
	}
	for _, frame := range native.Checkpoints {
		if len(frame.Entities) != 3 || len(frame.Native) != 3 {
			t.Fatal("missing controlled native entity")
		}
	}
	initial := native.Checkpoints[0].Entities[index]
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	if err := w.Do(func(tx *world.Tx) {
		cell := cube.PosFromVec3(vec(initial.Position))
		for x := -3; x <= 3; x++ {
			for z := -3; z <= 3; z++ {
				tx.SetBlock(cube.Pos{cell[0] + x, 64, cell[2] + z}, block.Stone{}, nil)
			}
		}
		if index == 1 {
			tx.SetBlock(cell, block.Stone{}, nil)
		}
		e := tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: vec(initial.Position), Velocity: vec(initial.Velocity)}, item.NewStack(item.Diamond{}, 1))).(*Ent)
		for n, frame := range native.Checkpoints {
			if n > 0 {
				e.Tick(tx, int64(n-1))
			}
			want := frame.Entities[index]
			if !e.Position().ApproxEqualThreshold(vec(want.Position), 2e-5) || !e.Velocity().ApproxEqualThreshold(vec(want.Velocity), 2e-6) {
				t.Errorf("frame %d: position=%v velocity=%v, native=%v %v", n, e.Position(), e.Velocity(), vec(want.Position), vec(want.Velocity))
			}
			live := frame.Native[index]
			// Heap data is a separate phase. Only compare it where the live
			// native position and age agree with the Script API frame.
			liveFeet := live.Position.Sub(mgl64.Vec3{0, .125, 0})
			if liveFeet.ApproxEqualThreshold(vec(want.Position), 2e-5) && live.ItemAge == n {
				if got := e.Behaviour().(*ItemBehaviour).passive.mc.depenetrationBits; got != live.Depenetration.Bits {
					t.Errorf("frame %d: penetration bits=%d, native=%d", n, got, live.Depenetration.Bits)
				}
			}
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}
