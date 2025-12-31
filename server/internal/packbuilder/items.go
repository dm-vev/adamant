package packbuilder

import (
	"encoding/json"
	"fmt"
	"github.com/df-mc/dragonfly/server/world"
	"image"
	"image/png"
	"os"
	"path/filepath"
	_ "unsafe" // Imported for compiler directives.
)

// buildItems builds all the item-related files for the resource pack. This includes textures, language
// entries and item atlas.
func buildItems(dir string) (count int, lang []string, err error) {
	if err := os.Mkdir(filepath.Join(dir, "items"), os.ModePerm); err != nil {
		return 0, nil, fmt.Errorf("create items dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "textures/items"), os.ModePerm); err != nil {
		return 0, nil, fmt.Errorf("create item textures dir: %w", err)
	}

	textureData := make(map[string]any)
	for _, item := range world.CustomItems() {
		identifier, _ := item.EncodeItem()
		lang = append(lang, fmt.Sprintf("item.%s.name=%s", identifier, item.Name()))

		name, ok := identifierName(identifier)
		if !ok {
			return 0, nil, fmt.Errorf("invalid item identifier %q", identifier)
		}
		textureData[identifier] = map[string]string{"textures": fmt.Sprintf("textures/items/%s.png", name)}

		if err := buildItemTexture(dir, name, item.Texture()); err != nil {
			return 0, nil, fmt.Errorf("build item texture %s: %w", name, err)
		}

		count++
	}

	if err := buildItemAtlas(dir, map[string]any{
		"resource_pack_name": "vanilla",
		"texture_name":       "atlas.items",
		"texture_data":       textureData,
	}); err != nil {
		return 0, nil, fmt.Errorf("build item atlas: %w", err)
	}
	return count, lang, nil
}

// buildItemTexture creates a PNG file for the item from the provided image and name and writes it to the pack.
func buildItemTexture(dir, name string, img image.Image) error {
	texture, err := os.Create(filepath.Join(dir, "textures/items", name+".png"))
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

// buildItemAtlas creates the identifier to texture mapping and writes it to the pack.
func buildItemAtlas(dir string, atlas map[string]any) error {
	b, err := json.Marshal(atlas)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "textures/item_texture.json"), b, 0666); err != nil {
		return err
	}
	return nil
}
