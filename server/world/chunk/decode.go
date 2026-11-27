package chunk

import (
	"bytes"
	"fmt"
	"sync"

	"github.com/dm-vev/adamant/server/block/cube"
)

var decodeBufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// DiskDecode decodes the data from a SerialisedData object into a chunk and returns it. If the data was invalid,
// an error is returned.
//
// The BlockRegistry passed must be finalized and must correspond to the runtime IDs used in the chunk data.
func DiskDecode(br BlockRegistry, data SerialisedData, r cube.Range) (*Chunk, error) {
	c := newChunk(br, r, len(data.Biomes) == 0)
	buf := decodeBufPool.Get().(*bytes.Buffer)
	defer func() {
		*buf = bytes.Buffer{}
		decodeBufPool.Put(buf)
	}()

	if len(data.Biomes) != 0 {
		*buf = *bytes.NewBuffer(data.Biomes)
		if err := decodeBiomes(buf, c, DiskEncoding); err != nil {
			return nil, err
		}
	}
	cache := diskCodecCacheFor(br)
	for i, sub := range data.SubChunks {
		if len(sub) == 0 {
			// No data for this sub chunk.
			continue
		}
		if len(sub) == 3 && sub[0] == 9 && sub[1] == 0 {
			index := uint8(int8(sub[2]) - int8(c.r[0]>>4))
			if int(index) >= len(c.sub) {
				return nil, fmt.Errorf("invalid sub-chunk index %d: chunk range has %d sub-chunks", index, len(c.sub))
			}
			c.sub[index].storages = nil
			continue
		}
		index := uint8(i)
		*buf = *bytes.NewBuffer(sub)
		if decodeErr := decodeSubChunkIntoWithCache(buf, c, &index, DiskEncoding, cache); decodeErr != nil {
			return nil, decodeErr
		}
		c.recordMissingBlockStates(index, c.sub[index])
	}
	return c, nil
}
func (c *Chunk) recordMissingBlockStates(subIndex uint8, sub *SubChunk) {
	baseY := int(c.SubY(int16(subIndex)))
	for layer, storage := range sub.storages {
		if len(storage.missingStateEntries) == 0 {
			continue
		}
		for x := byte(0); x < 16; x++ {
			for z := byte(0); z < 16; z++ {
				for y := byte(0); y < 16; y++ {
					paletteIndex := storage.paletteIndex(x, y, z)
					entry, missing := storage.missingStateEntries[paletteIndex]
					if !missing {
						continue
					}
					if c.missingBlockStates == nil {
						c.missingBlockStates = make(map[missingBlockStateKey][]string)
					}
					c.missingBlockStates[missingBlockStateKey{
						position: cube.Pos{int(x), baseY + int(y), int(z)},
						layer:    uint8(layer),
					}] = entry.properties
				}
			}
		}
	}
}

// decodeSubChunkInto decodes a SubChunk from a bytes.Buffer directly into c.sub[*index].
func decodeSubChunkInto(buf *bytes.Buffer, c *Chunk, index *byte, e Encoding) error {
	return decodeSubChunkIntoWithCache(buf, c, index, e, diskCodecCacheFor(c.br))
}

func decodeSubChunkIntoWithCache(buf *bytes.Buffer, c *Chunk, index *byte, e Encoding, cache *diskCodecCache) error {
	ver, err := buf.ReadByte()
	if err != nil {
		return fmt.Errorf("error reading version: %w", err)
	}
	switch ver {
	default:
		return fmt.Errorf("unknown sub chunk version %v: can't decode", ver)
	case 1:
		if int(*index) >= len(c.sub) {
			return fmt.Errorf("invalid sub-chunk index %d: chunk range has %d sub-chunks", *index, len(c.sub))
		}
		storage, err := decodeBlockStorageWithCache(buf, e, c.br, cache)
		if err != nil {
			return err
		}
		c.sub[*index].storages = []*PalettedStorage{storage}
	case 8, 9:
		// Version 8 allows up to 256 layers for one sub chunk.
		storageCount, err := buf.ReadByte()
		if err != nil {
			return fmt.Errorf("error reading storage count: %w", err)
		}
		if ver == 9 {
			uIndex, err := buf.ReadByte()
			if err != nil {
				return fmt.Errorf("error reading sub-chunk index: %w", err)
			}
			// The index as written here isn't the actual index of the sub-chunk within the chunk. Rather, it is the Y
			// value of the sub-chunk. This means that we need to translate it to an index.
			*index = uint8(int8(uIndex) - int8(c.r[0]>>4))
		}
		if int(*index) >= len(c.sub) {
			return fmt.Errorf("invalid sub-chunk index %d: chunk range has %d sub-chunks", *index, len(c.sub))
		}
		if storageCount == 0 {
			c.sub[*index].storages = nil
			return nil
		}
		storages := make([]*PalettedStorage, storageCount)
		for i := byte(0); i < storageCount; i++ {
			storages[i], err = decodeBlockStorageWithCache(buf, e, c.br, cache)
			if err != nil {
				return err
			}
		}
		c.sub[*index].storages = storages
	}
	return nil
}

