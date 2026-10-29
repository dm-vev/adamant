package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

type vanillaItem struct {
	RuntimeID      int32          `nbt:"runtime_id"`
	ComponentBased bool           `nbt:"component_based"`
	Version        int32          `nbt:"version"`
	Data           map[string]any `nbt:"data,omitempty"`
}

// encodeItemsNBT preserves the protocol entries' complete component payloads in
// the map layout consumed by both Dragonfly vanilla item loaders.
func encodeItemsNBT(entries []v2193.ItemEntry) ([]byte, error) {
	items := make(map[string]vanillaItem, len(entries))
	runtimeIDs := make(map[int16]string, len(entries))
	for _, entry := range entries {
		if entry.Name == "" {
			return nil, fmt.Errorf("item has an empty name")
		}
		if previous, exists := items[entry.Name]; exists {
			return nil, fmt.Errorf("duplicate item name %q (runtime IDs %d and %d)", entry.Name, previous.RuntimeID, entry.RuntimeID)
		}
		if previous, exists := runtimeIDs[entry.RuntimeID]; exists {
			return nil, fmt.Errorf("duplicate runtime ID %d for %q and %q", entry.RuntimeID, previous, entry.Name)
		}
		runtimeIDs[entry.RuntimeID] = entry.Name
		items[entry.Name] = vanillaItem{
			RuntimeID: int32(entry.RuntimeID), ComponentBased: entry.ComponentBased,
			Version: entry.Version, Data: entry.Data,
		}
	}
	encoded, err := nbt.Marshal(items)
	if err != nil {
		return nil, fmt.Errorf("marshal NBT: %w", err)
	}
	return encoded, nil
}

// writeNewFileAtomic publishes data in the destination directory without ever
// replacing an existing destination.
func writeNewFileAtomic(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".vanilla-items-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	if _, err = temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err = temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	if err = os.Link(tempPath, path); err != nil {
		return err
	}
	return nil
}
