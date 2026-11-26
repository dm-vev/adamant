// Package structure decodes and transforms Java world-generation structure templates.
package structure

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strconv"
	"strings"
	"sync"

	"github.com/dm-vev/adamant/server/internal/gamedata"
	"github.com/dm-vev/adamant/server/internal/vanilla"
	"github.com/dm-vev/adamant/server/world"
	"github.com/google/uuid"
)

// LiquidSettings controls ambient-fluid preservation during template placement.
type LiquidSettings uint8

const (
	ApplyWaterlogging LiquidSettings = iota
	IgnoreWaterlogging
)

// ParseLiquidSettings decodes the source codec, including its default.
func ParseLiquidSettings(value any) (LiquidSettings, error) {
	switch value {
	case nil, "apply_waterlogging":
		return ApplyWaterlogging, nil
	case "ignore_waterlogging":
		return IgnoreWaterlogging, nil
	default:
		return ApplyWaterlogging, fmt.Errorf("invalid liquid settings %v", value)
	}
}

// Block is a palette state and a template-relative position. NBT is retained verbatim.
// NativeBlock carries Java state properties stored in native block actors, or an entity adapted to a native block.
type Block struct {
	Pos [3]int32
	// Placement-only batch header. A flattened list pool element starts a new
	// fluid pass here; LiquidSettings applies until the next TemplateStart.
	TemplateStart  bool
	LiquidSettings LiquidSettings
	State          world.BlockState
	NBT            map[string]any
	NativeBlock    world.Block
}

// Entity is an entity record from a Java structure template.
type Entity struct {
	Pos         [3]float64
	BlockPos    [3]int32
	NBT         map[string]any
	Record      int32
	Seed        int64
	SpawnReason string
	// ID preserves the fresh identity allocated by Source-style mob creation.
	// Template descriptors without it use their deterministic placement key.
	ID uuid.UUID
	// Prepared spawns carry complete Source state and their independent RNG.
	SpawnFinalized bool
	EntityRandom   *vanilla.Random
	// Passengers are absolute placement descriptors installed after this vehicle.
	Passengers []Entity
}

// Template is an immutable decoded Java structure template.
type Template struct {
	Size     [3]int32
	Palettes [][]Block
	Entities []Entity
}

// Load loads a Java structure by its path below data/minecraft/structure, without the .nbt suffix.
func Load(name string) (*Template, error) {
	return loadTemplate(name, false)
}

// LoadOrCreate matches StructureTemplateManager.getOrCreate for a missing resource: Java
// creates a blank template with zero size and no palettes or entities.
func LoadOrCreate(name string) (*Template, error) {
	return loadTemplate(name, true)
}

func loadTemplate(name string, createMissing bool) (*Template, error) {
	name = strings.TrimPrefix(name, "/")
	if strings.Contains(name, "..") || strings.HasSuffix(name, ".nbt") {
		return nil, fmt.Errorf("structure: invalid name %q", name)
	}
	template, err := defaultTemplateCache.load(name, func() (*Template, error) {
		return decodeEmbeddedTemplate(name)
	})
	if err != nil {
		if createMissing && errors.Is(err, fs.ErrNotExist) {
			return &Template{}, nil
		}
		return nil, err
	}
	return template, nil
}

type rawTemplateBlock struct {
	pos      [3]int32
	state    int32
	hasPos   bool
	hasState bool
	nbt      map[string]any
}

type templateDecodeScratch struct {
	br        bytes.Reader
	gz        *gzip.Reader
	buf       bytes.Buffer
	rawBlocks []rawTemplateBlock
}

const (
	maxTemplateDecodeScratch = 8
	maxScratchBufCap         = 1 << 20
	maxScratchRawBlocksCap   = 65536
)

var freeTemplateDecodeScratch struct {
	sync.Mutex
	items []*templateDecodeScratch
}

func acquireTemplateDecodeScratch() *templateDecodeScratch {
	freeTemplateDecodeScratch.Lock()
	if n := len(freeTemplateDecodeScratch.items); n > 0 {
		s := freeTemplateDecodeScratch.items[n-1]
		freeTemplateDecodeScratch.items[n-1] = nil
		freeTemplateDecodeScratch.items = freeTemplateDecodeScratch.items[:n-1]
		freeTemplateDecodeScratch.Unlock()
		return s
	}
	freeTemplateDecodeScratch.Unlock()
	return &templateDecodeScratch{}
}

