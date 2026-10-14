package mcdb

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// TestStructureTemplatesPersistUnderBedrockKeys checks that a structure saved
// to disk survives reopening the world, under the LevelDB key Bedrock
// Dedicated Server uses, while one only kept in memory does not.
func TestStructureTemplatesPersistUnderBedrockKeys(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	w := world.Config{Provider: db, Synchronous: true, RandomTickSpeed: -1}.New()
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 70, 0}, block.Stone{}, nil)
		template := tx.CaptureStructure(cube.Pos{0, 70, 0}, [3]int{1, 1, 1}, false)
		if err := w.SaveStructureTemplate("house", template, true); err != nil {
			t.Fatal(err)
		}
		if err := w.SaveStructureTemplate("draft", template, false); err != nil {
			t.Fatal(err)
		}
	})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ldb.Get([]byte("structuretemplate_house"), nil); err != nil {
		t.Fatalf("saved structure is not under its Bedrock key: %v", err)
	}
	w = world.Config{Provider: db, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	house, ok, err := w.StructureTemplate("house")
	if err != nil || !ok {
		t.Fatalf("structure saved to disk is gone after reopening: %v", err)
	}
	if b, _ := house.Block(cube.Pos{}); b != (block.Stone{}) {
		t.Fatalf("reopened structure holds %#v, want stone", b)
	}
	if _, ok, _ := w.StructureTemplate("draft"); ok {
		t.Fatal("structure only saved to memory survived reopening the world")
	}
}
