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

func TestShulkerBoxEnsureInitInitialisesRuntimeFields(t *testing.T) {
	w := world.Config{Generator: world.NopGenerator{}, Provider: world.NopProvider{}}.New()
	defer w.Close()

	pos := cube.Pos{0, 64, 0}

	done := w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos, ShulkerBox{Type: NormalShulkerBox()}, nil)

		blk, ok := tx.Block(pos).(ShulkerBox)
		if !ok {
			t.Fatalf("expected shulker box at %v, got %T", pos, tx.Block(pos))
		}
		if blk.inventory != nil || blk.viewerMu != nil || blk.viewers != nil || blk.progress != nil || blk.animationStatus != nil {
			t.Fatalf("expected zero-value shulker box runtime fields to be nil")
		}

		inv := blk.Inventory(tx, pos)
		if inv == nil {
			t.Fatalf("expected Inventory to return an initialised inventory")
		}

		blkAfter := tx.Block(pos).(ShulkerBox)
		if blkAfter.inventory == nil {
			t.Fatalf("expected inventory to be initialised")
		}
		if blkAfter.viewerMu == nil {
			t.Fatalf("expected viewer mutex to be initialised")
		}
		if blkAfter.viewers == nil {
			t.Fatalf("expected viewers map to be initialised")
		}
		if blkAfter.progress == nil {
			t.Fatalf("expected progress tracker to be initialised")
		}
		if blkAfter.animationStatus == nil {
			t.Fatalf("expected animation status tracker to be initialised")
		}
		if inv != blkAfter.inventory {
			t.Fatalf("inventory returned by Inventory() should match stored inventory pointer")
		}
	})

	<-done
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
