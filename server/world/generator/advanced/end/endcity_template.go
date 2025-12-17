package end

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/mc112"
)

type endCityTemplate struct {
	Size   [3]int
	States []endCityState
	Blocks []endCityBlock
}

type endCityState struct {
	Name       string
	Properties map[string]string
}

type endCityBlock struct {
	Pos   [3]int
	State int
	NBT   map[string]any
}

var (
	endCityTemplatesOnce sync.Once
	endCityTemplates     map[string]*endCityTemplate
	endCityTemplatesErr  error
)

func getEndCityTemplate(name string) (*endCityTemplate, error) {
	endCityTemplatesOnce.Do(func() {
		endCityTemplates, endCityTemplatesErr = loadEndCityTemplates()
	})
	if endCityTemplatesErr != nil {
		return nil, endCityTemplatesErr
	}
	t, ok := endCityTemplates[name]
	if !ok {
		return nil, fmt.Errorf("mc112: missing end city template %q", name)
	}
	return t, nil
}

func loadEndCityTemplates() (map[string]*endCityTemplate, error) {
	entries, err := fs.ReadDir(endCityFS, "structures/endcity")
	if err != nil {
		return nil, fmt.Errorf("mc112: read end city templates: %w", err)
	}
	out := make(map[string]*endCityTemplate, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".nbt") {
			continue
		}
		b, err := endCityFS.ReadFile(filepath.ToSlash(filepath.Join("structures/endcity", entry.Name())))
		if err != nil {
			return nil, fmt.Errorf("mc112: read end city template %s: %w", entry.Name(), err)
		}
		t, err := parseEndCityTemplate(b)
		if err != nil {
			return nil, fmt.Errorf("mc112: parse end city template %s: %w", entry.Name(), err)
		}
		out[strings.TrimSuffix(entry.Name(), ".nbt")] = t
	}
	return out, nil
}

func parseEndCityTemplate(gzipNBT []byte) (*endCityTemplate, error) {
	gr, err := gzip.NewReader(bytes.NewReader(gzipNBT))
	if err != nil {
		return nil, err
	}
	defer gr.Close()

	raw, err := io.ReadAll(gr)
	if err != nil {
		return nil, err
	}
	root, err := mc112.DecodeNBT(raw)
	if err != nil {
		return nil, err
	}

	size, err := readIntList3(root, "size")
	if err != nil {
		return nil, err
	}
	states, err := readPalette(root)
	if err != nil {
		return nil, err
	}
	blocks, err := readBlocks(root)
	if err != nil {
		return nil, err
	}

	return &endCityTemplate{
		Size:   size,
		States: states,
		Blocks: blocks,
	}, nil
}

func readIntList3(root map[string]any, key string) ([3]int, error) {
	var out [3]int
	raw, ok := root[key]
	if !ok {
		return out, fmt.Errorf("java nbt: missing %q", key)
	}
	list, ok := raw.([]int32)
	if !ok || len(list) != 3 {
		return out, fmt.Errorf("java nbt: %q expected int[3], got %T", key, raw)
	}
	out[0], out[1], out[2] = int(list[0]), int(list[1]), int(list[2])
	return out, nil
}

func readPalette(root map[string]any) ([]endCityState, error) {
	raw, ok := root["palette"]
	if !ok {
		return nil, fmt.Errorf("java nbt: missing %q", "palette")
	}
	list, ok := raw.([]map[string]any)
	if !ok {
		return nil, fmt.Errorf("java nbt: %q expected list[compound], got %T", "palette", raw)
	}
	out := make([]endCityState, 0, len(list))
	for i, entry := range list {
		name, _ := entry["Name"].(string)
		if name == "" {
			return nil, fmt.Errorf("java nbt: palette[%d] missing Name", i)
		}
		props := map[string]string{}
		if rawProps, ok := entry["Properties"]; ok {
			propCompound, ok := rawProps.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("java nbt: palette[%d] Properties expected compound, got %T", i, rawProps)
			}
			for k, v := range propCompound {
				if s, ok := v.(string); ok {
					props[k] = s
				}
			}
		}
		if len(props) == 0 {
			props = nil
		}
		out = append(out, endCityState{Name: name, Properties: props})
	}
	return out, nil
}

func readBlocks(root map[string]any) ([]endCityBlock, error) {
	raw, ok := root["blocks"]
	if !ok {
		return nil, fmt.Errorf("java nbt: missing %q", "blocks")
	}
	list, ok := raw.([]map[string]any)
	if !ok {
		return nil, fmt.Errorf("java nbt: %q expected list[compound], got %T", "blocks", raw)
	}

	out := make([]endCityBlock, 0, len(list))
	for i, entry := range list {
		posRaw, ok := entry["pos"]
		if !ok {
			return nil, fmt.Errorf("java nbt: blocks[%d] missing pos", i)
		}
		posList, ok := posRaw.([]int32)
		if !ok || len(posList) != 3 {
			return nil, fmt.Errorf("java nbt: blocks[%d] pos expected int[3], got %T", i, posRaw)
		}
		stateRaw, ok := entry["state"]
		if !ok {
			return nil, fmt.Errorf("java nbt: blocks[%d] missing state", i)
		}
		state, ok := stateRaw.(int32)
		if !ok {
			return nil, fmt.Errorf("java nbt: blocks[%d] state expected int, got %T", i, stateRaw)
		}

		var tile map[string]any
		if rawNBT, ok := entry["nbt"]; ok {
			if m, ok := rawNBT.(map[string]any); ok {
				tile = m
			}
		}

		out = append(out, endCityBlock{
			Pos:   [3]int{int(posList[0]), int(posList[1]), int(posList[2])},
			State: int(state),
			NBT:   tile,
		})
	}
	return out, nil
}
