package chunk

import (
	"bytes"
	"maps"
	"slices"
	"unsafe"
)

const (
	// uint32ByteSize is the amount of bytes in a uint32.
	uint32ByteSize = 4
	// uint32BitSize is the amount of bits in a uint32.
	uint32BitSize = uint32ByteSize * 8
)

// PalettedStorage is a storage of 4096 blocks encoded in a variable amount of uint32s, storages may have values
// with a bit size per block of 0, 1, 2, 3, 4, 5, 6, 8 or 16 bits.
// 3 of these formats have additional padding in every uint32 and an additional uint32 at the end, to cater
// for the blocks that don't fit. This padding is present when the storage has a block size of 3, 5 or 6
// bytes.
// Methods on PalettedStorage must not be called simultaneously from multiple goroutines.
type PalettedStorage struct {
	// bitsPerIndex is the amount of bits required to store one block. The number increases as the block
	// storage holds more unique block states.
	bitsPerIndex uint16
	// filledBitsPerIndex returns the amount of blocks that are actually filled per uint32.
	filledBitsPerIndex uint16
	// indexMask is the equivalent of 1 << bitsPerIndex - 1.
	indexMask uint32

	// indicesStart holds an unsafe.Pointer to the first byte in the indices slice below.
	indicesStart unsafe.Pointer

	// Palette holds all block runtime IDs that the indices in the indices slice point to. These runtime IDs
	// point to block states.
	palette             *Palette
	missingStateEntries map[uint16]missingStateEntry
	javaStateIDs        map[uint16]uint32
	// indices contains all indices in the PalettedStorage and changes only when the whole storage is resized.
	indices []uint32
}

// newPalettedStorage creates a new block storage using the uint32 slice as the indices and the palette passed.
// The bits per block are calculated using the length of the uint32 slice.
func newPalettedStorage(indices []uint32, palette *Palette) *PalettedStorage {
	var (
		bitsPerIndex       = uint16(len(indices) / uint32BitSize / uint32ByteSize)
		indexMask          = (uint32(1) << bitsPerIndex) - 1
		indicesStart       = (unsafe.Pointer)(unsafe.SliceData(indices))
		filledBitsPerIndex uint16
	)
	if bitsPerIndex != 0 {
		filledBitsPerIndex = uint32BitSize / bitsPerIndex * bitsPerIndex
	}
	return &PalettedStorage{filledBitsPerIndex: filledBitsPerIndex, indexMask: indexMask, indicesStart: indicesStart, bitsPerIndex: bitsPerIndex, indices: indices, palette: palette}
}

// emptyStorage creates a PalettedStorage filled completely with a value v.
func emptyStorage(v uint32) *PalettedStorage {
	return newPalettedStorage([]uint32{}, newPalette(0, []uint32{v}))
}

func (storage *PalettedStorage) Clone() *PalettedStorage {
	clone := newPalettedStorage(slices.Clone(storage.indices), storage.palette.Clone())
	clone.missingStateEntries = maps.Clone(storage.missingStateEntries)
	clone.javaStateIDs = maps.Clone(storage.javaStateIDs)
	return clone
}

// Palette returns the Palette of the PalettedStorage.
func (storage *PalettedStorage) Palette() *Palette {
	return storage.palette
}

// At returns the value of the PalettedStorage at a given x, y and z.
func (storage *PalettedStorage) At(x, y, z byte) uint32 {
	return storage.palette.Value(storage.paletteIndex(x&15, y&15, z&15))
}

// Set sets a value at a specific x, y and z. The Palette and PalettedStorage are expanded
// automatically to make space for the value, should that be needed.
func (storage *PalettedStorage) Set(x, y, z byte, v uint32) {
	x, y, z = x&15, y&15, z&15
	index := storage.palette.Index(v)
	if len(storage.javaStateIDs) != 0 {
		current := storage.paletteIndex(x, y, z)
		_, currentHasJava := storage.javaStateIDs[current]
		targetHasJava := false
		if index != -1 {
			_, targetHasJava = storage.javaStateIDs[uint16(index)]
		}
		if currentHasJava || targetHasJava {
			hasSource := storage.palette.Value(current) == v
			index = int16(storage.paletteIndexForMetadata(v, 0, current, hasSource))
		}
	}
	if index == -1 {
		index = storage.addNew(v)
	}
	storage.setPaletteIndex(x, y, z, uint16(index))
}

