package session

import (
	"encoding/binary"
	"math/bits"
	"slices"
	"sync"
	"unsafe"

	"github.com/dm-vev/adamant/server/world"
	"github.com/dm-vev/adamant/server/world/chunk"
)

// hostLittleEndian reports whether the CPU stores multi-byte integers in
// little-endian byte order, allowing []uint32 palette index words to be copied
// directly onto the wire without per-word encoding.
var hostLittleEndian = func() bool {
	var v uint16 = 1
	return *(*byte)(unsafe.Pointer(&v)) == 1
}()

// chunkWireBufferPool holds scratch buffers for variable-length chunk payloads
// before bytes are copied out at their exact length.
var chunkWireBufferPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 1024)
		return &b
	},
}

// encodeSubChunk returns the sub-chunk at index ind of the chunk passed as
// Bedrock clients that know blocks by the hashes passed read it: the
// sub-chunk version, the count of its layers and its Y, then each layer as
// a paletted storage whose palette holds network block hashes, as BDS
// sends them with block network ID hashes enabled.
// Clients resolve a hash by its state, not by where the state sits in their
// own palette.
func encodeSubChunk(c *chunk.Chunk, ind int, hashes *blockHashes) []byte {
	layers := c.Sub()[ind].Layers()
	hashTable := hashes.hash
	hashLen := uint32(len(hashTable))

	total := 3
	for _, storage := range layers {
		words, b := storage.Indices()
		p := storage.Palette()
		pLen := p.Len()
		total += 1 + len(words)*4
		if b != 0 {
			total += varint32Len(int32(pLen))
		}
		for i := range pLen {
			var hash uint32
			if rid := p.Value(uint16(i)); rid < hashLen {
				hash = hashTable[rid]
			}
			total += varint32Len(int32(hash))
		}
	}

	out := make([]byte, total)
	out[0] = chunk.SubChunkVersion
	out[1] = byte(len(layers))
	out[2] = uint8(ind + (c.Range()[0] >> 4))
	pos := 3
	for _, storage := range layers {
		words, b := storage.Indices()
		out[pos] = byte(b<<1) | 1
		pos++
		if n := len(words) * 4; n > 0 {
			putUint32sLE(out[pos:pos+n], words)
			pos += n
		}
		p := storage.Palette()
		pLen := p.Len()
		if b != 0 {
			pos += putVarint32(out[pos:], int32(pLen))
		}
		for i := range pLen {
			var hash uint32
			if rid := p.Value(uint16(i)); rid < hashLen {
				hash = hashTable[rid]
			}
			pos += putVarint32(out[pos:], int32(hash))
		}
	}
	return out
}

// encodeBiomes returns the biomes of the chunk passed as Bedrock clients read
// them: a paletted storage for each sub-chunk from the bottom, one equal to
// the storage below it written as a single byte, whose palette holds the
// positions of the biomes in world.BiomeIDs. A biome that is not registered
// is written as the first.
func encodeBiomes(c *chunk.Chunk) []byte {
	b := encodeBiomesTerminated(c)
	return b[: len(b)-1 : len(b)-1]
}

// encodeBiomesTerminated returns the biomes of the chunk as encodeBiomes
// does, and a zero byte after them: the payload of a column whose sub-chunks
// the client asks for.
func encodeBiomesTerminated(c *chunk.Chunk) []byte {
	storages := c.BiomeStorages()
	var stackBuf [64]byte
	pos := 0
	var previous *chunk.PalettedStorage
	for idx, storage := range storages {
		if storage.Equal(previous) {
			if pos >= len(stackBuf) {
				return encodeBiomesTerminatedPooled(stackBuf[:pos], storages[idx:], previous)
			}
			stackBuf[pos] = 0x7f<<1 | 1
			pos++
			previous = storage
			continue
		}
		words, bpb := storage.Indices()
		p := storage.Palette()
		pLen := p.Len()
		need := 1 + len(words)*4 + 5 + pLen*5
		if len(stackBuf)-pos < need {
			return encodeBiomesTerminatedPooled(stackBuf[:pos], storages[idx:], previous)
		}
		stackBuf[pos] = byte(bpb<<1) | 1
		pos++
		if wordBytes := len(words) * 4; wordBytes > 0 {
			putUint32sLE(stackBuf[pos:pos+wordBytes], words)
			pos += wordBytes
		}
		if bpb != 0 {
			pos += putVarint32(stackBuf[pos:], int32(pLen))
		}
		for i := range pLen {
			index, _ := world.BiomeIndex(p.Value(uint16(i)))
			pos += putVarint32(stackBuf[pos:], int32(index))
		}
		previous = storage
	}
	out := make([]byte, pos+1)
	copy(out, stackBuf[:pos])
	return out
}

// encodeBiomesTerminatedPooled finishes biome encoding using a pooled scratch
// buffer when the encoded biomes exceed the stack fast-path buffer.
func encodeBiomesTerminatedPooled(prefix []byte, storages []*chunk.PalettedStorage, previous *chunk.PalettedStorage) []byte {
	buf := chunkWireBufferPool.Get().(*[]byte)
	b := (*buf)[:0]
	b = append(b, prefix...)
	for _, storage := range storages {
		if storage.Equal(previous) {
			b = append(b, 0x7f<<1|1)
			previous = storage
			continue
		}
		words, bpb := storage.Indices()
		p := storage.Palette()
		pLen := p.Len()
		wordBytes := len(words) * 4
		b = slices.Grow(b, 1+wordBytes+5+pLen*5)
		b = append(b, byte(bpb<<1)|1)
		if wordBytes > 0 {
			n := len(b)
			b = b[:n+wordBytes]
			putUint32sLE(b[n:], words)
		}
		if bpb != 0 {
			b = appendVarint32(b, int32(pLen))
		}
		for i := range pLen {
			index, _ := world.BiomeIndex(p.Value(uint16(i)))
			b = appendVarint32(b, int32(index))
		}
		previous = storage
	}
	b = append(b, 0)
	return releaseChunkWireBuffer(buf, b)
}

// putUint32sLE writes words in little-endian byte order into dst, which must
// have length at least len(words)*4.
func putUint32sLE(dst []byte, words []uint32) {
	if hostLittleEndian {
		copy(dst, unsafe.Slice((*byte)(unsafe.Pointer(unsafe.SliceData(words))), len(words)*4))
		return
	}
	for i, w := range words {
		binary.LittleEndian.PutUint32(dst[i*4:], w)
	}
}

// varint32Len returns the encoded length in bytes of v as a zigzag varint32.
func varint32Len(v int32) int {
	u := uint32((v << 1) ^ (v >> 31))
	return (bits.Len32(u|1) + 6) / 7
}

// putVarint32 writes v to dst as a zigzag varint32 and returns bytes written.
func putVarint32(dst []byte, v int32) int {
	u := uint32((v << 1) ^ (v >> 31))
	i := 0
	for u >= 0x80 {
		dst[i] = byte(u) | 0x80
		u >>= 7
		i++
	}
	dst[i] = byte(u)
	return i + 1
}

// appendVarint32 appends v to b as the zigzag varint Bedrock clients read.
func appendVarint32(b []byte, v int32) []byte {
	u := uint32((v << 1) ^ (v >> 31))
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
