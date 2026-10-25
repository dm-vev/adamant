package overworld

import (
	"fmt"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// javaChunkBlocks are blocks of complete chunks the Java 26.3 dedicated
// server generated for seed 12345, read from its region files.
var javaChunkBlocks = []struct {
	chunk world.ChunkPos
	pos   cube.Pos
	state string
}{
	// A desert keeps sand under its top layer of sand, by the floor rule of
	// SurfaceRuleData that also applies under the floor.
	{world.ChunkPos{-151, 42}, cube.Pos{-2410, 59, 678}, "minecraft:sand"},
	{world.ChunkPos{-151, 42}, cube.Pos{-2410, 61, 678}, "minecraft:sand"},
	{world.ChunkPos{-151, 42}, cube.Pos{-2410, 58, 678}, "minecraft:sandstone"},
	// The ceiling and rails of mineshaft corridors are drawn from the random
	// seeded by the index of the mineshaft among the underground structures
	// of the registry, sorted by identifier.
	{world.ChunkPos{100, 100}, cube.Pos{1615, 38, 1608}, "minecraft:gravel"},
	{world.ChunkPos{100, 100}, cube.Pos{1608, 36, 1607}, "minecraft:rail[shape=east_west,waterlogged=false]"},
	{world.ChunkPos{100, 100}, cube.Pos{1610, 36, 1607}, "minecraft:rail[shape=east_west,waterlogged=false]"},
	{world.ChunkPos{100, 100}, cube.Pos{1605, 36, 1607}, "minecraft:cave_air"},
	{world.ChunkPos{100, 100}, cube.Pos{1606, 38, 1607}, "minecraft:cave_air"},
}

// TestChunksMatchTheJavaServer generates the chunks of javaChunkBlocks and
// checks the blocks the Java server generated in them.
func TestChunksMatchTheJavaServer(t *testing.T) {
	g := NewOverworld(12345)
	chunks := map[world.ChunkPos]*chunk.Chunk{}
	for _, b := range javaChunkBlocks {
		c, ok := chunks[b.chunk]
		if !ok {
			c = chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
			g.GenerateChunk(b.chunk, c, world.GenerationContext{})
			chunks[b.chunk] = c
		}
		state, err := world.CanonicalState(world.DefaultBlockRegistry, world.NativeStateAt(c, uint8(b.pos[0]&15), int16(b.pos[1]), uint8(b.pos[2]&15)))
		if err != nil {
			t.Fatal(err)
		}
		if got := javaStateString(state); got != b.state {
			t.Errorf("%v is %s, want %s as Java generates it", b.pos, got, b.state)
		}
	}
}

func javaStateString(state world.BlockState) string {
	if len(state.Properties) == 0 {
		return state.Name
	}
	keys := make([]string, 0, len(state.Properties))
	for k := range state.Properties {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	s := state.Name + "["
	for i, k := range keys {
		if i > 0 {
			s += ","
		}
		s += fmt.Sprintf("%s=%v", k, state.Properties[k])
	}
	return s + "]"
}
