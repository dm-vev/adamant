package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func init() {
	worldFinaliseBlockRegistry()
}

type containerViewerStub struct {
	world.NopViewer
	slots []int
}

func (v *containerViewerStub) ViewSlotChange(slot int, _ item.Stack) {
	v.slots = append(v.slots, slot)
}

func TestNewShulkerBoxInitialisesRuntimeFields(t *testing.T) {
	box := NewShulkerBox()
	if box.inventory == nil {
		t.Fatalf("expected inventory to be initialised")
	}
	if box.viewerMu == nil {
		t.Fatalf("expected viewer mutex to be initialised")
	}
	if box.viewers == nil {
		t.Fatalf("expected viewers map to be initialised")
	}
	if box.progress == nil {
		t.Fatalf("expected progress tracker to be initialised")
	}
	if box.animationStatus == nil {
		t.Fatalf("expected animation status tracker to be initialised")
	}
}

func TestShulkerBoxEncodeNBTInitialisesInventory(t *testing.T) {
	box := ShulkerBox{Type: BlueShulkerBox(), Facing: cube.FaceSouth, CustomName: "Storage"}

	nbt := box.EncodeNBT()
	if _, ok := nbt["Items"].([]map[string]any); !ok {
		t.Fatalf("expected Items list in encoded NBT")
	}
	if nbt["facing"] != uint8(cube.FaceSouth) {
		t.Fatalf("expected facing to be preserved, got %v", nbt["facing"])
	}
	if nbt["CustomName"].(string) != "Storage" {
		t.Fatalf("expected custom name to be preserved")
	}

	decoded := ShulkerBox{Type: BlueShulkerBox()}.DecodeNBT(nbt).(ShulkerBox)
	if decoded.inventory == nil {
		t.Fatalf("expected decoded shulker box to have an initialised inventory")
	}
	if decoded.progress == nil || decoded.animationStatus == nil {
		t.Fatalf("expected decoded shulker box to have animation trackers initialised")
	}
}

func TestShulkerBoxInventoryRejectsNestedBoxes(t *testing.T) {
	box := NewShulkerBox()
	viewer := &containerViewerStub{}

	// Install a test slot change observer to capture calls.
	box.viewerMu.Lock()
	box.viewers[viewer] = struct{}{}
	box.viewerMu.Unlock()

	// Attempt to insert another shulker box; the callback should not notify viewers.
	shulkerStack := item.NewStack(ShulkerBox{Type: RedShulkerBox()}, 1)
	if err := box.inventory.SetItem(0, shulkerStack); err != nil {
		t.Fatalf("unexpected error setting slot: %v", err)
	}

	if got, _ := box.inventory.Item(0); !got.Equal(shulkerStack) {
		t.Fatalf("expected shulker box to be stored in inventory, got %v", got)
	}
	if len(viewer.slots) != 0 {
		t.Fatalf("expected no slot change notifications for nested shulker boxes")
	}

	// Non-shulker items should be accepted and propagated to viewers.
	apple := item.NewStack(item.Apple{}, 1)
	if err := box.inventory.SetItem(1, apple); err != nil {
		t.Fatalf("unexpected error setting apple: %v", err)
	}
	if got, _ := box.inventory.Item(1); !got.Equal(apple) {
		t.Fatalf("expected apple to be stored, got %v", got)
	}
	if len(viewer.slots) != 1 || viewer.slots[0] != 1 {
		t.Fatalf("expected a single slot change notification for non-shulker items, got %v", viewer.slots)
	}
}

func TestShulkerBoxBreakDropsPreserveInventory(t *testing.T) {
	box := NewShulkerBox()
	box.Type = PurpleShulkerBox()
	box.CustomName = "Treasure"

	loot := item.NewStack(item.Diamond{}, 2)
	if err := box.inventory.SetItem(5, loot); err != nil {
		t.Fatalf("unexpected error populating inventory: %v", err)
	}

	drops := box.BreakInfo().Drops(item.ToolNone{}, nil)
	if len(drops) != 1 {
		t.Fatalf("expected a single drop stack, got %d", len(drops))
	}

	dropBox, ok := drops[0].Item().(ShulkerBox)
	if !ok {
		t.Fatalf("expected drop item to be a shulker box, got %T", drops[0].Item())
	}
	if dropBox.inventory == nil {
		t.Fatalf("expected dropped shulker box to have an inventory")
	}
	got, err := dropBox.inventory.Item(5)
	if err != nil {
		t.Fatalf("unexpected error reading dropped inventory: %v", err)
	}
	if !got.Equal(loot) {
		t.Fatalf("expected drop inventory to contain %v, got %v", loot, got)
	}
	if dropBox.CustomName != box.CustomName {
		t.Fatalf("expected custom name to be preserved, got %q", dropBox.CustomName)
	}
	if dropBox.Type != box.Type {
		t.Fatalf("expected shulker box type %v to be preserved, got %v", box.Type, dropBox.Type)
	}
	if items, ok := dropBox.EncodeNBT()["Items"].([]map[string]any); !ok || len(items) != 1 {
		t.Fatalf("expected encoded NBT to contain one item entry, got %v", dropBox.EncodeNBT()["Items"])
	}
}
