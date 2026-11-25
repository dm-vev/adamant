package bedrock

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"maps"
	"path"
	"sort"
	"strings"

	"github.com/dm-vev/adamant/net/bedrock/resource"
	"github.com/google/uuid"
)

// PackBuilder generates a valid Bedrock resource pack (.mcpack zip) in memory
// from custom textures, sounds, and arbitrary files, compiling directly into a
// *resource.Pack without manual manifest.json boilerplate.
type PackBuilder struct {
	name             string
	description      string
	headerUUID       uuid.UUID
	moduleUUID       uuid.UUID
	version          [3]int
	minEngineVersion [3]int
	files            map[string][]byte
	itemTextures     map[string]string
	blockTextures    map[string]string
	sounds           map[string][]string
	err              error
}

// NewPackBuilder starts a runtime Bedrock resource pack builder.
func NewPackBuilder(name, description string) *PackBuilder {
	return &PackBuilder{
		name:             name,
		description:      description,
		headerUUID:       uuid.New(),
		moduleUUID:       uuid.New(),
		version:          [3]int{1, 0, 0},
		minEngineVersion: [3]int{1, 21, 0},
		files:            make(map[string][]byte),
		itemTextures:     make(map[string]string),
		blockTextures:    make(map[string]string),
		sounds:           make(map[string][]string),
	}
}

// UUIDs overrides the pack's header and resources module UUIDs (useful for
// deterministic packs so clients cache them across restarts).
func (b *PackBuilder) UUIDs(header, module uuid.UUID) *PackBuilder {
	if header != uuid.Nil {
		b.headerUUID = header
	}
	if module != uuid.Nil {
		b.moduleUUID = module
	}
	return b
}

// Version sets the pack semantic version [major, minor, patch].
func (b *PackBuilder) Version(major, minor, patch int) *PackBuilder {
	b.version = [3]int{major, minor, patch}
	return b
}

// MinEngineVersion sets the manifest's min_engine_version [major, minor, patch].
func (b *PackBuilder) MinEngineVersion(major, minor, patch int) *PackBuilder {
	b.minEngineVersion = [3]int{major, minor, patch}
	return b
}

// AddFile adds an arbitrary file at relPath inside the pack archive.
func (b *PackBuilder) AddFile(relPath string, data []byte) *PackBuilder {
	clean := path.Clean(strings.TrimPrefix(strings.ReplaceAll(relPath, "\\", "/"), "/"))
	if clean == "." || clean == "" || strings.HasPrefix(clean, "../") {
		b.err = fmt.Errorf("sdk/bedrock: invalid pack path %q", relPath)
		return b
	}
	b.files[clean] = append([]byte(nil), data...)
	return b
}

// AddPNG encodes img as PNG and adds it at relPath inside the pack archive.
func (b *PackBuilder) AddPNG(relPath string, img image.Image) *PackBuilder {
	if img == nil {
		b.err = errors.New("sdk/bedrock: nil image")
		return b
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		b.err = fmt.Errorf("sdk/bedrock: encode png %q: %w", relPath, err)
		return b
	}
	return b.AddFile(relPath, buf.Bytes())
}

// AddItemTexture encodes img to textures/items/<key>.png and registers key in
// textures/item_texture.json.
func (b *PackBuilder) AddItemTexture(key string, img image.Image) *PackBuilder {
	if key == "" {
		b.err = errors.New("sdk/bedrock: empty item texture key")
		return b
	}
	rel := "textures/items/" + key
	b.AddPNG(rel+".png", img)
	b.itemTextures[key] = rel
	return b
}

// AddBlockTexture encodes img to textures/blocks/<key>.png and registers key in
// textures/terrain_texture.json.
func (b *PackBuilder) AddBlockTexture(key string, img image.Image) *PackBuilder {
	if key == "" {
		b.err = errors.New("sdk/bedrock: empty block texture key")
		return b
	}
	rel := "textures/blocks/" + key
	b.AddPNG(rel+".png", img)
	b.blockTextures[key] = rel
	return b
}

