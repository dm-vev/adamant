package menu_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/creative"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// inTx runs f in a transaction of a fresh synchronous world, which spawns
// the entities of the game.
func inTx(t *testing.T, f func(tx *world.Tx)) {
	t.Helper()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(f)
}

var (
	apples = func(n int) item.Stack { return item.NewStack(item.Apple{}, n) }
	stone  = func(n int) item.Stack { return item.NewStack(block.Stone{}, n) }
	cursed = item.NewStack(item.Helmet{Tier: item.ArmourTierLeather{}}, 1).WithEnchantments(item.NewEnchantment(enchantment.CurseOfBinding, 1))
)

// TestMove checks moves within the inventory: onto a stack of the same
// item and into an empty slot, and the refusals of more than the source
// holds, more than the destination takes and stacks that differ. A refused
// move changes nothing.
func TestMove(t *testing.T) {
	for _, c := range []struct {
		name           string
		from, to       item.Stack
		count          int
		ok             bool
		wantFrom, want item.Stack
	}{
		{name: "onto apples", from: apples(10), to: apples(5), count: 4, ok: true, wantFrom: apples(6), want: apples(9)},
		{name: "into an empty slot", from: apples(10), count: 3, ok: true, wantFrom: apples(7), want: apples(3)},
		{name: "all of it", from: apples(10), count: 10, ok: true, want: apples(10)},
		{name: "more than there is", from: apples(10), count: 11},
		{name: "more than fits", from: apples(10), to: apples(60), count: 5},
		{name: "onto stone", from: apples(10), to: stone(1), count: 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			inTx(t, func(tx *world.Tx) {
				b, inv := ownerBatch(tx, nil, &fakeOwner{mode: world.GameModeSurvival})
				_ = inv.Main.SetItem(0, c.from)
				_ = inv.Main.SetItem(1, c.to)
				err := b.Move(ref(inv.Main, 0, 7), ref(inv.Main, 1, 8), c.count)
				if (err == nil) != c.ok {
					t.Fatalf("Move: %v, want accepted %v", err, c.ok)
				}
				if !c.ok {
					if len(b.Changes()) != 0 || !slotOf(inv.Main, 0).Equal(c.from) || !slotOf(inv.Main, 1).Equal(c.to) {
						t.Fatalf("a refused move changed %v", b.Changes())
					}
					return
				}
				if got := slotOf(inv.Main, 0); !got.Equal(c.wantFrom) {
					t.Errorf("the source holds %v, want %v", got, c.wantFrom)
				}
				if got := slotOf(inv.Main, 1); !got.Equal(c.want) {
					t.Errorf("the destination holds %v, want %v", got, c.want)
				}
				changes := b.Changes()
				if len(changes) != 2 || changes[0].Ref.Name != 7 || changes[1].Ref.Name != 8 {
					t.Errorf("the journal holds %v, want the source then the destination", changes)
				}
			})
		})
	}
}

// TestMoveOntoItsOwnSlot checks that a move or a swap of a slot with
// itself is refused with ErrSameSlot and changes nothing: a slot of the
// inventory, of the armour, the off hand and the cursor, part or all of
// what it holds. A move onto its own slot took the items out of it and
// added them to what it held before: six apples became seven. BDS
// 1.26.52.3 refuses such a request (status 48), whatever the slot.
func TestMoveOntoItsOwnSlot(t *testing.T) {
	inTx(t, func(tx *world.Tx) {
		b, inv := ownerBatch(tx, nil, &fakeOwner{mode: world.GameModeSurvival})
		_ = inv.Main.SetItem(1, apples(6))
		_ = inv.OffHand.SetItem(0, item.NewStack(item.Totem{}, 1))
		inv.Armour.SetHelmet(cursed)
		_ = inv.UI.SetItem(menu.UICursor, apples(3))
		for _, c := range []struct {
			name  string
			slot  menu.Ref
			count int
		}{
			{"an apple of the inventory", ref(inv.Main, 1, 1), 1},
			{"all the apples of the inventory", ref(inv.Main, 1, 1), 6},
			{"the totem of the off hand", ref(inv.OffHand, 0, 2), 1},
			{"the cursed helmet", ref(inv.Armour.Inventory(), 0, 3), 1},
			{"an apple of the cursor", ref(inv.UI, menu.UICursor, 4), 1},
		} {
			before, _ := c.slot.Inv.Item(c.slot.Index)
			if err := b.Move(c.slot, c.slot, c.count); !errors.Is(err, menu.ErrSameSlot) {
				t.Errorf("%s moved onto its own slot: %v, want ErrSameSlot", c.name, err)
			}
			if err := b.Swap(c.slot, c.slot); !errors.Is(err, menu.ErrSameSlot) {
				t.Errorf("%s swapped with itself: %v, want ErrSameSlot", c.name, err)
			}
			if after, _ := c.slot.Inv.Item(c.slot.Index); !after.Equal(before) {
				t.Errorf("%s: the slot holds %v, held %v", c.name, after, before)
			}
		}
		if n := len(b.Changes()); n != 0 {
			t.Errorf("moves onto their own slot made %d changes", n)
		}
	})
}

