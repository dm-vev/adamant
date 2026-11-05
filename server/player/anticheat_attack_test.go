package player

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// kinds returns the kinds of the violations of c that h recorded.
func (h *violationRecorder) kinds(c anticheat.Check) map[string]int {
	_, k := (traceResult{violations: h.violations}).count(c)
	return k
}

// TestAttackDetectsKillaura checks that three targets attacked in a tick
// are a violation of Attack, logged by default, and refused with cancel.
func TestAttackDetectsKillaura(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		conf := anticheat.Default()
		if cancel {
			conf = withPolicy(anticheat.Attack, func(p *anticheat.Policy) { p.Action = anticheat.Cancel })
		}
		withAttacker(t, conf, 2.5, func(tx *world.Tx, p *Player, cow world.Entity, h *violationRecorder) {
			cows := []world.Entity{cow}
			for _, z := range []float64{1.5, -0.5} {
				cows = append(cows, tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{2.5, 100, z}})))
			}
			p.data.Rot = cube.Rotation{-90, 0}
			hits := 0
			for _, c := range cows {
				if p.AttackEntity(c) {
					hits++
				}
			}
			if k := h.kinds(anticheat.Attack); k["targets"] != 2 {
				t.Fatalf("cancel %v: violations %+v, want two of targets", cancel, h.violations)
			}
			if want := map[bool]int{false: 3, true: 1}[cancel]; hits != want {
				t.Fatalf("cancel %v: %d hits, want %d", cancel, hits, want)
			}
		})
	}
}

// TestAttackRate checks that 40 attacks a second are a violation of
// Attack, 20 are not.
func TestAttackRate(t *testing.T) {
	for _, tc := range []struct {
		interval time.Duration
		want     bool
	}{{50 * time.Millisecond, false}, {25 * time.Millisecond, true}} {
		withAttacker(t, anticheat.Default(), 2.5, func(tx *world.Tx, p *Player, cow world.Entity, h *violationRecorder) {
			wall := time.Unix(4_000_000, 0)
			p.anticheat().now = func() time.Time { return wall }
			p.data.Rot = cube.Rotation{-90, 0}
			for range 40 {
				p.AttackEntity(cow)
				p.Tick(tx, p.tickCount)
				wall = wall.Add(tc.interval)
			}
			if got := h.kinds(anticheat.Attack)["rate"] > 0; got != tc.want {
				t.Fatalf("an attack every %v: violations %+v", tc.interval, h.violations)
			}
		})
	}
}

// TestAttackAim checks that an attack the look of the player is turned
// away from is a violation of AttackAim, that the aim of the client counts
// for its look, and that a target behind a wall is one with LineOfSight.
func TestAttackAim(t *testing.T) {
	withAttacker(t, anticheat.Default(), 3.5, func(tx *world.Tx, p *Player, cow world.Entity, h *violationRecorder) {
		// East, at the cow: no violation.
		p.data.Rot = cube.Rotation{-90, 0}
		p.AttackEntity(cow)
		if len(h.violations) != 0 {
			t.Fatalf("an attack looking at the target: %+v", h.violations)
		}
		// West, away from it.
		p.Tick(tx, p.tickCount)
		p.data.Rot = cube.Rotation{90, 0}
		p.AttackEntity(cow)
		if h.kinds(anticheat.AttackAim)["aim"] != 1 {
			t.Fatalf("an attack looking away: %+v", h.violations)
		}
		// The client aims at it by touch while it looks away.
		p.Tick(tx, p.tickCount)
		p.anticheat().attack.aim = mgl64.Vec3{1, 0, 0}
		p.AttackEntity(cow)
		if h.kinds(anticheat.AttackAim)["aim"] != 1 {
			t.Fatalf("an attack aimed by touch: %+v", h.violations)
		}
		// A wall between.
		p.Tick(tx, p.tickCount)
		p.anticheat().attack.aim = mgl64.Vec3{}
		p.data.Rot = cube.Rotation{-90, 0}
		tx.SetBlock(cube.Pos{2, 101, 0}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{2, 100, 0}, block.Stone{}, nil)
		p.AttackEntity(cow)
		if h.kinds(anticheat.AttackAim)["sight"] != 1 {
			t.Fatalf("an attack through a wall: %+v", h.violations)
		}
	})
}

