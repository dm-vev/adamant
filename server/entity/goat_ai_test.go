package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// goatTestWorld returns a world with a floor of stone at y 63 and a goat in
// it, its cooldowns before ramming and long jumping set as passed.
func goatTestWorld(t *testing.T, pos mgl64.Vec3, ram, jump int) (*world.World, *world.EntityHandle) {
	t.Helper()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	var goat *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		for x := -20; x <= 20; x++ {
			for z := -20; z <= 20; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
		goat = tx.AddEntity(NewGoat(world.EntitySpawnOpts{Position: pos})).H()
		e, _ := goat.Entity(tx)
		b := e.(*Goat).behaviour().br
		b.set(memRamCooldownTicks, ram)
		b.set(memLongJumpCooldownTicks, jump)
	})
	return w, goat
}

func stepTicks(t *testing.T, w *world.World, n int) {
	t.Helper()
	for range n {
		if err := w.StepTicks(1); err != nil {
			t.Fatal(err)
		}
		w.AdvanceTick()
	}
}

// TestGoatsRamMobs checks PrepareRamNearestTarget and RamTarget: a goat
// lines up with a mob standing nearby, lowers its head and charges.
func TestGoatsRamMobs(t *testing.T) {
	w, _ := goatTestWorld(t, mgl64.Vec3{.5, 64, .5}, 1, 100000)
	var cow *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		cow = tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{5.5, 64, .5}})).H()
		// A cow that strolls about makes the goat line up anew each time.
		e, _ := cow.Entity(tx)
		ai, _ := e.(*Cow).Ent.mobAI()
		ai.NoAI = true
	})
	// A goat strolling when it picks its target walks its stroll out first,
	// as PathNavigation.createPath hands back the path it follows, and may
	// run out of time to line up, after which it waits 30 seconds.
	for range 4000 {
		stepTicks(t, w, 1)
		hurt := false
		<-w.Exec(func(tx *world.Tx) {
			e, _ := cow.Entity(tx)
			hurt = e.(Living).Health() < e.(Living).MaxHealth()
		})
		if hurt {
			return
		}
	}
	t.Fatal("goat did not ram a cow five blocks away in over three minutes")
}

// TestGoatsLongJumpOffLedges checks LongJumpToRandomPos and LongJumpMidJump:
// a goat on a pillar it cannot walk down from leaps to the ground, landing
// without harm.
func TestGoatsLongJumpOffLedges(t *testing.T) {
	// The goat stands four blocks up, too high to walk down but low enough
	// to jump.
	w, goat := goatTestWorld(t, mgl64.Vec3{.5, 68, .5}, 100000, 100000)
	<-w.Exec(func(tx *world.Tx) {
		for y := 64; y <= 67; y++ {
			tx.SetBlock(cube.Pos{0, y, 0}, block.Stone{}, nil)
		}
	})
	// A goat off the ground puts off its jump, so it stands on the pillar
	// first.
	stepTicks(t, w, 5)
	<-w.Exec(func(tx *world.Tx) {
		e, _ := goat.Entity(tx)
		e.(*Goat).behaviour().br.set(memLongJumpCooldownTicks, 1)
	})
	jumped := false
	for range 600 {
		stepTicks(t, w, 1)
		landed := false
		<-w.Exec(func(tx *world.Tx) {
			e, _ := goat.Entity(tx)
			g := e.(*Goat)
			jumped = jumped || g.JumpGoalJumping()
			landed = g.Position()[1] < 65 && g.OnGround()
			if g.Health() < g.MaxHealth() {
				t.Fatal("goat was hurt by its long jump")
			}
		})
		if landed {
			if !jumped {
				t.Fatal("goat came down without a long jump")
			}
			return
		}
	}
	t.Fatal("goat on a pillar did not long jump to the ground in half a minute")
}

// TestGoatsGiveMilk checks the milking of Goat.mobInteract: adults give
// milk, kids do not.
func TestGoatsGiveMilk(t *testing.T) {
	w, goat := goatTestWorld(t, mgl64.Vec3{.5, 64, .5}, 100000, 100000)
	<-w.Exec(func(tx *world.Tx) {
		e, _ := goat.Entity(tx)
		if !e.(*Goat).Milk(tx) {
			t.Fatal("adult goat gave no milk")
		}
		kid := tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{3.5, 64, .5}}.New(GoatType, GoatConfig{Age: -24000})).(*Goat)
		if kid.Milk(tx) {
			t.Fatal("kid gave milk")
		}
	})
}