// TestMoveBundles checks moves into and out of bundles: what fits goes in
// and the rest stays, a full bundle and an empty one refuse, and what comes
// out goes onto a stack of its item only. Each plays the sound of the
// bundle.
func TestMoveBundles(t *testing.T) {
	inTx(t, func(tx *world.Tx) {
		b, inv := ownerBatch(tx, nil, &fakeOwner{mode: world.GameModeSurvival})
		_ = inv.Main.SetItem(0, item.NewStack(item.Bundle{}, 1))
		_ = inv.Main.SetItem(1, apples(64))
		_ = inv.Main.SetItem(5, apples(3))
		_ = inv.Main.SetItem(2, stone(4))
		_ = inv.Main.SetItem(3, item.NewStack(item.Bundle{}, 1))
		bundle, from := ref(inv.Main, 0, 0), ref(inv.Main, 1, 1)
		if err := b.Move(ref(inv.Main, 5, 5), bundle, 3); err != nil {
			t.Fatal(err)
		}
		if err := b.Move(from, bundle, 64); err != nil {
			t.Fatal(err)
		}
		left := slotOf(inv.Main, 1)
		if left.Count() == 0 || left.Count() == 64 {
			t.Fatalf("%d apples left of 64 put in a bundle of 3: all or none went in", left.Count())
		}
		if err := b.Move(from, bundle, 1); err == nil {
			t.Error("an apple went in a full bundle")
		}
		if err := b.Move(bundle, ref(inv.Main, 2, 2), 1); err == nil {
			t.Error("an apple came out of a bundle onto stone")
		}
		if err := b.Move(bundle, from, 1); err != nil {
			t.Fatal(err)
		}
		if got := slotOf(inv.Main, 1).Count(); got != left.Count()+1 {
			t.Errorf("%d apples after one came out of the bundle onto %d", got, left.Count())
		}
		if err := b.Move(ref(inv.Main, 3, 3), from, 1); err == nil {
			t.Error("an apple came out of an empty bundle")
		}
		if err := b.Move(ref(inv.Main, 3, 3), ref(inv.Main, 4, 4), 1); err != nil {
			t.Errorf("the empty bundle did not move to an empty slot: %v", err)
		}
	})
}

// cancelling is an inventory.Handler that logs its calls and cancels those
// named in cancel, as "take 3".
type cancelling struct {
	log    *[]string
	cancel string
}

func (h cancelling) note(ctx *inventory.Context, kind string, slot int, it item.Stack) {
	key := fmt.Sprintf("%s %d", kind, slot)
	*h.log = append(*h.log, fmt.Sprintf("%s of %d", key, it.Count()))
	if key == h.cancel {
		ctx.Cancel()
	}
}
func (h cancelling) HandleTake(ctx *inventory.Context, slot int, it item.Stack) {
	h.note(ctx, "take", slot, it)
}
func (h cancelling) HandlePlace(ctx *inventory.Context, slot int, it item.Stack) {
	h.note(ctx, "place", slot, it)
}
func (h cancelling) HandleDrop(ctx *inventory.Context, slot int, it item.Stack) {
	h.note(ctx, "drop", slot, it)
}

