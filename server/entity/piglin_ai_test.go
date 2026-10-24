package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// immunePiglin spawns an adult piglin with a golden sword that does not turn
// into a zombified piglin in the overworld.
func immunePiglin(t *testing.T, tx *world.Tx, id string, x float64) *world.EntityHandle {
	t.Helper()
	h := spawnAnimal(t, tx, id, x, map[string]any{"IsImmuneToZombification": byte(1)})
	e, _ := h.Entity(tx)
	if p, ok := e.(*entity.Piglin); ok {
		p.SetHeldItems(item.NewStack(item.Sword{Tier: item.ToolTierGold}, 1), item.Stack{})
	}
	return h
}

// goldenPlayer adds a survival player wearing a golden helmet, which
// piglins leave alone.
func goldenPlayer(tx *world.Tx, pos mgl64.Vec3) *player.Player {
	p := addSurvivalPlayer(tx, pos)
	p.Armour().SetHelmet(item.NewStack(item.Helmet{Tier: item.ArmourTierGold{}}, 1))
	return p
}

func playerHurt(tx *world.Tx, h *world.EntityHandle) bool {
	e, _ := h.Entity(tx)
	return e.(entity.Living).Health() < e.(entity.Living).MaxHealth()
}

// TestPiglinsAttackPlayersWithoutGold checks PiglinSpecificSensor and the
// FIGHT activity: a piglin attacks a player wearing no gold.
func TestPiglinsAttackPlayersWithoutGold(t *testing.T) {
	w := wolfTestWorld(t)
	var p *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		immunePiglin(t, tx, "minecraft:piglin", .5)
		p = addSurvivalPlayer(tx, mgl64.Vec3{6.5, 64, .5}).H()
	})
	for range 600 {
		w.AdvanceTick()
		hurt := false
		<-w.Exec(func(tx *world.Tx) { hurt = playerHurt(tx, p) })
		if hurt {
			return
		}
	}
	t.Fatal("piglin did not attack a player wearing no gold six blocks away in half a minute")
}

// TestPiglinsLeavePlayersInGoldAlone checks PiglinAi.isWearingSafeArmor: a
// piglin leaves a player wearing a golden helmet alone.
func TestPiglinsLeavePlayersInGoldAlone(t *testing.T) {
	w := wolfTestWorld(t)
	var p *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		immunePiglin(t, tx, "minecraft:piglin", .5)
		p = goldenPlayer(tx, mgl64.Vec3{3.5, 64, .5}).H()
	})
	for range 400 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			if playerHurt(tx, p) {
				t.Fatal("piglin attacked a player wearing a golden helmet")
			}
		})
	}
}

// TestPiglinsBarterForGold checks PiglinAi.mobInteract and
// stopHoldingOffHandItem: a piglin given a gold ingot admires it for six
// seconds and throws its bartering loot to the player.
func TestPiglinsBarterForGold(t *testing.T) {
	w := wolfTestWorld(t)
	var piglin *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		piglin = immunePiglin(t, tx, "minecraft:piglin", .5)
		p := goldenPlayer(tx, mgl64.Vec3{3.5, 64, .5})
		p.SetHeldItems(item.NewStack(item.GoldIngot{}, 1), item.Stack{})
		e, _ := piglin.Entity(tx)
		if !e.(*entity.Piglin).Interact(tx, p, &item.UseContext{}) {
			t.Fatal("piglin did not take a gold ingot")
		}
		if !e.(*entity.Piglin).Admiring() {
			t.Fatal("piglin given a gold ingot does not admire it")
		}
	})
	for range 200 {
		w.AdvanceTick()
		bartered := false
		<-w.Exec(func(tx *world.Tx) {
			for e := range tx.Entities() {
				if i, ok := e.(*entity.Ent); ok && i.H().Type() == entity.ItemType {
					bartered = true
				}
			}
		})
		if bartered {
			<-w.Exec(func(tx *world.Tx) {
				e, _ := piglin.Entity(tx)
				if e.(*entity.Piglin).Admiring() {
					t.Fatal("piglin still admires the gold it bartered away")
				}
			})
			return
		}
	}
	t.Fatal("piglin did not barter a gold ingot in ten seconds")
}

