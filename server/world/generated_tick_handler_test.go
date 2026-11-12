package world_test

import (
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/mcdb"
)

// handlerTickGenerator returns ticks Java worldgen leaves: on the leaves at
// the edge of a tree, on cave air, on sand and on water. Of these only the
// water has a handler: Leaves decay and Sand falls by their neighbour updates.
type handlerTickGenerator struct{}

func (handlerTickGenerator) DefaultSpawn(world.Dimension) cube.Pos { return cube.Pos{8, 64, 8} }

func (handlerTickGenerator) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk, _ world.GenerationContext) world.GenerationResult {
	if pos != (world.ChunkPos{}) {
		return world.GenerationResult{}
	}
	c.SetBlock(4, 70, 4, 0, world.BlockRuntimeID(block.Leaves{Type: block.OakLeaves()}))
	c.SetBlock(6, 63, 6, 0, world.BlockRuntimeID(block.Stone{}))
	c.SetBlock(6, 64, 6, 0, world.BlockRuntimeID(block.Sand{}))
	return world.GenerationResult{ScheduledTicks: []chunk.ScheduledTick{
		{Pos: cube.Pos{4, 70, 4}, Kind: chunk.BlockTick, Type: "minecraft:oak_leaves"},
		{Pos: cube.Pos{6, 64, 6}, Kind: chunk.BlockTick, Type: "minecraft:sand"},
		{Pos: cube.Pos{2, 20, 2}, Kind: chunk.BlockTick, Type: "minecraft:cave_air"},
		{Pos: cube.Pos{8, 64, 8}, Kind: chunk.FluidTick, Type: "minecraft:water"},
	}}
}

// TestBlockTicksWithoutHandlerAreNotQueued checks that a generated or saved
// block tick of a type no native block handles, such as the ticks Java
// worldgen leaves on leaves, is not queued, while the others are.
func TestBlockTicksWithoutHandlerAreNotQueued(t *testing.T) {
	want := []string{"minecraft:water"}
	types := func(w *world.World) []string {
		var got []string
		<-w.Do(func(tx *world.Tx) {
			tx.Block(cube.Pos{8, 64, 8})
			for _, tick := range tx.ScheduledTicks(world.ChunkPos{}) {
				got = append(got, tick.Type)
			}
		}).Done()
		return got
	}
	dir := t.TempDir()
	db, err := mcdb.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	w := world.Config{Provider: db, Generator: handlerTickGenerator{}, Synchronous: true, RandomTickSpeed: -1}.New()
	if got := types(w); !slices.Equal(got, want) {
		t.Errorf("generated ticks queued: %v, want %v", got, want)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if db, err = mcdb.Open(dir); err != nil {
		t.Fatal(err)
	}

	// A save of an earlier version holds the generated ticks of leaves.
	col, err := db.LoadColumn(world.ChunkPos{}, world.Overworld)
	if err != nil {
		t.Fatal(err)
	}
	col.ScheduledTicks = append(col.ScheduledTicks, chunk.SavedScheduledTick{
		ScheduledTick: chunk.ScheduledTick{Pos: cube.Pos{4, 70, 4}, Kind: chunk.BlockTick, Type: "minecraft:oak_leaves"},
	})
	if err := db.StoreColumn(world.ChunkPos{}, world.Overworld, col); err != nil {
		t.Fatal(err)
	}
	w = world.Config{Provider: db, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	if got := types(w); !slices.Equal(got, want) {
		t.Errorf("saved ticks queued: %v, want %v", got, want)
	}
}
