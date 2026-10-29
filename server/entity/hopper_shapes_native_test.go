package entity

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

type hopperShapeSource struct {
	world.BlockSource
	position cube.Pos
	block    world.Block
}

func (s hopperShapeSource) Block(pos cube.Pos) world.Block {
	if pos == s.position {
		return s.block
	}
	return block.Air{}
}

func TestHopperCollisionShapesMatchExecutedClient(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_hopper_shapes.json")
	if err != nil {
		t.Fatal(err)
	}
	type nativeShape struct {
		Face     cube.Face
		Position cube.Pos
		Boxes    [][6]float64
	}
	var native struct {
		Complete      bool
		Shapes        []nativeShape
		BrewingShapes []nativeShape `json:"brewing_shapes"`
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Shapes) != 126 || len(native.BrewingShapes) != 21 {
		t.Fatal("incomplete original hopper collision capture")
	}
	for name, cases := range map[string][]nativeShape{"hopper": native.Shapes, "brewing_stand": native.BrewingShapes} {
		for _, c := range cases {
			t.Run(fmt.Sprintf("%s/face%d/%v", name, c.Face, c.Position), func(t *testing.T) {
				var b world.Block = block.Hopper{Facing: c.Face}
				if name == "brewing_stand" {
					b = block.BrewingStand{}
				}
				source := hopperShapeSource{position: c.Position, block: b}
				query := cube.Box(-1, -1, -1, 2, 2, 2).Translate(c.Position.Vec3())
				got := nativeBlockBBoxsAround(source, query, cube.Range{-64, 319})
				if got != nil {
					defer blockBBoxPool.Put(got[:0])
				}
				if len(got) != len(c.Boxes) {
					t.Fatalf("shape count=%d, native=%d", len(got), len(c.Boxes))
				}
				for i, b := range c.Boxes {
					want := cube.Box(b[0], b[1], b[2], b[3], b[4], b[5])
					if got[i] != want {
						t.Errorf("shape %d=%v, native=%v", i, got[i], want)
					}
				}
			})
		}
	}
}
