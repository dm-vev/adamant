package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// addLookingPlayer adds a survival player at pos facing yaw degrees, 90
// looking towards negative x.
func addLookingPlayer(tx *world.Tx, pos mgl64.Vec3, yaw float64) *world.EntityHandle {
	return tx.AddEntity((world.EntitySpawnOpts{}).New(player.Type, player.Config{Position: pos, Rotation: cube.Rotation{yaw, 0}, GameMode: world.GameModeSurvival})).H()
}

// TestCreakingsFreezeWhenWatched checks Creaking.checkCanMove: a creaking a
// player looks at wakes up but stays still.
func TestCreakingsFreezeWhenWatched(t *testing.T) {
	w := happyGhastWorld(t)
	var c, p *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		c = spawnAnimal(t, tx, "minecraft:creaking", .5, nil)
		p = addLookingPlayer(tx, mgl64.Vec3{6.5, 64, .5}, 90)
	})
	// The creaking notices players within a second, and may take a step
	// before it does.
	for range 30 {
		w.AdvanceTick()
	}
	var start mgl64.Vec3
	<-w.Exec(func(tx *world.Tx) { e, _ := c.Entity(tx); start = e.Position() })
	for range 200 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			e, _ := c.Entity(tx)
			if pos := e.Position(); (mgl64.Vec2{pos[0] - start[0], pos[2] - start[2]}).Len() > 1e-6 {
				t.Fatalf("watched creaking moved from %v to %v", start, pos)
			}
		})
	}
	<-w.Exec(func(tx *world.Tx) {
		e, _ := c.Entity(tx)
		if state, _ := e.(*entity.Creaking).CreakingState(); state != 1 {
			t.Fatalf("watched creaking has state %d, want 1 (hostile and observed)", state)
		}
		if playerHurt(tx, p) {
			t.Fatal("frozen creaking hurt the player watching it")
		}
	})
}

// TestCreakingsAttackWhenUnwatched checks CreakingAi's fight activity: a
// creaking woken up by a player that then looks away walks up to them and
// attacks.
func TestCreakingsAttackWhenUnwatched(t *testing.T) {
	w := happyGhastWorld(t)
	var p *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		spawnAnimal(t, tx, "minecraft:creaking", .5, nil)
		p = addLookingPlayer(tx, mgl64.Vec3{6.5, 64, .5}, 90)
	})
	for range 60 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		e, _ := p.Entity(tx)
		e.(*player.Player).Move(mgl64.Vec3{}, 180, 0)
	})
	for range 600 {
		w.AdvanceTick()
		hurt := false
		<-w.Exec(func(tx *world.Tx) { hurt = playerHurt(tx, p) })
		if hurt {
			return
		}
	}
	t.Fatal("creaking did not attack a player that looked away in half a minute")
}
