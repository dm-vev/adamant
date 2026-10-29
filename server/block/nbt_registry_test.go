package block

import (
	"math"
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/nbt"
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

func TestSculkFallbackNBTPersistsWithoutDecoderContext(t *testing.T) {
	listener := (&world.VibrationData{}).EncodeNBT()
	source := map[string]any{
		"id": "SculkShrieker", "warning_level": int32(3),
		"listener": listener, "future_java_field": []int32{7, -9},
	}
	actor := world.DecodeNBT(SculkShrieker{}, source, world.DefaultBlockRegistry).(SculkShrieker)
	encoded, err := nbt.MarshalEncoding(actor.EncodeNBT(), nbt.LittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	var persisted map[string]any
	if err := nbt.UnmarshalEncoding(encoded, &persisted, nbt.LittleEndian); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"warning_level": int32(3), "listener": listener,
		"future_java_field": []int32{7, -9},
	}
	if got := persisted["adamant:java"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("persisted source state includes decoder context or lost data: got=%v want=%v", got, want)
	}
}