func decodeBlockStorage(buf *bytes.Buffer, e Encoding, br BlockRegistry) (*PalettedStorage, error) {
	return decodeBlockStorageWithCache(buf, e, br, diskCodecCacheFor(br))
}

func decodeBlockStorageWithCache(buf *bytes.Buffer, e Encoding, br BlockRegistry, cache *diskCodecCache) (*PalettedStorage, error) {
	migration := paletteStateMigration{decodeCanonical: e.network() == 0, cache: cache}
	storage, err := decodePalettedStorage(buf, e, blockPaletteMigrationEncoding{
		BlockPaletteEncoding: BlockPaletteEncoding{Blocks: br},
		migration:            &migration,
	})
	if err != nil {
		return nil, err
	}
	storage.missingStateEntries = migration.entries
	storage.stateIDs = migration.stateIDs
	return storage, nil
}

// decodeBiomes reads the paletted storages holding biomes from buf and stores it into the Chunk passed.
func decodeBiomes(buf *bytes.Buffer, c *Chunk, e Encoding) error {
	var last *PalettedStorage
	if buf.Len() != 0 {
		for i := 0; i < len(c.sub); i++ {
			b, err := decodePalettedStorage(buf, e, BiomePaletteEncoding)
			if err != nil {
				return err
			}
			// b == nil means this paletted storage had the flag pointing to the previous one. It basically means we should
			// inherit whatever palette we decoded last.
			if i == 0 && b == nil {
				// This should never happen and there is no way to handle this.
				return fmt.Errorf("first biome storage pointed to previous one")
			}
			if b == nil {
				// This means this paletted storage had the flag pointing to the previous one. It basically means we should
				// inherit whatever palette we decoded last.
				last.markShared()
				b = last
			} else {
				if b.bitsPerIndex == 0 && len(b.palette.values) == 1 && b.palette.values[0] == 0 {
					b = zeroBiomeStorage
				}
				last = b
			}
			c.biomes[i] = b
		}
	}
	return nil
}

// decodePalettedStorage decodes a PalettedStorage from a bytes.Buffer. The Encoding passed is used to read the
// block storage.
func decodePalettedStorage(buf *bytes.Buffer, e Encoding, pe paletteEncoding) (*PalettedStorage, error) {
	blockSize, err := buf.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("error reading block size: %w", err)
	}
	blockSize >>= 1
	if blockSize == 0x7f {
		return nil, nil
	}

	size := paletteSize(blockSize)
	if size > 32 {
		return nil, fmt.Errorf("cannot read paletted storage (size=%v) %T: size too large", blockSize, pe)
	}
	uint32Count := size.uint32s()

	var uint32s []uint32
	if uint32Count > 0 {
		byteCount := uint32Count * 4
		data := buf.Next(byteCount)
		if len(data) != byteCount {
			return nil, fmt.Errorf("cannot read paletted storage (size=%v) %T: not enough block data present: expected %v bytes, got %v", blockSize, pe, byteCount, len(data))
		}
		uint32s = make([]uint32, uint32Count)
		for i := 0; i < uint32Count; i++ {
			// Explicitly don't use the binary package to greatly improve performance of reading the uint32s.
			uint32s[i] = uint32(data[i*4]) | uint32(data[i*4+1])<<8 | uint32(data[i*4+2])<<16 | uint32(data[i*4+3])<<24
		}
	}

	if _, ok := e.(diskEncoding); ok {
		if size == 0 {
			u := &uniformPalettedStorage{}
			u.palette.values = u.val[:1:1]
			u.storage.palette = &u.palette
			if err := decodeDiskPaletteInto(buf, size, pe, &u.palette); err != nil {
				return &u.storage, err
			}
			return &u.storage, nil
		}
		pair := &palettedStorageWithPalette{}
		pair.storage.palette = &pair.palette
		pair.storage.initIndices(uint32s)
		if err := decodeDiskPaletteInto(buf, size, pe, &pair.palette); err != nil {
			return &pair.storage, err
		}
		return &pair.storage, nil
	}
	p, err := e.decodePalette(buf, size, pe)
	return newPalettedStorage(uint32s, p), err
}
