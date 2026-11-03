package main

import (
	"fmt"

	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/nbt"
)

// encodeBlocksNBT keeps the block definitions of StartGame, the vanilla
// blocks the game defines by data, as a map of their properties by name.
func encodeBlocksNBT(entries []v2193.BlockEntry) ([]byte, error) {
	blocks := make(map[string]map[string]any, len(entries))
	for _, entry := range entries {
		if entry.Name == "" {
			return nil, fmt.Errorf("block has an empty name")
		}
		if _, exists := blocks[entry.Name]; exists {
			return nil, fmt.Errorf("duplicate block name %q", entry.Name)
		}
		blocks[entry.Name] = entry.Properties
	}
	encoded, err := nbt.Marshal(blocks)
	if err != nil {
		return nil, fmt.Errorf("marshal NBT: %w", err)
	}
	return encoded, nil
}
