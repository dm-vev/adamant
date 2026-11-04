package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
)

func TestStructureBlockEdited(t *testing.T) {
	b := StructureBlock{Powered: true, Author: "a", Strict: true}
	got := b.Edited(StructureBlockEdit{
		Mode: StructureBlockLoad, Name: "n", DataField: "d",
		IncludePlayers: true, ShowBoundingBox: true, IgnoreEntities: true, RemoveBlocks: true,
		Size:     [3]int{-1, 400, 10},
		Offset:   [3]int{-70, 385, 3},
		Rotation: 5, Mirror: 7, AnimationMode: 4,
		Integrity: 150, Seed: -9, AnimationSeconds: -1,
		RedstoneSaveMode: 3, LastTouchedPlayerID: 12,
	})
	want := StructureBlock{
		Mode: StructureBlockLoad, Name: "n", DataField: "d",
		IncludePlayers: true, ShowBoundingBox: true, IgnoreEntities: true, RemoveBlocks: true,
		Size:     [3]int{0, 384, 10},
		Offset:   cube.Pos{-64, 384, 3},
		Rotation: 1, Mirror: 3, AnimationMode: 1,
		Integrity: 100, Seed: -9, AnimationSeconds: 0,
		RedstoneSaveMode: 1, LastTouchedPlayerID: 12,
		Powered: true, Author: "a", Strict: true,
	}
	if got != want {
		t.Fatalf("Edited =\n%#v, want\n%#v", got, want)
	}
	if got := b.Edited(StructureBlockEdit{Integrity: -3}); got.Integrity != 0 {
		t.Fatalf("an integrity of -3 became %v, want 0", got.Integrity)
	}
}
