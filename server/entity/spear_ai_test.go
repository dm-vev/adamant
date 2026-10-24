package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// spearTestWorld returns a world with a stone floor at night, so zombies do
// not burn.
func spearTestWorld(t *testing.T) *world.World {
	t.Helper()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	w.SetTime(18000)
	<-w.Exec(func(tx *world.Tx) {
		for x := -24; x <= 24; x++ {
			for z := -24; z <= 24; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
	})
	return w
}

// TestZombiesStabWithSpears checks SpearUseGoal and KineticWeapon: a zombie
// with a spear lowers it and charges its target, stabbing it.
func TestZombiesStabWithSpears(t *testing.T) {
	w := spearTestWorld(t)
	var golem *world.EntityHandle
	used := false
	<-w.Exec(func(tx *world.Tx) {
		z := tx.AddEntity(NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 64, .5}}))
		z.(interface{ state() *hostileState }).state().main = item.NewStack(item.Spear{Tier: item.ToolTierIron}, 1)
		g := tx.AddEntity(NewIronGolem(world.EntitySpawnOpts{Position: mgl64.Vec3{12.5, 64, .5}}))
		golem = g.H()
		// A golem that fights back would kill the zombie first.
		m, _ := mobOf(g)
		m.AI.NoAI = true
	})
	for range 1200 {
		stepTicks(t, w, 1)
		stabbed := false
		<-w.Exec(func(tx *world.Tx) {
			for e := range tx.Entities() {
				if z, ok := e.(*Zombie); ok && z.UsingItem() {
					used = true
				}
			}
			g, ok := golem.Entity(tx)
			stabbed = !ok || g.(Living).Health() < g.(Living).MaxHealth()
		})
		if stabbed {
			if !used {
				t.Fatal("zombie hurt its target without lowering its spear")
			}
			return
		}
	}
	t.Fatalf("zombie with a spear did not stab an iron golem twelve blocks away in a minute (lowered %v)", used)
}
