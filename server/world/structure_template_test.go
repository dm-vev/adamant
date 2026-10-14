package world_test

import (
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

func structureTestWorld(t *testing.T) *world.World {
	t.Helper()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	return w
}

// TestBedrockStructureTemplatesLoad checks that a template saved by Bedrock
// Dedicated Server places its blocks and block entities, and that air in it
// replaces the blocks in the world.
func TestBedrockStructureTemplatesLoad(t *testing.T) {
	raw, err := os.ReadFile("testdata/bds_structure_template.nbt")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := nbt.UnmarshalEncoding(raw, &m, nbt.LittleEndian); err != nil {
		t.Fatal(err)
	}
	w := structureTestWorld(t)
	template, err := world.DecodeStructureTemplate(m, w.BlockRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if template.Size() != [3]int{3, 3, 3} {
		t.Fatalf("size %v, want 3x3x3", template.Size())
	}
	<-w.Exec(func(tx *world.Tx) {
		pos := cube.Pos{10, 70, 10}
		tx.SetBlock(pos.Add(cube.Pos{2, 0, 2}), block.Dirt{}, nil)
		tx.PlaceStructure(template, pos, world.StructurePlacement{Integrity: 1})
		if _, ok := tx.Block(pos).(block.Stairs); !ok {
			t.Fatalf("got %#v at the corner, want the oak stairs of the template", tx.Block(pos))
		}
		if _, ok := tx.Block(pos.Add(cube.Pos{1, 1, 1})).(block.Chest); !ok {
			t.Fatalf("got %#v in the middle, want the chest of the template", tx.Block(pos.Add(cube.Pos{1, 1, 1})))
		}
		if _, ok := tx.Block(pos.Add(cube.Pos{0, 2, 0})).(block.Glass); !ok {
			t.Fatalf("got %#v on top, want glass", tx.Block(pos.Add(cube.Pos{0, 2, 0})))
		}
		if _, ok := tx.Block(pos.Add(cube.Pos{2, 0, 2})).(block.Air); !ok {
			t.Fatalf("got %#v where the template has air, want the dirt replaced", tx.Block(pos.Add(cube.Pos{2, 0, 2})))
		}
	})
}

// TestRotatedStructuresKeepTheirShape checks that rotating a structure turns
// stairs with it and keeps double chests paired, with a copy of their items.
func TestRotatedStructuresKeepTheirShape(t *testing.T) {
	w := structureTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		src := cube.Pos{0, 70, 0}
		tx.SetBlock(src, block.Stairs{Block: block.Planks{Wood: block.OakWood()}, Facing: cube.East}, nil)
		// A double chest is two chests pointing at each other.
		left := block.NewChest().DecodeNBT(map[string]any{"pairx": int32(2), "pairz": int32(0)}).(block.Chest)
		right := block.NewChest().DecodeNBT(map[string]any{"pairx": int32(1), "pairz": int32(0)}).(block.Chest)
		left.Facing, right.Facing = cube.North, cube.North
		tx.SetBlock(src.Add(cube.Pos{1, 0, 0}), left, nil)
		tx.SetBlock(src.Add(cube.Pos{2, 0, 0}), right, nil)
		_, _ = tx.Block(src.Add(cube.Pos{1, 0, 0})).(block.Chest).Inventory(tx, src.Add(cube.Pos{1, 0, 0})).AddItem(item.NewStack(item.Diamond{}, 3))
		template := tx.CaptureStructure(src, [3]int{3, 1, 1}, false)

		dst := cube.Pos{20, 70, 20}
		tx.PlaceStructure(template, dst, world.StructurePlacement{Rotation: world.StructureRotationClockwise90, Integrity: 1})
		// Clockwise, east turns south and the row along X runs along Z.
		stairs, ok := tx.Block(dst).(block.Stairs)
		if !ok || stairs.Facing != cube.South {
			t.Fatalf("got %#v at the rotated corner, want stairs facing south", tx.Block(dst))
		}
		chest, ok := tx.Block(dst.Add(cube.Pos{0, 0, 1})).(block.Chest)
		if !ok || !chest.Paired() || chest.Facing != cube.East {
			t.Fatalf("got %#v, want a paired chest facing east", tx.Block(dst.Add(cube.Pos{0, 0, 1})))
		}
		inv := chest.Inventory(tx, dst.Add(cube.Pos{0, 0, 1}))
		if inv.Size() != 54 || len(inv.Items()) != 1 || inv.Items()[0].Count() != 3 {
			t.Fatalf("rotated double chest holds %v in %d slots, want the 3 diamonds in 54", inv.Items(), inv.Size())
		}
		_ = inv.Clear()
		source := tx.Block(src.Add(cube.Pos{1, 0, 0})).(block.Chest).Inventory(tx, src.Add(cube.Pos{1, 0, 0}))
		if len(source.Items()) != 1 {
			t.Fatal("emptying the placed chest emptied the chest the structure was saved from")
		}
	})
}

