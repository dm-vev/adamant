package world

import (
	"fmt"
	"maps"
	"reflect"
	"strconv"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/go-gl/mathgl/mgl64"
)

// structureFormatVersion is the format_version Bedrock writes to its
// structure templates.
const structureFormatVersion = 2

// StructureTemplate is a cuboid of blocks and entities, like the ones
// structure blocks save and load. It is stored in Bedrock's .mcstructure
// format, so that templates, the world they are saved in and exported files
// are exchangeable with Bedrock Dedicated Server.
type StructureTemplate struct {
	size [3]int
	// origin is the world position the template was saved from, which the
	// absolute positions in its block and entity data are relative to.
	origin cube.Pos
	// blocks holds the block at each position, indexed by x*sizeY*sizeZ +
	// y*sizeZ + z. A nil block is a structure void, which leaves the world
	// untouched when the template is placed.
	blocks []Block
	// liquids holds the liquid in the second layer of a position, if any.
	liquids []Liquid
	// actors holds the block entity data of the blocks that have one.
	actors map[int]map[string]any
	// entities holds the native NBT of the entities, with their absolute
	// position relative to origin.
	entities []map[string]any
}

// NewStructureTemplate returns a template of the size passed filled with
// structure voids.
func NewStructureTemplate(size [3]int) *StructureTemplate {
	n := max(size[0], 0) * max(size[1], 0) * max(size[2], 0)
	return &StructureTemplate{size: size, blocks: make([]Block, n), liquids: make([]Liquid, n), actors: map[int]map[string]any{}}
}

// Size returns the size of the template along the X, Y and Z axes.
func (t *StructureTemplate) Size() [3]int { return t.size }

func (t *StructureTemplate) index(pos cube.Pos) (int, bool) {
	if pos[0] < 0 || pos[1] < 0 || pos[2] < 0 || pos[0] >= t.size[0] || pos[1] >= t.size[1] || pos[2] >= t.size[2] {
		return 0, false
	}
	return pos[0]*t.size[1]*t.size[2] + pos[1]*t.size[2] + pos[2], true
}

func (t *StructureTemplate) position(i int) cube.Pos {
	return cube.Pos{i / (t.size[1] * t.size[2]), i / t.size[2] % t.size[1], i % t.size[2]}
}

// SetBlock sets the block, liquid and block entity data at a position
// relative to the lowest corner of the template. A nil block is a structure
// void. The data is the native block entity NBT and is ignored for blocks
// without one.
func (t *StructureTemplate) SetBlock(pos cube.Pos, b Block, l Liquid, data map[string]any) {
	i, ok := t.index(pos)
	if !ok {
		return
	}
	t.blocks[i], t.liquids[i] = b, l
	delete(t.actors, i)
	if _, ok := b.(NBTer); ok && data != nil {
		t.actors[i] = data
	}
}

// Block returns the block and liquid at a position relative to the lowest
// corner of the template. The block is nil for structure voids.
func (t *StructureTemplate) Block(pos cube.Pos) (Block, Liquid) {
	i, ok := t.index(pos)
	if !ok {
		return nil, nil
	}
	return t.blocks[i], t.liquids[i]
}

// AddEntity adds the native NBT of an entity to the template. Its position is
// read from the Pos tag, relative to the lowest corner of the template.
func (t *StructureTemplate) AddEntity(data map[string]any) {
	data = maps.Clone(data)
	pos := readVec3(data, "Pos").Add(t.origin.Vec3())
	data["Pos"] = []float32{float32(pos[0]), float32(pos[1]), float32(pos[2])}
	t.entities = append(t.entities, data)
}

// EntityCount returns the number of entities in the template.
func (t *StructureTemplate) EntityCount() int { return len(t.entities) }

// TransformedBounds returns the lowest and highest corner, inclusive, that the
// template occupies relative to its placement position after mirroring and
// rotating it around that position.
func (t *StructureTemplate) TransformedBounds(mirror StructureMirror, rotation StructureRotation) (cube.Pos, cube.Pos) {
	a := TransformStructurePos(cube.Pos{}, mirror, rotation)
	b := TransformStructurePos(cube.Pos{t.size[0] - 1, t.size[1] - 1, t.size[2] - 1}, mirror, rotation)
	return cube.Pos{min(a[0], b[0]), min(a[1], b[1]), min(a[2], b[2])}, cube.Pos{max(a[0], b[0]), max(a[1], b[1]), max(a[2], b[2])}
}

