package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

func TestSubChunkHeightMapPreservesXZCoordinates(t *testing.T) {
	c := chunk.New(world.DefaultBlockRegistry, cube.Range{0, 31})
	stone := world.DefaultBlockRegistry.BlockRuntimeID(block.Stone{})
	c.SetBlock(2, 6, 5, 0, stone)
	c.SetBlock(5, 10, 2, 0, stone)
	col := &world.Column{Chunk: c}
	entry := benchmarkSession(false).subChunkEntry(v2193.SubChunkOffset{}, 0, col, make(map[uint64]struct{}))
	if entry.HeightMapType != v2193.HeightMapDataHasData {
		t.Fatalf("heightmap type = %d, expected surface data", entry.HeightMapType)
	}
	heights, ok := entry.HeightMapData.Value()
	if !ok || heights[5][2] != 7 || heights[2][5] != 11 {
		t.Fatalf("expected first-air heights at (x=2,z=5):7 and (x=5,z=2):11, got present=%v, heights=%v", ok, heights)
	}
	render, ok := entry.RenderHeightMapData.Value()
	if !ok || render[5][2] != 7 || render[2][5] != 11 {
		t.Fatalf("render heightmap changed surface coordinates: present=%v, heights=%v", ok, render)
	}
}
