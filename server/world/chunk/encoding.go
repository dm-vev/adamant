package chunk

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"slices"

	"github.com/dm-vev/adamant/server/internal/gamedata"
	"github.com/df-mc/worldupgrader/blockupgrader"
	"github.com/dm-vev/adamant/net/nbt"
)

const (
	nbtTagEnd    byte = 0
	nbtTagByte   byte = 1
	nbtTagInt16  byte = 2
	nbtTagInt32  byte = 3
	nbtTagInt64  byte = 4
	nbtTagString byte = 8
	nbtTagStruct byte = 10
)

var (
	fastNBTEntryPrefix  = []byte{nbtTagStruct, 0, 0, nbtTagString, 4, 0, 'n', 'a', 'm', 'e'}
	fastNBTStatesHeader = []byte{nbtTagStruct, 6, 0, 's', 't', 'a', 't', 'e', 's'}
	fastNBTVersionHdr   = []byte{nbtTagEnd, nbtTagInt32, 7, 0, 'v', 'e', 'r', 's', 'i', 'o', 'n'}
)

type (
	// Encoding is an encoding type used for Chunk encoding. Its implementation is DiskEncoding, which encodes a Chunk
	// to the intermediate representation of the world format.
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
	// BiomePaletteEncoding is the paletteEncoding used for encoding a palette of biomes.
	BiomePaletteEncoding biomePaletteEncoding
)

// biomePaletteEncoding implements the encoding of biome palettes to disk.
type biomePaletteEncoding struct{}

func (biomePaletteEncoding) encode(buf *bytes.Buffer, v uint32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	_, _ = buf.Write(b[:])
}
func (biomePaletteEncoding) decode(buf *bytes.Buffer) (uint32, error) {
	b := buf.Next(4)
	if len(b) != 4 {
		return 0, io.ErrUnexpectedEOF
	}
	return binary.LittleEndian.Uint32(b), nil
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
	dec          *nbt.Decoder
	enc          *nbt.Encoder
	scratchProps map[string]any
}

type blockPaletteMigrationEncoding struct {
	BlockPaletteEncoding
	migration *paletteStateMigration
	javaIDs   map[uint16]uint32
}

func (bpe BlockPaletteEncoding) encode(buf *bytes.Buffer, v uint32) {
	entry := bpe.EncodeBlockState(v)
	if writeFastBlockEntry(buf, entry) {
		return
	}
	_ = nbt.NewEncoderWithEncoding(buf, nbt.LittleEndian).Encode(entry)
}
func (bpe BlockPaletteEncoding) decode(buf *bytes.Buffer) (uint32, error) {
	if rid, _, ok := tryDecodeFastBlockEntry(buf, bpe.Blocks, nil); ok {
		return rid, nil
	}
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
		entry = missingStateEntry{state: bpe.EncodeBlockState(v)}
	}
	if bpe.javaIDs[index] == 0 && writeFastBlockEntry(buf, entry.state) {
		bpe.migration.index++
		return
	}
	native := map[string]any{
		"name": entry.state.Name, "states": entry.state.State, "version": entry.state.Version,
	}
	if id := bpe.javaIDs[index]; id != 0 {
		if name, properties, found := gamedata.BlockStateFromID(id); found {
			native["adamant:java"] = map[string]any{"name": name, "properties": properties}
		}
	}
	if bpe.migration.enc == nil {
		bpe.migration.enc = nbt.NewEncoderWithEncoding(buf, nbt.LittleEndian)
	}
	_ = bpe.migration.enc.Encode(native)
	bpe.migration.index++
}

func (bpe blockPaletteMigrationEncoding) decode(buf *bytes.Buffer) (uint32, error) {
	if rid, scratch, ok := tryDecodeFastBlockEntry(buf, bpe.Blocks, bpe.migration.scratchProps); ok {
		bpe.migration.scratchProps = scratch
		bpe.migration.index++
		return rid, nil
	}
	index := bpe.migration.index
	if bpe.migration.dec == nil {
		bpe.migration.dec = nbt.NewDecoderWithEncoding(buf, nbt.LittleEndian)
	}
	var m map[string]any
	if err := bpe.migration.dec.Decode(&m); err != nil {
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
	rid, missing, entry, err := bpe.decodeBlockState(m)
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
	// The upgrade fills these in place: what the saved state lacked of them
	// is to be derived from the neighbours of its blocks.
	lacked := lackedDerivedProperties(name, state)

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
			if _, supplied := upgraded.Properties[key]; !supplied || slices.Contains(lacked, key) {
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
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], uint32(p.Len()))
		_, _ = buf.Write(b[:])
	}
	if bpe, ok := e.(BlockPaletteEncoding); ok {
		var enc *nbt.Encoder
		for _, v := range p.values {
			entry := bpe.EncodeBlockState(v)
			if writeFastBlockEntry(buf, entry) {
				continue
			}
			if enc == nil {
				enc = nbt.NewEncoderWithEncoding(buf, nbt.LittleEndian)
			}
			_ = enc.Encode(entry)
		}
		return
	}
	for _, v := range p.values {
		e.encode(buf, v)
	}
}
func (d diskEncoding) decodePalette(buf *bytes.Buffer, blockSize paletteSize, e paletteEncoding) (*Palette, error) {
	p := &Palette{}
	if err := decodeDiskPaletteInto(buf, blockSize, e, p); err != nil {
		return p, err
	}
	return p, nil
}