// StateID returns the source Java state ID at a position, or zero when absent.
func (storage *PalettedStorage) StateID(x, y, z byte) uint32 {
	return storage.javaStateIDs[storage.paletteIndex(x&15, y&15, z&15)]
}

// valueAndStateID returns what At and StateID return at a position, looking
// up its palette index once.
func (storage *PalettedStorage) valueAndStateID(x, y, z byte) (uint32, uint32) {
	index := storage.paletteIndex(x&15, y&15, z&15)
	if len(storage.javaStateIDs) == 0 {
		return storage.palette.Value(index), 0
	}
	return storage.palette.Value(index), storage.javaStateIDs[index]
}

// SetJavaStateID attaches source metadata to the current native state without changing it.
func (storage *PalettedStorage) SetJavaStateID(x, y, z byte, id uint32) {
	x, y, z = x&15, y&15, z&15
	current := storage.paletteIndex(x, y, z)
	if storage.javaStateIDs[current] == id {
		return
	}
	rid := storage.palette.Value(current)
	index := storage.paletteIndexForMetadata(rid, id, current, true)
	storage.setPaletteIndex(x, y, z, index)
}

func (storage *PalettedStorage) paletteIndexForMetadata(rid, javaID uint32, sourceIndex uint16, hasSource bool) uint16 {
	var missing missingStateEntry
	wantMissing := false
	if hasSource {
		missing, wantMissing = storage.missingStateEntries[sourceIndex]
	}
	for index, value := range storage.palette.values {
		candidateIndex := uint16(index)
		if value != rid || storage.javaStateIDs[candidateIndex] != javaID {
			continue
		}
		candidate, hasMissing := storage.missingStateEntries[candidateIndex]
		if hasMissing == wantMissing && (!hasMissing || (slices.Equal(candidate.properties, missing.properties) && candidate.state.Name == missing.state.Name && candidate.state.Version == missing.state.Version && maps.Equal(candidate.state.State, missing.state.State))) {
			return candidateIndex
		}
	}
	index, resize := storage.palette.Add(rid)
	if resize {
		storage.resize(storage.palette.size)
	}
	newIndex := uint16(index)
	if wantMissing {
		if storage.missingStateEntries == nil {
			storage.missingStateEntries = make(map[uint16]missingStateEntry)
		}
		storage.missingStateEntries[newIndex] = missing
	}
	if javaID != 0 {
		if storage.javaStateIDs == nil {
			storage.javaStateIDs = make(map[uint16]uint32)
		}
		storage.javaStateIDs[newIndex] = javaID
	}
	return newIndex
}

// SetPaletteIndex sets a precomputed palette index at x, y and z.
// x, y and z are masked to 0..15.
func (storage *PalettedStorage) SetPaletteIndex(x, y, z byte, index uint16) {
	x, y, z = x&15, y&15, z&15
	current := storage.paletteIndex(x, y, z)
	targetRID := storage.palette.Value(index)
	_, currentHasJava := storage.javaStateIDs[current]
	_, targetHasJava := storage.javaStateIDs[index]
	if currentHasJava || targetHasJava {
		index = storage.paletteIndexForMetadata(targetRID, 0, index, true)
	}
	storage.setPaletteIndex(x, y, z, index)
}

