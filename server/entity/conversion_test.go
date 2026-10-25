package entity_test

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestZombiesDrownIntoDrowned checks Zombie's drowning ConversionTracker and
// ConversionType.SINGLE: a zombie with its eyes under water for 30 seconds
// shakes for 15 and turns into a drowned that keeps its helmet and name.
func TestZombiesDrownIntoDrowned(t *testing.T) {
	w := happyGhastWorld(t)
	var h *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		for x := -2; x <= 2; x++ {
			for z := -2; z <= 2; z++ {
				for y := 64; y <= 67; y++ {
					switch {
					case y == 67 || x == -2 || x == 2 || z == -2 || z == 2:
						tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, nil)
					default:
						tx.SetBlock(cube.Pos{x, y, z}, block.Water{Still: true, Depth: 8}, nil)
					}
				}
			}
		}
		h = spawnAnimal(t, tx, "minecraft:zombie", .5, map[string]any{"PersistenceRequired": byte(1), "equipment": map[string]any{
			"head": map[string]any{"id": "minecraft:iron_helmet", "count": int32(1)},
		}})
		e, _ := h.Entity(tx)
		e.(*entity.Zombie).SetNameTag("Bob")
	})
	shook := false
	for range 1000 {
		w.AdvanceTick()
		var drowned *entity.Drowned
		<-w.Exec(func(tx *world.Tx) {
			if e, ok := h.Entity(tx); ok {
				shook = shook || e.(*entity.Zombie).Shaking()
				return
			}
			for e := range tx.Entities() {
				if d, ok := e.(*entity.Drowned); ok {
					drowned = d
					if !shook {
						t.Fatal("zombie turned into a drowned without shaking first")
					}
					if head, _, _, _ := d.EquipmentArmour(); head.Empty() {
						t.Fatal("drowned lost the helmet of the zombie it was")
					}
					if d.NameTag() != "Bob" {
						t.Fatalf("drowned is named %q, want the zombie's name Bob", d.NameTag())
					}
				}
			}
			if drowned == nil {
				t.Fatal("zombie vanished without turning into a drowned")
			}
		})
		if drowned != nil {
			return
		}
	}
	t.Fatal("zombie under water for 50 seconds did not turn into a drowned")
}

// TestZombieVillagersAreCured checks ZombieVillager.startConverting and
// finishConversion: a weakened zombie villager fed a golden apple turns back
// into a villager that keeps its profession and favours its curer.
func TestZombieVillagersAreCured(t *testing.T) {
	w := happyGhastWorld(t)
	// It is night, so the zombie villager does not burn, and no monsters
	// spawn to fight around it.
	w.SetTime(18000)
	w.StopTime()
	w.SetSpawnMobs(false)
	var h *world.EntityHandle
	var curer world.Entity
	<-w.Exec(func(tx *world.Tx) {
		zv := tx.AddEntity(entity.NewZombieVillager(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 64, .5}})).(*entity.ZombieVillager)
		h = zv.H()
		zv.AddEffect(effect.New(effect.Weakness, 1, time.Minute))
		p := addSurvivalPlayer(tx, mgl64.Vec3{2.5, 64, .5})
		curer = p
		p.SetHeldItems(item.NewStack(item.GoldenApple{}, 1), item.Stack{})
		if !zv.Interact(tx, p, &item.UseContext{}) {
			t.Fatal("weakened zombie villager refused a golden apple")
		}
		if !zv.Shaking() {
			t.Fatal("zombie villager being cured does not shake")
		}
	})
	for range 6100 {
		w.AdvanceTick()
		cured := false
		<-w.Exec(func(tx *world.Tx) {
			if _, ok := h.Entity(tx); ok {
				return
			}
			for e := range tx.Entities() {
				if v, ok := e.(*entity.Villager); ok {
					cured = true
					p, _ := curer.H().Entity(tx)
					if v.Reputation(p) <= 0 {
						t.Fatal("cured villager does not favour the player who cured it")
					}
				}
			}
			if !cured {
				t.Fatal("zombie villager vanished without turning into a villager")
			}
		})
		if cured {
			return
		}
	}
	t.Fatal("zombie villager fed a golden apple was not cured in five minutes")
}

// TestLightningTurnsVillagersIntoWitches checks Villager.thunderHit.
func TestLightningTurnsVillagersIntoWitches(t *testing.T) {
	w := happyGhastWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		addVillager(t, tx, mgl64.Vec3{.5, 64, .5})
		tx.AddEntity(entity.NewLightning(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 64, .5}}))
	})
	for range 5 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		witches, villagers := 0, 0
		for e := range tx.Entities() {
			switch e.(type) {
			case *entity.Witch:
				witches++
			case *entity.Villager:
				villagers++
			}
		}
		if witches != 1 || villagers != 0 {
			t.Fatalf("struck villager left %d witches and %d villagers, want a witch", witches, villagers)
		}
	})
}
