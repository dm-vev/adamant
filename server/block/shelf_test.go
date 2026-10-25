package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// shelfTestWorld runs f with shelves facing north along x from 0, at z 0.
func shelfTestWorld(t *testing.T, f func(tx *world.Tx)) {
	t.Helper()
	w := world.Config{Generator: world.NopGenerator{}, Provider: world.NopProvider{}}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(f)
}

// powerShelf powers the shelf at pos from a block of redstone behind it, as
// the neighbour update of the block would.
func powerShelf(pos cube.Pos, tx *world.Tx) {
	tx.SetBlock(pos.Side(cube.FaceSouth), RedstoneBlock{}, nil)
	tx.Block(pos).(Shelf).NeighbourUpdateTick(pos, pos.Side(cube.FaceSouth), tx)
}

// TestShelfSwapsTheHeldItem checks ShelfBlock.useItemOn on an unpowered
// shelf: the front column clicked picks the slot, which swaps with the held
// stack, and only a comparator behind the shelf reads its slots.
func TestShelfSwapsTheHeldItem(t *testing.T) {
	shelfTestWorld(t, func(tx *world.Tx) {
		pos := cube.Pos{0, 64, 0}
		tx.SetBlock(pos, Shelf{Facing: cube.North}, nil)
		user := &chiseledBookshelfUser{held: item.NewStack(item.Apple{}, 5), inv: inventory.New(36, nil)}
		ctx := &item.UseContext{}
		// A shelf facing north has its first slot at the east, seen from the
		// front.
		if !tx.Block(pos).(Shelf).ActivateAt(pos, cube.FaceNorth, mgl64.Vec3{0.9, 0.5, 0}, tx, user, ctx) {
			t.Fatal("clicking the front of a shelf with an item did nothing")
		}
		shelf := tx.Block(pos).(Shelf)
		if shelf.Items[0].Count() != 5 || ctx.CountSub != 5 {
			t.Fatalf("shelf slots %v, held count taken %d; want the five apples in slot 0", shelf.Items, ctx.CountSub)
		}
		if tx.Block(pos).(Shelf).ActivateAt(pos, cube.FaceUp, mgl64.Vec3{0.9, 1, 0.5}, tx, user, &item.UseContext{}) {
			t.Fatal("the top of a shelf took an item")
		}
		if out := shelf.ComparatorOutputTowards(tx, pos, cube.FaceSouth); out != 1 {
			t.Fatalf("comparator behind the shelf read %d, want 1", out)
		}
		if out := shelf.ComparatorOutputTowards(tx, pos, cube.FaceEast); out != 0 {
			t.Fatalf("comparator beside the shelf read %d, want 0", out)
		}
		user.held = item.NewStack(item.Bread{}, 2)
		ctx = &item.UseContext{}
		shelf.ActivateAt(pos, cube.FaceNorth, mgl64.Vec3{0.9, 0.5, 0}, tx, user, ctx)
		if got := tx.Block(pos).(Shelf).Items[0]; got.Count() != 2 || !ctx.ReplaceHeldItem || ctx.NewItem.Count() != 5 {
			t.Fatalf("swap left %v on the shelf and %v in hand", got, ctx.NewItem)
		}
		restored := tx.Block(pos).(Shelf).DecodeNBT(tx.Block(pos).(Shelf).EncodeNBT()).(Shelf)
		if restored.Items[0].Count() != 2 || !restored.Items[1].Empty() {
			t.Fatalf("shelf actor round trip gave %v", restored.Items)
		}
	})
}

// TestPoweredShelvesChainAndSwapTheHotbar checks SideChainPartBlock: powered
// shelves side by side chain up to three, and a click on one swaps the end of
// the hotbar with the shelves of its chain, left to right.
func TestPoweredShelvesChainAndSwapTheHotbar(t *testing.T) {
	shelfTestWorld(t, func(tx *world.Tx) {
		// Seen from the front of a shelf facing north, left is east.
		positions := []cube.Pos{{3, 64, 0}, {2, 64, 0}, {1, 64, 0}, {0, 64, 0}}
		for _, pos := range positions {
			tx.SetBlock(pos, Shelf{Facing: cube.North}, nil)
		}
		for _, pos := range positions {
			powerShelf(pos, tx)
		}
		want := []uint8{shelfLeft, shelfCenter, shelfRight, shelfUnconnected}
		for i, pos := range positions {
			if got := tx.Block(pos).(Shelf).PoweredShelfType; got != want[i] {
				t.Fatalf("shelf %d of the row is part %d, want %d", i, got, want[i])
			}
		}
		user := &chiseledBookshelfUser{inv: inventory.New(36, nil)}
		for slot := range 9 {
			_ = user.inv.SetItem(slot, item.NewStack(item.Stick{}, slot+1))
		}
		ctx := &item.UseContext{}
		if !tx.Block(positions[1]).(Shelf).ActivateAt(positions[1], cube.FaceNorth, mgl64.Vec3{0.5, 0.5, 0}, tx, user, ctx) || !ctx.HeldItemSet {
			t.Fatal("clicking a powered shelf did not swap the hotbar")
		}
		for i, pos := range positions[:3] {
			for slot, stack := range tx.Block(pos).(Shelf).Items {
				if stack.Count() != i*3+slot+1 {
					t.Fatalf("slot %d of shelf %d holds %v, want %d sticks", slot, i, stack, i*3+slot+1)
				}
			}
		}
		for slot := range 9 {
			if stack, _ := user.inv.Item(slot); !stack.Empty() {
				t.Fatalf("hotbar slot %d kept %v after the swap", slot, stack)
			}
		}
		// Unpowering the middle shelf breaks the chain on both sides.
		tx.SetBlock(positions[1].Side(cube.FaceSouth), nil, nil)
		tx.Block(positions[1]).(Shelf).NeighbourUpdateTick(positions[1], positions[1].Side(cube.FaceSouth), tx)
		want = []uint8{shelfUnconnected, shelfUnconnected, shelfUnconnected}
		for i, pos := range positions[:3] {
			if got := tx.Block(pos).(Shelf).PoweredShelfType; got != want[i] {
				t.Fatalf("after unpowering, shelf %d of the row is part %d, want %d", i, got, want[i])
			}
		}
	})
}

// TestHoppersFillAndEmptyShelves checks the shelf as a container of hoppers:
// they push into the first slot with room and pull from the first occupied.
func TestHoppersFillAndEmptyShelves(t *testing.T) {
	shelfTestWorld(t, func(tx *world.Tx) {
		pos := cube.Pos{0, 64, 0}
		shelf := Shelf{Facing: cube.North}
		shelf.Items[0] = item.NewStack(item.Apple{}, 64)
		tx.SetBlock(pos, shelf, nil)
		h := NewHopper()
		_ = h.inventory.SetItem(0, item.NewStack(item.Apple{}, 3))
		if !shelf.InsertItem(h, pos, tx) {
			t.Fatal("hopper did not push into the shelf")
		}
		got := tx.Block(pos).(Shelf)
		if got.Items[0].Count() != 64 || got.Items[1].Count() != 1 {
			t.Fatalf("shelf after a push holds %v", got.Items)
		}
		pull := NewHopper()
		if !got.ExtractItem(pull, pos, tx) || tx.Block(pos).(Shelf).Items[0].Count() != 63 {
			t.Fatalf("hopper pull left %v", tx.Block(pos).(Shelf).Items)
		}
	})
}