func releaseTemplateDecodeScratch(s *templateDecodeScratch) {
	if s.buf.Cap() > maxScratchBufCap {
		s.buf = bytes.Buffer{}
	} else {
		s.buf.Reset()
	}
	clear(s.rawBlocks)
	if cap(s.rawBlocks) > maxScratchRawBlocksCap {
		s.rawBlocks = nil
	} else {
		s.rawBlocks = s.rawBlocks[:0]
	}
	s.br.Reset(nil)
	freeTemplateDecodeScratch.Lock()
	if len(freeTemplateDecodeScratch.items) < maxTemplateDecodeScratch {
		freeTemplateDecodeScratch.items = append(freeTemplateDecodeScratch.items, s)
	}
	freeTemplateDecodeScratch.Unlock()
}

func decodeEmbeddedTemplate(name string) (*Template, error) {
	data, err := gamedata.Assets().ReadFile("assets/data/minecraft/structure/" + name + ".nbt")
	if err != nil {
		return nil, fmt.Errorf("structure %q: %w", name, err)
	}
	scratch := acquireTemplateDecodeScratch()
	defer releaseTemplateDecodeScratch(scratch)
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		scratch.br.Reset(data)
		if scratch.gz == nil {
			scratch.gz, err = gzip.NewReader(&scratch.br)
		} else {
			err = scratch.gz.Reset(&scratch.br)
		}
		if err != nil {
			scratch.gz = nil
			return nil, fmt.Errorf("structure %q gzip: %w", name, err)
		}
		scratch.buf.Reset()
		_, err = io.Copy(&scratch.buf, scratch.gz)
		closeErr := scratch.gz.Close()
		if err != nil {
			return nil, fmt.Errorf("structure %q gzip: %w", name, err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("structure %q gzip: %w", name, closeErr)
		}
		data = scratch.buf.Bytes()
	}
	template, err := decodeTemplateNBT(data, scratch)
	if err != nil {
		return nil, fmt.Errorf("structure %q: %w", name, err)
	}
	return template, nil
}

func decodeTemplateNBT(data []byte, scratch *templateDecodeScratch) (*Template, error) {
	r := nbtReader{buf: data}
	rootType, err := r.readByte()
	if err != nil {
		return nil, fmt.Errorf("NBT: %w", err)
	}
	if rootType != nbtTagCompound {
		return nil, fmt.Errorf("NBT: nbt: root tag type %d, expected compound", rootType)
	}
	if _, err := r.readStringBytes(); err != nil {
		return nil, fmt.Errorf("NBT: %w", err)
	}
	var (
		t                                             = &Template{}
		hasSize, hasBlocks, hasSinglePal, hasMultiPal bool
		singlePal                                     []world.BlockState
		multiPal                                      [][]world.BlockState
		rawBlocks                                     []rawTemplateBlock
	)
	if scratch != nil {
		rawBlocks = scratch.rawBlocks[:0]
	}
	for {
		tagType, err := r.readByte()
		if err != nil {
			return nil, fmt.Errorf("NBT: %w", err)
		}
		if tagType == nbtTagEnd {
			break
		}
		field, err := r.readStringBytes()
		if err != nil {
			return nil, fmt.Errorf("NBT: %w", err)
		}
		switch string(field) {
		case "size":
			if err := r.readInt32List3(tagType, &t.Size); err != nil {
				return nil, fmt.Errorf("size: %w", err)
			}
			hasSize = true
		case "palette":
			if singlePal, err = r.readPaletteStates(tagType, 0); err != nil {
				return nil, err
			}
			hasSinglePal = true
		case "palettes":
			if multiPal, err = r.readMultiPalettes(tagType); err != nil {
				return nil, err
			}
			hasMultiPal = true
		case "blocks":
			if rawBlocks, err = r.readRawBlocks(tagType, rawBlocks); err != nil {
				return nil, err
			}
			if scratch != nil {
				scratch.rawBlocks = rawBlocks
			}
			hasBlocks = true
		case "entities":
			if t.Entities, err = r.readTemplateEntities(tagType); err != nil {
				return nil, err
			}
		default:
			if err := r.skipPayload(tagType, 1); err != nil {
				return nil, fmt.Errorf("NBT: %w", err)
			}
		}
	}
	if !hasSize {
		return nil, fmt.Errorf("size: missing")
	}
	var palettes [][]world.BlockState
	switch {
	case hasMultiPal:
		palettes = multiPal
	case hasSinglePal:
		palettes = [][]world.BlockState{singlePal}
	default:
		return nil, fmt.Errorf("missing palettes")
	}
	if !hasBlocks {
		return nil, fmt.Errorf("palette 0: missing blocks")
	}
	t.Palettes = make([][]Block, len(palettes))
	for pi, states := range palettes {
		blocks, err := buildBlocksFromRaw(rawBlocks, states)
		if err != nil {
			return nil, fmt.Errorf("palette %d: %w", pi, err)
		}
		t.Palettes[pi] = blocks
	}
	return t, nil
}

