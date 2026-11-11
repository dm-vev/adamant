package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// dispenseOnce loads a dispenser facing east at the origin with the stack in its
// first slot, builds what surrounds it with set (given the position of the block
// in front) and runs its scheduled tick. It returns the position of the
// dispenser; the test goes on in the same transaction.
func dispenseOnce(tx *world.Tx, stack item.Stack, set func(front cube.Pos)) cube.Pos {
	pos := loadDispenser(tx, stack)
	if set != nil {
		set(pos.Side(cube.FaceEast))
	}
	tx.Block(pos).(block.Dispenser).ScheduledTick(pos, tx, nil)
	return pos
}

// slotOf returns the stack in the first slot of the dispenser at pos.
func slotOf(tx *world.Tx, pos cube.Pos) item.Stack {
	stack, _ := tx.Block(pos).(block.Dispenser).Inventory(tx, pos).Item(0)
	return stack
}

// TestDispenserShearsSheep checks ShearsDispenseItemBehavior: a sheep in front
// loses its wool, which drops from it, and the shears are worn down by one; with
// nothing to shear the dispenser keeps the shears.
func TestDispenserShearsSheep(t *testing.T) {
	t.Parallel()
	w, _ := dispenserWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		var sheep world.Entity
		pos := dispenseOnce(tx, item.NewStack(item.Shears{}, 1), func(front cube.Pos) {
			sheep = tx.AddEntity(entity.NewSheep(world.EntitySpawnOpts{Position: front.Vec3Middle()}))
		})
		if !sheep.(*entity.Sheep).Sheared() {
			t.Error("the sheep was not sheared")
		}
		if got := slotOf(tx, pos); got.Durability() != got.MaxDurability()-1 {
			t.Errorf("shears have durability %d of %d, want one less", got.Durability(), got.MaxDurability())
		}
		wool := 0
		for _, e := range entitiesOfType(tx, entity.ItemType) {
			if stack := e.(*entity.Ent).Behaviour().(*entity.ItemBehaviour).Item(); stack.Count() > 0 {
				wool += stack.Count()
			}
		}
		if wool == 0 {
			t.Error("no wool dropped")
		}
	})
	w2, _ := dispenserWorld(t)
	<-w2.Exec(func(tx *world.Tx) {
		pos := dispenseOnce(tx, item.NewStack(item.Shears{}, 1), nil)
		if got := slotOf(tx, pos); got.Empty() || got.Durability() != got.MaxDurability() {
			t.Errorf("shears with nothing to shear are %v, want them kept intact", got)
		}
		if items := entitiesOfType(tx, entity.ItemType); len(items) != 0 {
			t.Errorf("%d items dropped, want none", len(items))
		}
	})
}

// TestDispenserShearsBeehive checks that shears take the honeycombs of a hive
// with honey level 5 and reset it.
func TestDispenserShearsBeehive(t *testing.T) {
	t.Parallel()
	w, _ := dispenserWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		pos := dispenseOnce(tx, item.NewStack(item.Shears{}, 1), func(front cube.Pos) {
			tx.SetBlock(front, block.Beehive{Facing: cube.West, HoneyLevel: 5}, nil)
		})
		if hive, ok := tx.Block(pos.Side(cube.FaceEast)).(block.Beehive); !ok || hive.HoneyLevel != 0 {
			t.Errorf("hive is %v, want its honey gone", tx.Block(pos.Side(cube.FaceEast)))
		}
		combs := 0
		for _, e := range entitiesOfType(tx, entity.ItemType) {
			stack := e.(*entity.Ent).Behaviour().(*entity.ItemBehaviour).Item()
			if _, ok := stack.Item().(item.Honeycomb); ok {
				combs += stack.Count()
			}
		}
		if combs != 3 {
			t.Errorf("%d honeycombs dropped, want 3", combs)
		}
		if got := slotOf(tx, pos); got.Durability() != got.MaxDurability()-1 {
			t.Errorf("shears have durability %d of %d, want one less", got.Durability(), got.MaxDurability())
		}
	})
}

// TestDispenserBrushesArmadillo checks that a brush takes a scute from an
// armadillo in front, wearing down by 16.
func TestDispenserBrushesArmadillo(t *testing.T) {
	t.Parallel()
	w, _ := dispenserWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		pos := dispenseOnce(tx, item.NewStack(item.Brush{}, 1), func(front cube.Pos) {
			tx.AddEntity(entity.NewArmadillo(world.EntitySpawnOpts{Position: front.Vec3Middle()}))
		})
		if got := slotOf(tx, pos); got.Durability() != got.MaxDurability()-16 {
			t.Errorf("brush has durability %d of %d, want 16 less", got.Durability(), got.MaxDurability())
		}
		if items := entitiesOfType(tx, entity.ItemType); len(items) != 1 {
			t.Errorf("%d items dropped, want the scute", len(items))
		}
	})
}

