package mcdb

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

func TestInhabitedTimePersistsAcrossProviderRestart(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	pos := world.ChunkPos{12, -7}
	col := &chunk.Column{
		Chunk:        chunk.New(world.DefaultBlockRegistry, world.Overworld.Range()),
		InhabitedTime: 3_600_001,
	}
	if err := db.StoreColumn(pos, world.Overworld, col); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	loaded, err := db.LoadColumn(pos, world.Overworld)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.InhabitedTime != col.InhabitedTime {
		t.Fatalf("inhabited time after restart = %d, want %d", loaded.InhabitedTime, col.InhabitedTime)
	}
}
