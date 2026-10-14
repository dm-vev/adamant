package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// TestJigsawNBTMatchesBedrock checks a jigsaw reads the block entity data
// Bedrock Dedicated Server writes for a new jigsaw and for one its editor
// changed.
func TestJigsawNBTMatchesBedrock(t *testing.T) {
	fresh := Jigsaw{Facing: cube.FaceDown}.DecodeNBT(map[string]any{"BlockEntityVersion": int32(0), "final_state": "minecraft:air", "id": "JigsawBlock", "joint": "rollable",
		"name": "minecraft:empty", "placement_priority": int32(0), "selection_priority": int32(0), "target": "minecraft:empty", "target_pool": "minecraft:empty"}).(Jigsaw)
	if fresh != NewJigsaw(cube.FaceDown, 0) {
		t.Fatalf("new Bedrock jigsaw decoded as %+v, want the defaults %+v", fresh, NewJigsaw(cube.FaceDown, 0))
	}
	edited := Jigsaw{Facing: cube.FaceNorth}.DecodeNBT(map[string]any{"id": "JigsawBlock", "name": "minecraft:start", "target": "minecraft:bottom", "target_pool": "minecraft:village/plains/streets",
		"final_state": "minecraft:stone", "joint": "aligned", "placement_priority": int32(3), "selection_priority": int32(2)}).(Jigsaw)
	want := Jigsaw{Facing: cube.FaceNorth, Name: "minecraft:start", Target: "minecraft:bottom", Pool: "minecraft:village/plains/streets", FinalState: "minecraft:stone", Joint: "aligned", PlacementPriority: 3, SelectionPriority: 2}
	if edited != want {
		t.Fatalf("edited jigsaw decoded as %+v, want %+v", edited, want)
	}
	if encoded := edited.DecodeNBT(edited.EncodeNBT()).(Jigsaw); encoded != edited {
		t.Fatalf("jigsaw changed through its NBT: %+v", encoded)
	}
}

// TestJigsawsTurnWithStructures checks that a jigsaw in a rotated structure
// points the new way and keeps its connection settings.
func TestJigsawsTurnWithStructures(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		src := cube.Pos{0, 70, 0}
		j := NewJigsaw(cube.FaceNorth, 0)
		j.Name, j.Target, j.Pool = "minecraft:a", "minecraft:b", "minecraft:pool"
		tx.SetBlock(src, j, nil)
		up := NewJigsaw(cube.FaceUp, 0)
		tx.SetBlock(src.Add(cube.Pos{0, 1, 0}), up, nil)
		template := tx.CaptureStructure(src, [3]int{1, 2, 1}, false)
		dst := cube.Pos{20, 70, 20}
		tx.PlaceStructure(template, dst, world.StructurePlacement{Rotation: world.StructureRotationClockwise90, Integrity: 1})
		got, ok := tx.Block(dst).(Jigsaw)
		if !ok || got.Facing != cube.FaceEast || got.Name != "minecraft:a" || got.Target != "minecraft:b" || got.Pool != "minecraft:pool" {
			t.Fatalf("rotated jigsaw is %+v, want it facing east with its names and pool", tx.Block(dst))
		}
		// A jigsaw facing up with its top north turns its top east.
		if got, ok := tx.Block(dst.Add(cube.Pos{0, 1, 0})).(Jigsaw); !ok || got.Facing != cube.FaceUp || got.Rotation != 1 {
			t.Fatalf("rotated upward jigsaw is %+v, want it facing up with rotation 1", tx.Block(dst.Add(cube.Pos{0, 1, 0})))
		}
	})
}
