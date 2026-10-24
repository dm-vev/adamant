package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// immuneHoglin spawns a hoglin that does not turn into a zoglin in the
// overworld.
func immuneHoglin(t *testing.T, tx *world.Tx, x float64) *world.EntityHandle {
	t.Helper()
	return spawnAnimal(t, tx, "minecraft:hoglin", x, map[string]any{"IsImmuneToZombification": byte(1)})
}

// TestHoglinsAttackPlayers checks the FIGHT activity of HoglinAi: a hoglin
// charges a player nearby and gores them.
func TestHoglinsAttackPlayers(t *testing.T) {
	w := wolfTestWorld(t)
	var p *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		immuneHoglin(t, tx, .5)
		p = addSurvivalPlayer(tx, mgl64.Vec3{6.5, 64, .5}).H()
	})
	for range 600 {
		w.AdvanceTick()
		hurt := false
		<-w.Exec(func(tx *world.Tx) {
			e, _ := p.Entity(tx)
			hurt = e.(entity.Living).Health() < e.(entity.Living).MaxHealth()
		})
		if hurt {
			return
		}
	}
	t.Fatal("hoglin did not attack a player six blocks away in half a minute")
}

// TestHoglinsShunRepellents checks BecomePassiveIfMemoryPresent and
// SetWalkTargetAwayFrom: a hoglin near warped fungus leaves the player
// alone and walks away from the fungus.
func TestHoglinsShunRepellents(t *testing.T) {
	w := wolfTestWorld(t)
	var hoglin, p *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 63, 0}, block.Nylium{}, nil)
		tx.SetBlock(cube.Pos{0, 64, 0}, block.Fungus{Warped: true}, nil)
		hoglin = immuneHoglin(t, tx, 2.5)
	})
	// The hoglin notices the fungus within a second, as its sensors scan
	// once a second, and stays pacified for ten seconds after.
	for range 40 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) { p = addSurvivalPlayer(tx, mgl64.Vec3{4.5, 64, .5}).H() })
	far := false
	for range 160 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			e, _ := p.Entity(tx)
			if e.(entity.Living).Health() < e.(entity.Living).MaxHealth() {
				t.Fatal("hoglin near warped fungus attacked a player")
			}
			far = far || horizontalDistance(entityPos(tx, hoglin), mgl64.Vec3{.5, 64, .5}) >= 8
		})
	}
	if !far {
		t.Fatal("hoglin did not walk eight blocks away from warped fungus in ten seconds")
	}
}

// TestHoglinsTurnIntoZoglins checks Hoglin.isConverting: a hoglin in the
// overworld becomes a zoglin after fifteen seconds.
func TestHoglinsTurnIntoZoglins(t *testing.T) {
	w := wolfTestWorld(t)
	<-w.Exec(func(tx *world.Tx) { spawnAnimal(t, tx, "minecraft:hoglin", .5, nil) })
	for range 320 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if countEntities(tx, "minecraft:hoglin") != 0 || countEntities(tx, "minecraft:zoglin") != 1 {
			t.Fatalf("%d hoglins and %d zoglins after sixteen seconds in the overworld, want 0 and 1", countEntities(tx, "minecraft:hoglin"), countEntities(tx, "minecraft:zoglin"))
		}
	})
}

// TestZoglinsAttackAnyMob checks the brain of Zoglin: a zoglin attacks the
// mobs around it, here a cow.
func TestZoglinsAttackAnyMob(t *testing.T) {
	w := wolfTestWorld(t)
	var cow *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		tx.AddEntity(entity.NewZoglin(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 64, .5}}))
		cow = spawnAnimal(t, tx, "minecraft:cow", 5.5, nil)
	})
	for range 600 {
		w.AdvanceTick()
		hurt := false
		<-w.Exec(func(tx *world.Tx) {
			e, ok := cow.Entity(tx)
			hurt = !ok || e.(entity.Living).Health() < e.(entity.Living).MaxHealth()
		})
		if hurt {
			return
		}
	}
	t.Fatal("zoglin did not attack a cow five blocks away in half a minute")
}