// EncodeNBT encodes the template to the NBT of a Bedrock .mcstructure file.
func (t *StructureTemplate) EncodeNBT() map[string]any {
	var palette []any
	indices := map[string]int32{}
	paletteIndex := func(b Block) int32 {
		name, props := b.EncodeBlock()
		key := name + fmt.Sprint(props)
		if i, ok := indices[key]; ok {
			return i
		}
		i := int32(len(palette))
		indices[key] = i
		palette = append(palette, map[string]any{"name": name, "states": props, "version": chunk.CurrentBlockVersion})
		return i
	}
	n := len(t.blocks)
	blocks, liquids := make([]int32, n), make([]int32, n)
	hasLiquids := false
	positionData := map[string]any{}
	for i, b := range t.blocks {
		blocks[i], liquids[i] = -1, -1
		if b == nil {
			continue
		}
		blocks[i] = paletteIndex(b)
		if l := t.liquids[i]; l != nil {
			liquids[i], hasLiquids = paletteIndex(l), true
		}
		if data, ok := t.actors[i]; ok {
			data = maps.Clone(data)
			pos := t.origin.Add(t.position(i))
			data["x"], data["y"], data["z"] = int32(pos[0]), int32(pos[1]), int32(pos[2])
			positionData[strconv.Itoa(i)] = map[string]any{"block_entity_data": data}
		}
	}
	layers := []any{int32Array(blocks)}
	if hasLiquids {
		layers = append(layers, int32Array(liquids))
	}
	entities := make([]any, len(t.entities))
	for i, e := range t.entities {
		entities[i] = e
	}
	if palette == nil {
		palette = []any{}
	}
	return map[string]any{
		"format_version": int32(structureFormatVersion),
		"size":           []int32{int32(t.size[0]), int32(t.size[1]), int32(t.size[2])},
		"structure": map[string]any{
			"block_indices": layers,
			"entities":      entities,
			"palette": map[string]any{"default": map[string]any{
				"block_palette":       palette,
				"block_position_data": positionData,
			}},
		},
		"structure_world_origin": []int32{int32(t.origin[0]), int32(t.origin[1]), int32(t.origin[2])},
	}
}

// int32Array returns the values as a Go array, which NBT encodes as an
// int array rather than a list of ints, as Bedrock writes block indices.
func int32Array(values []int32) any {
	arr := reflect.New(reflect.ArrayOf(len(values), reflect.TypeFor[int32]())).Elem()
	reflect.Copy(arr, reflect.ValueOf(values))
	return arr.Interface()
}

// DecodeStructureTemplate decodes the NBT of a Bedrock .mcstructure file,
// resolving its block states with the registry passed.
func DecodeStructureTemplate(m map[string]any, registry BlockRegistry) (*StructureTemplate, error) {
	size, ok := nbtInts(m["size"])
	if !ok || len(size) != 3 || size[0] < 0 || size[1] < 0 || size[2] < 0 {
		return nil, fmt.Errorf("structure template: invalid size %v", m["size"])
	}
	t := NewStructureTemplate([3]int{size[0], size[1], size[2]})
	if origin, ok := nbtInts(m["structure_world_origin"]); ok && len(origin) == 3 {
		t.origin = cube.Pos{origin[0], origin[1], origin[2]}
	}
	structure, _ := m["structure"].(map[string]any)
	palettes, _ := structure["palette"].(map[string]any)
	defaultPalette, _ := palettes["default"].(map[string]any)
	rawPalette, _ := defaultPalette["block_palette"].([]any)
	encoding := chunk.BlockPaletteEncoding{Blocks: registry}
	palette := make([]Block, len(rawPalette))
	for i, raw := range rawPalette {
		entry, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("structure template: invalid palette entry %v", raw)
		}
		rid, err := encoding.DecodeBlockState(entry)
		if err != nil {
			return nil, fmt.Errorf("structure template: %w", err)
		}
		palette[i] = registry.BlockByRuntimeIDOrAir(rid)
	}
	layers := reflect.ValueOf(structure["block_indices"])
	if layers.Kind() != reflect.Slice {
		return nil, fmt.Errorf("structure template: invalid block indices")
	}
	n := len(t.blocks)
	for layer := range min(layers.Len(), 2) {
		indices, ok := nbtInts(layers.Index(layer).Interface())
		if !ok || len(indices) != n {
			return nil, fmt.Errorf("structure template: block indices layer %d has %d entries, want %d", layer, len(indices), n)
		}
		for i, index := range indices {
			if index < 0 {
				continue
			}
			if index >= len(palette) {
				return nil, fmt.Errorf("structure template: block index %d outside palette of %d", index, len(palette))
			}
			if layer == 0 {
				t.blocks[i] = palette[index]
			} else if l, ok := palette[index].(Liquid); ok && t.blocks[i] != nil {
				t.liquids[i] = l
			}
		}
	}
	positionData, _ := defaultPalette["block_position_data"].(map[string]any)
	for key, raw := range positionData {
		i, err := strconv.Atoi(key)
		if err != nil || i < 0 || i >= n {
			continue
		}
		wrapper, _ := raw.(map[string]any)
		data, _ := wrapper["block_entity_data"].(map[string]any)
		if _, ok := t.blocks[i].(NBTer); ok && data != nil {
			t.actors[i] = data
		}
	}
	entities, _ := structure["entities"].([]any)
	for _, raw := range entities {
		if data, ok := raw.(map[string]any); ok {
			t.entities = append(t.entities, data)
		}
	}
	return t, nil
}

// nbtInts reads a list or array of ints, which Bedrock writes as either.
func nbtInts(v any) ([]int, bool) {
	r := reflect.ValueOf(v)
	if r.Kind() != reflect.Slice && r.Kind() != reflect.Array {
		return nil, false
	}
	out := make([]int, r.Len())
	for i := range out {
		e := r.Index(i)
		if e.Kind() == reflect.Interface {
			e = e.Elem()
		}
		switch e.Kind() {
		case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Int:
			out[i] = int(e.Int())
		case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			out[i] = int(e.Uint())
		default:
			return nil, false
		}
	}
	return out, true
}

// structureEntityPos returns the position of an entity in the template,
// relative to the lowest corner of the template.
func (t *StructureTemplate) structureEntityPos(data map[string]any) mgl64.Vec3 {
	return readVec3(data, "Pos").Sub(t.origin.Vec3())
}
