package chunk

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"slices"

	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/worldupgrader/blockupgrader"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

type (
	// Encoding is an encoding type used for Chunk encoding. Implementations of this interface are DiskEncoding and
	// NetworkEncoding, which can be used to encode a Chunk to an intermediate disk or network representation respectively.
	Encoding interface {
		encodePalette(buf *bytes.Buffer, p *Palette, e paletteEncoding)
		decodePalette(buf *bytes.Buffer, blockSize paletteSize, e paletteEncoding) (*Palette, error)
		network() byte
	}
	// paletteEncoding is an encoding type used for Chunk encoding. It is used to encode different types of palettes
	// (for example, blocks or biomes) differently.
	paletteEncoding interface {
		encode(buf *bytes.Buffer, v uint32)
		decode(buf *bytes.Buffer) (uint32, error)
	}
)

var (
	// DiskEncoding is the Encoding for writing a Chunk to disk. It writes block palettes using NBT and does not use
	// varints.
	DiskEncoding diskEncoding
	// NetworkEncoding is the Encoding used for sending a Chunk over network. It does not use NBT and writes varints.
	NetworkEncoding networkEncoding
	// BiomePaletteEncoding is the paletteEncoding used for encoding a palette of biomes.
	BiomePaletteEncoding biomePaletteEncoding
	// BiomeIDToRuntimeID maps a biome ID to a runtime ID for network encoding.
	BiomeIDToRuntimeID func(id uint32) (runtimeID uint32, ok bool)
	// BiomeRuntimeIDToID maps a biome runtime ID to a biome ID for network decoding.
	BiomeRuntimeIDToID func(runtimeID uint32) (id uint32, ok bool)
)

// biomePaletteEncoding implements the encoding of biome palettes to disk.
type biomePaletteEncoding struct{}

func (biomePaletteEncoding) encode(buf *bytes.Buffer, v uint32) {
	_ = binary.Write(buf, binary.LittleEndian, v)
}
func (biomePaletteEncoding) decode(buf *bytes.Buffer) (uint32, error) {
	var v uint32
	return v, binary.Read(buf, binary.LittleEndian, &v)
}

// BlockPaletteEncoding implements the encoding of block palettes to disk. It requires a BlockRegistry for converting
// between runtime IDs and block states.
type BlockPaletteEncoding struct {
	Blocks BlockRegistry
}

type missingStateEntry struct {
	properties []string
	state      blockEntry
}

type paletteStateMigration struct {
	index        uint16
	entries      map[uint16]missingStateEntry
	javaStateIDs map[uint16]uint32
	decodeJava   bool
}

type blockPaletteMigrationEncoding struct {
	BlockPaletteEncoding
	migration *paletteStateMigration
	javaIDs   map[uint16]uint32
}

func (bpe BlockPaletteEncoding) encode(buf *bytes.Buffer, v uint32) {
	_ = nbt.NewEncoderWithEncoding(buf, nbt.LittleEndian).Encode(bpe.EncodeBlockState(v))
}
func (bpe BlockPaletteEncoding) decode(buf *bytes.Buffer) (uint32, error) {
	var m map[string]any
	if err := nbt.NewDecoderWithEncoding(buf, nbt.LittleEndian).Decode(&m); err != nil {
		return 0, fmt.Errorf("error decoding block palette entry: %w", err)
	}
	rid, _, _, err := bpe.decodeBlockState(m)
	return rid, err
}

func (bpe blockPaletteMigrationEncoding) encode(buf *bytes.Buffer, v uint32) {
	index := bpe.migration.index
	entry, ok := bpe.migration.entries[index]
	if !ok {
		entry = missingStateEntry{state: bpe.BlockPaletteEncoding.EncodeBlockState(v)}
	}
	native := map[string]any{
		"name": entry.state.Name, "states": entry.state.State, "version": entry.state.Version,
	}
	if id := bpe.javaIDs[index]; id != 0 {
		if name, properties, found := gamedata.BlockStateFromID(id); found {
			native["adamant:java"] = map[string]any{"name": name, "properties": properties}
		}
	}
	_ = nbt.NewEncoderWithEncoding(buf, nbt.LittleEndian).Encode(native)
	bpe.migration.index++
}

