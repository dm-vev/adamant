package session

import (
	"encoding/binary"
	"slices"
	"sync"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// chunkWireBufferPool holds the buffers sub-chunks and biomes are encoded
// into before the bytes are copied out at their length.
var chunkWireBufferPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 1024)
		return &b
	},
}

// encodeSubChunk returns the sub-chunk at index ind of the chunk passed as
// Bedrock clients read it: the sub-chunk version, the count of its layers and
// its Y, then each layer as a paletted storage whose palette holds network
// block hashes, as BDS sends them with block network ID hashes enabled.
// Clients resolve a hash by its state, not by where the state sits in their
// own palette.
func encodeSubChunk(c *chunk.Chunk, ind int) []byte {
	buf := chunkWireBufferPool.Get().(*[]byte)
	layers := c.Sub()[ind].Layers()
	br := c.BlockRegistry()
	b := append((*buf)[:0], chunk.SubChunkVersion, byte(len(layers)), uint8(ind+(c.Range()[0]>>4)))
	for _, storage := range layers {
		var palette bool
		if b, palette = appendStorageIndices(b, storage, nil); !palette {
			continue
		}
		p := storage.Palette()
		for i := range p.Len() {
			hash, _ := br.RuntimeIDToHash(p.Value(uint16(i)))
			b = appendVarint32(b, int32(hash))
		}
	}
	return releaseChunkWireBuffer(buf, b)
}

// encodeBiomes returns the biomes of the chunk passed as Bedrock clients read
// them: a paletted storage for each sub-chunk from the bottom, one equal to
// the storage below it written as a single byte, whose palette holds the
// positions of the biomes in world.BiomeIDs. A biome that is not registered
// is written as the first.
func encodeBiomes(c *chunk.Chunk) []byte {
	buf := chunkWireBufferPool.Get().(*[]byte)
	b := (*buf)[:0]
	var previous *chunk.PalettedStorage
	for _, storage := range c.BiomeStorages() {
		var palette bool
		b, palette = appendStorageIndices(b, storage, previous)
		previous = storage
		if !palette {
			continue
		}
		p := storage.Palette()
		for i := range p.Len() {
			index, _ := world.BiomeIndex(p.Value(uint16(i)))
			b = appendVarint32(b, int32(index))
		}
	}
	return releaseChunkWireBuffer(buf, b)
}

// appendStorageIndices appends to b the header and the indices of a paletted
// storage, and the length of its palette if the storage has more than one
// value, and reports whether the values of its palette are to follow. They
// are not if the storage equals the previous one, which is then written as
// the single byte 0xff.
func appendStorageIndices(b []byte, storage, previous *chunk.PalettedStorage) ([]byte, bool) {
	if storage.Equal(previous) {
		return append(b, 0x7f<<1|1), false
	}
	words, bits := storage.Indices()
	b = append(b, byte(bits<<1)|1)
	n := len(b)
	b = slices.Grow(b, len(words)*4)[:n+len(words)*4]
	for i, w := range words {
		binary.LittleEndian.PutUint32(b[n+i*4:], w)
	}
	if bits != 0 {
		b = appendVarint32(b, int32(storage.Palette().Len()))
	}
	return b, true
}

// appendVarint32 appends v to b as the zigzag varint Bedrock clients read.
func appendVarint32(b []byte, v int32) []byte {
	u := uint32(v) << 1
	if v < 0 {
		u = ^u
	}
	for u >= 0x80 {
		b = append(b, byte(u)|0x80)
		u >>= 7
	}
	return append(b, byte(u))
}

// releaseChunkWireBuffer returns a copy of the bytes b encoded into the
// buffer of the pool, at their length, and puts the buffer back into the pool
// unless it grew past what is worth keeping.
func releaseChunkWireBuffer(buf *[]byte, b []byte) []byte {
	out := make([]byte, len(b))
	copy(out, b)
	if cap(b) <= maxPooledChunkEncodeCap {
		*buf = b[:0]
		chunkWireBufferPool.Put(buf)
	}
	return out
}
