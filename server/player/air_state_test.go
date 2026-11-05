package player

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// The bodies below are the ones tickAirSupply, AirSupply and the setters
// held before they moved to airState (P3d), over the state alone with what
// the world says (can the player breathe, is the air frozen) as arguments.

// oldTickAir returns whether the player is hurt by drowning.
func oldTickAir(s *airState, canBreathe, frozen bool) (hurt bool) {
	if !canBreathe {
		if s.airSupplyTicks -= 1; s.airSupplyTicks <= -20 {
			s.airSupplyTicks = 0
			hurt = true
		}
		s.breathing = false
	} else if !s.breathing && s.airSupplyTicks < s.maxAirSupplyTicks && !frozen {
		s.airSupplyTicks = min(s.airSupplyTicks+5, s.maxAirSupplyTicks)
		s.breathing = s.airSupplyTicks == s.maxAirSupplyTicks
	}
	return hurt
}

// newTickAir is tickAirSupply with the world reads and the hurt as
// arguments, in the order of the methods it calls.
func newTickAir(s *airState, canBreathe, frozen bool) (hurt bool) {
	if !canBreathe {
		if s.airDrain() {
			hurt = true
		}
		s.breathing = false
	} else if s.airRefillable() && !frozen {
		s.airRefill()
	}
	return hurt
}

func TestAirStateMatchesItsOldBodies(t *testing.T) {
	rng := rand.New(rand.NewPCG(9, 10))
	for run := 0; run < 300; run++ {
		var got, want airState
		got.maxAirSupplyTicks = []int{300, 20, 7, 0}[rng.IntN(4)]
		got.airSupplyTicks = rng.IntN(got.maxAirSupplyTicks+1) - rng.IntN(25)
		got.breathing = rng.IntN(2) == 0
		want = got
		for step := 0; step < 120; step++ {
			switch rng.IntN(5) {
			case 0:
				d := time.Duration(rng.IntN(20000)-2000) * time.Millisecond
				got.airSetLeft(d)
				want.airSupplyTicks = int(d.Milliseconds() / 50)
			case 1:
				d := time.Duration(rng.IntN(20000)) * time.Millisecond
				got.airSetMax(d)
				want.maxAirSupplyTicks = int(d.Milliseconds() / 50)
			default:
				breathe, frozen := rng.IntN(3) != 0, rng.IntN(4) == 0
				if g, w := newTickAir(&got, breathe, frozen), oldTickAir(&want, breathe, frozen); g != w {
					t.Fatalf("step %d: hurt = %v, want %v", step, g, w)
				}
			}
			if got != want {
				t.Fatalf("step %d: state\n got %+v\nwant %+v", step, got, want)
			}
			if g, w := got.airLeft(), time.Duration(want.airSupplyTicks)*time.Second/20; g != w {
				t.Fatalf("step %d: airLeft = %v, want %v", step, g, w)
			}
			if g, w := got.airMax(), time.Duration(want.maxAirSupplyTicks)*time.Second/20; g != w {
				t.Fatalf("step %d: airMax = %v, want %v", step, g, w)
			}
		}
	}
}

// The two predicates are the arithmetic of canBreathe and airFrozen over
// every combination of what they read.
func TestAirPredicatesTruthTable(t *testing.T) {
	for m := 0; m < 16; m++ {
		canTakeDamage, wb, cp, naut := m&1 != 0, m&2 != 0, m&4 != 0, m&8 != 0
		if got, want := airNeedless(canTakeDamage, wb, cp, naut), !canTakeDamage || wb || cp || naut; got != want {
			t.Errorf("airNeedless(%v, %v, %v, %v) = %v, want %v", canTakeDamage, wb, cp, naut, got, want)
		}
		if got, want := airHeldByNautilus(naut, wb, cp), naut && !wb && !cp; got != want {
			t.Errorf("airHeldByNautilus(%v, %v, %v) = %v, want %v", naut, wb, cp, got, want)
		}
	}
}

type countHurtHandler struct {
	NopHandler
	hurts *int
}

func (h countHurtHandler) HandleHurt(_ *Context, _ *float64, _ bool, _ *time.Duration, src world.DamageSource) {
	if _, ok := src.(entity.DrowningDamageSource); ok {
		*h.hurts++
	}
}

// A survival player held under water ticks through tickAirSupply as the
// old body did: the supply and the drowning hits, tick by tick.
func TestTickAirSupplyUnderwaterMatchesTheOldBody(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer func() { _ = w.Close() }()
	<-w.Exec(func(tx *world.Tx) {
		for y := 1; y <= 4; y++ {
			tx.SetBlock(cube.Pos{0, y, 0}, block.Water{Depth: 8, Still: true}, nil)
		}
		pos := mgl64.Vec3{0.5, 1, 0.5}
		p := tx.AddEntity(world.EntitySpawnOpts{Position: pos}.New(Type, Config{Position: pos, GameMode: world.GameModeSurvival})).(*Player)
		hurts := 0
		p.Handle(countHurtHandler{hurts: &hurts})
		p.SetAirSupply(3 * time.Second)

		want := airState{airSupplyTicks: p.airSupplyTicks, maxAirSupplyTicks: p.maxAirSupplyTicks, breathing: p.breathing}
		wantHurts := 0
		for tick := 0; tick < 200; tick++ {
			if oldTickAir(&want, p.canBreathe(), p.airFrozen()) {
				wantHurts++
			}
			p.tickAirSupply()
			// The immunity after a hit lasts half a second of the clock, so
			// the hit 20 ticks after the first falls inside it in a test
			// that ticks at once: only whether there was a first is compared.
			if p.airSupplyTicks != want.airSupplyTicks || p.breathing != want.breathing || (hurts > 0) != (wantHurts > 0) {
				t.Fatalf("tick %d: air %d breathing %v hurts %d, want air %d breathing %v hurts %d",
					tick, p.airSupplyTicks, p.breathing, hurts, want.airSupplyTicks, want.breathing, wantHurts)
			}
		}
		if hurts == 0 {
			t.Fatal("the player never drowned")
		}
	})
}