func (bpe blockPaletteMigrationEncoding) decode(buf *bytes.Buffer) (uint32, error) {
	index := bpe.migration.index
	var m map[string]any
	if err := nbt.NewDecoderWithEncoding(buf, nbt.LittleEndian).Decode(&m); err != nil {
		return 0, fmt.Errorf("error decoding block palette entry %d: %w", index, err)
	}
	var javaID uint32
	if bpe.migration.decodeJava {
		if extension, exists := m["adamant:java"]; exists {
			javaState, ok := extension.(map[string]any)
			if !ok {
				return 0, fmt.Errorf("invalid adamant:java metadata in block palette entry %d: expected compound", index)
			}
			name, ok := javaState["name"].(string)
			if !ok {
				return 0, fmt.Errorf("invalid adamant:java metadata in block palette entry %d: missing string name", index)
			}
			properties, ok := javaState["properties"].(map[string]any)
			if !ok {
				return 0, fmt.Errorf("invalid adamant:java metadata in block palette entry %d: missing properties compound", index)
			}
			var err error
			javaID, err = gamedata.BlockStateID(name, properties)
			if err != nil {
				return 0, fmt.Errorf("invalid adamant:java metadata in block palette entry %d: %w", index, err)
			}
			delete(m, "adamant:java")
		}
	}
	rid, missing, entry, err := bpe.BlockPaletteEncoding.decodeBlockState(m)
	if err != nil {
		return 0, fmt.Errorf("error decoding block palette entry %d: %w", index, err)
	}
	if len(missing) != 0 {
		if bpe.migration.entries == nil {
			bpe.migration.entries = make(map[uint16]missingStateEntry)
		}
		bpe.migration.entries[index] = missingStateEntry{properties: missing, state: entry}
	}
	if javaID != 0 {
		if bpe.migration.javaStateIDs == nil {
			bpe.migration.javaStateIDs = make(map[uint16]uint32)
		}
		bpe.migration.javaStateIDs[index] = javaID
	}
	bpe.migration.index++
	return rid, nil
}

func (bpe BlockPaletteEncoding) EncodeBlockState(v uint32) blockEntry {
	// Get the block state registered with the runtime IDs we have in the palette of the block storage
	// as we need the name and data value to store.
	name, props, _ := bpe.Blocks.RuntimeIDToState(v)
	return blockEntry{Name: name, State: props, Version: CurrentBlockVersion}
}

func (bpe BlockPaletteEncoding) DecodeBlockState(m map[string]any) (uint32, error) {
	rid, _, _, err := bpe.decodeBlockState(m)
	return rid, err
}

func (bpe BlockPaletteEncoding) decodeBlockState(m map[string]any) (uint32, []string, blockEntry, error) {
	// Decode the name and version of the block entry.
	name, _ := m["name"].(string)
	version, _ := m["version"].(int32)

	// Now check for a state field.
	stateI, ok := m["states"]
	if version < 17694723 {
		// This entry is a pre-1.13 block state, so decode the meta value instead.
		meta, _ := m["val"].(int16)

		// Upgrade the pre-1.13 state into a post-1.13 state.
		state, ok := upgradeLegacyEntry(name, meta)
		if !ok {
			return 0, nil, blockEntry{}, fmt.Errorf("cannot find mapping for legacy block entry: %v, %v", name, meta)
		}

		// Update the name, state, and version.
		name = state.Name
		stateI = state.State
		version = state.Version
	} else if !ok {
		// The state is a post-1.13 block state, but the states field is missing, likely due to a broken world
		// conversion.
		stateI = make(map[string]any)
	}
	state, ok := stateI.(map[string]any)
	if !ok {
		return 0, nil, blockEntry{}, fmt.Errorf("invalid state in block entry")
	}

	// Upgrade the block state if necessary.
	upgraded := blockupgrader.Upgrade(blockupgrader.BlockState{
		Name:       name,
		Properties: state,
		Version:    version,
	})

	v, ok := bpe.Blocks.StateToRuntimeID(upgraded.Name, upgraded.Properties)
	if !ok {
		return 0, nil, blockEntry{}, fmt.Errorf("cannot get runtime ID of block state %v{%+v} %v", upgraded.Name, upgraded.Properties, upgraded.Version)
	}
	_, canonical, found := bpe.Blocks.RuntimeIDToState(v)
	var missing []string
	if found {
		for key := range canonical {
			if _, supplied := upgraded.Properties[key]; !supplied {
				missing = append(missing, key)
			}
		}
		slices.Sort(missing)
	}
	entry := blockEntry{Name: upgraded.Name, State: upgraded.Properties, Version: CurrentBlockVersion}
	return v, missing, entry, nil
}

