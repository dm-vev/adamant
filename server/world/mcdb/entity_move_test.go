package mcdb

import (
	"testing"

	_ "github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

func entityAt(id int64, x, z float32) chunk.Entity {
	return chunk.Entity{ID: id, Data: map[string]any{
		"identifier": "minecraft:pig",
		"Pos":        []any{x, float32(64), z},
	}}
}

func loadEntityIDs(t *testing.T, db *DB, pos world.ChunkPos) []int64 {
	t.Helper()
	col, err := db.LoadColumn(pos, world.Overworld)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]int64, 0, len(col.Entities))
	for _, e := range col.Entities {
		ids = append(ids, e.ID)
	}
	return ids
}

// TestEntityMovedBetweenChunksSurvivesSaveOrder saves an entity that walked
// from chunk A into chunk B with B saved before A. A must not delete the
// record B now holds, and an entity removed from the world is still deleted.
func TestEntityMovedBetweenChunksSurvivesSaveOrder(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a, b := world.ChunkPos{0, 0}, world.ChunkPos{1, 0}
	column := func(entities ...chunk.Entity) *chunk.Column {
		return &chunk.Column{Chunk: chunk.New(world.DefaultBlockRegistry, world.Overworld.Range()), Entities: entities}
	}
	const walker, neighbour, killed = 1, 2, 3

	if err := db.StoreColumn(a, world.Overworld, column(entityAt(walker, 8, 8), entityAt(killed, 4, 4))); err != nil {
		t.Fatal(err)
	}
	if err := db.StoreColumn(b, world.Overworld, column(entityAt(walker, 20, 8), entityAt(neighbour, 24, 8))); err != nil {
		t.Fatal(err)
	}
	if err := db.StoreColumn(a, world.Overworld, column()); err != nil {
		t.Fatal(err)
	}

	if got := loadEntityIDs(t, db, b); len(got) != 2 || got[0] != walker || got[1] != neighbour {
		t.Fatalf("chunk B entities = %v, want [%d %d]", got, walker, neighbour)
	}
	if got := loadEntityIDs(t, db, a); len(got) != 0 {
		t.Fatalf("chunk A entities = %v, want none", got)
	}
	if _, err := db.ldb.Get(entityIndex(killed), nil); err == nil {
		t.Fatal("record of the entity removed from chunk A was kept")
	}
}

// TestDanglingEntityReferenceKeepsChunkEntities loads a chunk whose entity
// list names a record that no longer exists, as worlds saved before the
// ownership check hold: the other entities of the chunk still load.
func TestDanglingEntityReferenceKeepsChunkEntities(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	pos := world.ChunkPos{0, 0}
	col := &chunk.Column{Chunk: chunk.New(world.DefaultBlockRegistry, world.Overworld.Range()), Entities: []chunk.Entity{entityAt(1, 8, 8), entityAt(2, 9, 9)}}
	if err := db.StoreColumn(pos, world.Overworld, col); err != nil {
		t.Fatal(err)
	}
	if err := db.ldb.Delete(entityIndex(1), nil); err != nil {
		t.Fatal(err)
	}
	if got := loadEntityIDs(t, db, pos); len(got) != 1 || got[0] != 2 {
		t.Fatalf("entities = %v, want [2]", got)
	}
}
