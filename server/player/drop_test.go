package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
)

// dropInventoryHandler records the drops an inventory sees and cancels them
// if cancel is set.
type dropInventoryHandler struct {
	inventory.NopHandler
	cancel bool
	slots  []int
	stacks []item.Stack
}

func (h *dropInventoryHandler) HandleDrop(ctx *inventory.Context, slot int, it item.Stack) {
	h.slots = append(h.slots, slot)
	h.stacks = append(h.stacks, it)
	if h.cancel {
		ctx.Cancel()
	}
}

// cancelItemDropHandler cancels every drop of the player.
type cancelItemDropHandler struct{ NopHandler }

func (cancelItemDropHandler) HandleItemDrop(ctx *Context, _ item.Stack) { ctx.Cancel() }

// droppedItems returns the stacks of the items in tx.
func droppedItems(tx *world.Tx) []item.Stack {
	var stacks []item.Stack
	for e := range tx.Entities() {
		if it, ok := e.(*entity.Ent); ok {
			if b, ok := it.Behaviour().(*entity.ItemBehaviour); ok {
				stacks = append(stacks, b.Item())
			}
		}
	}
	return stacks
}

func TestPlayerDropFromInventory(t *testing.T) {
	w, _ := clientTestWorld(t)
	withClientPlayer(t, w, nil, func(tx *world.Tx, p *Player) {
		h := &dropInventoryHandler{}
		p.Inventory().Handle(h)
		_ = p.Inventory().SetItem(5, item.NewStack(item.Apple{}, 10))
		_ = p.SetHeldSlot(2)

		if err := p.DropFromInventory(5, 3); err != nil {
			t.Fatalf("DropFromInventory: %v", err)
		}
		if it, _ := p.Inventory().Item(5); it.Count() != 7 {
			t.Fatalf("slot 5 holds %v after the drop, want 7 apples", it)
		}
		// The handler of the inventory is told the held slot, as the
		// Bedrock session told it before.
		if len(h.slots) != 1 || h.slots[0] != 2 || h.stacks[0].Count() != 3 {
			t.Fatalf("HandleDrop saw slots %v and stacks %v, want slot 2 and 3 apples", h.slots, h.stacks)
		}
		if got := droppedItems(tx); len(got) != 1 || got[0].Count() != 3 {
			t.Fatalf("dropped %v, want one item of 3 apples", got)
		}
	})
}

func TestPlayerDropFromInventoryRefuses(t *testing.T) {
	cases := []struct {
		name  string
		stack item.Stack
		count int
	}{
		{"none", item.NewStack(item.Apple{}, 4), 0},
		{"more than held", item.NewStack(item.Apple{}, 4), 5},
		{"empty slot", item.Stack{}, 1},
		{"locked in inventory", item.NewStack(item.Apple{}, 4).WithLockMode(item.LockInInventory), 1},
		{"locked in slot", item.NewStack(item.Apple{}, 4).WithLockMode(item.LockInSlot), 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w, _ := clientTestWorld(t)
			withClientPlayer(t, w, nil, func(tx *world.Tx, p *Player) {
				h := &dropInventoryHandler{}
				p.Inventory().Handle(h)
				_ = p.Inventory().SetItem(0, c.stack)
				if err := p.DropFromInventory(0, c.count); err == nil {
					t.Fatal("DropFromInventory succeeded")
				}
				if it, _ := p.Inventory().Item(0); !it.Equal(c.stack) || it.Count() != c.stack.Count() {
					t.Fatalf("slot 0 holds %v after a refused drop, want %v", it, c.stack)
				}
				if len(h.slots) != 0 || len(droppedItems(tx)) != 0 {
					t.Fatal("a refused drop reached the handler or the world")
				}
			})
		})
	}
}

func TestPlayerDropFromInventoryCancelled(t *testing.T) {
	t.Run("inventory", func(t *testing.T) {
		w, _ := clientTestWorld(t)
		withClientPlayer(t, w, nil, func(tx *world.Tx, p *Player) {
			p.Inventory().Handle(&dropInventoryHandler{cancel: true})
			_ = p.Inventory().SetItem(0, item.NewStack(item.Apple{}, 4))
			if err := p.DropFromInventory(0, 2); err == nil {
				t.Fatal("DropFromInventory succeeded though the inventory cancelled it")
			}
			if it, _ := p.Inventory().Item(0); it.Count() != 4 || len(droppedItems(tx)) != 0 {
				t.Fatalf("slot 0 holds %v after a cancelled drop, want 4 apples and nothing dropped", it)
			}
		})
	})
	t.Run("player", func(t *testing.T) {
		w, _ := clientTestWorld(t)
		withClientPlayer(t, w, nil, func(tx *world.Tx, p *Player) {
			p.Handle(cancelItemDropHandler{})
			_ = p.Inventory().SetItem(0, item.NewStack(item.Apple{}, 4))
			// Drop returns 0 when cancelled: the slot keeps the stack and
			// the session is not ended.
			if err := p.DropFromInventory(0, 2); err != nil {
				t.Fatalf("DropFromInventory: %v", err)
			}
			if it, _ := p.Inventory().Item(0); it.Count() != 4 || len(droppedItems(tx)) != 0 {
				t.Fatalf("slot 0 holds %v after a cancelled drop, want 4 apples and nothing dropped", it)
			}
		})
	})
}