// TestStructureVoidsAndIntegrityLeaveBlocks checks that structure voids keep
// the blocks they are placed over, and that an integrity of zero places no
// block.
func TestStructureVoidsAndIntegrityLeaveBlocks(t *testing.T) {
	w := structureTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		src := cube.Pos{0, 70, 0}
		tx.SetBlock(src, block.Stone{}, nil)
		tx.SetBlock(src.Add(cube.Pos{1, 0, 0}), block.StructureVoid{}, nil)
		template := tx.CaptureStructure(src, [3]int{2, 1, 1}, false)

		dst := cube.Pos{20, 70, 20}
		tx.SetBlock(dst.Add(cube.Pos{1, 0, 0}), block.Dirt{}, nil)
		tx.PlaceStructure(template, dst, world.StructurePlacement{Integrity: 1})
		if _, ok := tx.Block(dst.Add(cube.Pos{1, 0, 0})).(block.Dirt); !ok {
			t.Fatalf("structure void replaced the dirt with %#v", tx.Block(dst.Add(cube.Pos{1, 0, 0})))
		}
		other := cube.Pos{40, 70, 40}
		tx.PlaceStructure(template, other, world.StructurePlacement{Integrity: 0, Seed: 1})
		if _, ok := tx.Block(other).(block.Air); !ok {
			t.Fatalf("a structure without integrity placed %#v", tx.Block(other))
		}
	})
}

// TestStructureEntitiesAreCopied checks that saving a structure with entities
// copies them, and placing it spawns new ones at the rotated positions.
func TestStructureEntitiesAreCopied(t *testing.T) {
	w := structureTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		src := cube.Pos{0, 70, 0}
		pig := tx.AddEntity(entity.NewPig(world.EntitySpawnOpts{Position: mgl64.Vec3{2.5, 70, 0.5}}))
		without := tx.CaptureStructure(src, [3]int{3, 2, 1}, false)
		with := tx.CaptureStructure(src, [3]int{3, 2, 1}, true)
		if without.EntityCount() != 0 || with.EntityCount() != 1 {
			t.Fatalf("captured %d and %d pigs, want none without entities and one with them", without.EntityCount(), with.EntityCount())
		}
		dst := cube.Pos{20, 70, 20}
		tx.PlaceStructure(with, dst, world.StructurePlacement{Rotation: world.StructureRotationClockwise90, Integrity: 1})
		var pigs []world.Entity
		for e := range tx.Entities() {
			if e.H().Type() == entity.PigType {
				pigs = append(pigs, e)
			}
		}
		if len(pigs) != 2 {
			t.Fatalf("%d pigs after placing the structure, want the original and a copy", len(pigs))
		}
		for _, e := range pigs {
			if e.H() == pig.H() {
				continue
			}
			if e.H().UUID() == pig.H().UUID() {
				t.Fatal("the copied pig shares the identity of the original")
			}
			// The pig 2.5 blocks along X ends up 2.5 blocks along Z, at the
			// west edge of the rotated structure.
			if want := (mgl64.Vec3{20.5, 70, 22.5}); e.Position().Sub(want).Len() > 1e-4 {
				t.Fatalf("copied pig at %v, want %v", e.Position(), want)
			}
		}
	})
}