// TestMoveAndSwapCallHandlers checks the handlers of the inventories a move
// and a swap take from and place in: the take, then the place, at the
// index of each slot with the stack moved. A take or place cancelled
// refuses the operation, and the calls after a cancel are not made.
func TestMoveAndSwapCallHandlers(t *testing.T) {
	for _, c := range []struct {
		name, cancel string
		swap, ok     bool
		want         []string
	}{
		{name: "move", ok: true, want: []string{"take 0 of 3", "place 1 of 3"}},
		{name: "move, the take cancelled", cancel: "take 0", want: []string{"take 0 of 3"}},
		{name: "move, the place cancelled", cancel: "place 1", want: []string{"take 0 of 3", "place 1 of 3"}},
		{name: "swap", swap: true, ok: true, want: []string{"take 0 of 10", "place 0 of 4", "take 1 of 4", "place 1 of 10"}},
		{name: "swap, the first take cancelled", swap: true, cancel: "take 0", want: []string{"take 0 of 10"}},
		{name: "swap, the last place cancelled", swap: true, cancel: "place 1", want: []string{"take 0 of 10", "place 0 of 4", "take 1 of 4", "place 1 of 10"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			inTx(t, func(tx *world.Tx) {
				b, inv := ownerBatch(tx, nil, &fakeOwner{mode: world.GameModeSurvival})
				var log []string
				inv.Main.Handle(cancelling{log: &log, cancel: c.cancel})
				_ = inv.Main.SetItem(0, apples(10))
				_ = inv.Main.SetItem(1, stone(4))
				var err error
				if c.swap {
					err = b.Swap(ref(inv.Main, 0, 0), ref(inv.Main, 1, 1))
				} else {
					_ = inv.Main.SetItem(1, item.Stack{})
					err = b.Move(ref(inv.Main, 0, 0), ref(inv.Main, 1, 1), 3)
				}
				if (err == nil) != c.ok {
					t.Fatalf("%v, want accepted %v", err, c.ok)
				}
				if !slices.Equal(log, c.want) {
					t.Errorf("the handler saw %q, want %q", log, c.want)
				}
				if !c.ok && len(b.Changes()) != 0 {
					t.Errorf("a cancelled operation changed %v", b.Changes())
				}
			})
		})
	}
}

// TestMoveLocks checks the locks of items: an item locked in the inventory
// moves among the slots of the player and the cursor only, an item locked
// in its slot not at all (its own slot is no move: ErrSameSlot), and
// neither is dropped or destroyed.
func TestMoveLocks(t *testing.T) {
	inTx(t, func(tx *world.Tx) {
		o := &fakeOwner{mode: world.GameModeCreative}
		b, inv := ownerBatch(tx, nil, o)
		inInventory := apples(2).WithLockMode(item.LockInInventory)
		inSlot := apples(2).WithLockMode(item.LockInSlot)
		_ = inv.Main.SetItem(0, inInventory)
		_ = inv.Main.SetItem(1, inSlot)
		cursor, grid := ref(inv.UI, menu.UICursor, 0), ref(inv.UI, menu.UICraftingLarge, 0)
		if err := b.Move(ref(inv.Main, 0, 0), grid, 1); err == nil {
			t.Error("an item locked in the inventory moved to the crafting grid")
		}
		if err := b.Move(ref(inv.Main, 0, 0), cursor, 1); err != nil {
			t.Errorf("an item locked in the inventory did not move to the cursor: %v", err)
		}
		if err := b.Move(ref(inv.Main, 0, 0), ref(inv.OffHand, 0, 0), 1); err != nil {
			t.Errorf("an item locked in the inventory did not move to the off hand: %v", err)
		}
		if err := b.Move(ref(inv.Main, 1, 0), ref(inv.Main, 2, 0), 1); err == nil {
			t.Error("an item locked in its slot moved to another")
		}
		if err := b.Swap(ref(inv.Main, 2, 0), ref(inv.Main, 1, 0)); err == nil {
			t.Error("an item locked in its slot was swapped away")
		}
		if err := b.Swap(ref(inv.Main, 1, 0), ref(inv.Main, 1, 0)); !errors.Is(err, menu.ErrSameSlot) {
			t.Errorf("an item locked in its slot swapped with itself: %v, want ErrSameSlot", err)
		}
		if err := b.Drop(ref(inv.Main, 1, 0), 1); err == nil {
			t.Error("an item locked in its slot was dropped")
		}
		if err := b.Destroy(ref(inv.Main, 1, 0), 1); err == nil {
			t.Error("an item locked in its slot was destroyed")
		}
		if err := b.Drop(cursor, 1); err == nil {
			t.Error("an item locked in the inventory was dropped from the cursor")
		}
		// Out of the slots of the player, a lock holds no more.
		_ = inv.UI.SetItem(menu.UICraftingLarge, inSlot)
		if err := b.Move(grid, ref(inv.Main, 5, 0), 1); err != nil {
			t.Errorf("an item locked in its slot of the grid did not move: %v", err)
		}
	})
}

