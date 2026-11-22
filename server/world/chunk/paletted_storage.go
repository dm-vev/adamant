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
	// sharedStorageFlag is stored in the top bit of filledBitsPerIndex to mark
	// read-only / copy-on-write shared storages without changing struct layout.
	sharedStorageFlag uint16 = 1 << 15
)

// palettedStorageWithPalette co-allocates a PalettedStorage and its Palette in
// a single heap object, cutting per-storage allocations in half while keeping
// the exported struct layouts untouched.
type palettedStorageWithPalette struct {
	storage PalettedStorage
	palette Palette
}

// uniformPalettedStorage co-allocates a 0-bit PalettedStorage, its Palette, and
// its single palette value in a single heap object.
type uniformPalettedStorage struct {
	storage PalettedStorage
	palette Palette
	val     [1]uint32
}

// zeroBiomeStorage is an immutable 0-bit storage filled with biome ID 0, shared
// across chunks and sub-chunks until copy-on-write mutation in Chunk.SetBiome.
var zeroBiomeStorage = func() *PalettedStorage {
	u := &uniformPalettedStorage{
		val: [1]uint32{0},
	}
	initPalette(&u.palette, 0, u.val[:1:1])
	u.storage.filledBitsPerIndex = sharedStorageFlag
	u.storage.palette = &u.palette
	return &u.storage
}()

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

func (storage *PalettedStorage) isShared() bool {
	return storage.filledBitsPerIndex&sharedStorageFlag != 0
}

func (storage *PalettedStorage) markShared() {
	if storage.filledBitsPerIndex&sharedStorageFlag == 0 {
		storage.filledBitsPerIndex |= sharedStorageFlag
	}
}

func (storage *PalettedStorage) initIndices(indices []uint32) {
	bitsPerIndex := uint16(len(indices) / uint32BitSize / uint32ByteSize)
	storage.bitsPerIndex = bitsPerIndex
	storage.indexMask = (uint32(1) << bitsPerIndex) - 1
	storage.indices = indices
	if len(indices) != 0 {
		storage.indicesStart = unsafe.Pointer(unsafe.SliceData(indices))
	} else {
		storage.indicesStart = nil
	}
	if bitsPerIndex != 0 {
		storage.filledBitsPerIndex = uint32BitSize / bitsPerIndex * bitsPerIndex
	} else {
		storage.filledBitsPerIndex = 0
	}
}

// newPalettedStorage creates a new block storage using the uint32 slice as the indices and the palette passed.
// The bits per block are calculated using the length of the uint32 slice.
func newPalettedStorage(indices []uint32, palette *Palette) *PalettedStorage {
	storage := &PalettedStorage{palette: palette}
	storage.initIndices(indices)
	return storage
}

// newPalettedStorageWithValues creates a PalettedStorage co-allocated with its Palette.
func newPalettedStorageWithValues(indices []uint32, size paletteSize, values []uint32) *PalettedStorage {
	pair := &palettedStorageWithPalette{}
	initPalette(&pair.palette, size, values)
	pair.storage.palette = &pair.palette
	pair.storage.initIndices(indices)
	return &pair.storage
}

// emptyStorage creates a PalettedStorage filled completely with a value v.
func emptyStorage(v uint32) *PalettedStorage {
	u := &uniformPalettedStorage{val: [1]uint32{v}}
	initPalette(&u.palette, 0, u.val[:1:1])
	u.storage.palette = &u.palette
	return &u.storage
}

func (storage *PalettedStorage) Clone() *PalettedStorage {
	if storage.bitsPerIndex == 0 && len(storage.palette.values) == 1 && len(storage.missingStateEntries) == 0 && len(storage.javaStateIDs) == 0 {
		return emptyStorage(storage.palette.values[0])
	}
	pair := &palettedStorageWithPalette{
		palette: Palette{
			last:      storage.palette.last,
			lastIndex: storage.palette.lastIndex,
			size:      storage.palette.size,
			values:    slices.Clone(storage.palette.values),
		},
	}
	pair.storage.palette = &pair.palette
	pair.storage.initIndices(slices.Clone(storage.indices))
	pair.storage.missingStateEntries = maps.Clone(storage.missingStateEntries)
	pair.storage.javaStateIDs = maps.Clone(storage.javaStateIDs)
	return &pair.storage
}

// Palette returns the Palette of the PalettedStorage.
func (storage *PalettedStorage) Palette() *Palette {
	return storage.palette
}