func (r *nbtReader) readIntTag(tagType byte) (int64, bool, error) {
	switch tagType {
	case nbtTagByte:
		v, err := r.readByte()
		return int64(int8(v)), true, err
	case nbtTagShort:
		v, err := r.readInt16()
		return int64(v), true, err
	case nbtTagInt:
		v, err := r.readInt32()
		return int64(v), true, err
	case nbtTagLong:
		v, err := r.readInt64()
		return v, true, err
	default:
		return 0, false, nil
	}
}

func (r *nbtReader) readInt32List3(tagType byte, out *[3]int32) error {
	if tagType != nbtTagList {
		return fmt.Errorf("expected list tag, got %d", tagType)
	}
	elemType, err := r.readByte()
	if err != nil {
		return err
	}
	length, err := r.readInt32()
	if err != nil {
		return err
	}
	if length != 3 {
		return fmt.Errorf("expected three-element list, got length %d", length)
	}
	for i := range 3 {
		v, ok, err := r.readIntTag(elemType)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("element %d has tag type %d", i, elemType)
		}
		out[i] = int32(v)
	}
	return nil
}

func (r *nbtReader) readFloat64List3(tagType byte, out *[3]float64) error {
	if tagType != nbtTagList {
		return fmt.Errorf("expected list tag, got %d", tagType)
	}
	elemType, err := r.readByte()
	if err != nil {
		return err
	}
	length, err := r.readInt32()
	if err != nil {
		return err
	}
	if length != 3 {
		return fmt.Errorf("expected three-element list, got length %d", length)
	}
	for i := range 3 {
		switch elemType {
		case nbtTagFloat:
			v, err := r.readFloat32()
			if err != nil {
				return err
			}
			out[i] = float64(v)
		case nbtTagDouble:
			v, err := r.readFloat64()
			if err != nil {
				return err
			}
			out[i] = v
		default:
			return fmt.Errorf("element %d has tag type %d", i, elemType)
		}
	}
	return nil
}

func (r *nbtReader) readMultiPalettes(tagType byte) ([][]world.BlockState, error) {
	if tagType != nbtTagList {
		return nil, fmt.Errorf("palettes has tag type %d", tagType)
	}
	elemType, err := r.readByte()
	if err != nil {
		return nil, err
	}
	count, err := r.readInt32()
	if err != nil {
		return nil, err
	}
	if count < 0 {
		return nil, fmt.Errorf("nbt: negative list length %d", count)
	}
	if elemType == nbtTagEnd {
		if count != 0 {
			return nil, fmt.Errorf("nbt: non-empty list with end element type")
		}
		return make([][]world.BlockState, 0), nil
	}
	if elemType != nbtTagList {
		return nil, fmt.Errorf("palettes element has tag type %d", elemType)
	}
	out := make([][]world.BlockState, count)
	for pi := range int(count) {
		states, err := r.readPaletteStates(nbtTagList, pi)
		if err != nil {
			return nil, err
		}
		out[pi] = states
	}
	return out, nil
}

