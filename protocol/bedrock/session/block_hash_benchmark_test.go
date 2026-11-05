package session

import (
	"maps"
	"math"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// benchmarkStates returns n blocks of distinct states the game implements,
// spread over the states the clients of the default protocol know, by their
// hashes, so that the palette of the game does not change which they are.
func benchmarkStates(n int) []world.Block {
	br := world.DefaultBlockRegistry
	known := blockTable(oldProtocol(), br).rid
	var implemented []world.Block
	for _, netHash := range slices.Sorted(maps.Keys(known)) {
		b := br.BlockByRuntimeIDOrAir(known[netHash])
		if _, hash := b.Hash(); hash != math.MaxUint64 {
			implemented = append(implemented, b)
		}
	}
	blocks := make([]world.Block, n)
	step := len(implemented) / n
	for i := range blocks {
		blocks[i] = implemented[i*step]
	}
	return blocks
}

// BenchmarkEncodeSubChunkPalette encodes a sub-chunk whose palette holds 256
// states, each of which is sent as its network block hash.
func BenchmarkEncodeSubChunkPalette(b *testing.B) {
	br := world.DefaultBlockRegistry
	c := chunk.New(br, cube.Range{0, 15})
	for i, bl := range benchmarkStates(256) {
		c.SetBlock(uint8(i%16), int16(i/16), 0, 0, br.BlockRuntimeID(bl))
	}
	c.Compact()
	// The hashes of the registry are built as the server starts, not here.
	_ = encodeSubChunk(c, 0, hashesOf(c.BlockRegistry()))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = encodeSubChunk(c, 0, hashesOf(c.BlockRegistry()))
	}
}

// BenchmarkBlockUpdatePackets builds the packets of changes of blocks, each
// naming its block by network block hash: few changes of a sub-chunk, sent
// as an UpdateBlock each, and a sub-chunk of many, sent as one
// UpdateSubChunkBlocks.
func BenchmarkBlockUpdatePackets(b *testing.B) {
	states := benchmarkStates(64)
	for _, tc := range []struct {
		name  string
		count int
	}{{"few", subChunkBlocksAt - 1}, {"many", 64}} {
		updates := make([]world.BlockUpdate, tc.count)
		for i := range updates {
			updates[i] = world.BlockUpdate{Pos: cube.Pos{i % 16, i / 16, 0}, Block: states[i%len(states)]}
		}
		_ = blockUpdatePackets(tablesOf(nil, world.DefaultBlockRegistry), updates)
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = blockUpdatePackets(tablesOf(nil, world.DefaultBlockRegistry), updates)
			}
		})
	}
}
