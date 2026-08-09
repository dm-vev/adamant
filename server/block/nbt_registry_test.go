package block

import (
	"math"
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func TestContainerNBTRoundTripUsesBlockRegistry(t *testing.T) {
	registry, blockItem := containerTestBlockRegistry()
	want := item.NewStack(blockItem, 3)
	chest := NewChest()
	if err := chest.inventory.SetItem(4, want); err != nil {
		t.Fatal(err)
	}

	encoded := chest.EncodeNBT()
	decoded := world.DecodeNBT(Chest{}, encoded, registry).(Chest)
	got, err := decoded.inventory.Item(4)
	if err != nil {
		t.Fatal(err)
	}
	if got.Count() != want.Count() || got.Item() != blockItem {
		t.Fatalf("item = %#v x%d, want %#v x%d", got.Item(), got.Count(), blockItem, want.Count())
	}
	if reencoded := decoded.EncodeNBT(); !reflect.DeepEqual(reencoded, encoded) {
		t.Fatalf("round-trip NBT differs\ngot:  %#v\nwant: %#v", reencoded, encoded)
	}
}

func containerTestBlockRegistry() (world.BlockRegistry, containerTestBlockItem) {
	registry := world.NewBlockRegistry()
	blockItem := containerTestBlockItem{Powered: true}
	name, states := blockItem.EncodeBlock()
	registry.RegisterBlockState(world.BlockState{Name: name, Properties: states})
	registry.RegisterBlock(blockItem)
	registry.Finalize()
	return registry, blockItem
}

type containerTestBlockItem struct {
	Powered bool
}

func (b containerTestBlockItem) EncodeBlock() (string, map[string]any) {
	return "test:container_block_item", map[string]any{"powered": b.Powered}
}

func (containerTestBlockItem) EncodeItem() (string, int16) { return "test:container_block_item", 0 }
func (containerTestBlockItem) Hash() (uint64, uint64)      { return 0, math.MaxUint64 }
func (containerTestBlockItem) Model() world.BlockModel     { return nil }