func (r *nbtReader) readPaletteStates(tagType byte, pi int) ([]world.BlockState, error) {
	if tagType != nbtTagList {
		return nil, fmt.Errorf("palette %d has tag type %d", pi, tagType)
	}
	elemType, err := r.readByte()
	if err != nil {
		return nil, err
	}
	count, err := r.readInt32()
	if err != nil {
		return nil, err
	}
	if count < 0 {
		return nil, fmt.Errorf("nbt: negative list length %d", count)
	}
	if elemType == nbtTagEnd {
		if count != 0 {
			return nil, fmt.Errorf("nbt: non-empty list with end element type")
		}
		return make([]world.BlockState, 0), nil
	}
	if elemType != nbtTagCompound {
		return nil, fmt.Errorf("palette %d state has tag type %d", pi, elemType)
	}
	states := make([]world.BlockState, count)
	for i := range int(count) {
		st, err := r.readPaletteState(pi, i)
		if err != nil {
			return nil, err
		}
		states[i] = st
	}
	return states, nil
}

func (r *nbtReader) readPaletteState(pi, idx int) (world.BlockState, error) {
	var (
		nameCap, nameLow, nameID       string
		hasCap, hasLow, hasID          bool
		propsCap, propsLow             map[string]any
		hasPropsCap, hasPropsLow       bool
	)
	for {
		t, err := r.readByte()
		if err != nil {
			return world.BlockState{}, err
		}
		if t == nbtTagEnd {
			break
		}
		field, err := r.readStringBytes()
		if err != nil {
			return world.BlockState{}, err
		}
		switch string(field) {
		case "Name":
			if t == nbtTagString {
				if nameCap, err = r.readString(); err != nil {
					return world.BlockState{}, err
				}
				hasCap = true
				continue
			}
		case "name":
			if t == nbtTagString {
				if nameLow, err = r.readString(); err != nil {
					return world.BlockState{}, err
				}
				hasLow = true
				continue
			}
		case "id":
			if t == nbtTagString {
				if nameID, err = r.readString(); err != nil {
					return world.BlockState{}, err
				}
				hasID = true
				continue
			}
		case "Properties":
			if t == nbtTagCompound {
				if propsCap, err = r.readDecodedProperties(3); err != nil {
					return world.BlockState{}, err
				}
				hasPropsCap = true
				continue
			}
		case "properties":
			if t == nbtTagCompound {
				if propsLow, err = r.readDecodedProperties(3); err != nil {
					return world.BlockState{}, err
				}
				hasPropsLow = true
				continue
			}
		}
		if err := r.skipPayload(t, 3); err != nil {
			return world.BlockState{}, err
		}
	}
	var name string
	switch {
	case hasCap:
		name = nameCap
	case hasLow:
		name = nameLow
	case hasID:
		name = nameID
	default:
		return world.BlockState{}, fmt.Errorf("palette %d state %d has no name or id", pi, idx)
	}
	var props map[string]any
	if hasPropsCap {
		props = propsCap
	} else if hasPropsLow {
		props = propsLow
	}
	return world.BlockState{Name: name, Properties: props}, nil
}

func (r *nbtReader) readDecodedProperties(depth int) (map[string]any, error) {
	var props map[string]any
	for {
		t, err := r.readByte()
		if err != nil {
			return nil, err
		}
		if t == nbtTagEnd {
			return props, nil
		}
		key, err := r.readTagName()
		if err != nil {
			return nil, err
		}
		var val any
		if t == nbtTagString {
			text, err := r.readString()
			if err != nil {
				return nil, err
			}
			val = decodeStatePropertyString(text)
		} else {
			val, err = r.readPayload(t, depth)
			if err != nil {
				return nil, err
			}
		}
		if props == nil {
			props = make(map[string]any, 4)
		}
		props[key] = val
	}
}

func decodeStatePropertyString(text string) any {
	switch text {
	case "true":
		return true
	case "false":
		return false
	default:
		if len(text) > 0 {
			c := text[0]
			if c == '-' || c == '+' || (c >= '0' && c <= '9') {
				if n, err := strconv.ParseInt(text, 10, 32); err == nil {
					return int32(n)
				}
			}
		}
		return text
	}
}