// Indices returns the palette indices of the blocks of the storage, packed
// into uint32s as the world format lays them out (as many to a word as fit,
// low bits first), and the bits one index takes. The slice belongs to the
// storage: the caller must not change it.
func (storage *PalettedStorage) Indices() (words []uint32, bitsPerIndex uint16) {
	return storage.indices, storage.bitsPerIndex
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
	if len(storage.javaStateIDs) == 0 {
		return 0
	}
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
	if id == 0 && len(storage.javaStateIDs) == 0 {
		return
	}
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
	if len(storage.javaStateIDs) != 0 {
		current := storage.paletteIndex(x, y, z)
		targetRID := storage.palette.Value(index)
		_, currentHasJava := storage.javaStateIDs[current]
		_, targetHasJava := storage.javaStateIDs[index]
		if currentHasJava || targetHasJava {
			index = storage.paletteIndexForMetadata(targetRID, 0, index, true)
		}
	}
	storage.setPaletteIndex(x, y, z, index)
}

// Equal checks if two PalettedStorages are equal value wise. False is returned
// if either of the storages are nil.
func (storage *PalettedStorage) Equal(other *PalettedStorage) bool {
	if storage == nil || other == nil {
		return false
	}
	if storage == other {
		return true
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
	case 3:
		uint32Offset = index / 10
		bitOffset = (index - uint32Offset*10) * 3
	case 4:
		uint32Offset = index >> 3
		bitOffset = (index & 7) << 2
	case 5:
		uint32Offset = index / 6
		bitOffset = (index - uint32Offset*6) * 5
	case 6:
		uint32Offset = index / 5
		bitOffset = (index - uint32Offset*5) * 6
	case 8:
		uint32Offset = index >> 2
		bitOffset = (index & 3) << 3
	case 16:
		uint32Offset = index >> 1
		bitOffset = (index & 1) << 4
	default:
		filled := storage.filledBitsPerIndex &^ sharedStorageFlag
		offset := index * storage.bitsPerIndex
		uint32Offset, bitOffset = offset/filled, offset%filled
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
	case 3:
		uint32Offset = index / 10
		bitOffset = (index - uint32Offset*10) * 3
	case 4:
		uint32Offset = index >> 3
		bitOffset = (index & 7) << 2
	case 5:
		uint32Offset = index / 6
		bitOffset = (index - uint32Offset*6) * 5
	case 6:
		uint32Offset = index / 5
		bitOffset = (index - uint32Offset*5) * 6
	case 8:
		uint32Offset = index >> 2
		bitOffset = (index & 3) << 3
	case 16:
		uint32Offset = index >> 1
		bitOffset = (index & 1) << 4
	default:
		filled := storage.filledBitsPerIndex &^ sharedStorageFlag
		offset := index * storage.bitsPerIndex
		uint32Offset, bitOffset = offset/filled, offset%filled
	}

	ptr := (*uint32)(unsafe.Pointer(uintptr(storage.indicesStart) + uintptr(uint32Offset<<2)))
	*ptr = (*ptr &^ (storage.indexMask << bitOffset)) | (uint32(i) << bitOffset)
}

// resize changes the size of a PalettedStorage to newPaletteSize in place.
func (storage *PalettedStorage) resize(newPaletteSize paletteSize) {
	if newPaletteSize == paletteSize(storage.bitsPerIndex) {
		return // Don't resize if the size is already equal.
	}
	newIndices := make([]uint32, newPaletteSize.uint32s())
	if storage.bitsPerIndex != 0 && newPaletteSize != 0 {
		repackIndices(newIndices, uint16(newPaletteSize), storage.indices, storage.bitsPerIndex, nil)
	}
	storage.initIndices(newIndices)
}

// markUsedIndices marks every palette index referenced across the 4096 cells of indices.
func markUsedIndices(indices []uint32, bits uint16, used []bool) {
	if bits == 0 {
		used[0] = true
		return
	}
	if bits == 1 {
		var hasZero, hasOne bool
		for _, w := range indices {
			if w != 0 {
				hasOne = true
			}
			if w != ^uint32(0) {
				hasZero = true
			}
			if hasZero && hasOne {
				break
			}
		}
		if hasZero {
			used[0] = true
		}
		if hasOne && len(used) > 1 {
			used[1] = true
		}
		return
	}
	perWord := uint32BitSize / int(bits)
	mask := (uint32(1) << bits) - 1
	remaining := 4096
	if bits <= 6 {
		var mask64 uint64
		for _, w := range indices {
			n := min(perWord, remaining)
			for range n {
				mask64 |= 1 << (w & mask)
				w >>= bits
			}
			remaining -= n
			if remaining == 0 {
				break
			}
		}
		for i := range used {
			if mask64&(1<<uint(i)) != 0 {
				used[i] = true
			}
		}
		return
	}
	for _, w := range indices {
		n := min(perWord, remaining)
		for range n {
			used[w&mask] = true
			w >>= bits
		}
		remaining -= n
		if remaining == 0 {
			return
		}
	}
}

