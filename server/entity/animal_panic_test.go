package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// panicDistance is how far a Java mob with the movement speed attribute
// and PanicGoal speed modifier passed runs in 20 ticks on normal ground
// without stopping.
func panicDistance(attribute, modifier float64) float64 {
	s := attribute * modifier
	return 20 * .98 * s * s / (1 - .6*.91)
}

func panicking(tx *world.Tx, h *world.EntityHandle) bool {
	e, _ := h.Entity(tx)
	return e.(interface{ Panicking() bool }).Panicking()
}

// startsPanicking reports whether the mob panics within a second. Like
// Java's PanicGoal it may not find a spot to run to on its first tries:
// DefaultRandomPos picks spots up to four blocks above and below the mob,
// and on flat ground only those at its feet stand on a block.
func startsPanicking(w *world.World, h *world.EntityHandle) bool {
	for range 20 {
		w.AdvanceTick()
		started := false
		<-w.Exec(func(tx *world.Tx) { started = panicking(tx, h) })
		if started {
			return true
		}
	}
	return false
}

func hurt(w *world.World, h *world.EntityHandle, src func(tx *world.Tx) world.DamageSource) {
	<-w.Exec(func(tx *world.Tx) {
		e, _ := h.Entity(tx)
		e.(entity.Living).Hurt(1, src(tx))
	})
}

func playerAttack(tx *world.Tx) world.DamageSource {
	return entity.AttackDamageSource{Attacker: addSurvivalPlayer(tx, mgl64.Vec3{2.5, 64, .5})}
}

func TestAnimalsPanicWhenHurt(t *testing.T) {
	cases := []struct {
		id                  string
		nbt                 map[string]any
		attribute, modifier float64
		// hop is the distance a mob that hops covers in its fastest tick.
		hop float64
	}{
		{id: "minecraft:cow", attribute: .2, modifier: 2},
		{id: "minecraft:mooshroom", attribute: .2, modifier: 2},
		{id: "minecraft:pig", attribute: .25, modifier: 1.25},
		{id: "minecraft:sheep", attribute: .23, modifier: 1.25},
		{id: "minecraft:chicken", attribute: .25, modifier: 1.4},
		// RabbitMoveControl moves a rabbit only while it hops: it pushes
		// off the ground at its speed for a tick and glides in the air.
		{id: "minecraft:rabbit", attribute: .3, modifier: 2.2, hop: .3 * 2.2 * .98 * .3 * 2.2},
		{id: "minecraft:cat", attribute: .3, modifier: 1.5},
		{id: "minecraft:fox", attribute: .3, modifier: 2.2},
		{id: "minecraft:polar_bear", nbt: map[string]any{"Age": int32(-24000)}, attribute: .25, modifier: 2},
		// TurtleMoveControl halves the speed of a turtle on land, to no less
		// than 0.06, before easing it an eighth of the way to its target
		// of 0.3 every tick, which settles at 0.09.
		{id: "minecraft:turtle", attribute: .09, modifier: 1},
		{id: "minecraft:strider", attribute: .175, modifier: 1.65},
		{id: "minecraft:goat", attribute: .2, modifier: 2},
		{id: "minecraft:sniffer", attribute: .1, modifier: 2},
		// SmoothSwimmingMoveControl walks at a tenth of the frog's speed.
		{id: "minecraft:frog", attribute: .1, modifier: 2},
		{id: "minecraft:copper_golem", attribute: .2, modifier: 1.5},
		{id: "minecraft:camel", attribute: .09, modifier: 4},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			w := traderTestWorld(t)
			// A camel that never changed its pose is still standing up
			// during the first 52 game ticks of a world.
			for range 53 {
				w.AdvanceTick()
			}
			var h *world.EntityHandle
			<-w.Exec(func(tx *world.Tx) {
				// Camels on grass may stop to eat it even while panicking.
				for x := -20; x <= 20; x++ {
					for z := -20; z <= 20; z++ {
						tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
					}
				}
				h = spawnAnimal(t, tx, c.id, .5, c.nbt)
			})
			w.AdvanceTick()
			<-w.Exec(func(tx *world.Tx) {
				if panicking(tx, h) {
					t.Fatal("animal panics before it is hurt")
				}
			})
			hurt(w, h, playerAttack)
			if !startsPanicking(w, h) {
				t.Fatal("hurt animal does not panic")
			}
			var last mgl64.Vec3
			<-w.Exec(func(tx *world.Tx) { e, _ := h.Entity(tx); last = e.Position() })
			run, fastest := 0.0, 0.0
			for range 40 {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					e, _ := h.Entity(tx)
					pos := e.Position()
					pos[1], last[1] = 0, 0
					step := pos.Sub(last).Len()
					run, last, fastest = run+step, pos, max(fastest, step)
				})
			}
			// PanicGoal stops where its path ends and, on flat ground, often
			// finds no spot to run to, so the mob runs at full speed only
			// now and then during the two seconds it panics.
			want, peak := panicDistance(c.attribute, c.modifier), panicDistance(c.attribute, c.modifier)/20
			if c.hop > 0 {
				peak = c.hop
				want = 20 * c.hop
			}
			if fastest < .9*peak {
				t.Fatalf("panicking %s ran at most %.3f blocks a tick, want at least %.3f", c.id, fastest, .9*peak)
			}
			if run < .5*want {
				t.Fatalf("panicking %s ran %.2f blocks in two seconds, want at least %.2f", c.id, run, .5*want)
			}
		})
	}
}

func TestWolvesPandasAndArmadillosOnlyPanicFromHazards(t *testing.T) {
	for _, id := range []string{"minecraft:wolf", "minecraft:panda", "minecraft:armadillo"} {
		t.Run(id, func(t *testing.T) {
			w := traderTestWorld(t)
			var h *world.EntityHandle
			<-w.Exec(func(tx *world.Tx) { h = spawnAnimal(t, tx, id, .5, nil) })
			w.AdvanceTick()
			hurt(w, h, playerAttack)
			if startsPanicking(w, h) {
				t.Fatal("panics from an attack")
			}
			hurt(w, h, func(*world.Tx) world.DamageSource { return block.FireDamageSource{} })
			if !startsPanicking(w, h) {
				t.Fatal("does not panic from fire")
			}
		})
	}
}

// TestBrainPanicLastsFiveSeconds checks AnimalPanic: unlike PanicGoal, which
// forgets the damage after two seconds, a mob with a brain panics for 100 to
// 120 ticks after one hit.
func TestBrainPanicLastsFiveSeconds(t *testing.T) {
	w := traderTestWorld(t)
	var h *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) { h = spawnAnimal(t, tx, "minecraft:goat", .5, nil) })
	w.AdvanceTick()
	hurt(w, h, playerAttack)
	for range 95 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if !panicking(tx, h) {
			t.Fatal("goat calmed down before the panic's minimum duration")
		}
	})
	for range 30 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if panicking(tx, h) {
			t.Fatal("goat still panics after the panic's maximum duration")
		}
	})
}
