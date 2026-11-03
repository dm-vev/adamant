package menu_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
)

// testName names a slot by its index plus 100 times its role, so that a
// test sees which role a menu named a slot for.
func testName(s menu.Slot, r menu.Role) menu.Name {
	return menu.Name(int(r)*100 + s.Index)
}

// testBatch begins a batch over fresh inventories of a player with m open.
func testBatch(tx *world.Tx, m *menu.Menu, done func()) (*menu.Tx, menu.Inventories) {
	inv := menu.Inventories{
		Main:       inventory.New(36, nil),
		OffHand:    inventory.New(1, nil),
		EnderChest: inventory.New(27, nil),
		UI:         inventory.New(54, nil),
		Armour:     inventory.NewArmour(nil),
	}
	return menu.Begin(menu.Opts{Tx: tx, Inv: inv, Menu: m, Name: testName, Done: done}), inv
}

func ref(inv *inventory.Inventory, index int, name menu.Name) menu.Ref {
	return menu.Ref{Slot: menu.Slot{Inv: inv, Index: index}, Name: name}
}

// TestBatchJournalsAndRollsBack checks the journal of a batch, that holds
// each change with the name and the stack passed, and that Rollback
// restores what each slot held first, a slot changed twice too.
func TestBatchJournalsAndRollsBack(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		ends := 0
		b, inv := testBatch(tx, nil, func() { ends++ })
		apples := item.NewStack(item.Apple{}, 16)
		_ = inv.Main.SetItem(0, apples)

		if err := b.SetSlot(ref(inv.Main, 0, 1), apples.Grow(-1)); err != nil {
			t.Fatal(err)
		}
		if err := b.SetSlot(ref(inv.Main, 0, 2), apples.Grow(-2)); err != nil {
			t.Fatal(err)
		}
		// More than an apple stacks to: the inventory clamps it, the
		// journal holds what was passed.
		many := item.NewStack(item.Apple{}, 100)
		if err := b.SetSlot(ref(inv.Main, 1, 3), many); err != nil {
			t.Fatal(err)
		}
		if err := b.SetSlot(ref(inv.Main, 99, 4), apples); err == nil {
			t.Fatal("a slot out of range was set")
		}
		changes := b.Changes()
		if len(changes) != 3 {
			t.Fatalf("%d changes, want 3", len(changes))
		}
		if c := changes[1]; c.Ref.Name != 2 || c.Before.Count() != 15 || c.After.Count() != 14 {
			t.Errorf("the second change is %v: %v -> %v", c.Ref.Name, c.Before, c.After)
		}
		if c := changes[2]; c.After.Count() != 100 {
			t.Errorf("the journal holds %v, not the stack passed", c.After)
		}

		b.Rollback()
		b.Commit()
		if first, _ := inv.Main.Item(0); first.Count() != 16 {
			t.Errorf("slot 0 holds %v after the rollback, want 16 apples", first)
		}
		if second, _ := inv.Main.Item(1); !second.Empty() {
			t.Errorf("slot 1 holds %v after the rollback", second)
		}
		if ends != 1 {
			t.Errorf("Done was called %d times, want once", ends)
		}
	})
}

// TestBatchTakesResults checks the results of crafts: one result is taken
// at once, as index 0 of all the results of the batch; of several, each is
// taken by its index, once.
func TestBatchTakesResults(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		b, inv := testBatch(tx, nil, nil)
		sugar, bottle := item.NewStack(item.Sugar{}, 3), item.NewStack(item.GlassBottle{}, 1)
		if err := b.AddResults(sugar, bottle); err != nil {
			t.Fatal(err)
		}
		if len(b.Changes()) != 0 {
			t.Fatal("results of two were taken before the client took them")
		}
		if err := b.TakeResult(1); err != nil {
			t.Fatal(err)
		}
		if err := b.TakeResult(1); err == nil {
			t.Error("a result was taken twice")
		}
		if err := b.TakeResult(2); err == nil {
			t.Error("a result out of range was taken")
		}
		if got, _ := inv.UI.Item(menu.UIResult); !got.Equal(bottle) {
			t.Errorf("the result slot holds %v, want %v", got, bottle)
		}
		if c := b.Changes()[0]; c.Ref.Name != testName(menu.Slot{Index: menu.UIResult}, menu.RoleResult) || c.Ref.Inv != inv.UI {
			t.Errorf("the result was named %v", c.Ref.Name)
		}
		if err := b.TakeResult(0); err != nil {
			t.Fatal(err)
		}
		// A craft of one result takes index 0 of all the results, which
		// was taken: the craft fails, as the session's did.
		if err := b.AddResults(item.NewStack(item.Stick{}, 4)); err == nil {
			t.Error("a single result was taken as an index taken before")
		}
	})
}

// TestBatchCommitUpdatesComparators checks that a batch that changed the
// inventory of the chest of its menu updates the blocks around the chest,
// as comparators read it, and that one that changed only the player's slots
// does not. The comparator by the chest stands on nothing, so that the
// update breaks it.
func TestBatchCommitUpdatesComparators(t *testing.T) {
	for _, chestChanged := range []bool{true, false} {
		// The comparator broken drops as an item entity.
		w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry}.New()
		chest, comparator := cube.Pos{0, 64, 0}, cube.Pos{1, 64, 0}
		quiet := &world.SetOpts{DisableBlockUpdates: true}
		var m *menu.Menu
		<-w.Exec(func(tx *world.Tx) {
			tx.SetBlock(chest, block.NewChest(), quiet)
			tx.SetBlock(comparator, block.RedstoneComparator{Facing: cube.East}, quiet)
			m = menu.OpenBlock(tx, chest, &fakeViewer{}, nil)
		})
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			if _, standing := tx.Block(comparator).(block.RedstoneComparator); !standing {
				t.Fatal("the comparator broke before the batch")
			}
			b, inv := testBatch(tx, m, nil)
			target := ref(inv.Main, 0, 0)
			if chestChanged {
				target = ref(m.Inventory(), 0, 0)
			}
			if err := b.SetSlot(target, item.NewStack(block.Stone{}, 64)); err != nil {
				t.Fatal(err)
			}
			b.Commit()
		})
		// The world updates neighbours in its tick.
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			if _, standing := tx.Block(comparator).(block.RedstoneComparator); standing == chestChanged {
				t.Errorf("chest changed %v: the comparator by it is still there: %v", chestChanged, standing)
			}
		})
		_ = w.Close()
	}
}

// TestBatchEnablesCrafterSlots checks that a stack put in a disabled slot
// of the crafter of the menu enables the slot first, and that an empty one
// does not.
func TestBatchEnablesCrafterSlots(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		pos := cube.Pos{0, 64, 0}
		crafter := block.NewCrafter()
		crafter.DisabledSlots = 1<<2 | 1<<3
		tx.SetBlock(pos, crafter, nil)
		m := menu.OpenBlock(tx, pos, &fakeViewer{}, nil)
		b, _ := testBatch(tx, m, nil)
		if err := b.SetSlot(ref(m.Inventory(), 3, 0), item.Stack{}); err != nil {
			t.Fatal(err)
		}
		if err := b.SetSlot(ref(m.Inventory(), 2, 0), item.NewStack(item.Stick{}, 1)); err != nil {
			t.Fatal(err)
		}
		b.Commit()
		got := tx.Block(pos).(block.Crafter).DisabledSlots
		if want := uint16(1 << 3); uint16(got) != want {
			t.Errorf("disabled slots %b, want %b", got, want)
		}
		if n := len(b.Changes()); n != 2 {
			t.Errorf("%d changes, want 2", n)
		}
	})
}
