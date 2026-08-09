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
	// Display tiles still use the legacy default registry path and are unrelated to nested inventory decoding.
	delete(encoded, "CustomDisplayTile")
	delete(encoded, "DisplayTile")
	delete(encoded, "DisplayOffset")

	decoded := new(world.EntityData)
	world.DecodeEntityNBT(ChestMinecartType, encoded, decoded, registry)
	got, err := decoded.Data.(*MinecartContainerBehaviour).inv.Item(8)
	if err != nil {
		t.Fatal(err)
	}
	assertEntityBlockItem(t, got, want, blockItem)
}

func TestFallingBlockNBTUsesBlockRegistry(t *testing.T) {
	registry, blockItem := entityTestBlockRegistry()
	encoded := FallingBlockType.EncodeNBT(&world.EntityData{Data: FallingBlockBehaviourConfig{Block: blockItem}.New()})

	decoded := new(world.EntityData)
	world.DecodeEntityNBT(FallingBlockType, encoded, decoded, registry)
	if got := decoded.Data.(*FallingBlockBehaviour).block; got != blockItem {
		t.Fatalf("falling block = %#v, want %#v", got, blockItem)
	}
}

func assertEntityBlockItem(t *testing.T, got, want item.Stack, blockItem entityTestBlockItem) {
	t.Helper()
	if got.Count() != want.Count() || got.Item() != blockItem {
		t.Fatalf("item = %#v x%d, want %#v x%d", got.Item(), got.Count(), blockItem, want.Count())
	}
}

func entityTestBlockRegistry() (world.BlockRegistry, entityTestBlockItem) {
	blockItem := entityTestBlockItem{Powered: true}
	return entityBlockRegistry{block: blockItem}, blockItem
}

type entityBlockRegistry struct {
	world.BlockRegistry
	block entityTestBlockItem
}

func (r entityBlockRegistry) BlockByName(name string, properties map[string]any) (world.Block, bool) {
	wantName, wantProperties := r.block.EncodeBlock()
	if name == wantName && reflect.DeepEqual(properties, wantProperties) {
		return r.block, true
	}
	return nil, false
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
