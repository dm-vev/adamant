package chunk

import (
	"bytes"
	"slices"
	"sync"
)

const (
	// SubChunkVersion is the current version of the written sub chunks, specifying the format they are
	// written on disk and over network.
	SubChunkVersion = 9
	// CurrentBlockVersion is the block-state encoding version observed in persisted palettes
	// from BDS 1.26.52.3. It is distinct from the game/protocol version.
	CurrentBlockVersion int32 = 18168865
)

var (
	// pool is used to pool byte buffers used for encoding chunks.
	pool = sync.Pool{
		New: func() any {
			return bytes.NewBuffer(make([]byte, 0, 1024))
		},
	}
)

type (
	// SerialisedData holds the serialised data of a chunk. It consists of the chunk's block data itself, a height
	// map, the biomes and entities and block entities.
	SerialisedData struct {
		// sub holds the data of the serialised sub chunks in a chunk. Sub chunks that are empty or that otherwise
		// don't exist are represented as an empty slice (or technically, nil).
		SubChunks [][]byte
		// Biomes is the biome data of the chunk, which is composed of a biome storage for each sub-chunk.
		Biomes []byte
	}
	// blockEntry represents a block as found in a disk save of a world.
	blockEntry struct {
		Name    string         `nbt:"name"`
		State   map[string]any `nbt:"states"`
		Version int32          `nbt:"version"`
	}
)

// Encode encodes Chunk to an intermediate representation SerialisedData, in the Encoding passed.
func Encode(c *Chunk, e Encoding) SerialisedData {
	d := SerialisedData{SubChunks: make([][]byte, len(c.sub))}
	for i := range c.sub {
		d.SubChunks[i] = EncodeSubChunk(c, e, i)
	}
	d.Biomes = EncodeBiomes(c, e)
	return d
}

// EncodeSubChunk encodes a sub-chunk from a chunk into bytes, in the Encoding passed.
func EncodeSubChunk(c *Chunk, e Encoding, ind int) []byte {
	s := c.sub[ind]
	if len(s.storages) == 0 {
		return []byte{SubChunkVersion, 0, uint8(ind + (c.r[0] >> 4))}
	}
	buf := pool.Get().(*bytes.Buffer)
	defer func() {
		buf.Reset()
		pool.Put(buf)
	}()

	_ = buf.WriteByte(SubChunkVersion)
	_ = buf.WriteByte(byte(len(s.storages)))
	_ = buf.WriteByte(uint8(ind + (c.r[0] >> 4)))
	for _, storage := range s.storages {
		pe := paletteEncoding(BlockPaletteEncoding{Blocks: c.br})
		if e.network() == 0 && (len(storage.missingStateEntries) != 0 || len(storage.stateIDs) != 0) {
			pe = blockPaletteMigrationEncoding{
				BlockPaletteEncoding: BlockPaletteEncoding{Blocks: c.br},
				migration:            &paletteStateMigration{entries: storage.missingStateEntries, cache: diskCodecCacheFor(c.br)},
				stateIDs:             storage.stateIDs,
			}
		}
		encodePalettedStorage(buf, storage, nil, e, pe)
	}
	return slices.Clone(buf.Bytes())
}

// EncodeBiomes encodes the biomes of a chunk into bytes, in the Encoding passed.
func EncodeBiomes(c *Chunk, e Encoding) []byte {
	buf := pool.Get().(*bytes.Buffer)
	defer func() {
		buf.Reset()
		pool.Put(buf)
	}()

	var previous *PalettedStorage
	for _, b := range c.biomes {
		encodePalettedStorage(buf, b, previous, e, BiomePaletteEncoding)
		previous = b
	}
	biomes := make([]byte, buf.Len())
	_, _ = buf.Read(biomes)
	return biomes
}

// encodePalettedStorage encodes a PalettedStorage into a bytes.Buffer. The Encoding passed is used to write the Palette
// of the PalettedStorage.
func encodePalettedStorage(buf *bytes.Buffer, storage, previous *PalettedStorage, e Encoding, pe paletteEncoding) {
	if storage.Equal(previous) {
		_ = buf.WriteByte(0x7f<<1 | e.network())
		return
	}
	_ = buf.WriteByte(byte(storage.bitsPerIndex<<1) | e.network())
	if len(storage.indices) != 0 {
		buf.Grow(len(storage.indices) * 4)
		var chunkBuf [512]byte
		indices := storage.indices
		for len(indices) != 0 {
			n := min(len(indices), len(chunkBuf)/4)
			for i, v := range indices[:n] {
				chunkBuf[i*4] = byte(v)
				chunkBuf[i*4+1] = byte(v >> 8)
				chunkBuf[i*4+2] = byte(v >> 16)
				chunkBuf[i*4+3] = byte(v >> 24)
			}
			_, _ = buf.Write(chunkBuf[:n*4])
			indices = indices[n:]
		}
	}

	e.encodePalette(buf, storage.palette, pe)
}
