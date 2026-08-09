package item

import (
	"math"
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

func TestBlockItemNBTRoundTripUsesRegistry(t *testing.T) {
	registry := world.NewBlockRegistry()
	for _, powered := range []bool{false, true} {
		b := registryBlockItem{Powered: powered}
		name, states := b.EncodeBlock()
		registry.RegisterBlockState(world.BlockState{Name: name, Properties: states})
		registry.RegisterBlock(b)
	}
	registry.Finalize()

	want := NewStack(registryBlockItem{Powered: true}, 3)
	encoded := WriteNBT(want, true)
	got := ReadNBT(encoded, nil, registry)
	if got.Count() != want.Count() {
		t.Fatalf("count = %d, want %d", got.Count(), want.Count())
	}
	if gotItem, ok := got.Item().(registryBlockItem); !ok || !gotItem.Powered {
		t.Fatalf("item = %#v, want powered registryBlockItem", got.Item())
	}
	if reencoded := WriteNBT(got, true); !reflect.DeepEqual(reencoded, encoded) {
		t.Fatalf("round-trip NBT differs\ngot:  %#v\nwant: %#v", reencoded, encoded)
	}
}

type registryBlockItem struct {
	Powered bool
}

func (b registryBlockItem) EncodeBlock() (string, map[string]any) {
	return "test:registry_block", map[string]any{"powered": b.Powered}
}

func (registryBlockItem) EncodeItem() (string, int16) { return "test:registry_block", 0 }
func (registryBlockItem) Hash() (uint64, uint64)      { return 0, math.MaxUint64 }
func (registryBlockItem) Model() world.BlockModel     { return nil }
