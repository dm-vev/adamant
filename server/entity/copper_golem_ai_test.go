package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func chestInventory(tx *world.Tx, pos cube.Pos) *inventory.Inventory {
	return tx.Block(pos).(interface {
		Inventory(*world.Tx, cube.Pos) *inventory.Inventory
	}).Inventory(tx, pos)
}

func countItem(inv *inventory.Inventory, it world.Item) (n int) {
	name, _ := it.EncodeItem()
	for _, s := range inv.Slots() {
		if s.Empty() {
			continue
		}
		if sn, _ := s.Item().EncodeItem(); sn == name {
			n += s.Count()
		}
	}
	return n
}

// copperGolemSortingWorld places a copper chest holding twenty cobblestone,
// a chest holding the items passed and a copper golem between them.
func copperGolemSortingWorld(t *testing.T, chestItems ...item.Stack) (*world.World, cube.Pos, cube.Pos, *world.EntityHandle) {
	t.Helper()
	w := wolfTestWorld(t)
	source, dest := cube.Pos{4, 64, 0}, cube.Pos{-4, 64, 0}
	var golem *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(source, block.NewCopperChest(), nil)
		tx.SetBlock(dest, block.NewChest(), nil)
		_, _ = chestInventory(tx, source).AddItem(item.NewStack(block.Cobblestone{}, 20))
		for _, s := range chestItems {
			_, _ = chestInventory(tx, dest).AddItem(s)
		}
		golem = spawnAnimal(t, tx, "minecraft:copper_golem", .5, nil)
	})
	return w, source, dest, golem
}

// TestCopperGolemsSortItemsIntoChests checks TransportItemsBetweenContainers
// for copper golems: a copper golem takes sixteen items out of a copper
// chest and puts them into a chest holding the same item.
func TestCopperGolemsSortItemsIntoChests(t *testing.T) {
	w, source, dest, golem := copperGolemSortingWorld(t, item.NewStack(block.Cobblestone{}, 1))
	carried := false
	for range 2400 {
		w.AdvanceTick()
		done := false
		<-w.Exec(func(tx *world.Tx) {
			e, _ := golem.Entity(tx)
			carried = carried || !e.(*entity.CopperGolem).CarriedItem().Empty()
			done = countItem(chestInventory(tx, dest), block.Cobblestone{}) == 17
		})
		if done {
			<-w.Exec(func(tx *world.Tx) {
				if n := countItem(chestInventory(tx, source), block.Cobblestone{}); n != 4 {
					t.Fatalf("copper chest holds %d cobblestone after the golem took sixteen, want 4", n)
				}
				e, _ := golem.Entity(tx)
				if !e.(*entity.CopperGolem).CarriedItem().Empty() {
					t.Fatal("copper golem still carries items it put into a chest")
				}
			})
			return
		}
	}
	t.Fatalf("copper golem did not sort cobblestone into a chest in two minutes (carried %v)", carried)
}

// TestCopperGolemsLeaveChestsWithOtherItems checks
// TransportItemsBetweenContainers.matchesLeavingItemsRequirement: a copper
// golem does not put items into a chest holding other items.
func TestCopperGolemsLeaveChestsWithOtherItems(t *testing.T) {
	w, _, dest, golem := copperGolemSortingWorld(t, item.NewStack(item.Stick{}, 1))
	carried := false
	for range 1600 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			e, _ := golem.Entity(tx)
			carried = carried || !e.(*entity.CopperGolem).CarriedItem().Empty()
			if n := countItem(chestInventory(tx, dest), block.Cobblestone{}); n != 0 {
				t.Fatalf("copper golem put %d cobblestone into a chest of sticks", n)
			}
		})
	}
	if !carried {
		t.Fatal("copper golem did not take items out of the copper chest")
	}
}

