package session

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// Network block IDs BDS 1.26.52 sends for these states with block network ID
// hashes enabled.
const (
	bdsStoneNetworkID      = 2150698529
	bdsGrassBlockNetworkID = 3727763636
)

// TestChunksNameBlocksByNetworkHash checks clients receive blocks as BDS
// sends them: by network block hash, independent of where the state sits in
// the server's palette.
func TestChunksNameBlocksByNetworkHash(t *testing.T) {
	br := world.DefaultBlockRegistry
	c := chunk.New(br, world.Overworld.Range())
	stone, grass := br.BlockRuntimeID(block.Stone{}), br.BlockRuntimeID(block.Grass{})
	c.SetBlock(0, 0, 0, 0, stone)
	c.SetBlock(1, 0, 0, 0, grass)
	index := int(-world.Overworld.Range().Min() >> 4)
	payload := chunk.EncodeSubChunk(c, chunk.NetworkEncoding, index)

	buf := bytes.NewBuffer(payload[3:])
	header, _ := buf.ReadByte()
	bits := int(header >> 1)
	words := (4096 + 32/bits - 1) / (32 / bits)
	buf.Next(words * 4)
	var count int32
	if err := protocol.Varint32(buf, &count); err != nil {
		t.Fatal(err)
	}
	palette := map[uint32]bool{}
	for range count {
		var v int32
		if err := protocol.Varint32(buf, &v); err != nil {
			t.Fatal(err)
		}
		palette[uint32(v)] = true
	}
	if !palette[bdsStoneNetworkID] || !palette[bdsGrassBlockNetworkID] {
		t.Fatalf("sub-chunk palette %v lacks the network IDs of stone and grass", palette)
	}
}

// TestItemStacksNameBlocksByNetworkHash checks block items carry the network
// block ID of their block both ways.
func TestItemStacksNameBlocksByNetworkHash(t *testing.T) {
	br := world.DefaultBlockRegistry
	stack := stackFromItem(br, item.NewStack(block.Stone{}, 3))
	if uint32(stack.BlockRuntimeID) != bdsStoneNetworkID {
		t.Fatalf("stone item has block ID %d, BDS sends %d", uint32(stack.BlockRuntimeID), uint32(bdsStoneNetworkID))
	}
	back := stackToItem(br, stack)
	if _, ok := back.Item().(block.Stone); !ok || back.Count() != 3 {
		t.Fatalf("stone item read back as %v", back)
	}
	unknown := stack
	unknown.BlockRuntimeID = int32(binary.LittleEndian.Uint32([]byte{1, 2, 3, 4}))
	if !stackToItem(br, unknown).Empty() {
		t.Fatal("an unknown network block ID resolved to a block")
	}
}