// TestPiglinsPickUpGold checks NearestItemSensor, StartAdmiringItemIfSeen
// and GoToWantedItem: a piglin walks to a gold ingot on the ground, picks it
// up and admires it.
func TestPiglinsPickUpGold(t *testing.T) {
	w := wolfTestWorld(t)
	var piglin *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		piglin = immunePiglin(t, tx, "minecraft:piglin", .5)
		tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{6.5, 64, .5}}, item.NewStack(item.GoldIngot{}, 1)))
	})
	for range 600 {
		w.AdvanceTick()
		admiring := false
		<-w.Exec(func(tx *world.Tx) {
			e, _ := piglin.Entity(tx)
			admiring = e.(*entity.Piglin).Admiring()
		})
		if admiring {
			return
		}
	}
	t.Fatal("piglin did not pick up a gold ingot six blocks away in half a minute")
}

// TestPiglinsGuardChests checks PiglinAi.angerNearbyPiglins: a piglin that
// sees a player in gold open a chest attacks them.
func TestPiglinsGuardChests(t *testing.T) {
	w := wolfTestWorld(t)
	var p *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{4, 64, 2}, block.NewChest(), nil)
		immunePiglin(t, tx, "minecraft:piglin", .5)
		p = goldenPlayer(tx, mgl64.Vec3{4.5, 64, .5}).H()
	})
	// The piglin notices the player within a second.
	for range 40 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		e, _ := p.Entity(tx)
		e.(*player.Player).OpenBlockContainer(cube.Pos{4, 64, 2}, tx)
	})
	for range 600 {
		w.AdvanceTick()
		hurt := false
		<-w.Exec(func(tx *world.Tx) { hurt = playerHurt(tx, p) })
		if hurt {
			return
		}
	}
	t.Fatal("piglin did not attack a player who opened a chest in front of it")
}

// TestPiglinBrutesAttackPlayersInGold checks PiglinBruteAi: a brute attacks
// even a player wearing gold.
func TestPiglinBrutesAttackPlayersInGold(t *testing.T) {
	w := wolfTestWorld(t)
	var p *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		immunePiglin(t, tx, "minecraft:piglin_brute", .5)
		p = goldenPlayer(tx, mgl64.Vec3{5.5, 64, .5}).H()
	})
	for range 600 {
		w.AdvanceTick()
		hurt := false
		<-w.Exec(func(tx *world.Tx) { hurt = playerHurt(tx, p) })
		if hurt {
			return
		}
	}
	t.Fatal("piglin brute did not attack a player in gold five blocks away in half a minute")
}

// TestPiglinsTurnIntoZombifiedPiglins checks AbstractPiglin.isConverting.
func TestPiglinsTurnIntoZombifiedPiglins(t *testing.T) {
	w := wolfTestWorld(t)
	<-w.Exec(func(tx *world.Tx) { spawnAnimal(t, tx, "minecraft:piglin", .5, nil) })
	for range 320 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if n, z := countEntities(tx, "minecraft:piglin"), countEntities(tx, "minecraft:zombie_pigman"); n != 0 || z != 1 {
			t.Fatalf("%d piglins and %d zombified piglins after sixteen seconds in the overworld, want 0 and 1", n, z)
		}
	})
}

// TestPiglinsChargeWithSpears checks SpearApproach, SpearAttack and
// SpearRetreat: a piglin with a golden spear charges a player wearing no
// gold and stabs them.
func TestPiglinsChargeWithSpears(t *testing.T) {
	w := wolfTestWorld(t)
	var pig, p *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		pig = immunePiglin(t, tx, "minecraft:piglin", .5)
		e, _ := pig.Entity(tx)
		e.(*entity.Piglin).SetHeldItems(item.NewStack(item.Spear{Tier: item.ToolTierGold}, 1), item.Stack{})
		p = addSurvivalPlayer(tx, mgl64.Vec3{12.5, 64, .5}).H()
	})
	lowered := false
	for range 1200 {
		w.AdvanceTick()
		hurt := false
		<-w.Exec(func(tx *world.Tx) {
			e, _ := pig.Entity(tx)
			lowered = lowered || e.(*entity.Piglin).UsingItem()
			hurt = playerHurt(tx, p)
		})
		if hurt {
			if !lowered {
				t.Fatal("piglin hurt a player without lowering its spear")
			}
			return
		}
	}
	t.Fatalf("piglin with a spear did not stab a player twelve blocks away in a minute (lowered %v)", lowered)
}