func (r *nbtReader) readRawBlocks(tagType byte, dst []rawTemplateBlock) ([]rawTemplateBlock, error) {
	if tagType != nbtTagList {
		return nil, fmt.Errorf("blocks has tag type %d", tagType)
	}
	elemType, err := r.readByte()
	if err != nil {
		return nil, err
	}
	count, err := r.readInt32()
	if err != nil {
		return nil, err
	}
	if count < 0 {
		return nil, fmt.Errorf("nbt: negative list length %d", count)
	}
	if elemType == nbtTagEnd {
		if count != 0 {
			return nil, fmt.Errorf("nbt: non-empty list with end element type")
		}
		return dst[:0], nil
	}
	if elemType != nbtTagCompound {
		return nil, fmt.Errorf("blocks element has tag type %d", elemType)
	}
	n := int(count)
	if cap(dst) < n {
		dst = make([]rawTemplateBlock, n)
	} else {
		dst = dst[:n]
	}
	for i := range n {
		if err := r.readRawBlock(&dst[i], i); err != nil {
			return nil, err
		}
	}
	return dst, nil
}

func (r *nbtReader) readRawBlock(dst *rawTemplateBlock, idx int) error {
	*dst = rawTemplateBlock{}
	for {
		t, err := r.readByte()
		if err != nil {
			return err
		}
		if t == nbtTagEnd {
			return nil
		}
		field, err := r.readStringBytes()
		if err != nil {
			return err
		}
		switch string(field) {
		case "pos":
			if err := r.readInt32List3(t, &dst.pos); err != nil {
				return fmt.Errorf("block %d pos: %w", idx, err)
			}
			dst.hasPos = true
		case "state":
			v, ok, err := r.readIntTag(t)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("block %d invalid state tag %d", idx, t)
			}
			dst.state = int32(v)
			dst.hasState = true
		case "nbt":
			if t != nbtTagCompound {
				return fmt.Errorf("block %d NBT has tag type %d", idx, t)
			}
			nbt, err := r.readCompound(3)
			if err != nil {
				return err
			}
			dst.nbt = nbt
		default:
			if err := r.skipPayload(t, 3); err != nil {
				return err
			}
		}
	}
}

func (r *nbtReader) readTemplateEntities(tagType byte) ([]Entity, error) {
	if tagType != nbtTagList {
		return nil, fmt.Errorf("entities has tag type %d", tagType)
	}
	elemType, err := r.readByte()
	if err != nil {
		return nil, err
	}
	count, err := r.readInt32()
	if err != nil {
		return nil, err
	}
	if count < 0 {
		return nil, fmt.Errorf("nbt: negative list length %d", count)
	}
	if elemType == nbtTagEnd {
		if count != 0 {
			return nil, fmt.Errorf("nbt: non-empty list with end element type")
		}
		return make([]Entity, 0), nil
	}
	if elemType != nbtTagCompound {
		return nil, fmt.Errorf("entity has tag type %d", elemType)
	}
	entities := make([]Entity, count)
	for i := range int(count) {
		if err := r.readTemplateEntity(&entities[i], int32(i)); err != nil {
			return nil, err
		}
	}
	return entities, nil
}

func (r *nbtReader) readTemplateEntity(dst *Entity, record int32) error {
	var hasPos, hasBlockPos, hasNBT bool
	dst.Record = record
	for {
		t, err := r.readByte()
		if err != nil {
			return err
		}
		if t == nbtTagEnd {
			break
		}
		field, err := r.readStringBytes()
		if err != nil {
			return err
		}
		switch string(field) {
		case "pos":
			if err := r.readFloat64List3(t, &dst.Pos); err != nil {
				return fmt.Errorf("entity %d pos: %w", record, err)
			}
			hasPos = true
		case "blockPos":
			if err := r.readInt32List3(t, &dst.BlockPos); err != nil {
				return fmt.Errorf("entity %d blockPos: %w", record, err)
			}
			hasBlockPos = true
		case "nbt":
			if t != nbtTagCompound {
				return fmt.Errorf("entity %d has no NBT compound", record)
			}
			nbt, err := r.readCompound(3)
			if err != nil {
				return err
			}
			dst.NBT = nbt
			hasNBT = true
		default:
			if err := r.skipPayload(t, 3); err != nil {
				return err
			}
		}
	}
	if !hasPos {
		return fmt.Errorf("entity %d pos: missing", record)
	}
	if !hasBlockPos {
		return fmt.Errorf("entity %d blockPos: missing", record)
	}
	if !hasNBT {
		return fmt.Errorf("entity %d has no NBT compound", record)
	}
	return nil
}

