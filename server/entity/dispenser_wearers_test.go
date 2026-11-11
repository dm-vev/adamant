package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// wearable is a piece of armour a dispenser equips, the slot of the armour
// inventory it goes into, and whether the dispenser keeps it when that slot is
// full (the optional behaviours) instead of ejecting it.
type wearable struct {
	name  string
	stack item.Stack
	slot  func(a *inventory.Armour) item.Stack
	kept  bool
}

var wearables = []wearable{
	{"helmet", item.NewStack(item.Helmet{Tier: item.ArmourTierDiamond{}}, 1), (*inventory.Armour).Helmet, false},
	{"chestplate", item.NewStack(item.Chestplate{Tier: item.ArmourTierDiamond{}}, 1), (*inventory.Armour).Chestplate, false},
	{"leggings", item.NewStack(item.Leggings{Tier: item.ArmourTierDiamond{}}, 1), (*inventory.Armour).Leggings, false},
	{"boots", item.NewStack(item.Boots{Tier: item.ArmourTierDiamond{}}, 1), (*inventory.Armour).Boots, false},
	{"elytra", item.NewStack(item.Elytra{}, 1), (*inventory.Armour).Chestplate, false},
	{"turtle shell", item.NewStack(item.TurtleShell{}, 1), (*inventory.Armour).Helmet, false},
	{"skeleton skull", item.NewStack(block.Skull{Type: block.SkeletonSkull()}, 1), (*inventory.Armour).Helmet, false},
	{"zombie head", item.NewStack(block.Skull{Type: block.ZombieHead()}, 1), (*inventory.Armour).Helmet, false},
	{"wither skeleton skull", item.NewStack(block.Skull{Type: block.WitherSkeletonSkull()}, 1), (*inventory.Armour).Helmet, true},
	{"carved pumpkin", item.NewStack(block.Pumpkin{Carved: true}, 1), (*inventory.Armour).Helmet, true},
}

// refill puts the stack into the first slot of the dispenser at pos and runs
// the scheduled tick of the dispenser again.
func refill(tx *world.Tx, pos cube.Pos, stack item.Stack) {
	d := tx.Block(pos).(block.Dispenser)
	_ = d.Inventory(tx, pos).SetItem(0, stack)
	d.ScheduledTick(pos, tx, nil)
}

// TestDispenserEquipsPlayers checks EquipmentDispenseItemBehavior on a player
// standing in front: armour, an elytra, a skull and a carved pumpkin go into
// the slot of the player that matches them, and not into a slot that is full:
// the item is ejected then, or kept when the behaviour is an optional one.
func TestDispenserEquipsPlayers(t *testing.T) {
	t.Parallel()
	for _, c := range wearables {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			w, _ := dispenserWorld(t)
			<-w.Exec(func(tx *world.Tx) {
				var p *player.Player
				pos := dispenseOnce(tx, c.stack, func(front cube.Pos) { p = addSurvivalPlayer(tx, front.Vec3Middle()) })
				if got := c.slot(p.Armour()); got.Empty() || !got.Comparable(c.stack) {
					t.Fatalf("the slot holds %v, want the %v", got, c.stack)
				}
				if left := slotOf(tx, pos); !left.Empty() {
					t.Errorf("dispenser holds %v, want the item used", left)
				}
				if items := entitiesOfType(tx, entity.ItemType); len(items) != 0 {
					t.Errorf("%d items dropped, want none", len(items))
				}

				// A second one finds the slot full: the player keeps the first.
				worn := c.slot(p.Armour())
				refill(tx, pos, c.stack)
				if got := c.slot(p.Armour()); got.Count() != 1 || !got.Comparable(worn) {
					t.Errorf("the slot holds %v after the second, want the first", got)
				}
				items, left := len(entitiesOfType(tx, entity.ItemType)), slotOf(tx, pos)
				if c.kept && (items != 0 || left.Count() != 1) {
					t.Errorf("%d items dropped and the dispenser holds %v, want the item kept", items, left)
				}
				if !c.kept && (items != 1 || !left.Empty()) {
					t.Errorf("%d items dropped and the dispenser holds %v, want the item ejected", items, left)
				}
			})
		})
	}
}

