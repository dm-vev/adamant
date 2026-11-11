package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// gearedMob is a mob that keeps six equipment slots, as the dispenser sees it.
type gearedMob interface {
	world.Entity
	HeldItems() (mainHand, offHand item.Stack)
	EquipmentArmour() (head, chest, legs, feet item.Stack)
	PersistenceRequired() bool
}

func spawnGeared(t *testing.T, tx *world.Tx, id string, pos mgl64.Vec3, pickUp byte) gearedMob {
	t.Helper()
	h, err := entity.NewFromStructureNBT(map[string]any{"id": id, "CanPickUpLoot": pickUp}, world.EntitySpawnOpts{Position: pos, SpawnFinalized: true})
	if err != nil {
		t.Fatal(err)
	}
	return tx.AddEntity(h).(gearedMob)
}

// TestDispenserEquipsMobsThatPickUpLoot checks LivingEntity.canEquipWithDispenser
// and Mob.canDispenserEquipIntoSlot: a mob that may pick up loot takes a helmet
// into its empty head slot and a shield into its empty off hand, and then stays
// in the world; a slot that is full takes nothing and the item is ejected; and a
// mob that may not pick up loot takes nothing, and the dispenser ejects the item.
// A piglin always may.
func TestDispenserEquipsMobsThatPickUpLoot(t *testing.T) {
	t.Parallel()
	helmet := item.NewStack(item.Helmet{Tier: item.ArmourTierDiamond{}}, 1)
	shield := item.NewStack(item.Shield{}, 1)
	for _, c := range []struct {
		id     string
		pickUp byte
		takes  bool
	}{
		{"minecraft:zombie", 1, true},
		{"minecraft:zombie", 0, false},
		{"minecraft:husk", 1, true},
		{"minecraft:skeleton", 1, true},
		{"minecraft:drowned", 1, true},
		{"minecraft:drowned", 0, false},
		{"minecraft:zombified_piglin", 1, true},
		{"minecraft:zombified_piglin", 0, false},
		{"minecraft:piglin", 0, true},
	} {
		name := c.id
		if !c.takes {
			name += " that may not pick up loot"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			w, _ := dispenserWorld(t)
			<-w.Exec(func(tx *world.Tx) {
				var mob gearedMob
				pos := dispenseOnce(tx, helmet, func(front cube.Pos) { mob = spawnGeared(t, tx, c.id, front.Vec3Middle(), c.pickUp) })
				head, _, _, _ := mob.EquipmentArmour()
				items := len(entitiesOfType(tx, entity.ItemType))
				if !c.takes {
					if !head.Empty() || !slotOf(tx, pos).Empty() || items != 1 {
						t.Errorf("head holds %v, dispenser %v and %d items dropped, want nothing worn and the helmet ejected", head, slotOf(tx, pos), items)
					}
					refill(tx, pos, shield)
					if _, off := mob.HeldItems(); !off.Empty() || len(entitiesOfType(tx, entity.ItemType)) != 2 {
						t.Errorf("off hand holds %v, want the shield ejected", off)
					}
					return
				}
				if !head.Comparable(helmet) || !slotOf(tx, pos).Empty() || items != 0 {
					t.Fatalf("head holds %v, dispenser %v and %d items dropped, want the helmet worn and nothing dropped", head, slotOf(tx, pos), items)
				}
				if !mob.PersistenceRequired() {
					t.Error("the mob may despawn, want Mob.setPersistenceRequired")
				}

				// The head is full now: the second helmet is ejected.
				refill(tx, pos, helmet)
				if len(entitiesOfType(tx, entity.ItemType)) != 1 || !slotOf(tx, pos).Empty() {
					t.Errorf("%d items dropped, want the second helmet ejected", len(entitiesOfType(tx, entity.ItemType)))
				}

				// The off hand is empty, and takes a shield.
				mainBefore, _ := mob.HeldItems()
				refill(tx, pos, shield)
				if main, off := mob.HeldItems(); !off.Comparable(shield) || main.Item() != mainBefore.Item() {
					t.Errorf("hands hold %v and %v, want the shield in the off hand and the main hand as it was", main, off)
				}
				if len(entitiesOfType(tx, entity.ItemType)) != 1 {
					t.Errorf("%d items dropped, want only the second helmet", len(entitiesOfType(tx, entity.ItemType)))
				}
			})
		})
	}
}

// TestDispenserEquipsEveryPieceOnMobs checks the Equippable slot of each item a
// dispenser equips: armour goes to the slot it is worn in, a carved pumpkin, a
// skull and a turtle shell to the head, an elytra to the chest.
func TestDispenserEquipsEveryPieceOnMobs(t *testing.T) {
	t.Parallel()
	for _, c := range wearables {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			w, _ := dispenserWorld(t)
			<-w.Exec(func(tx *world.Tx) {
				var mob gearedMob
				pos := dispenseOnce(tx, c.stack, func(front cube.Pos) { mob = spawnGeared(t, tx, "minecraft:zombie", front.Vec3Middle(), 1) })
				head, chest, legs, feet := mob.EquipmentArmour()
				worn := map[string]item.Stack{"helmet": head, "chestplate": chest, "leggings": legs, "boots": feet}
				slot := map[string]string{"helmet": "helmet", "chestplate": "chestplate", "leggings": "leggings", "boots": "boots",
					"elytra": "chestplate", "turtle shell": "helmet", "skeleton skull": "helmet", "zombie head": "helmet",
					"wither skeleton skull": "helmet", "carved pumpkin": "helmet"}[c.name]
				if got := worn[slot]; !got.Comparable(c.stack) || !slotOf(tx, pos).Empty() {
					t.Errorf("%s holds %v, want the %s", slot, got, c.name)
				}
				for name, other := range worn {
					if name != slot && !other.Empty() {
						t.Errorf("%s holds %v, want it empty", name, other)
					}
				}
			})
		})
	}
}

// TestDispenserArmourOnPiglinsIsSureToDrop checks Mob.setGuaranteedDrop for the
// armour of a piglin: the piece a dispenser gave it drops when it dies, with the
// chance of an item a piglin picked up, and the pieces it spawned with do not.
func TestDispenserArmourOnPiglinsIsSureToDrop(t *testing.T) {
	t.Parallel()
	w, _ := dispenserWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		var mob gearedMob
		dispenseOnce(tx, item.NewStack(item.Chestplate{Tier: item.ArmourTierGold{}}, 1), func(front cube.Pos) {
			mob = spawnGeared(t, tx, "minecraft:piglin", front.Vec3Middle(), 0)
		})
		chances := entity.PiglinArmourDropChances(mob)
		if want := [4]float32{.085, 2, .085, .085}; chances != want {
			t.Errorf("armour drop chances are %v, want %v", chances, want)
		}
	})
}