// diskEncoding implements the Chunk encoding for writing to disk.
type diskEncoding struct{}

func (diskEncoding) network() byte { return 0 }
func (diskEncoding) encodePalette(buf *bytes.Buffer, p *Palette, e paletteEncoding) {
	if p.size != 0 {
		_ = binary.Write(buf, binary.LittleEndian, uint32(p.Len()))
	}
	for _, v := range p.values {
		e.encode(buf, v)
	}
}
func (diskEncoding) decodePalette(buf *bytes.Buffer, blockSize paletteSize, e paletteEncoding) (*Palette, error) {
	paletteCount := uint32(1)
	if blockSize != 0 {
		if err := binary.Read(buf, binary.LittleEndian, &paletteCount); err != nil {
			return nil, fmt.Errorf("error reading palette entry count: %w", err)
		}
	}

	var err error
	palette := newPalette(blockSize, make([]uint32, paletteCount))
	for i := uint32(0); i < paletteCount; i++ {
		palette.values[i], err = e.decode(buf)
		if err != nil {
			return nil, err
		}
	}
	if paletteCount == 0 {
		return palette, fmt.Errorf("invalid palette entry count: found 0, but palette with %v bits per block must have at least 1 value", blockSize)
	}
	return palette, nil
}

// networkEncoding implements the Chunk encoding for sending over network.
// Block palettes hold network block hashes, as BDS sends them with block
// network ID hashes enabled: clients resolve a hash by its state, not by the
// position of the state in their own palette.
type networkEncoding struct{}

func (networkEncoding) network() byte { return 1 }
func (networkEncoding) encodePalette(buf *bytes.Buffer, p *Palette, pe paletteEncoding) {
	if p.size != 0 {
		_ = protocol.WriteVarint32(buf, int32(p.Len()))
	}
	if _, ok := pe.(biomePaletteEncoding); ok && BiomeIDToRuntimeID != nil {
		for _, val := range p.values {
			rid, found := BiomeIDToRuntimeID(val)
			if !found {
				rid = 0
			}
			_ = protocol.WriteVarint32(buf, int32(rid))
		}
		return
	}
	if br, ok := blockPaletteRegistry(pe); ok {
		for _, val := range p.values {
			hash, _ := br.RuntimeIDToHash(val)
			_ = protocol.WriteVarint32(buf, int32(hash))
		}
		return
	}
	for _, val := range p.values {
		_ = protocol.WriteVarint32(buf, int32(val))
	}
}
func (networkEncoding) decodePalette(buf *bytes.Buffer, blockSize paletteSize, pe paletteEncoding) (*Palette, error) {
	var paletteCount int32 = 1
	if blockSize != 0 {
		if err := protocol.Varint32(buf, &paletteCount); err != nil {
			return nil, fmt.Errorf("error reading palette entry count: %w", err)
		}
		if paletteCount <= 0 {
			return nil, fmt.Errorf("invalid palette entry count %v", paletteCount)
		}
	}

	br, blocks := blockPaletteRegistry(pe)
	values, temp := make([]uint32, paletteCount), int32(0)
	for i := range paletteCount {
		if err := protocol.Varint32(buf, &temp); err != nil {
			return nil, fmt.Errorf("error decoding palette entry: %w", err)
		}
		val := uint32(temp)
		if _, ok := pe.(biomePaletteEncoding); ok && BiomeRuntimeIDToID != nil {
			id, found := BiomeRuntimeIDToID(val)
			if found {
				val = id
			} else {
				val = 0
			}
		} else if blocks {
			rid, found := br.HashToRuntimeID(val)
			if !found {
				return nil, fmt.Errorf("unknown network block hash %v", val)
			}
			val = rid
		}
		values[i] = val
	}
	return &Palette{values: values, size: blockSize}, nil
}

// blockPaletteRegistry returns the registry a block palette encoding resolves
// its states with.
func blockPaletteRegistry(pe paletteEncoding) (BlockRegistry, bool) {
	switch pe := pe.(type) {
	case BlockPaletteEncoding:
		return pe.Blocks, pe.Blocks != nil
	case blockPaletteMigrationEncoding:
		return pe.Blocks, pe.Blocks != nil
	}
	return nil, false
}
