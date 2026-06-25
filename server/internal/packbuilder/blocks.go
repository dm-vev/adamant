package packbuilder

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	_ "unsafe" // Imported for compiler directives.

	"github.com/df-mc/dragonfly/server/world"
)

// buildBlocks builds all the block-related files for the resource pack. This includes textures, geometries, language
// entries and terrain texture atlas.
func buildBlocks(reg world.BlockRegistry, dir string) (count int, lang []string, err error) {
	if err := os.MkdirAll(filepath.Join(dir, "models/blocks"), os.ModePerm); err != nil {
		return 0, nil, fmt.Errorf("create block models dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "textures/blocks"), os.ModePerm); err != nil {
		return 0, nil, fmt.Errorf("create block textures dir: %w", err)
	}

	textureData := make(map[string]any)
	for identifier, blk := range reg.CustomBlocks() {
		b, ok := blk.(world.CustomBlockBuildable)
		if !ok {
			continue
		}

		name, ok := identifierName(identifier)
		if !ok {
			return 0, nil, fmt.Errorf("invalid block identifier %q", identifier)
		}
		lang = append(lang, fmt.Sprintf("tile.%s.name=%s", identifier, b.Name()))
		for name, texture := range b.Textures() {
			textureData[name] = map[string]string{"textures": "textures/blocks/" + name}
			if err := buildBlockTexture(dir, name, texture); err != nil {
				return 0, nil, fmt.Errorf("build block texture %s: %w", name, err)
			}
		}
		if b.Geometry() != nil {
			if err := os.WriteFile(filepath.Join(dir, "models/blocks", fmt.Sprintf("%s.geo.json", name)), b.Geometry(), 0666); err != nil {
				return 0, nil, fmt.Errorf("write geometry for %s: %w", name, err)
			}
		}
		count++
	}

	if err := buildBlockAtlas(dir, map[string]any{
		"resource_pack_name": "vanilla",
		"texture_name":       "atlas.terrain",
		"padding":            8,
		"num_mip_levels":     4,
		"texture_data":       textureData,
	}); err != nil {
		return 0, nil, fmt.Errorf("build block atlas: %w", err)
	}
	return count, lang, nil
}

// buildBlockTexture creates a PNG file for the block from the provided image and name and writes it to the pack.
func buildBlockTexture(dir, name string, img image.Image) error {
	texture, err := os.Create(filepath.Join(dir, fmt.Sprintf("textures/blocks/%s.png", name)))
	if err != nil {
		return err
	}
	if err := png.Encode(texture, img); err != nil {
		closeErr := texture.Close()
		if closeErr != nil {
			return fmt.Errorf("encode image: %w (close error: %v)", err, closeErr)
		}
		return err
	}
	if err := texture.Close(); err != nil {
		return err
	}
	return nil
}

// buildBlockAtlas creates the identifier to texture mapping and writes it to the pack.
func buildBlockAtlas(dir string, atlas map[string]any) error {
	b, err := json.Marshal(atlas)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "textures/terrain_texture.json"), b, 0666); err != nil {
		return err
	}
	return nil
}
