package block

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type stairTestBlocks map[cube.Pos]world.Block

func (s stairTestBlocks) Block(pos cube.Pos) world.Block {
	if b, ok := s[pos]; ok {
		return b
	}
	return Air{}
}

func referenceStair(t *testing.T, name string, facing int, upsideDown bool) Stairs {
	t.Helper()
	b, ok := world.BlockByName(name, map[string]any{
		"minecraft:corner": "none", "upside_down_bit": upsideDown, "weirdo_direction": int32(facing),
	})
	if !ok {
		t.Fatalf("stair material missing: %s", name)
	}
	s, ok := b.(Stairs)
	if !ok {
		t.Fatalf("stair material has no implementation: %s (%T)", name, b)
	}
	s.Facing, s.UpsideDown = cube.Direction(3-facing), upsideDown
	return s
}

func TestStairCornersMatchBDS(t *testing.T) {
	data, err := os.ReadFile("testdata/bds_stairs_1.26.52.3.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Facing     int
			UpsideDown bool
			Neighbors  []struct {
				X, Z, Facing int
				UpsideDown   bool
				Type         string
			}
			Corner              string
			CollisionTopSamples [][3]float64
			RayHits             [][3]float64
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for i, scenario := range fixture.Cases {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			pos := cube.Pos{0, 64, 0}
			stair := referenceStair(t, "minecraft:oak_stairs", scenario.Facing, scenario.UpsideDown)
			source := stairTestBlocks{pos: stair}
			for _, neighbor := range scenario.Neighbors {
				name := neighbor.Type
				if name == "" {
					name = "minecraft:stone_brick_stairs"
				}
				source[cube.Pos{neighbor.X, pos[1], neighbor.Z}] = referenceStair(t, name, neighbor.Facing, neighbor.UpsideDown)
			}
			stair.Corner = stair.cornerAt(pos, source)
			if got := stair.Corner.String(); got != scenario.Corner {
				t.Fatalf("corner = %s, BDS = %s; neighbors = %+v", got, scenario.Corner, scenario.Neighbors)
			}
			// Only stationary grounded probes inside the target block are included.
			// BDS pushes other dropped items sideways; those are not height samples.
			for _, sample := range scenario.CollisionTopSamples {
				height := 0.0
				for _, box := range stair.Model().BBox(pos, source) {
					min, max := box.Min(), box.Max()
					if sample[0] > min[0] && sample[0] < max[0] && sample[2] > min[2] && sample[2] < max[2] {
						height = math.Max(height, max[1])
					}
				}
				if math.Abs(height-sample[1]) > 0.00001 {
					t.Errorf("collision height at (%g,%g) = %g, BDS = %g", sample[0], sample[2], height, sample[1])
				}
			}
			startY, endY := 2.0, -1.0
			if scenario.UpsideDown {
				startY, endY = -1.5, 1.5
			}
			for _, sample := range scenario.RayHits {
				start := pos.Vec3().Add(mgl64.Vec3{sample[0], startY, sample[2]})
				end := pos.Vec3().Add(mgl64.Vec3{sample[0], endY, sample[2]})
				want := pos.Vec3().Add(mgl64.Vec3(sample))
				hit, ok := trace.BlockIntercept(pos, source, stair, start, end, trace.RayShape)
				if !ok || hit.Position().Sub(want).Len() > 0.00001 {
					t.Errorf("ray hit = %v (found=%t), BDS = %v", hit.Position(), ok, want)
				}
			}
		})
	}
}

func TestStairNeighborRemovalClearsPersistedCorner(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	pos := cube.Pos{8, 64, 8}
	stair := referenceStair(t, "minecraft:oak_stairs", 0, true)
	neighbor := referenceStair(t, "minecraft:stone_brick_stairs", 2, true)
	runWorld(w, func(tx *world.Tx) {
		front := pos.Side(stair.Facing.Face())
		tx.SetBlock(pos, stair, nil)
		tx.SetBlock(front, neighbor, nil)
		stair.NeighbourUpdateTick(pos, front, tx)
		got := tx.Block(pos).(Stairs)
		if got.Corner != model.StairCornerOuterRight || got.Facing != stair.Facing || !got.UpsideDown || got.Block != stair.Block {
			t.Fatalf("neighbor update lost corner/orientation/material: %#v", got)
		}
		tx.SetBlock(front, nil, nil)
		got.NeighbourUpdateTick(pos, front, tx)
		if got := tx.Block(pos).(Stairs); got.Corner != model.StairCornerNone {
			t.Fatalf("removed neighbor left stale corner: %#v", got)
		}
	})
}

func TestStairReconstructionPreservesExplicitCorner(t *testing.T) {
	pos := cube.Pos{0, 64, 0}
	stair := referenceStair(t, "minecraft:oak_stairs", 0, false)
	stair.Corner = model.StairCornerInnerLeft
	source := stairTestBlocks{}
	got := stair.ReconstructMissingState(pos, []string{"unrelated"}, source).(Stairs)
	if got.Corner != model.StairCornerInnerLeft {
		t.Fatal("explicit saved corner overwritten")
	}
	got = stair.ReconstructMissingState(pos, []string{"minecraft:corner"}, source).(Stairs)
	if got.Corner != model.StairCornerNone || got.Facing != stair.Facing || got.UpsideDown != stair.UpsideDown || got.Block != stair.Block {
		t.Fatalf("missing corner migration changed other attributes: %#v", got)
	}
}

func TestStairModelUsesPersistedShapeWithoutNeighbors(t *testing.T) {
	for _, scenario := range []struct {
		corner model.StairCorner
		volume float64
	}{
		{model.StairCornerNone, 0.75},
		{model.StairCornerInnerLeft, 0.875},
		{model.StairCornerInnerRight, 0.875},
		{model.StairCornerOuterLeft, 0.625},
		{model.StairCornerOuterRight, 0.625},
	} {
		for half := range 2 {
			s := model.Stair{Facing: cube.East, UpsideDown: half != 0, Corner: scenario.corner}
			volume := 0.0
			for _, box := range s.BBox(cube.Pos{}, stairTestBlocks{}) {
				volume += box.Volume()
			}
			if volume != scenario.volume {
				t.Errorf("corner %s upsideDown=%t volume = %g, want %g", scenario.corner, s.UpsideDown, volume, scenario.volume)
			}
		}
	}
}
