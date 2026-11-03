package session

import (
	_ "embed"
	"maps"
	"slices"

	"github.com/df-mc/dragonfly/server/internal/bedrockdata"
	"github.com/df-mc/dragonfly/server/internal/blockinternal"
	"github.com/df-mc/dragonfly/server/internal/iteminternal"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/nbt"
)

// StartContent is the content of the game the client is defined in
// StartGame and the ItemRegistry after it: the blocks, the custom items and
// the custom dimensions, made into their Bedrock entries once. A nil
// StartContent holds no entries.
type StartContent struct {
	blocks      []v2193.BlockEntry
	customItems []v2193.ItemEntry
	dimensions  []v2193.DimensionDefinition
}

// NewStartContent makes the entries of the content of the game: the vanilla
// blocks the game defines by data and the custom blocks of br, the custom
// items and the block items of the custom blocks of br, and the custom
// dimensions passed.
func NewStartContent(br world.BlockRegistry, items []world.CustomItem, dims []world.DimensionRegistration) *StartContent {
	return &StartContent{
		blocks:      blockEntries(br),
		customItems: customItemEntries(br, items),
		dimensions:  dimensionDefinitions(dims),
	}
}

// Blocks returns the block entries of StartGame.
func (c *StartContent) Blocks() []v2193.BlockEntry {
	if c == nil {
		return nil
	}
	return c.blocks
}

// Dimensions returns the dimension definitions of StartGame.
func (c *StartContent) Dimensions() []v2193.DimensionDefinition {
	if c == nil {
		return nil
	}
	return c.dimensions
}

// Items returns the item entries of StartGame: the vanilla items, in no
// order, then the custom items. It makes a new slice on every call.
func (c *StartContent) Items() []v2193.ItemEntry {
	entries := make([]v2193.ItemEntry, 0, len(vanillaItems))
	for name, e := range vanillaItems {
		entries = append(entries, v2193.ItemEntry{
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

// ItemRegistry returns the entries of the ItemRegistry written after
// StartGame, the custom items, in a new slice.
func (c *StartContent) ItemRegistry() []v2193.ItemEntry {
	if c == nil {
		return []v2193.ItemEntry{}
	}
	items := make([]v2193.ItemEntry, len(c.customItems))
	copy(items, c.customItems)
	return items
}

// blockEntries returns the definitions of blocks the client gets at spawn:
// the vanilla blocks the game defines by data, as BDS sends them, and the
// custom blocks of br.
func blockEntries(br world.BlockRegistry) []v2193.BlockEntry {
	custom := slices.Collect(maps.Values(br.CustomBlocks()))
	entries := make([]v2193.BlockEntry, 0, len(vanillaBlocks)+len(custom))

	for _, name := range slices.Sorted(maps.Keys(vanillaBlocks)) {
		entries = append(entries, v2193.BlockEntry{Name: name, Properties: vanillaBlocks[name]})
	}
	for i, b := range custom {
		name, _ := b.EncodeBlock()
		entries = append(entries, v2193.BlockEntry{
			Name:       name,
			Properties: blockinternal.Components(name, b, 10000+int32(i)),
		})
	}
	return entries
}

// customItemEntries returns the entries of the custom items passed that
// are no block items, and of the block items of the custom blocks of br.
func customItemEntries(br world.BlockRegistry, custom []world.CustomItem) []v2193.ItemEntry {
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

	entries := make([]v2193.ItemEntry, 0, len(items))

	for _, it := range items {
		name, _ := it.EncodeItem()
		rid, _, ok := world.ItemRuntimeID(it)
		if !ok {
			continue
		}
		_, isCustomBlock := it.(world.CustomBlock)
		entryVersion := v2193.ItemEntryVersionDataDriven
		if isCustomBlock {
			entryVersion = v2193.ItemEntryVersionNone
		}
		entries = append(entries, v2193.ItemEntry{
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
func dimensionDefinitions(dimensions []world.DimensionRegistration) []v2193.DimensionDefinition {
	defs := make([]v2193.DimensionDefinition, 0, len(dimensions))
	for _, registration := range dimensions {
		r := registration.Dimension.Range()
		defs = append(defs, v2193.DimensionDefinition{
			Name:          registration.Name,
			MinimumY:      int32(r.Min()),
			HeightRange:   int32(r.Height() + 1),
			Generator:     v2193.GeneratorVoid,
			DimensionType: int32(registration.ID),
		})
	}
	return defs
}

var (
	//go:embed vanilla_blocks.nbt
	vanillaBlocksData []byte
	// vanillaBlocks are the properties of the vanilla blocks the game defines
	// by data, by name, as BDS defines them to the client in StartGame. The
	// client draws none of these blocks, such as stairs and slabs of wool or
	// concrete, unless the server defines them.
	vanillaBlocks = map[string]map[string]any{}

	// vanillaItems are the vanilla items of the item registry of BDS, by
	// name, as the client is defined them in StartGame.
	vanillaItems = map[string]struct {
		RuntimeID      int32          `nbt:"runtime_id"`
		ComponentBased bool           `nbt:"component_based"`
		Version        int32          `nbt:"version"`
		Data           map[string]any `nbt:"data,omitempty"`
	}{}
)

// init reads the vanilla item entries and block definitions from their
// embedded NBT.
func init() {
	_ = nbt.Unmarshal(bedrockdata.VanillaItems, &vanillaItems)
	_ = nbt.Unmarshal(vanillaBlocksData, &vanillaBlocks)
}