// TestDispenserDoesNotEquipSpectators checks LivingEntity.canEquipWithDispenser:
// a spectator in front takes nothing.
func TestDispenserDoesNotEquipSpectators(t *testing.T) {
	t.Parallel()
	w, _ := dispenserWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		var p *player.Player
		dispenseOnce(tx, wearables[0].stack, func(front cube.Pos) {
			p = tx.AddEntity((world.EntitySpawnOpts{}).New(player.Type, player.Config{Position: front.Vec3Middle(), GameMode: world.GameModeSpectator})).(*player.Player)
		})
		if got := p.Armour().Helmet(); !got.Empty() {
			t.Errorf("the spectator wears %v", got)
		}
		if items := entitiesOfType(tx, entity.ItemType); len(items) != 1 {
			t.Errorf("%d items dropped, want the helmet ejected", len(items))
		}
	})
}

// TestDispenserElytraNeedsAFreeChestSlot checks that an elytra and a
// chestplate share the chest slot of a player: an elytra does not replace a
// chestplate, and a chestplate does not replace an elytra.
func TestDispenserElytraNeedsAFreeChestSlot(t *testing.T) {
	t.Parallel()
	chestplate, elytra := wearables[1].stack, wearables[4].stack
	for _, c := range []struct{ worn, dispensed item.Stack }{{chestplate, elytra}, {elytra, chestplate}} {
		w, _ := dispenserWorld(t)
		<-w.Exec(func(tx *world.Tx) {
			var p *player.Player
			dispenseOnce(tx, c.dispensed, func(front cube.Pos) {
				p = addSurvivalPlayer(tx, front.Vec3Middle())
				p.Armour().SetChestplate(c.worn)
			})
			if got := p.Armour().Chestplate(); !got.Comparable(c.worn) {
				t.Errorf("the chest slot holds %v, want the %v worn before", got, c.worn)
			}
			if items := entitiesOfType(tx, entity.ItemType); len(items) != 1 {
				t.Errorf("%d items dropped, want the %v ejected", len(items), c.dispensed)
			}
		})
	}
}

// TestDispenserEquipsArmourStands checks the same pieces on an armour stand:
// each goes into its slot, a slot that is full takes no second piece, and the
// skull and the pumpkin go on its head.
func TestDispenserEquipsArmourStands(t *testing.T) {
	t.Parallel()
	for _, c := range wearables {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			w, _ := dispenserWorld(t)
			<-w.Exec(func(tx *world.Tx) {
				var stand world.Entity
				pos := dispenseOnce(tx, c.stack, func(front cube.Pos) {
					stand = tx.AddEntity(entity.NewArmourStand(world.EntitySpawnOpts{Position: front.Vec3Middle()}))
				})
				armour := stand.(*entity.Ent).Behaviour().(*entity.ArmourStandBehaviour).Armour()
				if got := c.slot(armour); got.Empty() || !got.Comparable(c.stack) {
					t.Fatalf("the stand's slot holds %v, want the %v", got, c.stack)
				}
				if left := slotOf(tx, pos); !left.Empty() {
					t.Errorf("dispenser holds %v, want the item used", left)
				}
				refill(tx, pos, c.stack)
				items, left := len(entitiesOfType(tx, entity.ItemType)), slotOf(tx, pos)
				if c.kept && (items != 0 || left.Count() != 1) {
					t.Errorf("%d items dropped and the dispenser holds %v, want the item kept", items, left)
				}
				if !c.kept && (items != 1 || !left.Empty()) {
					t.Errorf("%d items dropped and the dispenser holds %v, want the item ejected", items, left)
				}
			})
		})
	}
}