func decodeDiskPaletteInto(buf *bytes.Buffer, blockSize paletteSize, e paletteEncoding, p *Palette) error {
	paletteCount := uint32(1)
	if blockSize != 0 {
		b := buf.Next(4)
		if len(b) != 4 {
			return fmt.Errorf("error reading palette entry count: %w", io.ErrUnexpectedEOF)
		}
		paletteCount = binary.LittleEndian.Uint32(b)
	}
	if paletteCount == 0 {
		initPalette(p, blockSize, nil)
		return fmt.Errorf("invalid palette entry count: found 0, but palette with %v bits per block must have at least 1 value", blockSize)
	}

	values := p.values
	if uint32(len(values)) != paletteCount {
		values = make([]uint32, paletteCount)
	}
	initPalette(p, blockSize, values)
	var err error
	for i := uint32(0); i < paletteCount; i++ {
		values[i], err = e.decode(buf)
		if err != nil {
			return err
		}
	}
	return nil
}

func canFastEncodeStates(states map[string]any) bool {
	for k, v := range states {
		if len(k) > math.MaxInt16 {
			return false
		}
		switch tv := v.(type) {
		case uint8, bool, int16, int32, int64:
		case string:
			if len(tv) > math.MaxInt16 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func writeFastNBTString(buf *bytes.Buffer, s string) {
	var lenBuf [2]byte
	binary.LittleEndian.PutUint16(lenBuf[:], uint16(len(s)))
	_, _ = buf.Write(lenBuf[:])
	_, _ = buf.WriteString(s)
}

func writeFastBlockEntry(buf *bytes.Buffer, e blockEntry) bool {
	if len(e.Name) > math.MaxInt16 || !canFastEncodeStates(e.State) {
		return false
	}
	_, _ = buf.Write(fastNBTEntryPrefix)
	writeFastNBTString(buf, e.Name)
	_, _ = buf.Write(fastNBTStatesHeader)
	var num [8]byte
	for k, v := range e.State {
		switch tv := v.(type) {
		case uint8:
			_ = buf.WriteByte(nbtTagByte)
			writeFastNBTString(buf, k)
			_ = buf.WriteByte(tv)
		case bool:
			_ = buf.WriteByte(nbtTagByte)
			writeFastNBTString(buf, k)
			if tv {
				_ = buf.WriteByte(1)
			} else {
				_ = buf.WriteByte(0)
			}
		case int16:
			_ = buf.WriteByte(nbtTagInt16)
			writeFastNBTString(buf, k)
			binary.LittleEndian.PutUint16(num[:2], uint16(tv))
			_, _ = buf.Write(num[:2])
		case int32:
			_ = buf.WriteByte(nbtTagInt32)
			writeFastNBTString(buf, k)
			binary.LittleEndian.PutUint32(num[:4], uint32(tv))
			_, _ = buf.Write(num[:4])
		case int64:
			_ = buf.WriteByte(nbtTagInt64)
			writeFastNBTString(buf, k)
			binary.LittleEndian.PutUint64(num[:8], uint64(tv))
			_, _ = buf.Write(num[:8])
		case string:
			_ = buf.WriteByte(nbtTagString)
			writeFastNBTString(buf, k)
			writeFastNBTString(buf, tv)
		}
	}
	_, _ = buf.Write(fastNBTVersionHdr)
	binary.LittleEndian.PutUint32(num[:4], uint32(e.Version))
	num[4] = nbtTagEnd
	_, _ = buf.Write(num[:5])
	return true
}

func readFastNBTStringBytes(data []byte, pos int) ([]byte, int, bool) {
	if pos+2 > len(data) {
		return nil, 0, false
	}
	n := int(int16(binary.LittleEndian.Uint16(data[pos : pos+2])))
	pos += 2
	if n < 0 || pos+n > len(data) {
		return nil, 0, false
	}
	return data[pos : pos+n], pos + n, true
}

func parseFastNBTStates(data []byte, pos int, scratch map[string]any) (int, map[string]any, int, bool) {
	if pos >= len(data) {
		return 0, scratch, 0, false
	}
	if data[pos] == nbtTagEnd {
		return pos + 1, scratch, 0, true
	}
	if scratch == nil {
		scratch = make(map[string]any, 4)
	} else {
		clear(scratch)
	}
	count := 0
	for {
		if pos >= len(data) {
			return 0, scratch, 0, false
		}
		tag := data[pos]
		pos++
		if tag == nbtTagEnd {
			return pos, scratch, count, true
		}
		keyBytes, nextPos, ok := readFastNBTStringBytes(data, pos)
		if !ok {
			return 0, scratch, 0, false
		}
		pos = nextPos
		var val any
		switch tag {
		case nbtTagByte:
			if pos >= len(data) {
				return 0, scratch, 0, false
			}
			val = data[pos]
			pos++
		case nbtTagInt16:
			if pos+2 > len(data) {
				return 0, scratch, 0, false
			}
			val = int16(binary.LittleEndian.Uint16(data[pos : pos+2]))
			pos += 2
		case nbtTagInt32:
			if pos+4 > len(data) {
				return 0, scratch, 0, false
			}
			val = int32(binary.LittleEndian.Uint32(data[pos : pos+4]))
			pos += 4
		case nbtTagInt64:
			if pos+8 > len(data) {
				return 0, scratch, 0, false
			}
			val = int64(binary.LittleEndian.Uint64(data[pos : pos+8]))
			pos += 8
		case nbtTagString:
			sBytes, np, ok := readFastNBTStringBytes(data, pos)
			if !ok {
				return 0, scratch, 0, false
			}
			val = string(sBytes)
			pos = np
		default:
			return 0, scratch, 0, false
		}
		key := string(keyBytes)
		if _, dup := scratch[key]; dup {
			return 0, scratch, 0, false
		}
		scratch[key] = val
		count++
	}
}

func tryDecodeFastBlockEntry(buf *bytes.Buffer, blocks BlockRegistry, scratch map[string]any) (uint32, map[string]any, bool) {
	data := buf.Bytes()
	if len(data) < 4 || data[0] != nbtTagStruct {
		return 0, scratch, false
	}
	_, pos, ok := readFastNBTStringBytes(data, 1)
	if !ok {
		return 0, scratch, false
	}
	var (
		nameBytes  []byte
		version    int32
		stateCount int
		hasName    bool
		hasVersion bool
		hasStates  bool
	)
	for {
		if pos >= len(data) {
			return 0, scratch, false
		}
		tag := data[pos]
		pos++
		if tag == nbtTagEnd {
			break
		}
		keyBytes, nextPos, ok := readFastNBTStringBytes(data, pos)
		if !ok {
			return 0, scratch, false
		}
		pos = nextPos
		switch string(keyBytes) {
		case "name":
			if tag != nbtTagString || hasName {
				return 0, scratch, false
			}
			nameBytes, pos, ok = readFastNBTStringBytes(data, pos)
			if !ok {
				return 0, scratch, false
			}
			hasName = true
		case "version":
			if tag != nbtTagInt32 || hasVersion || pos+4 > len(data) {
				return 0, scratch, false
			}
			version = int32(binary.LittleEndian.Uint32(data[pos : pos+4]))
			pos += 4
			hasVersion = true
		case "states":
			if tag != nbtTagStruct || hasStates {
				return 0, scratch, false
			}
			pos, scratch, stateCount, ok = parseFastNBTStates(data, pos, scratch)
			if !ok {
				return 0, scratch, false
			}
			hasStates = true
		default:
			return 0, scratch, false
		}
	}
	if !hasName || !hasVersion || !hasStates || version != CurrentBlockVersion {
		return 0, scratch, false
	}
	if len(derivedProperties[string(nameBytes)]) != 0 {
		return 0, scratch, false
	}
	var props map[string]any
	if stateCount != 0 {
		props = scratch
	}
	rid, ok := blocks.StateToRuntimeID(string(nameBytes), props)
	if !ok {
		return 0, scratch, false
	}
	_, canonical, found := blocks.RuntimeIDToState(rid)
	if found {
		if len(canonical) != stateCount {
			return 0, scratch, false
		}
		for key := range canonical {
			if _, supplied := props[key]; !supplied {
				return 0, scratch, false
			}
		}
	}
	buf.Next(pos)
	return rid, scratch, true
}
