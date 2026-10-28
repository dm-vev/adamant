package block

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestPistonContactShapesMatchExecutedClient(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_piston_contact_shapes.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Face              cube.Face
			Expanding, Parent bool
			LastProgress      float32 `json:"last_progress"`
			Progress          float32
			Position          cube.Pos
			Shape             [6]float64
			Cells             []cube.Pos
			Boxes             [][6]float64
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 1152 {
		t.Fatal("incomplete executed-client contact-shape fixture")
	}
	for i, c := range native.Scenarios {
		t.Run(fmt.Sprintf("%d/face%d/expanding%t/parent%t/%g-%g", i, c.Face, c.Expanding, c.Parent, c.LastProgress, c.Progress), func(t *testing.T) {
			if len(c.Cells) != 2 || len(c.Boxes) != 2 {
				t.Fatal("missing original shape queries")
			}
			var cells []cube.Pos
			moving := pistonShapeNativeBlock{bounds: cube.Box(c.Shape[0], c.Shape[1], c.Shape[2], c.Shape[3], c.Shape[4], c.Shape[5]), cells: &cells}
			var piston *Piston
			if c.Parent {
				piston = &Piston{Facing: c.Face, LastProgress: c.LastProgress, Progress: c.Progress}
			}
			source := pistonShapeNativeSource{piston: piston}
			mb := MovingBlock{Moving: moving, PistonPos: cube.Pos{1, 65, 1}, Expanding: c.Expanding}
			boxes := movingBlockContactBoxes(mb, c.Position, piston, source)
			if !slices.Equal(cells, c.Cells) {
				t.Errorf("queried cells=%v, original=%v", cells, c.Cells)
			}
			if len(boxes) != 1 {
				t.Fatalf("contact bounds=%v", boxes)
			}
			lo := mgl64.Vec3{c.Boxes[0][0], c.Boxes[0][1], c.Boxes[0][2]}
			hi := mgl64.Vec3{c.Boxes[0][3], c.Boxes[0][4], c.Boxes[0][5]}
			for axis := range 3 {
				lo[axis] = min(lo[axis], c.Boxes[1][axis])
				hi[axis] = max(hi[axis], c.Boxes[1][axis+3])
			}
			if boxes[0].Min() != lo || boxes[0].Max() != hi {
				t.Errorf("contact bounds=%v..%v, original=%v..%v", boxes[0].Min(), boxes[0].Max(), lo, hi)
			}
			// Ordinary entity physics must use the same native moving shape.
			cells = nil
			physical := mb.Model().BBox(c.Position, source)
			if len(physical) != 1 || physical[0].Translate(c.Position.Vec3()) != boxes[0] || !slices.Equal(cells, c.Cells) {
				t.Error("moving-block physics differs from native contact bounds")
			}
		})
	}
}

type pistonShapeNativeBlock struct {
	Stone
	bounds cube.BBox
	cells  *[]cube.Pos
}

func (b pistonShapeNativeBlock) Model() world.BlockModel { return pistonShapeNativeModel{block: b} }

type pistonShapeNativeModel struct {
	model.Solid
	block pistonShapeNativeBlock
}

func (m pistonShapeNativeModel) BBox(pos cube.Pos, _ world.BlockSource) []cube.BBox {
	*m.block.cells = append(*m.block.cells, pos)
	return []cube.BBox{m.block.bounds}
}

type pistonShapeNativeSource struct{ piston *Piston }

func (s pistonShapeNativeSource) Block(pos cube.Pos) world.Block {
	if s.piston != nil && pos == (cube.Pos{1, 65, 1}) {
		return *s.piston
	}
	return Air{}
}

func TestPistonHeadShapesMatchExecutedClient(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_piston_contact_shapes.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete bool
		Parts    []struct {
			Face  cube.Face
			Boxes [][6]float64
		} `json:"head_collisions"`
		World []struct {
			Face         cube.Face
			Parent       bool
			LastProgress float32 `json:"last_progress"`
			Progress     float32
			Position     cube.Pos
			Boxes        [][6]float64
		} `json:"head_world_collisions"`
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Parts) != 6 || len(native.World) != 192 {
		t.Fatal("incomplete original-client piston-head shapes")
	}
	for _, c := range native.Parts {
		parts := pistonHeadCollisionParts(c.Face)
		if len(c.Boxes) != 3 {
			t.Fatal("missing original head parts")
		}
		for i, p := range parts {
			b := c.Boxes[i]
			if p != cube.Box(b[0], b[1], b[2], b[3], b[4], b[5]) {
				t.Errorf("face %d part %d=%v, original=%v", c.Face, i, p, b)
			}
		}
	}
	for i, c := range native.World {
		t.Run(fmt.Sprintf("%d/face%d/parent%t", i, c.Face, c.Parent), func(t *testing.T) {
			var piston *Piston
			if c.Parent {
				piston = &Piston{Facing: c.Face, LastProgress: c.LastProgress, Progress: c.Progress}
			}
			source := pistonHeadNativeSource{piston: piston, pos: c.Position.Side(c.Face.Opposite())}
			boxes := (PistonHead{Facing: c.Face}).Model().BBox(c.Position, source)
			if len(boxes) != len(c.Boxes) {
				t.Fatalf("physical boxes=%v, original=%v", boxes, c.Boxes)
			}
			for n, box := range boxes {
				box = box.Translate(c.Position.Vec3())
				want := c.Boxes[n]
				if box != cube.Box(want[0], want[1], want[2], want[3], want[4], want[5]) {
					t.Errorf("box %d=%v, original=%v", n, box, want)
				}
			}
		})
	}
}

type pistonHeadNativeSource struct {
	piston *Piston
	pos    cube.Pos
}

func (s pistonHeadNativeSource) Block(pos cube.Pos) world.Block {
	if s.piston != nil && pos == s.pos {
		return *s.piston
	}
	return Air{}
}
