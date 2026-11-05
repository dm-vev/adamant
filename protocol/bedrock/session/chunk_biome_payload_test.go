package session

import (
	"bytes"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// TestLimitedChunkPayloadSharesTheBiomes checks that the payload sent to a
// client that asks for the sub-chunks of a column, its biomes and a byte
// after them, is the array of the biomes the column keeps, not a copy of it:
// a column sent to a client held both for as long as it was loaded.
func TestLimitedChunkPayloadSharesTheBiomes(t *testing.T) {
	t.Parallel()
	w := tablesOf(nil, world.DefaultBlockRegistry)
	col := &world.Column{Chunk: chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())}

	limited := w.limitedChunkPayload(col)
	biomes := w.networkBiomePayload(col)
	want := append(encodeBiomes(col.Chunk), 0)
	if !bytes.Equal(limited, want) {
		t.Errorf("the payload of a limited chunk is %x, want the biomes and a zero byte: %x", limited, want)
	}
	if !bytes.Equal(biomes, want[:len(want)-1]) {
		t.Errorf("the biomes are %x, want %x", biomes, want[:len(want)-1])
	}
	if len(limited) != len(biomes)+1 || &limited[0] != &biomes[0] {
		t.Error("the payload of a limited chunk is not the array of the biomes")
	}
	if cap(biomes) != len(biomes) {
		t.Error("the biomes may be appended to, over the byte after them")
	}
	if again := w.limitedChunkPayload(col); &again[0] != &limited[0] {
		t.Error("the payload of a limited chunk was made again")
	}

	// Asking for the limited payload first gives the same.
	other := &world.Column{Chunk: col.Chunk}
	first := w.limitedChunkPayload(other)
	if second := w.networkBiomePayload(other); &first[0] != &second[0] || !bytes.Equal(first, limited) {
		t.Error("the biomes made by a limited payload differ from those made first")
	}
}