func buildBlocksFromRaw(raw []rawTemplateBlock, states []world.BlockState) ([]Block, error) {
	blocks := make([]Block, len(raw))
	// Track per-palette native-actor status lazily: 0=unchecked, 1=none, 2=hasNative.
	var nativeStatusBuf [128]uint8
	var nativeStatus []uint8
	if len(states) <= len(nativeStatusBuf) {
		nativeStatus = nativeStatusBuf[:len(states)]
	} else {
		nativeStatus = make([]uint8, len(states))
	}
	for i := range raw {
		rb := &raw[i]
		if !rb.hasPos {
			return nil, fmt.Errorf("block %d pos: missing", i)
		}
		if !rb.hasState || rb.state < 0 || int(rb.state) >= len(states) {
			return nil, fmt.Errorf("block %d invalid state %d", i, rb.state)
		}
		b := &blocks[i]
		b.Pos = rb.pos
		b.State = states[rb.state]
		b.NBT = rb.nbt
		switch nativeStatus[rb.state] {
		case 0:
			nativeBlock, ok, err := NativeBlockForState(b.State)
			if err != nil {
				return nil, fmt.Errorf("block %d native actor: %w", i, err)
			}
			if ok {
				nativeStatus[rb.state] = 2
				b.NativeBlock = nativeBlock
			} else {
				nativeStatus[rb.state] = 1
			}
		case 2:
			nativeBlock, ok, err := NativeBlockForState(b.State)
			if err != nil {
				return nil, fmt.Errorf("block %d native actor: %w", i, err)
			}
			if ok {
				b.NativeBlock = nativeBlock
			}
		}
	}
	return blocks, nil
}

func decodeTemplate(root map[string]any) (*Template, error) {
	t := &Template{}
	if err := intList3(root["size"], &t.Size); err != nil {
		return nil, fmt.Errorf("size: %w", err)
	}
	var paletteValues any
	if raw, exists := root["palettes"]; exists {
		paletteValues = raw
	} else if palette, exists := root["palette"]; exists {
		paletteValues = []any{palette}
	} else {
		return nil, fmt.Errorf("missing palettes")
	}
	paletteCount, ok := templateListLen(paletteValues)
	if !ok {
		return nil, fmt.Errorf("palettes has type %T", paletteValues)
	}
	t.Palettes = make([][]Block, paletteCount)
	for pi := range paletteCount {
		rawPalette, _ := templateListValue(paletteValues, pi)
		stateCount, ok := templateListLen(rawPalette)
		if !ok {
			return nil, fmt.Errorf("palette %d has type %T", pi, rawPalette)
		}
		states := make([]world.BlockState, stateCount)
		for i := range stateCount {
			raw, _ := templateListValue(rawPalette, i)
			entry, ok := raw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("palette %d state %d has type %T", pi, i, raw)
			}
			name, ok := entry["Name"].(string)
			if !ok {
				name, ok = entry["name"].(string)
			}
			if !ok {
				name, ok = entry["id"].(string)
			}
			if !ok {
				return nil, fmt.Errorf("palette %d state %d has no name or id", pi, i)
			}
			props := entry["Properties"]
			if props == nil {
				props = entry["properties"]
			}
			properties, _ := props.(map[string]any)
			properties = world.DecodeStateProperties(properties)
			states[i] = world.BlockState{Name: name, Properties: properties}
		}
		blocks, err := decodeBlocks(root["blocks"], states)
		if err != nil {
			return nil, fmt.Errorf("palette %d: %w", pi, err)
		}
		t.Palettes[pi] = blocks
	}
	if raw, ok := root["entities"]; ok {
		count, ok := templateListLen(raw)
		if !ok {
			return nil, fmt.Errorf("entities has type %T", raw)
		}
		t.Entities = make([]Entity, count)
		for i := range count {
			value, _ := templateListValue(raw, i)
			m, ok := value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("entity %d has type %T", i, value)
			}
			if err := doubleList3(m["pos"], &t.Entities[i].Pos); err != nil {
				return nil, fmt.Errorf("entity %d pos: %w", i, err)
			}
			if err := intList3(m["blockPos"], &t.Entities[i].BlockPos); err != nil {
				return nil, fmt.Errorf("entity %d blockPos: %w", i, err)
			}
			nbt, ok := m["nbt"].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("entity %d has no NBT compound", i)
			}
			t.Entities[i].NBT = nbt
			t.Entities[i].Record = int32(i)
		}
	}
	return t, nil
}

