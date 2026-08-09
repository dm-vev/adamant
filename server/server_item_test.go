package server

import (
	"image"
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block/customblock"
	"github.com/df-mc/dragonfly/server/item/category"
	"github.com/df-mc/dragonfly/server/world"
)

func TestItemEntriesCustomItem(t *testing.T) {
	entries := itemEntries(world.DefaultBlockRegistry, []world.CustomItem{customItem{}})
	if len(entries) != 1 {
		t.Fatalf("got %d item entries, want 1", len(entries))
	}
	if got := entries[0]; got.Name != "minecraft:stick" || !got.ComponentBased {
		t.Fatalf("unexpected custom item entry: %#v", got)
	}
}

func TestItemEntriesUseServerBlockRegistry(t *testing.T) {
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
			for _, entry := range itemEntries(test.br, global) {
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
