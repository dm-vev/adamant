package menu_test

import (
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// fakeViewer records what a menu and the containers it opens show it, and
// the block actions of the chunks it views as the viewer of a loader.
type fakeViewer struct {
	world.NopViewer
	slots   []int
	actions []world.BlockAction
}

func (v *fakeViewer) ViewSlotChange(slot int, _ item.Stack) { v.slots = append(v.slots, slot) }
func (v *fakeViewer) ViewBlockAction(_ cube.Pos, a world.BlockAction) {
	v.actions = append(v.actions, a)
}
func (v *fakeViewer) ViewCrafterSlotState(int, bool)                                 {}
func (v *fakeViewer) ViewEnchantmentOptions([3]menu.EnchantmentOption)               {}
func (v *fakeViewer) ViewTrades(*menu.Menu, entity.Merchant, []entity.MerchantOffer) {}
func (v *fakeViewer) reset()                                                         { v.slots, v.actions = nil, nil }

// opened returns how often the viewer saw lids open and close.
func (v *fakeViewer) opened() (opens, closes int) {
	for _, a := range v.actions {
		switch a.(type) {
		case block.OpenAction:
			opens++
		case block.CloseAction:
			closes++
		}
	}
	return opens, closes
}

// newWorld returns a synchronous world and a viewer loading the chunk of the
// origin, so that it sees the block actions of the lids of containers there.
func newWorld(t *testing.T) (*world.World, *fakeViewer) {
	t.Helper()
	w := world.Config{Synchronous: true}.New()
	v := &fakeViewer{}
	l := world.NewLoader(1, w, v)
	<-w.Exec(func(tx *world.Tx) {
		l.Move(tx, mgl64.Vec3{})
		l.Load(tx, 9)
	})
	t.Cleanup(func() {
		<-w.Exec(func(tx *world.Tx) { l.Close(tx) })
		_ = w.Close()
	})
	return w, v
}

func TestOpenBlockKinds(t *testing.T) {
	tests := []struct {
		name string
		b    world.Block
		kind menu.Kind
		size int // of the inventory of the menu
	}{
		{"chest", block.NewChest(), menu.Generic, 27},
		{"barrel", block.NewBarrel(), menu.Generic, 27},
		{"furnace", block.NewFurnace(cube.North), menu.Furnace, 3},
		{"blast furnace", block.NewBlastFurnace(cube.North), menu.BlastFurnace, 3},
		{"smoker", block.NewSmoker(cube.North), menu.Smoker, 3},
		{"brewing stand", block.NewBrewingStand(), menu.BrewingStand, 5},
		{"hopper", block.NewHopper(), menu.Hopper, 5},
		{"crafter", block.NewCrafter(), menu.Crafter, 9},
		{"crafting table", block.CraftingTable{}, menu.Crafting, 1},
		{"enchanting table", block.EnchantingTable{}, menu.Enchantment, 1},
		{"anvil", block.Anvil{}, menu.Anvil, 1},
		{"beacon", block.Beacon{}, menu.Beacon, 1},
		{"lectern", block.Lectern{}, menu.Lectern, 1},
		{"loom", block.Loom{}, menu.Loom, 1},
		{"grindstone", block.Grindstone{}, menu.Grindstone, 1},
		{"cartography table", block.CartographyTable{}, menu.Cartography, 1},
		{"stonecutter", block.Stonecutter{}, menu.Stonecutter, 1},
		{"smithing table", block.SmithingTable{}, menu.Smithing, 1},
		{"stone", block.Stone{}, menu.Other, 1},
	}
	w, v := newWorld(t)
	pos := cube.Pos{1, 64, 1}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			<-w.Exec(func(tx *world.Tx) {
				tx.SetBlock(pos, test.b, nil)
				m := menu.OpenBlock(tx, pos, v, nil)
				defer m.Close(tx, nil)
				if m.Kind() != test.kind {
					t.Errorf("Kind() = %v, want %v", m.Kind(), test.kind)
				}
				if m.Pos() != pos || m.Entity() != nil {
					t.Errorf("menu is open for %v and entity %v, want %v and none", m.Pos(), m.Entity(), pos)
				}
				if got := m.Inventory().Size(); got != test.size {
					t.Errorf("inventory has %v slots, want %v", got, test.size)
				}
				if c, ok := tx.Block(pos).(block.Container); ok && m.Inventory() != c.Inventory(tx, pos) {
					t.Error("menu shows another inventory than that of the container")
				}
			})
		})
	}
}