// TestBinding checks the curse of binding: outside creative, an item of it
// does not leave its armour slot, whether moved, swapped or dropped (its own
// slot is no move: ErrSameSlot); in creative it leaves.
func TestBinding(t *testing.T) {
	for _, mode := range []world.GameMode{world.GameModeSurvival, world.GameModeCreative} {
		inTx(t, func(tx *world.Tx) {
			b, inv := ownerBatch(tx, nil, &fakeOwner{mode: mode})
			inv.Armour.SetHelmet(cursed)
			helmet := ref(inv.Armour.Inventory(), 0, 0)
			free := mode.CreativeInventory()
			if err := b.Move(helmet, helmet, 1); !errors.Is(err, menu.ErrSameSlot) {
				t.Errorf("%T: the cursed helmet moved to its own slot: %v, want ErrSameSlot", mode, err)
			}
			if err := b.Swap(ref(inv.Main, 0, 0), helmet); (err == nil) != free {
				t.Errorf("%T: swapped off: %v", mode, err)
			}
			_ = inv.Armour.Inventory().SetItem(0, cursed)
			if err := b.Drop(helmet, 1); (err == nil) != free {
				t.Errorf("%T: dropped: %v", mode, err)
			}
			_ = inv.Armour.Inventory().SetItem(0, cursed)
			if err := b.Move(helmet, ref(inv.Armour.Inventory(), 1, 0), 1); (err == nil) != free {
				t.Errorf("%T: moved to another armour slot: %v", mode, err)
			}
		})
	}
}

// TestMoveFromFurnaceResult checks that taking from the result slot of a
// furnace drops the experience it collected at the feet of the player, once,
// and that taking from its other slots does not.
func TestMoveFromFurnaceResult(t *testing.T) {
	inTx(t, func(tx *world.Tx) {
		pos := cube.Pos{0, 64, 0}
		tx.SetBlock(pos, block.NewFurnace(cube.North).DecodeNBT(map[string]any{"StoredXPInt": int32(7)}).(world.Block), nil)
		m := menu.OpenBlock(tx, pos, &fakeViewer{}, nil)
		o := &fakeOwner{mode: world.GameModeSurvival, pos: mgl64.Vec3{3, 65, 3}}
		b, inv := ownerBatch(tx, m, o)
		_ = m.Inventory().SetItem(1, item.NewStack(item.Coal{}, 2))
		_ = m.Inventory().SetItem(2, item.NewStack(item.IronIngot{}, 3))
		orbs := func() (n, xp int) {
			for e := range tx.Entities() {
				if e.H().Type() == entity.ExperienceOrbType {
					if e.Position() != o.pos {
						t.Errorf("an orb at %v, not at the feet of the player", e.Position())
					}
					n, xp = n+1, xp+e.(*entity.Ent).Behaviour().(*entity.ExperienceOrbBehaviour).Experience()
				}
			}
			return n, xp
		}
		if err := b.Move(ref(m.Inventory(), 1, 0), ref(inv.Main, 0, 0), 1); err != nil {
			t.Fatal(err)
		}
		if n, _ := orbs(); n != 0 {
			t.Fatalf("%d orbs of the fuel taken", n)
		}
		if err := b.Move(ref(m.Inventory(), 2, 0), ref(inv.Main, 1, 0), 1); err != nil {
			t.Fatal(err)
		}
		if n, xp := orbs(); n == 0 || xp != 7 {
			t.Fatalf("%d orbs of %d experience, want the 7 the furnace collected", n, xp)
		}
		if err := b.Swap(ref(inv.Main, 1, 0), ref(m.Inventory(), 2, 0)); err != nil {
			t.Fatal(err)
		}
		if _, xp := orbs(); xp != 7 {
			t.Errorf("%d experience after the furnace gave its 7", xp)
		}
	})
}