// TestDispenserEquipsArmourStand checks EquipmentDispenseItemBehavior for an
// armour stand in front: it wears the piece in its slot, once, and a second
// piece of the same slot is ejected.
func TestDispenserEquipsArmourStand(t *testing.T) {
	t.Parallel()
	w, _ := dispenserWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		var stand world.Entity
		set := func(front cube.Pos) {
			stand = tx.AddEntity(entity.NewArmourStand(world.EntitySpawnOpts{Position: front.Vec3Middle()}))
		}
		chest := item.NewStack(item.Chestplate{Tier: item.ArmourTierDiamond{}}, 1)
		pos := loadDispenser(tx, chest)
		_ = tx.Block(pos).(block.Dispenser).Inventory(tx, pos).SetItem(1, chest)
		set(pos.Side(cube.FaceEast))
		tx.Block(pos).(block.Dispenser).ScheduledTick(pos, tx, nil)
		armour := stand.(*entity.Ent).Behaviour().(*entity.ArmourStandBehaviour).Armour()
		if armour.Chestplate().Empty() {
			t.Fatal("the armour stand does not wear the chestplate")
		}
		if items := entitiesOfType(tx, entity.ItemType); len(items) != 0 {
			t.Fatalf("%d items dropped, want none", len(items))
		}
		// A second chestplate does not fit: it is ejected.
		tx.Block(pos).(block.Dispenser).ScheduledTick(pos, tx, nil)
		if items := entitiesOfType(tx, entity.ItemType); len(items) != 1 {
			t.Errorf("%d items dropped, want the second chestplate", len(items))
		}
	})
}

// TestDispenserEquipsTamedHorse checks AbstractHorse.canDispenserEquipIntoSlot: a
// tamed horse takes a saddle and horse armour, a wild one does not; a tamed
// donkey takes a chest.
func TestDispenserEquipsTamedHorse(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		kind  uint8
		tamed bool
		stack item.Stack
		worn  bool
	}{
		{"saddle on a tamed horse", 0, true, item.NewStack(item.Saddle{}, 1), true},
		{"saddle on a wild horse", 0, false, item.NewStack(item.Saddle{}, 1), false},
		{"armour on a tamed horse", 0, true, item.NewStack(item.IronHorseArmour{}, 1), true},
		{"chest on a tamed donkey", 1, true, item.NewStack(block.Chest{}, 1), true},
		{"chest on a wild donkey", 1, false, item.NewStack(block.Chest{}, 1), false},
		{"chest on a horse", 0, true, item.NewStack(block.Chest{}, 1), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			w, _ := dispenserWorld(t)
			<-w.Exec(func(tx *world.Tx) {
				var mount world.Entity
				pos := dispenseOnce(tx, c.stack, func(front cube.Pos) {
					typ := world.EntityType(entity.HorseType)
					if c.kind == 1 {
						typ = entity.DonkeyType
					}
					mount = tx.AddEntity(world.EntitySpawnOpts{Position: front.Vec3Middle()}.New(typ, entity.MountConfig{Kind: c.kind, Tamed: c.tamed, Strength: 1}))
				})
				m := mount.(interface {
					Saddled() bool
					Chested() bool
					Armor() item.Stack
				})
				var got bool
				switch c.stack.Item().(type) {
				case item.Saddle:
					got = m.Saddled()
				case block.Chest:
					got = m.Chested()
				default:
					got = !m.Armor().Empty()
				}
				if got != c.worn {
					t.Fatalf("mount took the item: %v, want %v", got, c.worn)
				}
				left := slotOf(tx, pos)
				if c.worn && !left.Empty() {
					t.Errorf("dispenser holds %v, want the item used", left)
				}
				items := len(entitiesOfType(tx, entity.ItemType))
				if !c.worn && (left.Count() != 0 || items != 1) {
					// Not worn: ejected, as DefaultDispenseItemBehavior does for an item
					// the mob does not take.
					t.Errorf("dispenser holds %v with %d items dropped, want the item ejected", left, items)
				}
			})
		})
	}
}

// TestDispenserSaddlesPigsAndStriders checks Pig.canDispenserEquipIntoSlot and
// Strider.canDispenserEquipIntoSlot: an adult pig or strider in front takes a
// saddle it does not wear yet, tame or not, and a second one is ejected; a
// baby takes none.
func TestDispenserSaddlesPigsAndStriders(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		spawn func(world.EntitySpawnOpts) *world.EntityHandle
		baby  bool
	}{
		{"pig", entity.NewPig, false},
		{"strider", entity.NewStrider, false},
		{"baby strider", func(o world.EntitySpawnOpts) *world.EntityHandle {
			return o.New(entity.StriderType, entity.StriderConfig{Age: -24000})
		}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			w, _ := dispenserWorld(t)
			<-w.Exec(func(tx *world.Tx) {
				var mob world.Entity
				pos := dispenseOnce(tx, item.NewStack(item.Saddle{}, 1), func(front cube.Pos) {
					mob = tx.AddEntity(c.spawn(world.EntitySpawnOpts{Position: front.Vec3Middle()}))
				})
				if saddled := mob.(interface{ Saddled() bool }).Saddled(); saddled == c.baby {
					t.Fatalf("saddled=%v, want %v", saddled, !c.baby)
				}
				wantItems := 0
				if c.baby {
					wantItems = 1
				}
				if left := slotOf(tx, pos); !left.Empty() {
					t.Errorf("dispenser holds %v, want it used or ejected", left)
				}
				if items := entitiesOfType(tx, entity.ItemType); len(items) != wantItems {
					t.Errorf("%d items dropped, want %d", len(items), wantItems)
				}
				if c.baby {
					return
				}
				// The second saddle does not fit: it is ejected.
				_ = tx.Block(pos).(block.Dispenser).Inventory(tx, pos).SetItem(0, item.NewStack(item.Saddle{}, 1))
				tx.Block(pos).(block.Dispenser).ScheduledTick(pos, tx, nil)
				if items := entitiesOfType(tx, entity.ItemType); len(items) != 1 {
					t.Errorf("%d items dropped, want the second saddle", len(items))
				}
			})
		})
	}
}