// Equal checks if two PalettedStorages are equal value wise. False is returned
// if either of the storages are nil.
func (storage *PalettedStorage) Equal(other *PalettedStorage) bool {
	if storage == nil || other == nil {
		return false
	}
	if storage.palette == nil || other.palette == nil {
		return false
	}
	if len(storage.indices) != len(other.indices) || len(storage.palette.values) != len(other.palette.values) {
		return false
	}
	if len(storage.indices) > 0 {
		indicesA := unsafe.Slice((*byte)(unsafe.Pointer(unsafe.SliceData(storage.indices))), len(storage.indices)*uint32ByteSize)
		indicesB := unsafe.Slice((*byte)(unsafe.Pointer(unsafe.SliceData(other.indices))), len(other.indices)*uint32ByteSize)
		if !bytes.Equal(indicesA, indicesB) {
			return false
		}
	}
	if len(storage.palette.values) == 0 {
		return true
	}
	paletteA := unsafe.Slice((*byte)(unsafe.Pointer(unsafe.SliceData(storage.palette.values))), len(storage.palette.values)*uint32ByteSize)
	paletteB := unsafe.Slice((*byte)(unsafe.Pointer(unsafe.SliceData(other.palette.values))), len(other.palette.values)*uint32ByteSize)
	return bytes.Equal(paletteA, paletteB) && maps.Equal(storage.javaStateIDs, other.javaStateIDs)
}

// addNew adds a new value to the PalettedStorage's Palette and returns its index. If needed, the storage is resized.
func (storage *PalettedStorage) addNew(v uint32) int16 {
	index, resize := storage.palette.Add(v)
	if resize {
		storage.resize(storage.palette.size)
	}
	return index
}

// paletteIndex looks up the Palette index at a given x, y and z value in the PalettedStorage. This palette
// index is not the value at this offset, but merely an index in the Palette pointing to a value.
func (storage *PalettedStorage) paletteIndex(x, y, z byte) uint16 {
	if storage.bitsPerIndex == 0 {
		// Unfortunately our default logic cannot deal with 0 bits per index, meaning we'll have to special case
		// this. This comes with a little performance hit, but it seems to be the only way to go. An alternative would
		// be not to have 0 bits per block storages in memory, but that would cause a strongly increased memory usage
		// by biomes.
		return 0
	}
	index := (uint16(x) << 8) | (uint16(z) << 4) | uint16(y)
	var (
		uint32Offset uint16
		bitOffset    uint16
	)
	switch storage.bitsPerIndex {
	case 1:
		uint32Offset = index >> 5
		bitOffset = index & 31
	case 2:
		uint32Offset = index >> 4
		bitOffset = (index & 15) << 1
	case 4:
		uint32Offset = index >> 3
		bitOffset = (index & 7) << 2
	case 8:
		uint32Offset = index >> 2
		bitOffset = (index & 3) << 3
	case 16:
		uint32Offset = index >> 1
		bitOffset = (index & 1) << 4
	default:
		offset := index * storage.bitsPerIndex
		uint32Offset, bitOffset = offset/storage.filledBitsPerIndex, offset%storage.filledBitsPerIndex
	}

	w := *(*uint32)(unsafe.Pointer(uintptr(storage.indicesStart) + uintptr(uint32Offset<<2)))
	return uint16((w >> bitOffset) & storage.indexMask)
}

// setPaletteIndex sets the palette index at a given x, y and z to paletteIndex. This index should point
// to a value in the PalettedStorage's Palette.
func (storage *PalettedStorage) setPaletteIndex(x, y, z byte, i uint16) {
	if storage.bitsPerIndex == 0 {
		return
	}
	index := (uint16(x) << 8) | (uint16(z) << 4) | uint16(y)
	var (
		uint32Offset uint16
		bitOffset    uint16
	)
	switch storage.bitsPerIndex {
	case 1:
		uint32Offset = index >> 5
		bitOffset = index & 31
	case 2:
		uint32Offset = index >> 4
		bitOffset = (index & 15) << 1
	case 4:
		uint32Offset = index >> 3
		bitOffset = (index & 7) << 2
	case 8:
		uint32Offset = index >> 2
		bitOffset = (index & 3) << 3
	case 16:
		uint32Offset = index >> 1
		bitOffset = (index & 1) << 4
	default:
		offset := index * storage.bitsPerIndex
		uint32Offset, bitOffset = offset/storage.filledBitsPerIndex, offset%storage.filledBitsPerIndex
	}

	ptr := (*uint32)(unsafe.Pointer(uintptr(storage.indicesStart) + uintptr(uint32Offset<<2)))
	*ptr = (*ptr &^ (storage.indexMask << bitOffset)) | (uint32(i) << bitOffset)
}