// TestDrop checks drops: the player drops what the handler lets it, and
// the slot keeps what it did not drop.
func TestDrop(t *testing.T) {
	inTx(t, func(tx *world.Tx) {
		o := &fakeOwner{mode: world.GameModeSurvival}
		b, inv := ownerBatch(tx, nil, o)
		_ = inv.Main.SetItem(0, apples(10))
		var log []string
		inv.Main.Handle(cancelling{log: &log, cancel: "drop 1"})
		if err := b.Drop(ref(inv.Main, 0, 5), 3); err != nil {
			t.Fatal(err)
		}
		if len(o.dropped) != 1 || !o.dropped[0].Equal(apples(3)) || !slotOf(inv.Main, 0).Equal(apples(7)) {
			t.Fatalf("dropped %v, %v left", o.dropped, slotOf(inv.Main, 0))
		}
		if err := b.Drop(ref(inv.Main, 0, 5), 8); err == nil {
			t.Error("eight apples of seven were dropped")
		}
		o.refuseDrops = true
		if err := b.Drop(ref(inv.Main, 0, 5), 2); err != nil {
			t.Fatal(err)
		}
		if !slotOf(inv.Main, 0).Equal(apples(7)) {
			t.Errorf("%v left of a drop the player did not make", slotOf(inv.Main, 0))
		}
		_ = inv.Main.SetItem(1, apples(1))
		if err := b.Drop(ref(inv.Main, 1, 5), 1); err == nil {
			t.Error("a drop the handler cancelled was made")
		}
		if want := []string{"drop 0 of 3", "drop 0 of 2", "drop 1 of 1"}; !slices.Equal(log, want) {
			t.Errorf("the handler saw %q, want %q", log, want)
		}
	})
}

// TestDestroyAndTouch checks that only a player with the creative
// inventory destroys items, no more than there are, and that Touch
// journals a slot as it is.
func TestDestroyAndTouch(t *testing.T) {
	for _, mode := range []world.GameMode{world.GameModeSurvival, world.GameModeCreative, world.GameModeSpectator} {
		inTx(t, func(tx *world.Tx) {
			b, inv := ownerBatch(tx, nil, &fakeOwner{mode: mode})
			_ = inv.Main.SetItem(0, apples(10))
			err := b.Destroy(ref(inv.Main, 0, 0), 4)
			if (err == nil) != mode.CreativeInventory() {
				t.Fatalf("%T: Destroy: %v", mode, err)
			}
			if err == nil && !slotOf(inv.Main, 0).Equal(apples(6)) {
				t.Errorf("%T: %v left of ten apples, four destroyed", mode, slotOf(inv.Main, 0))
			}
			if mode.CreativeInventory() {
				if err := b.Destroy(ref(inv.Main, 0, 0), 7); err == nil {
					t.Errorf("%T: seven apples of six destroyed", mode)
				}
			}
		})
	}
	inTx(t, func(tx *world.Tx) {
		b, inv := ownerBatch(tx, nil, &fakeOwner{mode: world.GameModeSurvival})
		pickaxe := item.NewStack(item.Pickaxe{Tier: item.ToolTierIron}, 1).Damage(3)
		_ = inv.Main.SetItem(4, pickaxe)
		if err := b.Touch(ref(inv.Main, 4, 9)); err != nil {
			t.Fatal(err)
		}
		if c := b.Changes(); len(c) != 1 || c[0].Ref.Name != 9 || !c[0].Before.Equal(pickaxe) || !c[0].After.Equal(pickaxe) {
			t.Errorf("Touch journalled %v", c)
		}
	})
}

// TestCreateCreative checks that a creative item is made a full stack in
// the result slot, for a player with the creative inventory only, of an
// index of creative.Items().
func TestCreateCreative(t *testing.T) {
	// The server registers the creative items of the game; a test has
	// none but those it registers.
	creative.RegisterItem(creative.Item{Stack: apples(1)})
	inTx(t, func(tx *world.Tx) {
		b, inv := ownerBatch(tx, nil, &fakeOwner{mode: world.GameModeSurvival})
		if err := b.CreateCreative(0); err == nil {
			t.Error("a creative item was made in survival")
		}
		b, inv = ownerBatch(tx, nil, &fakeOwner{mode: world.GameModeCreative})
		for _, i := range []int{-1, len(creativeItems())} {
			if err := b.CreateCreative(i); err == nil {
				t.Errorf("creative item %d of %d was made", i, len(creativeItems()))
			}
		}
		i := slices.IndexFunc(creativeItems(), func(s item.Stack) bool { return s.Item() == item.Apple{} })
		if err := b.CreateCreative(i); err != nil {
			t.Fatal(err)
		}
		if got := slotOf(inv.UI, menu.UIResult); !got.Equal(apples(64)) {
			t.Errorf("the result slot holds %v, want 64 apples", got)
		}
	})
}

// creativeItems returns the stacks of the creative items.
func creativeItems() []item.Stack {
	var stacks []item.Stack
	for _, it := range creative.Items() {
		stacks = append(stacks, it.Stack)
	}
	return stacks
}
