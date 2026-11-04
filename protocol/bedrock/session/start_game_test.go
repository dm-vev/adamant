package session

import (
	"image"
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block/customblock"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/category"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

func TestCustomItemEntriesCustomItem(t *testing.T) {
	entries := customItemEntries(tablesOf(nil, world.DefaultBlockRegistry), []world.CustomItem{customItem{}})
	if len(entries) != 1 {
		t.Fatalf("got %d item entries, want 1", len(entries))
	}
	if got := entries[0]; got.Name != "minecraft:stick" || !got.ComponentBased {
		t.Fatalf("unexpected custom item entry: %#v", got)
	}
}

func TestCustomItemEntriesUseServerBlockRegistry(t *testing.T) {
	a, b := customBlockItem{id: "test:registry_a"}, customBlockItem{id: "test:registry_b"}
	unregistered := customBlockItem{id: "test:registry_unregistered"}
	world.RegisterItem(a)
	world.RegisterItem(b)
	registryA, registryB := world.NewBlockRegistry(), world.NewBlockRegistry()
	registryA.RegisterBlock(a)
	registryA.RegisterBlock(unregistered)
	registryB.RegisterBlock(b)

	global := []world.CustomItem{a, b, customItem{}}
	for _, test := range []struct {
		name    string
		br      world.BlockRegistry
		want    string
		notWant string
	}{
		{name: "A", br: registryA, want: a.id, notWant: b.id},
		{name: "B", br: registryB, want: b.id, notWant: a.id},
	} {
		t.Run(test.name, func(t *testing.T) {
			names := map[string]bool{}
			for _, entry := range customItemEntries(tablesOf(nil, test.br), global) {
				if entry.RuntimeID == 0 {
					t.Fatalf("entry %q has invalid runtime ID 0", entry.Name)
				}
				names[entry.Name] = true
			}
			if !names[test.want] || !names["minecraft:stick"] {
				t.Fatalf("entries %v do not contain registry block %q and independent custom item", names, test.want)
			}
			if names[test.notWant] {
				t.Fatalf("entries %v contain block %q from another registry", names, test.notWant)
			}
			if names[unregistered.id] {
				t.Fatalf("entries %v contain unregistered block item %q", names, unregistered.id)
			}
		})
	}
}

type customItem struct{}

func (customItem) EncodeItem() (string, int16) { return "minecraft:stick", 0 }
func (customItem) Name() string                { return "Test Stick" }
func (customItem) Texture() image.Image        { return image.NewRGBA(image.Rect(0, 0, 1, 1)) }
func (customItem) Category() category.Category { return category.Items() }

type customBlockItem struct {
	id string
}

func (b customBlockItem) EncodeBlock() (string, map[string]any) { return b.id, map[string]any{} }
func (b customBlockItem) EncodeItem() (string, int16)           { return b.id, 0 }
func (customBlockItem) Hash() (uint64, uint64)                  { return 0, math.MaxUint64 }
func (customBlockItem) Model() world.BlockModel                 { return nil }
func (customBlockItem) Properties() customblock.Properties      { return customblock.Properties{} }
func (b customBlockItem) Name() string                          { return b.id }
func (customBlockItem) Texture() image.Image                    { return image.NewRGBA(image.Rect(0, 0, 1, 1)) }
func (customBlockItem) Category() category.Category             { return category.Construction() }

// TestBlockEntriesDefineBlocksOfData checks that the client learns the
// vanilla blocks the game defines by data from the server, as from BDS: it
// draws none of them otherwise, and the tents of abandoned camps, of white
// wool stairs, stood invisible over their fences.
func TestBlockEntriesDefineBlocksOfData(t *testing.T) {
	blocks := map[string]map[string]any{}
	for _, b := range newStartContent(tablesOf(nil, world.DefaultBlockRegistry), nil, nil).Blocks() {
		blocks[b.Name] = b.Properties
	}
	for _, colour := range item.Colours() {
		for _, kind := range []string{"wool_stairs", "wool_slab", "wool_double_slab", "concrete_stairs", "concrete_slab", "concrete_double_slab"} {
			name := "minecraft:" + colour.String() + "_" + kind
			if _, ok := blocks[name]; !ok {
				t.Errorf("%s is not defined", name)
			}
		}
	}
	data, _ := blocks["minecraft:white_wool_stairs"]["vanilla_block_data"].(map[string]any)
	archetype, _ := data["block_archetype"].(map[string]any)
	if archetype["base_block"] != "minecraft:white_wool" || archetype["type"] != "stair_block" {
		t.Errorf("white wool stairs are built on %v, want stairs of white wool", archetype)
	}
}

// TestBlocksOfDataAreBlocksOfTheServer checks that the server has a block
// for each vanilla block it defines to the client.
func TestBlocksOfDataAreBlocksOfTheServer(t *testing.T) {
	if len(vanillaBlocks) == 0 {
		t.Fatal("no vanilla blocks are defined")
	}
	for name := range vanillaBlocks {
		if _, ok := world.DefaultBlockRegistry.StateToRuntimeID(name, nil); !ok {
			t.Errorf("%s is no block of the server", name)
		}
	}
}

// TestDimensionDefinitions checks that the client is defined the custom
// dimensions of the game by name, range and ID, generated as void.
func TestDimensionDefinitions(t *testing.T) {
	defs := newStartContent(tablesOf(nil, world.DefaultBlockRegistry), nil, []world.DimensionRegistration{
		{ID: 3, Name: "adamant:test", Dimension: world.Overworld},
	}).Dimensions()
	r := world.Overworld.Range()
	want := v2193.DimensionDefinition{Name: "adamant:test", MinimumY: int32(r.Min()), HeightRange: int32(r.Height() + 1), Generator: v2193.GeneratorVoid, DimensionType: 3}
	if len(defs) != 1 || defs[0] != want {
		t.Fatalf("dimensions = %#v, want %#v", defs, []v2193.DimensionDefinition{want})
	}
}
