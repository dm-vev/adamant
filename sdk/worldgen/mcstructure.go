package worldgen

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/dm-vev/adamant/net/nbt"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/world"
)

// LoadMCStructure decodes a Bedrock .mcstructure NBT stream into a
// *world.StructureTemplate using reg (defaults to world.DefaultBlockRegistry).
func LoadMCStructure(r io.Reader, reg world.BlockRegistry) (*world.StructureTemplate, error) {
	if r == nil {
		return nil, errors.New("sdk/worldgen: nil reader")
	}
	if reg == nil {
		reg = world.DefaultBlockRegistry
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("sdk/worldgen: read mcstructure: %w", err)
	}
	var raw map[string]any
	if err := nbt.UnmarshalEncoding(data, &raw, nbt.LittleEndian); err != nil {
		return nil, fmt.Errorf("sdk/worldgen: decode mcstructure NBT: %w", err)
	}
	tmpl, err := world.DecodeStructureTemplate(raw, reg)
	if err != nil {
		return nil, fmt.Errorf("sdk/worldgen: decode structure template: %w", err)
	}
	return tmpl, nil
}

// LoadMCStructureFile reads and decodes a Bedrock .mcstructure file from path.
func LoadMCStructureFile(path string, reg world.BlockRegistry) (*world.StructureTemplate, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("sdk/worldgen: open mcstructure %q: %w", path, err)
	}
	defer f.Close()
	return LoadMCStructure(f, reg)
}

// SaveMCStructure encodes tmpl into Bedrock's little-endian .mcstructure NBT
// format and writes it to w.
func SaveMCStructure(w io.Writer, tmpl *world.StructureTemplate) error {
	if w == nil || tmpl == nil {
		return errors.New("sdk/worldgen: nil writer or template")
	}
	data, err := nbt.MarshalEncoding(tmpl.EncodeNBT(), nbt.LittleEndian)
	if err != nil {
		return fmt.Errorf("sdk/worldgen: encode mcstructure NBT: %w", err)
	}
	if _, err := io.Copy(w, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("sdk/worldgen: write mcstructure: %w", err)
	}
	return nil
}

// SaveMCStructureFile atomically writes tmpl to path in Bedrock .mcstructure
// format.
func SaveMCStructureFile(path string, tmpl *world.StructureTemplate) error {
	if tmpl == nil || path == "" {
		return errors.New("sdk/worldgen: empty path or nil template")
	}
	var buf bytes.Buffer
	if err := SaveMCStructure(&buf, tmpl); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".mcstructure-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// FromTemplate converts a *world.StructureTemplate (e.g. loaded from a Bedrock
// .mcstructure file) into an immutable *Structure ready for Place or
// PlaceInChunk.
func FromTemplate(name string, tmpl *world.StructureTemplate) *Structure {
	if tmpl == nil {
		return NewStructure(name)
	}
	sz := tmpl.Size()
	blocks := make([]Block, 0, sz[0]*sz[1]*sz[2])
	for x := 0; x < sz[0]; x++ {
		for y := 0; y < sz[1]; y++ {
			for z := 0; z < sz[2]; z++ {
				pos := cube.Pos{x, y, z}
				b, _ := tmpl.Block(pos)
				if b != nil {
					blocks = append(blocks, Block{Offset: pos, Block: b})
				}
			}
		}
	}
	return NewStructure(name, blocks...)
}

// ToTemplate converts s into a *world.StructureTemplate suitable for saving as
// a Bedrock .mcstructure file. Runtime-ID entries are resolved using reg
// (defaults to world.DefaultBlockRegistry).
func (s *Structure) ToTemplate(reg world.BlockRegistry) *world.StructureTemplate {
	if s == nil {
		return world.NewStructureTemplate([3]int{})
	}
	if reg == nil {
		reg = world.DefaultBlockRegistry
	}
	sz := s.Size()
	tmpl := world.NewStructureTemplate([3]int{sz[0], sz[1], sz[2]})
	for _, entry := range s.blocks {
		b := entry.Block
		if entry.UseRID {
			b, _ = reg.BlockByRuntimeID(entry.RID)
		}
		if b != nil {
			tmpl.SetBlock(entry.Offset, b, nil, nil)
		}
	}
	return tmpl
}