// TestCopperGolemsOpenChestsTheyUse checks CopperGolemAi.onReachedTargetInteraction:
// a copper golem opens the chest it takes items from and shows it does.
func TestCopperGolemsOpenChestsTheyUse(t *testing.T) {
	w, source, _, golem := copperGolemSortingWorld(t)
	for range 1200 {
		w.AdvanceTick()
		opened := false
		<-w.Exec(func(tx *world.Tx) {
			e, _ := golem.Entity(tx)
			g := e.(*entity.CopperGolem)
			if g.State() == entity.CopperGolemGettingItem {
				opened = tx.Block(source).(block.CopperChest).Opened()
				if !opened {
					t.Fatal("copper golem getting items from a copper chest it did not open")
				}
			}
		})
		if opened {
			return
		}
	}
	t.Fatal("copper golem did not get items from a copper chest in a minute")
}

// TestCopperGolemsSpawnOnCopperChests checks the copper golem pattern of
// CarvedPumpkinBlock: a carved pumpkin on exposed copper becomes an exposed
// copper golem on an exposed copper chest.
func TestCopperGolemsSpawnOnCopperChests(t *testing.T) {
	w := wolfTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 64, 0}, block.Copper{Oxidation: block.ExposedOxidation()}, nil)
		// Placing the pumpkin completes the pattern.
		tx.SetBlock(cube.Pos{0, 65, 0}, block.Pumpkin{Carved: true, Facing: cube.North}, nil)
		chest, ok := tx.Block(cube.Pos{0, 64, 0}).(block.CopperChest)
		if !ok || chest.Oxidation != block.ExposedOxidation() || chest.Facing != cube.North {
			t.Fatalf("copper golem stands on %#v, want an exposed copper chest facing north", tx.Block(cube.Pos{0, 64, 0}))
		}
		found := false
		for e := range tx.Entities() {
			if g, ok := e.(*entity.CopperGolem); ok {
				found = true
				if g.WeatherState() != 1 {
					t.Fatalf("copper golem of exposed copper has weather state %d, want 1", g.WeatherState())
				}
				if pos := g.Position(); pos.Sub(mgl64.Vec3{.5, 65.05, .5}).Len() > 1e-9 {
					t.Fatalf("copper golem spawned at %v, want where the pumpkin was", pos)
				}
			}
		}
		if !found {
			t.Fatal("no copper golem spawned")
		}
		if _, ok := tx.Block(cube.Pos{0, 65, 0}).(block.Air); !ok {
			t.Fatal("pumpkin was not used up")
		}
	})
}

// TestCopperGolemsQueueForOpenChests checks CopperGolemAi.shouldQueueForTarget:
// a copper golem waits by a chest somebody else has open, and uses it once
// they close it.
func TestCopperGolemsQueueForOpenChests(t *testing.T) {
	w, _, dest, golem := copperGolemSortingWorld(t, item.NewStack(block.Cobblestone{}, 1))
	var other *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		other = spawnAnimal(t, tx, "minecraft:pig", -10.5, nil)
		tx.Block(dest).(block.Container).AddViewer(block.MobContainerOpener{Handle: other}, tx, dest)
	})
	carried := false
	for range 1600 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			e, _ := golem.Entity(tx)
			carried = carried || !e.(*entity.CopperGolem).CarriedItem().Empty()
			if n := countItem(chestInventory(tx, dest), block.Cobblestone{}); n != 1 {
				t.Fatalf("copper golem put cobblestone into a chest somebody has open (%d now)", n)
			}
		})
	}
	if !carried {
		t.Fatal("copper golem did not take items out of the copper chest")
	}
	<-w.Exec(func(tx *world.Tx) {
		tx.Block(dest).(block.Container).RemoveViewer(block.MobContainerOpener{Handle: other}, tx, dest)
	})
	for range 600 {
		w.AdvanceTick()
		done := false
		<-w.Exec(func(tx *world.Tx) { done = countItem(chestInventory(tx, dest), block.Cobblestone{}) == 17 })
		if done {
			return
		}
	}
	t.Fatal("copper golem did not use the chest in half a minute after it was closed")
}
