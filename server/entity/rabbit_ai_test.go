package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestRabbitsHop checks RabbitMoveControl and RabbitJumpControl: a rabbit
// lured by a carrot moves in hops, leaving the ground whenever it moves on.
func TestRabbitsHop(t *testing.T) {
	w := traderTestWorld(t)
	var h *world.EntityHandle
	playerPos := mgl64.Vec3{8.5, 64, .5}
	<-w.Exec(func(tx *world.Tx) {
		h = spawnAnimal(t, tx, "minecraft:rabbit", .5, nil)
		addSurvivalPlayer(tx, playerPos).SetHeldItems(item.NewStack(block.Carrot{}, 1), item.Stack{})
	})
	airborne, groundMoves, highest := 0, 0, 0.0
	var last mgl64.Vec3
	<-w.Exec(func(tx *world.Tx) { last = entityPos(tx, h) })
	for range 200 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			e, _ := h.Entity(tx)
			pos := e.Position()
			highest = max(highest, pos[1]-64)
			if horizontalDistance(pos, last) > .05 {
				if e.(interface{ OnGround() bool }).OnGround() {
					groundMoves++
				} else {
					airborne++
				}
			}
			last = pos
		})
	}
	if highest < .2 {
		t.Fatalf("rabbit rose at most %.2f blocks while lured, want it to hop", highest)
	}
	if airborne <= groundMoves*2 {
		t.Fatalf("rabbit moved %d ticks in the air and %d on the ground, want it to move mostly in hops", airborne, groundMoves)
	}
	<-w.Exec(func(tx *world.Tx) {
		if d := horizontalDistance(entityPos(tx, h), playerPos); d > 3.5 {
			t.Fatalf("rabbit hopped no closer than %.2f blocks to a player holding a carrot", d)
		}
	})
}

// TestRabbitsRaidCarrots checks RaidGardenGoal: a rabbit standing in a field
// of fully grown carrots nibbles one down a stage.
func TestRabbitsRaidCarrots(t *testing.T) {
	w := traderTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		for x := -2; x <= 2; x++ {
			for z := -2; z <= 2; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Farmland{Hydration: 7}, nil)
				c := block.Carrot{}
				c.Growth = 7
				tx.SetBlock(cube.Pos{x, 64, z}, c, nil)
			}
		}
		spawnAnimal(t, tx, "minecraft:rabbit", .5, nil)
	})
	eaten := false
	for tick := 0; tick < 400 && !eaten; tick++ {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			for x := -2; x <= 2; x++ {
				for z := -2; z <= 2; z++ {
					if c, ok := tx.Block(cube.Pos{x, 64, z}).(block.Carrot); ok && c.Growth == 6 {
						eaten = true
					}
				}
			}
		})
	}
	if !eaten {
		t.Fatal("rabbit in a carrot field ate no carrot in 20 seconds")
	}
}

// TestKillerBunniesAttackPlayers checks the goals Rabbit.setVariant gives
// the killer bunny: it hunts players and bites them for 8 damage.
func TestKillerBunniesAttackPlayers(t *testing.T) {
	w := traderTestWorld(t)
	var p *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		spawnAnimal(t, tx, "minecraft:rabbit", .5, map[string]any{"RabbitType": int32(99)})
		p = addSurvivalPlayer(tx, mgl64.Vec3{6.5, 64, .5}).H()
	})
	lowest := 20.0
	for range 300 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			e, _ := p.Entity(tx)
			lowest = min(lowest, e.(*player.Player).Health())
		})
	}
	if lowest > 20-8+1e-9 {
		t.Fatalf("player near a killer bunny kept %.2f health, want it bitten for 8", lowest)
	}
}