func TestOpenBlockRegistersTheViewer(t *testing.T) {
	w, v := newWorld(t)
	pos := cube.Pos{1, 64, 1}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos, block.NewChest(), nil)
		m := menu.OpenBlock(tx, pos, v, nil)
		if opens, closes := v.opened(); opens != 1 || closes != 0 {
			t.Fatalf("opening a chest opened its lid %v times and closed it %v times, want once and never", opens, closes)
		}
		_ = m.Inventory().SetItem(3, item.NewStack(item.Stick{}, 1))
		if len(v.slots) != 1 || v.slots[0] != 3 {
			t.Fatalf("viewer saw slot changes %v, want [3]", v.slots)
		}

		v.reset()
		m.Close(tx, nil)
		_ = m.Inventory().SetItem(4, item.NewStack(item.Stick{}, 1))
		if len(v.slots) != 0 {
			t.Fatalf("viewer saw slot changes %v after the menu closed", v.slots)
		}
		if opens, closes := v.opened(); opens != 0 || closes != 1 {
			t.Fatalf("closing a chest opened its lid %v times and closed it %v times, want never and once", opens, closes)
		}
	})
}

func TestOpenBlockEnderChest(t *testing.T) {
	w, v := newWorld(t)
	pos := cube.Pos{1, 64, 1}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos, block.NewEnderChest(), nil)
		ender := inventory.New(27, nil)
		first := menu.OpenBlock(tx, pos, v, ender)
		second := menu.OpenBlock(tx, pos, &fakeViewer{}, inventory.New(27, nil))
		if first.Kind() != menu.EnderChest || first.Inventory() != ender {
			t.Fatalf("ender chest opened as %v showing %p, want %v showing %p", first.Kind(), first.Inventory(), menu.EnderChest, ender)
		}
		first.Close(tx, nil)
		if opens, closes := v.opened(); opens != 1 || closes != 0 {
			t.Fatalf("two openers opened the lid %v times, one closing closed it %v times, want once and never", opens, closes)
		}
		second.Close(tx, nil)
		if _, closes := v.opened(); closes != 1 {
			t.Fatalf("the last opener closing closed the lid %v times, want once", closes)
		}
	})
}

// TestCloseReleasesTheBlockNowThere checks a menu closes on the block at its
// position when it closes, whatever it opened on.
func TestCloseReleasesTheBlockNowThere(t *testing.T) {
	w, v := newWorld(t)
	pos := cube.Pos{1, 64, 1}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos, block.NewEnderChest(), nil)
		ender := menu.OpenBlock(tx, pos, &fakeViewer{}, nil)
		tx.SetBlock(pos, block.Anvil{}, nil)
		anvil := menu.OpenBlock(tx, pos, v, nil)
		tx.SetBlock(pos, block.NewEnderChest(), nil)
		other := menu.OpenBlock(tx, pos, v, nil)
		v.reset()
		// The anvil's menu closes the ender chest now there, the ender chest's
		// the anvil's, as the session did.
		anvil.Close(tx, nil)
		if _, closes := v.opened(); closes != 1 {
			t.Fatalf("closing the menu of an anvil replaced by an ender chest closed the lid %v times, want once", closes)
		}
		ender.Close(tx, nil)
		other.Close(tx, nil)
	})
}

func TestOpenEntity(t *testing.T) {
	w, v := newWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		boat := tx.AddEntity(entity.NewChestBoat(world.EntitySpawnOpts{Position: mgl64.Vec3{-1.5, 64.2, 2.7}}, 0)).(*entity.ChestBoat)
		inv := boat.Behaviour().(*entity.ChestBoatBehaviour).Inventory()
		m := menu.OpenEntity(boat, inv, entity.ContainerChestBoat, v)
		if m.Kind() != menu.Entity || m.ContainerKind() != entity.ContainerChestBoat || m.Entity() != boat.H() || m.Inventory() != inv {
			t.Fatalf("menu is %v of %v for %v showing %p, want %v of %v for %v showing %p",
				m.Kind(), m.ContainerKind(), m.Entity(), m.Inventory(), menu.Entity, entity.ContainerChestBoat, boat.H(), inv)
		}
		// The coordinates are truncated towards zero, as the session did.
		if want := (cube.Pos{-1, 64, 2}); m.Pos() != want {
			t.Fatalf("menu is open at %v, want %v", m.Pos(), want)
		}
		_ = inv.SetItem(0, item.NewStack(item.Stick{}, 1))
		if len(v.slots) != 1 {
			t.Fatalf("viewer saw slot changes %v, want [0]", v.slots)
		}
		v.reset()
		m.Close(tx, nil)
		_ = inv.SetItem(1, item.NewStack(item.Stick{}, 1))
		if len(v.slots) != 0 {
			t.Fatalf("viewer saw slot changes %v after the menu closed", v.slots)
		}
	})
}

