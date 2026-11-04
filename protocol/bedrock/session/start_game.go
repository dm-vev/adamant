package session

import (
	"maps"
	"slices"

	"github.com/df-mc/dragonfly/protocol/bedrock/internal/bedrockdata"
	"github.com/df-mc/dragonfly/protocol/bedrock/internal/blockinternal"
	"github.com/df-mc/dragonfly/protocol/bedrock/internal/iteminternal"
	"github.com/df-mc/dragonfly/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// startContent is the content of the game the client is defined in
// StartGame and the ItemRegistry after it: the blocks, the vanilla and the
// custom items and the custom dimensions, made into their Bedrock entries
// of a protocol once. A nil startContent holds no entries.
type startContent struct {
	blocks       []mc.BlockEntry
	vanillaItems map[string]bedrockdata.ItemEntry
	customItems  []mc.ItemEntry
	dimensions   []mc.DimensionDefinition
}

// newStartContent makes the entries of the content of the game, as the
// clients of the tables passed know it: the vanilla blocks the game defines
// by data and the custom blocks of the registry of the tables, the custom
// items and the block items of its custom blocks, and the custom dimensions
// passed.
func newStartContent(w *wireTables, items []world.CustomItem, dims []world.DimensionRegistration) *startContent {
	return &startContent{
		blocks:       blockEntries(w.vanilla.Blocks(), w.br),
		vanillaItems: w.vanilla.Items(),
		customItems:  customItemEntries(w, items),
		dimensions:   dimensionDefinitions(dims),
	}
}

// Blocks returns the block entries of StartGame.
func (c *startContent) Blocks() []mc.BlockEntry {
	if c == nil {
		return nil
	}
	return c.blocks
}

// Dimensions returns the dimension definitions of StartGame.
func (c *startContent) Dimensions() []mc.DimensionDefinition {
	if c == nil {
		return nil
	}
	return c.dimensions
}

// Items returns the item entries of StartGame: the vanilla items, in no
// order, then the custom items. It makes a new slice on every call.
func (c *startContent) Items() []mc.ItemEntry {
	vanillaItems := c.vanilla()
	entries := make([]mc.ItemEntry, 0, len(vanillaItems))
	for name, e := range vanillaItems {
		entries = append(entries, mc.ItemEntry{
			Name:           name,
			RuntimeID:      int16(e.RuntimeID),
			ComponentBased: e.ComponentBased,
			Version:        e.Version,
			Data:           e.Data,
		})
	}
	if c != nil {
		entries = append(entries, c.customItems...)
	}
	return entries
}

// vanilla returns the vanilla items of the content, those of the model for
// a nil startContent.
func (c *startContent) vanilla() map[string]bedrockdata.ItemEntry {
	if c == nil {
		return tablesOf(nil, world.DefaultBlockRegistry).vanilla.Items()
	}
	return c.vanillaItems
}

// ItemRegistry returns the entries of the ItemRegistry written after
// StartGame, the custom items, in a new slice.
func (c *startContent) ItemRegistry() []mc.ItemEntry {
	if c == nil {
		return []mc.ItemEntry{}
	}
	items := make([]mc.ItemEntry, len(c.customItems))
	copy(items, c.customItems)
	return items
}

// blockEntries returns the definitions of blocks the client gets at spawn:
// the vanilla blocks the game defines by data, as BDS sends them, and the
// custom blocks of br.
func blockEntries(vanillaBlocks map[string]map[string]any, br world.BlockRegistry) []mc.BlockEntry {
	custom := slices.Collect(maps.Values(br.CustomBlocks()))
	entries := make([]mc.BlockEntry, 0, len(vanillaBlocks)+len(custom))

	for _, name := range slices.Sorted(maps.Keys(vanillaBlocks)) {
		entries = append(entries, mc.BlockEntry{Name: name, Properties: vanillaBlocks[name]})
	}
	for i, b := range custom {
		name, _ := b.EncodeBlock()
		entries = append(entries, mc.BlockEntry{
			Name:       name,
			Properties: blockinternal.Components(name, b, 10000+int32(i)),
		})
	}
	return entries
}

// customItemEntries returns the entries of the custom items passed that
// are no block items, and of the block items of the custom blocks of br.
func customItemEntries(w *wireTables, custom []world.CustomItem) []mc.ItemEntry {
	br := w.br
	items := make([]world.CustomItem, 0, len(custom)+len(br.CustomBlocks()))
	for _, it := range custom {
		if _, blockItem := it.(world.CustomBlock); !blockItem {
			items = append(items, it)
		}
	}
	for _, b := range br.CustomBlocks() {
		if it, ok := b.(world.CustomItem); ok {
			items = append(items, it)
		}
	}

	entries := make([]mc.ItemEntry, 0, len(items))

	for _, it := range items {
		name, _ := it.EncodeItem()
		rid, _, ok := w.itemRuntimeID(it)
		if !ok {
			continue
		}
		_, isCustomBlock := it.(world.CustomBlock)
		entryVersion := mc.ItemEntryVersionDataDriven
		if isCustomBlock {
			entryVersion = mc.ItemEntryVersionNone
		}
		entries = append(entries, mc.ItemEntry{
			Name:           name,
			ComponentBased: !isCustomBlock,
			RuntimeID:      int16(rid),
			Version:        entryVersion,
			Data:           iteminternal.Components(it),
		})
	}
	return entries
}

// dimensionDefinitions returns the definitions of the custom dimensions
// passed.
func dimensionDefinitions(dimensions []world.DimensionRegistration) []mc.DimensionDefinition {
	defs := make([]mc.DimensionDefinition, 0, len(dimensions))
	for _, registration := range dimensions {
		r := registration.Dimension.Range()
		defs = append(defs, mc.DimensionDefinition{
			Name:          registration.Name,
			MinimumY:      int32(r.Min()),
			HeightRange:   int32(r.Height() + 1),
			Generator:     mc.GeneratorVoid,
			DimensionType: int32(registration.ID),
		})
	}
	return defs
}
