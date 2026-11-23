package chunk

import "slices"

// SubChunk is a cube of blocks located in a chunk. It has a size of 16x16x16 blocks and forms part of a stack
// that forms a Chunk.
type SubChunk struct {
	air        uint32
	storages   []*PalettedStorage
	blockLight []uint8
	skyLight   []uint8
}

// Equals returns if the sub chunk passed is equal to the current one.
func (sub *SubChunk) Equals(s *SubChunk) bool {
	if s.air != sub.air || len(s.storages) != len(sub.storages) {
		return false
	}

	for i, st := range s.storages {
		if !st.Equal(sub.storages[i]) {
			return false
		}
	}

	return true
}

// NewSubChunk creates a new sub chunk. All sub chunks should be created through this function.
func NewSubChunk(air uint32) *SubChunk {
	return &SubChunk{air: air}
}

// Clone returns an independent copy of the SubChunk.
func (sub *SubChunk) Clone() *SubChunk {
	clone := &SubChunk{}
	sub.cloneInto(clone)
	return clone
}

func (sub *SubChunk) cloneInto(dst *SubChunk) {
	dst.air = sub.air
	if len(sub.storages) != 0 {
		dst.storages = make([]*PalettedStorage, len(sub.storages))
		for i, storage := range sub.storages {
			dst.storages[i] = storage.Clone()
		}
	}
	dst.blockLight = cloneLight(sub.blockLight)
	dst.skyLight = cloneLight(sub.skyLight)
}

// cloneBlocks returns a copy of the sub-chunk with its blocks and no light,
// sharing the read-only array of no light.
func (sub *SubChunk) cloneBlocksInto(dst *SubChunk) {
	dst.air = sub.air
	if len(sub.storages) != 0 {
		dst.storages = make([]*PalettedStorage, len(sub.storages))
		for i, storage := range sub.storages {
			dst.storages[i] = storage.Clone()
		}
	}
	dst.blockLight = noLight
	dst.skyLight = noLight
}

func cloneLight(light []uint8) []uint8 {
	if len(light) == 0 {
		return nil
	}
	switch &light[0] {
	case noLightPtr:
		return noLight
	case fullLightPtr:
		return fullLight
	default:
		return slices.Clone(light)
	}
}

// Empty checks if the SubChunk is considered empty. This is the case if the SubChunk has 0 block storages or if it has
// a single one that is completely filled with air.
func (sub *SubChunk) Empty() bool {
	return len(sub.storages) == 0 || (len(sub.storages) == 1 && len(sub.storages[0].palette.values) == 1 && sub.storages[0].palette.values[0] == sub.air)
}

// Layer returns a certain block storage/layer from a sub chunk. If no storage at the layer exists, the layer
// is created, as well as all layers between the current highest layer and the new highest layer.
func (sub *SubChunk) Layer(layer uint8) *PalettedStorage {
	for uint8(len(sub.storages)) <= layer {
		// Keep appending to storages until the requested layer is achieved. Makes working with new layers
		// much easier.
		sub.storages = append(sub.storages, emptyStorage(sub.air))
	}
	return sub.storages[layer]
}

// Layers returns all layers in the sub chunk. This method may also return an empty slice.
func (sub *SubChunk) Layers() []*PalettedStorage {
	return sub.storages
}

// Block returns the runtime ID of the block located at the given X, Y and Z. X, Y and Z must be in a
// range of 0-15.
func (sub *SubChunk) Block(x, y, z byte, layer uint8) uint32 {
	if uint8(len(sub.storages)) <= layer {
		return sub.air
	}
	return sub.storages[layer].At(x, y, z)
}

// SetBlock sets the given block runtime ID at the given X, Y and Z. X, Y and Z must be in a range of 0-15.
func (sub *SubChunk) SetBlock(x, y, z byte, layer uint8, block uint32) {
	sub.Layer(layer).Set(x, y, z, block)
}

// Fill fills the inclusive local box [x0..x1, y0..y1, z0..z1] (each 0..15) in
// layer 0 with rid, clearing any layer-1 blocks in the box, and returns the
// number of cells whose state changed.
func (sub *SubChunk) Fill(x0, y0, z0, x1, y1, z1 byte, rid uint32) int {
	if x0 == 0 && y0 == 0 && z0 == 0 && x1 == 15 && y1 == 15 && z1 == 15 {
		return sub.fillFull(rid)
	}
	if len(sub.storages) == 0 && rid == sub.air {
		return 0
	}
	if len(sub.storages) == 1 && sub.storages[0].bitsPerIndex == 0 && sub.storages[0].palette.values[0] == rid && len(sub.storages[0].javaStateIDs) == 0 {
		return 0
	}
	layer0 := sub.Layer(0)
	var layer1 *PalettedStorage
	if len(sub.storages) > 1 {
		layer1 = sub.storages[1]
	}
	changed := 0
	if len(layer0.javaStateIDs) == 0 && len(layer0.missingStateEntries) == 0 {
		idx := layer0.palette.Index(rid)
		if idx == -1 {
			idx = layer0.addNew(rid)
		}
		target := uint16(idx)
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				for y := y0; y <= y1; y++ {
					diff := layer0.paletteIndex(x, y, z) != target
					if diff {
						layer0.setPaletteIndex(x, y, z, target)
					}
					if layer1 != nil && layer1.At(x, y, z) != sub.air {
						layer1.Set(x, y, z, sub.air)
						diff = true
					}
					if diff {
						changed++
					}
				}
			}
		}
		return changed
	}
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			for y := y0; y <= y1; y++ {
				diff := layer0.At(x, y, z) != rid || layer0.StateID(x, y, z) != 0
				layer0.Set(x, y, z, rid)
				if layer1 != nil && layer1.At(x, y, z) != sub.air {
					layer1.Set(x, y, z, sub.air)
					diff = true
				}
				if diff {
					changed++
				}
			}
		}
	}
	return changed
}

