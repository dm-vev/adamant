package javadata

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"slices"
	"strings"
)

//go:embed assets/data/minecraft
var assets embed.FS

// Assets exposes the immutable embedded Java data-pack resources.
func Assets() embed.FS { return assets }


// LoadJSON returns an actual embedded Java data-pack resource as decoded JSON.
// kind identifies a JSON registry or tag; name omits namespace and .json.
func LoadJSON(kind, name string) (map[string]any, error) {
	base, extension, err := resourcePath(kind)
	if err != nil { return nil, err }
	if extension != ".json" { return nil, fmt.Errorf("Java data: resource kind %q is not JSON", kind) }
	name = strings.TrimPrefix(name, "minecraft:")
	name = strings.TrimPrefix(name, "/")
	if strings.Contains(name, "..") || strings.HasSuffix(name, ".json") { return nil, fmt.Errorf("Java data: invalid JSON resource name %q", name) }
	path := base + "/" + name + extension
	data, err := assets.ReadFile(path)
	if err != nil { return nil, fmt.Errorf("Java %s %q: %w", kind, name, err) }
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil { return nil, fmt.Errorf("Java %s %q: %w", kind, name, err) }
	return out, nil
}

// ResourceNames returns sorted relative names for the same resource kinds
// accepted by LoadJSON, plus binary structure templates.
func ResourceNames(kind string) ([]string, error) {
	dir, extension, err := resourcePath(kind)
	if err != nil { return nil, err }
	var names []string
	err = fs.WalkDir(assets, dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil { return err }
		if entry.IsDir() || !strings.HasSuffix(path, extension) { return nil }
		names = append(names, strings.TrimSuffix(strings.TrimPrefix(path, dir+"/"), extension))
		return nil
	})
	if err != nil { return nil, err }
	slices.Sort(names)
	return names, nil
}

func resourcePath(kind string) (string, string, error) {
	switch kind {
	case "template":
		return "assets/data/minecraft/structure", ".nbt", nil
	case "structure_set", "structure", "template_pool", "processor_list", "placed_feature", "biome", "block_state_provider":
		return "assets/data/minecraft/worldgen/" + kind, ".json", nil
	case "configured_feature":
		return "assets/data/minecraft/worldgen/feature", ".json", nil
	case "configured_carver":
		return "assets/data/minecraft/worldgen/carver", ".json", nil
	case "biome_tag":
		return "assets/data/minecraft/tags/worldgen/biome", ".json", nil
	case "block_tag":
		return "assets/data/minecraft/tags/block", ".json", nil
	default:
		return "", "", fmt.Errorf("Java data: unsupported resource kind %q", kind)
	}
}
