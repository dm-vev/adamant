package entity

import (
	"math"
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func TestDroppedItemNBTRoundTripUsesBlockRegistry(t *testing.T) {
	registry, blockItem := entityTestBlockRegistry()
	want := item.NewStack(blockItem, 2)
	behaviour := ItemBehaviourConfig{Item: want}.New(registry)
	encoded := ItemType.EncodeNBT(&world.EntityData{Data: behaviour})

	decoded := new(world.EntityData)
	world.DecodeEntityNBT(ItemType, encoded, decoded, registry)
	got := decoded.Data.(*ItemBehaviour).Item()
	assertEntityBlockItem(t, got, want, blockItem)
	if reencoded := ItemType.EncodeNBT(decoded); !reflect.DeepEqual(reencoded, encoded) {
		t.Fatalf("round-trip NBT differs\ngot:  %#v\nwant: %#v", reencoded, encoded)
	}
}

func TestMinecartContainerNBTRoundTripUsesBlockRegistry(t *testing.T) {
	registry, blockItem := entityTestBlockRegistry()
	want := item.NewStack(blockItem, 4)
	behaviour := chestMinecartConf.New()
	if err := behaviour.inv.SetItem(8, want); err != nil {
		t.Fatal(err)
	}
	encoded := ChestMinecartType.EncodeNBT(&world.EntityData{Data: behaviour})

	decoded := new(world.EntityData)
	world.DecodeEntityNBT(ChestMinecartType, encoded, decoded, registry)
	got, err := decoded.Data.(*MinecartContainerBehaviour).inv.Item(8)
	if err != nil {
		t.Fatal(err)
	}
	assertEntityBlockItem(t, got, want, blockItem)
	if reencoded := ChestMinecartType.EncodeNBT(decoded); !reflect.DeepEqual(reencoded, encoded) {
		t.Fatalf("round-trip NBT differs\ngot:  %#v\nwant: %#v", reencoded, encoded)
	}
}

func assertEntityBlockItem(t *testing.T, got, want item.Stack, blockItem entityTestBlockItem) {
	t.Helper()
	if got.Count() != want.Count() || got.Item() != blockItem {
		t.Fatalf("item = %#v x%d, want %#v x%d", got.Item(), got.Count(), blockItem, want.Count())
	}
}

func entityTestBlockRegistry() (world.BlockRegistry, entityTestBlockItem) {
	registry := world.NewBlockRegistry()
	blockItem := entityTestBlockItem{Powered: true}
	name, states := blockItem.EncodeBlock()
	registry.RegisterBlockState(world.BlockState{Name: name, Properties: states})
	registry.RegisterBlock(blockItem)
	registry.Finalize()
	return registry, blockItem
}

type entityTestBlockItem struct {
	Powered bool
}

func (b entityTestBlockItem) EncodeBlock() (string, map[string]any) {
	return "test:entity_block_item", map[string]any{"powered": b.Powered}
}

func (entityTestBlockItem) EncodeItem() (string, int16) { return "test:entity_block_item", 0 }
func (entityTestBlockItem) Hash() (uint64, uint64)      { return 0, math.MaxUint64 }
func (entityTestBlockItem) Model() world.BlockModel     { return nil }
