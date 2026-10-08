// Package javastructure decodes and transforms Java world-generation structure templates.
package javastructure

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/mc112"
)

// Block is a palette state and a template-relative position. NBT is retained verbatim.
// NativeBlock carries Java state properties stored in native block actors, or an entity adapted to a native block.
type Block struct {
	Pos         [3]int32
	State       world.BlockState
	NBT         map[string]any
	NativeBlock world.Block
}

// Entity is an entity record from a Java structure template.
type Entity struct {
	Pos         [3]float64
	BlockPos    [3]int32
	NBT         map[string]any
	Record      int32
	Seed        int64
	SpawnReason string
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
		return nil, fmt.Errorf("java structure: invalid name %q", name)
	}
	data, err := javadata.Assets().ReadFile("assets/data/minecraft/structure/" + name + ".nbt")
	if err != nil {
		if createMissing && errors.Is(err, fs.ErrNotExist) {
			return &Template{}, nil
		}
		return nil, fmt.Errorf("java structure %q: %w", name, err)
	}
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		reader, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("java structure %q gzip: %w", name, err)
		}
		data, err = io.ReadAll(reader)
		closeErr := reader.Close()
		if err != nil {
			return nil, fmt.Errorf("java structure %q gzip: %w", name, err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("java structure %q gzip: %w", name, closeErr)
		}
	}
	root, err := mc112.DecodeNBT(data)
	if err != nil {
		return nil, fmt.Errorf("java structure %q NBT: %w", name, err)
	}
	template, err := decodeTemplate(root)
	if err != nil {
		return nil, fmt.Errorf("java structure %q: %w", name, err)
	}
	return template, nil
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
		nativeBlock, ok, err := NativeBlockForJavaState(blocks[i].State)
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