func TestOpenMerchant(t *testing.T) {
	w, _ := newWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		villager := tx.AddEntity(entity.NewVillager(world.EntitySpawnOpts{Position: mgl64.Vec3{2.5, 64, 3.5}}))
		merchant := villager.(entity.Merchant)
		alex, steve := entity.NewVillager(world.EntitySpawnOpts{}), entity.NewVillager(world.EntitySpawnOpts{})

		m, ok := menu.OpenMerchant(villager, merchant, alex)
		if !ok {
			t.Fatal("a free villager refused to trade")
		}
		if m.Kind() != menu.Merchant || m.Entity() != villager.H() || m.Pos() != (cube.Pos{2, 64, 3}) || m.Inventory().Size() != 1 {
			t.Fatalf("menu is %v for %v at %v, want %v for %v at %v with a placeholder", m.Kind(), m.Entity(), m.Pos(), menu.Merchant, villager.H(), cube.Pos{2, 64, 3})
		}
		if e, got, err := m.Merchant(tx); err != nil || e != villager || got != merchant {
			t.Fatalf("Merchant() = %v, %v, %v, want the villager", e, got, err)
		}
		if _, ok := menu.OpenMerchant(villager, merchant, steve); ok {
			t.Fatal("a villager trading with one player agreed to trade with another")
		}
		m.Close(tx, alex)
		if m, ok := menu.OpenMerchant(villager, merchant, steve); !ok {
			t.Fatal("closing the trade did not free the villager")
		} else {
			m.Close(tx, steve)
		}

		tx.RemoveEntity(villager)
		if _, _, err := m.Merchant(tx); err == nil {
			t.Fatal("Merchant() found a merchant that left")
		}
	})
}

func TestMerchantOfOtherMenus(t *testing.T) {
	w, v := newWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		var none *menu.Menu
		if _, _, err := none.Merchant(tx); err == nil {
			t.Error("Merchant() of no menu did not fail")
		}
		m := menu.OpenBlock(tx, cube.Pos{1, 64, 1}, v, nil)
		if _, _, err := m.Merchant(tx); err == nil {
			t.Error("Merchant() of a block menu did not fail")
		}
	})
}

func TestGrid(t *testing.T) {
	w, v := newWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		var none *menu.Menu
		if first, size := none.Grid(); first != menu.UICraftingSmall || size != 4 {
			t.Errorf("Grid() of no menu = %v, %v, want %v, 4", first, size, menu.UICraftingSmall)
		}
		for _, test := range []struct {
			b           world.Block
			first, size int
		}{
			{block.CraftingTable{}, menu.UICraftingLarge, 9},
			{block.Anvil{}, menu.UICraftingSmall, 4},
			{block.NewChest(), menu.UICraftingSmall, 4},
		} {
			pos := cube.Pos{1, 64, 1}
			tx.SetBlock(pos, test.b, nil)
			m := menu.OpenBlock(tx, pos, v, nil)
			if first, size := m.Grid(); first != test.first || size != test.size {
				t.Errorf("Grid() of %v = %v, %v, want %v, %v", m.Kind(), first, size, test.first, test.size)
			}
			m.Close(tx, nil)
		}
	})
}

// TestEnchantmentOptionsRefuseWhatCannotBeEnchanted checks the verdict of
// EnchantmentOptions on its input, and that the options of a seed are the
// same each time they are drawn.
func TestEnchantmentOptionsRefuseWhatCannotBeEnchanted(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		pos := cube.Pos{0, 64, 0}
		tx.SetBlock(pos, block.EnchantingTable{}, nil)
		sword := item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1)
		if _, ok := menu.EnchantmentOptions(tx, 7, pos, item.NewStack(item.Stick{}, 1)); ok {
			t.Error("a stick can be enchanted")
		}
		if _, ok := menu.EnchantmentOptions(tx, 7, pos, sword.WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 1))); ok {
			t.Error("an enchanted sword can be enchanted again")
		}
		first, ok := menu.EnchantmentOptions(tx, 7, pos, sword)
		if !ok {
			t.Fatal("an iron sword cannot be enchanted")
		}
		again, _ := menu.EnchantmentOptions(tx, 7, pos, sword)
		if !reflect.DeepEqual(first, again) {
			t.Errorf("seed 7 drew %v, then %v", first, again)
		}
		for i, option := range first {
			if option.Cost < 1 || len(option.Enchantments) == 0 {
				t.Errorf("option %d of seed 7 is %+v", i, option)
			}
		}
	})
}