func templateListLen(value any) (int, bool) {
	switch list := value.(type) {
	case []any:
		return len(list), true
	case []map[string]any:
		return len(list), true
	case []string:
		return len(list), true
	case []float32:
		return len(list), true
	case []float64:
		return len(list), true
	default:
		return 0, false
	}
}

func templateListValue(value any, index int) (any, bool) {
	switch list := value.(type) {
	case []any:
		if index >= 0 && index < len(list) {
			return list[index], true
		}
	case []map[string]any:
		if index >= 0 && index < len(list) {
			return list[index], true
		}
	case []string:
		if index >= 0 && index < len(list) {
			return list[index], true
		}
	case []float32:
		if index >= 0 && index < len(list) {
			return list[index], true
		}
	case []float64:
		if index >= 0 && index < len(list) {
			return list[index], true
		}
	}
	return nil, false
}

func decodeBlocks(raw any, states []world.BlockState) ([]Block, error) {
	count, ok := templateListLen(raw)
	if !ok {
		return nil, fmt.Errorf("blocks has type %T", raw)
	}
	blocks := make([]Block, count)
	for i := range count {
		value, _ := templateListValue(raw, i)
		m, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("block %d has type %T", i, value)
		}
		if err := intList3(m["pos"], &blocks[i].Pos); err != nil {
			return nil, fmt.Errorf("block %d pos: %w", i, err)
		}
		state, ok := integer(m["state"])
		if !ok || state < 0 || state >= int64(len(states)) {
			return nil, fmt.Errorf("block %d invalid state %v", i, m["state"])
		}
		blocks[i].State = states[state]
		nativeBlock, ok, err := NativeBlockForState(blocks[i].State)
		if err != nil {
			return nil, fmt.Errorf("block %d native actor: %w", i, err)
		}
		if ok {
			blocks[i].NativeBlock = nativeBlock
		}
		if data, exists := m["nbt"]; exists {
			blocks[i].NBT, ok = data.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("block %d NBT has type %T", i, data)
			}
		}
	}
	return blocks, nil
}

func integer(v any) (int64, bool) {
	switch n := v.(type) {
	case int8:
		return int64(n), true
	case int16:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	}
	return 0, false
}

func intList3(value any, out *[3]int32) error {
	if list, ok := value.([]int32); ok && len(list) == 3 {
		*out = [3]int32(list)
		return nil
	}
	list, ok := value.([]any)
	if !ok || len(list) != 3 {
		return fmt.Errorf("expected three-element list, got %T", value)
	}
	for i := range 3 {
		n, ok := integer(list[i])
		if !ok {
			return fmt.Errorf("element %d has type %T", i, list[i])
		}
		out[i] = int32(n)
	}
	return nil
}

func doubleList3(value any, out *[3]float64) error {
	if list, ok := value.([]float64); ok && len(list) == 3 {
		*out = [3]float64(list)
		return nil
	}
	list, ok := value.([]any)
	if !ok || len(list) != 3 {
		return fmt.Errorf("expected three-element list, got %T", value)
	}
	for i := range 3 {
		switch n := list[i].(type) {
		case float32:
			out[i] = float64(n)
		case float64:
			out[i] = n
		default:
			return fmt.Errorf("element %d has type %T", i, list[i])
		}
	}
	return nil
}