func (sub *SubChunk) fillFull(rid uint32) int {
	if len(sub.storages) == 0 {
		if rid == sub.air {
			return 0
		}
		sub.storages = []*PalettedStorage{emptyStorage(rid)}
		return 4096
	}
	if len(sub.storages) == 1 && sub.storages[0].bitsPerIndex == 0 {
		if sub.storages[0].palette.values[0] == rid {
			return 0
		}
		if rid == sub.air {
			sub.storages = nil
		} else {
			sub.storages[0] = emptyStorage(rid)
		}
		return 4096
	}
	changed := 0
	layer0 := sub.storages[0]
	var layer1 *PalettedStorage
	if len(sub.storages) > 1 {
		layer1 = sub.storages[1]
	}
	idx := layer0.palette.Index(rid)
	hasMeta := len(layer0.javaStateIDs) != 0 || len(layer0.missingStateEntries) != 0
	if idx == -1 && !hasMeta && layer1 == nil {
		changed = 4096
	} else {
		for x := byte(0); x < 16; x++ {
			for z := byte(0); z < 16; z++ {
				for y := byte(0); y < 16; y++ {
					if layer0.At(x, y, z) != rid || (layer1 != nil && layer1.At(x, y, z) != sub.air) || (hasMeta && layer0.StateID(x, y, z) != 0) {
						changed++
					}
				}
			}
		}
	}
	if rid == sub.air {
		sub.storages = nil
	} else {
		sub.storages = []*PalettedStorage{emptyStorage(rid)}
	}
	return changed
}

// SetBlockLight sets the block light value at a specific position in the sub chunk.
func (sub *SubChunk) SetBlockLight(x, y, z byte, level uint8) {
	if len(sub.blockLight) == 0 || &sub.blockLight[0] == noLightPtr || &sub.blockLight[0] == fullLightPtr {
		if !sub.ensureBlockLightWritable(level) {
			return
		}
	}
	index := (uint16(x) << 8) | (uint16(z) << 4) | uint16(y)

	i := index >> 1
	bit := (index & 1) << 2
	sub.blockLight[i] = (sub.blockLight[i] & (0xf0 >> bit)) | (level << bit)
}

func (sub *SubChunk) ensureBlockLightWritable(level uint8) bool {
	if len(sub.blockLight) == 0 || &sub.blockLight[0] == noLightPtr {
		if level == 0 {
			return false
		}
		sub.blockLight = make([]uint8, 2048)
		return true
	}
	if &sub.blockLight[0] == fullLightPtr {
		if level == 15 {
			return false
		}
		sub.blockLight = slices.Clone(fullLight)
	}
	return true
}

// BlockLight returns the block light at a position. Unlit generation subchunks
// have no light storage and report zero until lighting is initialised.
func (sub *SubChunk) BlockLight(x, y, z byte) uint8 {
	if len(sub.blockLight) == 0 {
		return 0
	}
	index := (uint16(x) << 8) | (uint16(z) << 4) | uint16(y)
	return (sub.blockLight[index>>1] >> ((index & 1) << 2)) & 0xf
}

// SetSkyLight sets the skylight value at a specific position in the sub chunk.
func (sub *SubChunk) SetSkyLight(x, y, z byte, level uint8) {
	if len(sub.skyLight) == 0 || &sub.skyLight[0] == noLightPtr || &sub.skyLight[0] == fullLightPtr {
		if !sub.ensureSkyLightWritable(level) {
			return
		}
	}
	index := (uint16(x) << 8) | (uint16(z) << 4) | uint16(y)

	i := index >> 1
	bit := (index & 1) << 2
	sub.skyLight[i] = (sub.skyLight[i] & (0xf0 >> bit)) | (level << bit)
}

func (sub *SubChunk) ensureSkyLightWritable(level uint8) bool {
	if len(sub.skyLight) == 0 || &sub.skyLight[0] == noLightPtr {
		if level == 0 {
			return false
		}
		sub.skyLight = make([]uint8, 2048)
		return true
	}
	if &sub.skyLight[0] == fullLightPtr {
		if level == 15 {
			return false
		}
		sub.skyLight = slices.Clone(fullLight)
	}
	return true
}

// SkyLight returns the skylight value at a specific value at a specific position in the sub chunk.
func (sub *SubChunk) SkyLight(x, y, z byte) uint8 {
	if len(sub.skyLight) == 0 {
		return 0
	}
	index := (uint16(x) << 8) | (uint16(z) << 4) | uint16(y)
	return (sub.skyLight[index>>1] >> ((index & 1) << 2)) & 0xf
}

// Compact cleans the garbage from all block storages that sub chunk contains, so that they may be
// cleanly written to a database.
func (sub *SubChunk) compact() {
	if len(sub.storages) != 0 {
		write := 0
		for _, storage := range sub.storages {
			storage.compact()
			if len(storage.palette.values) == 1 && storage.palette.values[0] == sub.air {
				// If the palette has only air in it, it means the storage is empty, so we can ignore it.
				continue
			}
			sub.storages[write] = storage
			write++
		}
		if write == 0 {
			sub.storages = nil
		} else if write < len(sub.storages) {
			clear(sub.storages[write:])
			sub.storages = sub.storages[:write]
		}
	}
	sub.skyLight = sharedLight(sub.skyLight, true)
	sub.blockLight = sharedLight(sub.blockLight, true)
}
