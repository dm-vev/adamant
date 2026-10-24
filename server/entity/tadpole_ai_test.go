package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// tadpoleTestWorld returns a world with a pool of water three deep and a
// tadpole of the age passed in it.
func tadpoleTestWorld(t *testing.T, age int) (*world.World, *world.EntityHandle) {
	t.Helper()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	var tadpole *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		for x := -10; x <= 10; x++ {
			for z := -10; z <= 10; z++ {
				tx.SetBlock(cube.Pos{x, 60, z}, block.Stone{}, nil)
				for y := 61; y <= 63; y++ {
					if x == -10 || x == 10 || z == -10 || z == 10 {
						tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, nil)
					} else {
						tx.SetBlock(cube.Pos{x, y, z}, block.Water{Still: true, Depth: 8}, nil)
					}
				}
			}
		}
		tadpole = tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 62, .5}}.New(TadpoleType, AquaticMobConfig{ID: "minecraft:tadpole", MaxHealth: 6, Age: age})).H()
	})
	return w, tadpole
}

func tadpoleOf(tx *world.Tx, h *world.EntityHandle) (*Tadpole, bool) {
	e, ok := h.Entity(tx)
	if !ok {
		return nil, false
	}
	t, ok := e.(*Tadpole)
	return t, ok
}

// TestTadpolesGrowFasterOnSlimeBalls checks Tadpole.feed: a slime ball takes
// a tenth off the time a tadpole has left to grow up.
func TestTadpolesGrowFasterOnSlimeBalls(t *testing.T) {
	w, h := tadpoleTestWorld(t, 0)
	<-w.Exec(func(tx *world.Tx) {
		tp, _ := tadpoleOf(tx, h)
		b := tp.behaviour()
		ctx := &item.UseContext{}
		if !b.feedTadpole(tp.Ent, tx, item.NewStack(item.Slimeball{}, 1), ctx) || ctx.CountSub != 1 {
			t.Fatal("tadpole did not eat a slime ball")
		}
		// 24000 ticks left make 120 seconds sooner.
		if b.age != 2400 {
			t.Fatalf("fed tadpole is %d ticks old, want 2400", b.age)
		}
	})
}

// TestGoldenDandelionsStopTadpolesGrowing checks the age lock of
// Tadpole.mobInteract: a tadpole given a golden dandelion stays a tadpole
// and refuses slime balls.
func TestGoldenDandelionsStopTadpolesGrowing(t *testing.T) {
	w, h := tadpoleTestWorld(t, tadpoleTicksToBeFrog-5)
	<-w.Exec(func(tx *world.Tx) {
		tp, _ := tadpoleOf(tx, h)
		b := tp.behaviour()
		if !b.feedTadpole(tp.Ent, tx, item.NewStack(block.GoldenDandelion{}, 1), &item.UseContext{}) {
			t.Fatal("tadpole did not take a golden dandelion")
		}
		if !b.breeding.ageLocked || b.age != 0 {
			t.Fatalf("tadpole given a golden dandelion: locked %v, age %d, want locked at 0", b.breeding.ageLocked, b.age)
		}
		if b.feedTadpole(tp.Ent, tx, item.NewStack(item.Slimeball{}, 1), &item.UseContext{}) {
			t.Fatal("age locked tadpole ate a slime ball")
		}
	})
	stepTicks(t, w, 50)
	<-w.Exec(func(tx *world.Tx) {
		tp, ok := tadpoleOf(tx, h)
		if !ok || tp.behaviour().age != 0 {
			t.Fatal("age locked tadpole grew")
		}
	})
}

// TestTadpolesGrowIntoFrogs checks Tadpole.ageUp: a tadpole twenty minutes
// old turns into a frog.
func TestTadpolesGrowIntoFrogs(t *testing.T) {
	w, h := tadpoleTestWorld(t, tadpoleTicksToBeFrog-3)
	stepTicks(t, w, 5)
	<-w.Exec(func(tx *world.Tx) {
		if _, ok := h.Entity(tx); ok {
			t.Fatal("tadpole was still about after growing up")
		}
		frogs := 0
		for e := range tx.Entities() {
			if _, ok := e.(*Frog); ok {
				frogs++
			}
		}
		if frogs != 1 {
			t.Fatalf("%d frogs after a tadpole grew up, want 1", frogs)
		}
	})
}

// TestTadpolesSwimAbout checks the idle activity of TadpoleAi and the swim
// goal of AbstractFish: a tadpole swims about its pool.
func TestTadpolesSwimAbout(t *testing.T) {
	w, h := tadpoleTestWorld(t, 0)
	start := mgl64.Vec3{.5, 62, .5}
	for range 1200 {
		stepTicks(t, w, 1)
		moved := false
		<-w.Exec(func(tx *world.Tx) {
			tp, _ := tadpoleOf(tx, h)
			d := tp.Position().Sub(start)
			d[1] = 0
			moved = d.Len() > 3
		})
		if moved {
			return
		}
	}
	t.Fatal("tadpole did not swim three blocks in a minute")
}