// resize changes the size of a PalettedStorage to newPaletteSize. A new PalettedStorage is constructed,
// and all values available in the current storage are set in their appropriate locations in the
// new storage.
func (storage *PalettedStorage) resize(newPaletteSize paletteSize) {
	if newPaletteSize == paletteSize(storage.bitsPerIndex) {
		return // Don't resize if the size is already equal.
	}
	// Construct a new storage and set all values in there manually. We can't easily do this in a better
	// way, because all values will be at a different index with a different length.
	newStorage := newPalettedStorage(make([]uint32, newPaletteSize.uint32s()), storage.palette)
	for x := byte(0); x < 16; x++ {
		for y := byte(0); y < 16; y++ {
			for z := byte(0); z < 16; z++ {
				newStorage.setPaletteIndex(x, y, z, storage.paletteIndex(x, y, z))
			}
		}
	}
	newStorage.missingStateEntries = storage.missingStateEntries
	newStorage.javaStateIDs = storage.javaStateIDs
	// Set the new storage.
	*storage = *newStorage
}

// compact clears unused indexes in the palette by scanning for usages in the PalettedStorage. This is a
// relatively heavy task which should only happen right before the sub chunk holding this PalettedStorage is
// saved to disk. compact also shrinks the palette size if possible.
func (storage *PalettedStorage) compact() {
	if storage.palette.Len() == 0 {
		return
	}
	if storage.palette.Len() == 1 {
		// A single unique value can always be represented using 0 bits per index. This avoids scanning the
		// entire storage and drops any backing indices slice.
		storage.bitsPerIndex = 0
		storage.filledBitsPerIndex = 0
		storage.indexMask = 0
		storage.indicesStart = nil
		storage.indices = nil
		storage.palette.size = 0
		return
	}

	usedIndices := make([]bool, storage.palette.Len())
	for x := byte(0); x < 16; x++ {
		for y := byte(0); y < 16; y++ {
			for z := byte(0); z < 16; z++ {
				usedIndices[storage.paletteIndex(x, y, z)] = true
			}
		}
	}

	usedCount := 0
	allUsed := true
	for _, used := range usedIndices {
		if used {
			usedCount++
		} else {
			allUsed = false
		}
	}

	// If every palette entry is used and the palette size cannot shrink, nothing changes.
	// This avoids allocating a new indices slice and palette values slice for already-optimal storages.
	size := paletteSizeFor(usedCount)
	if allUsed && size == storage.palette.size {
		return
	}

	newRuntimeIDs := make([]uint32, 0, usedCount)
	conversion := make([]uint16, len(usedIndices))
	for index, used := range usedIndices {
		if used {
			conversion[index] = uint16(len(newRuntimeIDs))
			newRuntimeIDs = append(newRuntimeIDs, storage.palette.values[index])
		}
	}
	// Construct a new storage and set all values in there manually. We can't easily do this in a better
	// way, because all values will be at a different index with a different length.
	newStorage := newPalettedStorage(make([]uint32, size.uint32s()), newPalette(size, newRuntimeIDs))

	for x := byte(0); x < 16; x++ {
		for y := byte(0); y < 16; y++ {
			for z := byte(0); z < 16; z++ {
				// Replace all usages of the old palette indexes with the new indexes using the map we
				// produced earlier.
				newStorage.setPaletteIndex(x, y, z, conversion[storage.paletteIndex(x, y, z)])
			}
		}
	}
	if len(storage.missingStateEntries) != 0 {
		entries := make(map[uint16]missingStateEntry, len(storage.missingStateEntries))
		for index, entry := range storage.missingStateEntries {
			if int(index) < len(usedIndices) && usedIndices[index] {
				entries[conversion[index]] = entry
			}
		}
		newStorage.missingStateEntries = entries
	}
	if len(storage.javaStateIDs) != 0 {
		ids := make(map[uint16]uint32, len(storage.javaStateIDs))
		for index, id := range storage.javaStateIDs {
			if int(index) < len(usedIndices) && usedIndices[index] {
				ids[conversion[index]] = id
			}
		}
		newStorage.javaStateIDs = ids
	}
	*storage = *newStorage
}
