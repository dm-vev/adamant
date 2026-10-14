package mcdb

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	_ "github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// TestStaleBlockEntitiesAreDroppedOnLoad checks a saved block entity at a
// block without one, as worlds saved while the block was unimplemented hold,
// is discarded without an error and is gone once the column is saved again.
func TestStaleBlockEntitiesAreDroppedOnLoad(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	dir := t.TempDir()
	pos := world.ChunkPos{2, -3}
	stone, ok := world.DefaultBlockRegistry.StateToRuntimeID("minecraft:stone", map[string]any{})
	if !ok {
		t.Fatal("no stone state")
	}
	at := cube.Pos{2*16 + 1, 64, -3*16 + 1}
	c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
	c.SetBlock(1, 64, 1, 0, stone)
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	stale := chunk.BlockEntity{Pos: at, Data: map[string]any{"x": int32(at[0]), "y": int32(at[1]), "z": int32(at[2])}}
	if err := db.StoreColumn(pos, world.Overworld, &chunk.Column{Chunk: c, BlockEntities: []chunk.BlockEntity{stale}}); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err = Config{Log: log}.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	w := world.Config{Provider: db, Log: log, Synchronous: true}.New()
	<-w.Exec(func(tx *world.Tx) {
		if name, _ := tx.Block(at).EncodeBlock(); name != "minecraft:stone" {
			t.Errorf("loaded %s, want stone", name)
		}
	})
	w.Save()
	_ = w.Close()
	if strings.Contains(logs.String(), "level=ERROR") {
		t.Fatalf("loading a stale block entity logged an error:\n%s", logs.String())
	}

	db, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	col, err := db.LoadColumn(pos, world.Overworld)
	if err != nil {
		t.Fatal(err)
	}
	if len(col.BlockEntities) != 0 {
		t.Fatalf("the stale block entity was saved again: %v", col.BlockEntities)
	}
}
