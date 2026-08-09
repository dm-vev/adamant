package session

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func TestStackToItemUsesBlockRegistryForNestedNBT(t *testing.T) {
	registry := world.NewBlockRegistry()
	custom := sessionRegistryBlockItem{}
	name, states := custom.EncodeBlock()
	registry.RegisterBlockState(world.BlockState{Name: name, Properties: states})
	registry.RegisterBlock(custom)
	registry.Finalize()

	chest := block.NewChest()
	if err := chest.Inventory(nil, cube.Pos{}).SetItem(0, item.NewStack(custom, 2)); err != nil {
		t.Fatal(err)
	}
	got := stackToItem(registry, stackFromItem(registry, item.NewStack(chest, 1)))
	decoded, ok := got.Item().(block.Chest)
	if !ok {
		t.Fatalf("item = %#v, want block.Chest", got.Item())
	}
	nested, err := decoded.Inventory(nil, cube.Pos{}).Item(0)
	if err != nil {
		t.Fatal(err)
	}
	if nested.Count() != 2 || nested.Item() != custom {
		t.Fatalf("nested item = %#v x%d, want %#v x2", nested.Item(), nested.Count(), custom)
	}
}

func TestStackToItemClearsMissingLodestoneHandle(t *testing.T) {
	stack := stackFromItem(world.DefaultBlockRegistry, item.NewStack(item.Compass{TrackingHandle: 1}, 1))
	stack.NBTData = nil
	got := stackToItem(world.DefaultBlockRegistry, stack)
	compass, ok := got.Item().(item.Compass)
	if !ok || compass.TrackingHandle != 0 {
		t.Fatalf("decoded compass = %#v, want an unlinked compass", got.Item())
	}
}

type sessionRegistryBlockItem struct{}

func (sessionRegistryBlockItem) EncodeBlock() (string, map[string]any) {
	return "test:session_registry_block", nil
}
func (sessionRegistryBlockItem) EncodeItem() (string, int16) { return "test:session_registry_block", 0 }
func (sessionRegistryBlockItem) Hash() (uint64, uint64)      { return 0, math.MaxUint64 }
func (sessionRegistryBlockItem) Model() world.BlockModel     { return nil }