// AddSound writes soundData to sounds/<relPath> and registers eventName in
// sounds/sound_definitions.json.
func (b *PackBuilder) AddSound(eventName, relPath string, soundData []byte) *PackBuilder {
	if eventName == "" || relPath == "" {
		b.err = errors.New("sdk/bedrock: empty sound event or path")
		return b
	}
	clean := strings.TrimPrefix(path.Clean(strings.ReplaceAll(relPath, "\\", "/")), "/")
	b.AddFile("sounds/"+clean, soundData)
	noExt := strings.TrimSuffix(clean, path.Ext(clean))
	b.sounds[eventName] = append(b.sounds[eventName], "sounds/"+noExt)
	return b
}

// BuildZip produces the deterministic .mcpack zip archive bytes in memory.
func (b *PackBuilder) BuildZip() ([]byte, error) {
	if b == nil {
		return nil, errors.New("sdk/bedrock: nil PackBuilder")
	}
	if b.err != nil {
		return nil, b.err
	}
	if b.name == "" {
		return nil, errors.New("sdk/bedrock: pack name must not be empty")
	}

	files := maps.Clone(b.files)

	manifest := map[string]any{
		"format_version": 2,
		"header": map[string]any{
			"name":               b.name,
			"description":        b.description,
			"uuid":               b.headerUUID.String(),
			"version":            b.version,
			"min_engine_version": b.minEngineVersion,
		},
		"modules": []map[string]any{
			{
				"description": b.description,
				"type":        "resources",
				"uuid":        b.moduleUUID.String(),
				"version":     b.version,
			},
		},
	}
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("sdk/bedrock: marshal manifest.json: %w", err)
	}
	files["manifest.json"] = manifestJSON

	if len(b.itemTextures) > 0 {
		td := make(map[string]any, len(b.itemTextures))
		for k, v := range b.itemTextures {
			td[k] = map[string]any{"textures": v}
		}
		raw, err := json.MarshalIndent(map[string]any{
			"resource_pack_name": b.name,
			"texture_name":       "atlas.items",
			"texture_data":       td,
		}, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("sdk/bedrock: marshal item_texture.json: %w", err)
		}
		files["textures/item_texture.json"] = raw
	}

	if len(b.blockTextures) > 0 {
		td := make(map[string]any, len(b.blockTextures))
		for k, v := range b.blockTextures {
			td[k] = map[string]any{"textures": v}
		}
		raw, err := json.MarshalIndent(map[string]any{
			"resource_pack_name": b.name,
			"texture_name":       "atlas.terrain",
			"texture_data":       td,
		}, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("sdk/bedrock: marshal terrain_texture.json: %w", err)
		}
		files["textures/terrain_texture.json"] = raw
	}

	if len(b.sounds) > 0 {
		defs := make(map[string]any, len(b.sounds))
		for ev, list := range b.sounds {
			defs[ev] = map[string]any{"sounds": list}
		}
		raw, err := json.MarshalIndent(map[string]any{
			"format_version":    "1.14.0",
			"sound_definitions": defs,
		}, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("sdk/bedrock: marshal sound_definitions.json: %w", err)
		}
		files["sounds/sound_definitions.json"] = raw
	}

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, name := range names {
		w, err := zw.Create(name)
		if err != nil {
			_ = zw.Close()
			return nil, fmt.Errorf("sdk/bedrock: zip create %q: %w", name, err)
		}
		if _, err := w.Write(files[name]); err != nil {
			_ = zw.Close()
			return nil, fmt.Errorf("sdk/bedrock: zip write %q: %w", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("sdk/bedrock: zip close: %w", err)
	}
	return out.Bytes(), nil
}

// Build compiles the pack archive in memory and returns a ready-to-serve
// *resource.Pack suitable for bedrock.WithPacks.
func (b *PackBuilder) Build() (*resource.Pack, error) {
	data, err := b.BuildZip()
	if err != nil {
		return nil, err
	}
	return resource.Read(bytes.NewReader(data))
}
