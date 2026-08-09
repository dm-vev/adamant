package server

import (
	"image"
	"testing"

	"github.com/df-mc/dragonfly/server/item/category"
	"github.com/df-mc/dragonfly/server/world"
)

func TestItemEntriesCustomItem(t *testing.T) {
	entries := itemEntries([]world.CustomItem{customItem{}})
	if len(entries) != 1 {
		t.Fatalf("got %d item entries, want 1", len(entries))
	}
	if got := entries[0]; got.Name != "minecraft:stick" || !got.ComponentBased {
		t.Fatalf("unexpected custom item entry: %#v", got)
	}
}

type customItem struct{}

func (customItem) EncodeItem() (string, int16) { return "minecraft:stick", 0 }
func (customItem) Name() string                { return "Test Stick" }
func (customItem) Texture() image.Image        { return image.NewRGBA(image.Rect(0, 0, 1, 1)) }
func (customItem) Category() category.Category { return category.Items() }