// repackIndices sequentially unpacks 4096 indices from src (at srcBits per index),
// optionally remaps them through remap, and packs them into dst (at dstBits per index).
func repackIndices(dst []uint32, dstBits uint16, src []uint32, srcBits uint16, remap []uint16) {
	srcPerWord := uint32BitSize / int(srcBits)
	srcMask := (uint32(1) << srcBits) - 1
	dstPerWord := uint32BitSize / int(dstBits)
	outWordIdx, outCount := 0, 0
	var outVal uint32
	var outShift uint16
	remaining := 4096
	for _, w := range src {
		n := min(srcPerWord, remaining)
		for range n {
			v := uint16(w & srcMask)
			w >>= srcBits
			if remap != nil {
				v = remap[v]
			}
			outVal |= uint32(v) << outShift
			outShift += dstBits
			outCount++
			if outCount == dstPerWord {
				dst[outWordIdx] = outVal
				outWordIdx++
				outVal, outShift, outCount = 0, 0, 0
			}
		}
		remaining -= n
		if remaining == 0 {
			break
		}
	}
	if outCount > 0 {
		dst[outWordIdx] = outVal
	}
}

// compact clears unused indexes in the palette by scanning for usages in the PalettedStorage. This is a
// relatively heavy task which should only happen right before the sub chunk holding this PalettedStorage is
// saved to disk. compact also shrinks the palette size if possible.
func (storage *PalettedStorage) compact() {
	paletteLen := storage.palette.Len()
	if paletteLen == 0 {
		return
	}
	if paletteLen == 1 {
		storage.initIndices(nil)
		storage.palette.size = 0
		return
	}

	var usedBuf [256]bool
	var usedIndices []bool
	if paletteLen <= len(usedBuf) {
		usedIndices = usedBuf[:paletteLen]
	} else {
		usedIndices = make([]bool, paletteLen)
	}
	markUsedIndices(storage.indices, storage.bitsPerIndex, usedIndices)

	usedCount := 0
	allUsed := true
	for _, used := range usedIndices {
		if used {
			usedCount++
		} else {
			allUsed = false
		}
	}

	size := paletteSizeFor(usedCount)
	if allUsed && size == storage.palette.size {
		return
	}

	newRuntimeIDs := make([]uint32, 0, usedCount)
	var convBuf [256]uint16
	var conversion []uint16
	if paletteLen <= len(convBuf) {
		conversion = convBuf[:paletteLen]
	} else {
		conversion = make([]uint16, paletteLen)
	}
	for index, used := range usedIndices {
		if used {
			conversion[index] = uint16(len(newRuntimeIDs))
			newRuntimeIDs = append(newRuntimeIDs, storage.palette.values[index])
		}
	}

	var newIndices []uint32
	if size != 0 {
		newIndices = make([]uint32, size.uint32s())
		if storage.bitsPerIndex != 0 {
			repackIndices(newIndices, uint16(size), storage.indices, storage.bitsPerIndex, conversion)
		}
	}
	initPalette(storage.palette, size, newRuntimeIDs)
	storage.initIndices(newIndices)
	storage.remapMetadata(usedIndices, conversion)
}

func (storage *PalettedStorage) remapMetadata(usedIndices []bool, conversion []uint16) {
	if len(storage.missingStateEntries) != 0 {
		entries := make(map[uint16]missingStateEntry, len(storage.missingStateEntries))
		for index, entry := range storage.missingStateEntries {
			if int(index) < len(usedIndices) && usedIndices[index] {
				entries[conversion[index]] = entry
			}
		}
		storage.missingStateEntries = entries
	}
	if len(storage.javaStateIDs) != 0 {
		ids := make(map[uint16]uint32, len(storage.javaStateIDs))
		for index, id := range storage.javaStateIDs {
			if int(index) < len(usedIndices) && usedIndices[index] {
				ids[conversion[index]] = id
			}
		}
		storage.javaStateIDs = ids
	}
}