// TestAttackOff checks that with Attack and AttackAim off, nothing is
// judged.
func TestAttackOff(t *testing.T) {
	conf := withPolicy(anticheat.Attack, func(p *anticheat.Policy) { p.Enabled = false })
	p := conf.Policy(anticheat.AttackAim)
	p.Enabled = false
	conf.SetPolicy(anticheat.AttackAim, p)
	withAttacker(t, conf, 2.5, func(tx *world.Tx, pl *Player, cow world.Entity, h *violationRecorder) {
		pl.data.Rot = cube.Rotation{90, 0}
		for range 30 {
			pl.AttackEntity(cow)
		}
		if len(h.violations) != 0 {
			t.Fatalf("violations %+v", h.violations)
		}
	})
}

// aimAt returns the unit direction from the eyes of p to the point passed.
func aimAt(p *Player, to mgl64.Vec3) mgl64.Vec3 {
	return to.Sub(p.Position().Add(mgl64.Vec3{0, p.EyeHeight(), 0})).Normalize()
}

// TestAttackAimBedrockClients checks the aim of Bedrock clients: a tap on a
// touch screen and the camera of third person, whose rays pass through the
// hitbox of a target close by, where the angle from the look to the nearest
// point of its box is wide; a client without a ray, looking away, is still
// caught, and so is one whose rays point away from a target behind it.
func TestAttackAimBedrockClients(t *testing.T) {
	// The cow is 1.5 blocks to the side and 2 ahead of the look: its
	// nearest point is at a cosine of 0.82 from the look.
	for _, tc := range []struct {
		name  string
		aim   func(p *Player, cow world.Entity)
		cow   mgl64.Vec3
		wantV bool
	}{
		{"only the look, close and aside", func(*Player, world.Entity) {}, mgl64.Vec3{2.5, 100, 2}, true},
		{"a tap on the cow", func(p *Player, cow world.Entity) {
			p.anticheat().attack.aim = aimAt(p, cow.Position().Add(mgl64.Vec3{0, 0.7, 0}))
		}, mgl64.Vec3{2.5, 100, 2}, false},
		{"a tap on the edge of the cow", func(p *Player, cow world.Entity) {
			p.anticheat().attack.aim = aimAt(p, cow.Position().Add(mgl64.Vec3{-0.4, 1.3, -0.4}))
		}, mgl64.Vec3{2.5, 100, 2}, false},
		{"a tap beside the cow, within the margin", func(p *Player, cow world.Entity) {
			p.anticheat().attack.aim = aimAt(p, cow.Position().Add(mgl64.Vec3{0, 0.7, -0.45 - 0.3}))
		}, mgl64.Vec3{2.5, 100, 2}, false},
		{"a tap beside the cow, past the margin", func(p *Player, cow world.Entity) {
			p.anticheat().attack.aim = aimAt(p, cow.Position().Add(mgl64.Vec3{0, 0.7, -0.45 - 1.2}))
		}, mgl64.Vec3{2.5, 100, 2}, true},
		{"the camera of third person on the cow", func(p *Player, cow world.Entity) {
			p.anticheat().attack.camera = aimAt(p, cow.Position().Add(mgl64.Vec3{0, 0.7, 0}))
		}, mgl64.Vec3{2.5, 100, 2}, false},
		{"the camera looking the way of the look", func(p *Player, _ world.Entity) {
			p.anticheat().attack.camera = mgl64.Vec3{1, 0, 0}
		}, mgl64.Vec3{2.5, 100, 2}, true},
		{"a cow behind, the look and the camera ahead", func(p *Player, _ world.Entity) {
			p.anticheat().attack.camera = mgl64.Vec3{1, 0, 0}
		}, mgl64.Vec3{-3.5, 100, 0.5}, true},
		{"a cow behind, a tap ahead", func(p *Player, _ world.Entity) {
			p.anticheat().attack.aim = mgl64.Vec3{1, 0, 0.2}.Normalize()
		}, mgl64.Vec3{-3.5, 100, 0.5}, true},
		{"a cow behind, a tap at it", func(p *Player, cow world.Entity) {
			p.anticheat().attack.aim = aimAt(p, cow.Position().Add(mgl64.Vec3{0, 0.7, 0}))
		}, mgl64.Vec3{-3.5, 100, 0.5}, false},
	} {
		withAttacker(t, anticheat.Default(), 3.5, func(tx *world.Tx, p *Player, _ world.Entity, h *violationRecorder) {
			cow := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: tc.cow}))
			p.data.Rot = cube.Rotation{-90, 0}
			tc.aim(p, cow)
			p.AttackEntity(cow)
			if got := h.kinds(anticheat.AttackAim)["aim"] > 0; got != tc.wantV {
				t.Errorf("%s: violation %v, want %v: %+v", tc.name, got, tc.wantV, h.violations)
			}
		})
	}
}
