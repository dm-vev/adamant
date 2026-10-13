package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// frogTongueState is the stage of Java's ShootTongue behaviour.
type frogTongueState uint8

const (
	frogTongueDone frogTongueState = iota
	frogTongueMove
	frogTongueCatch
	frogTongueEat
)

// frogUnreachable is a tongue target the frog found no path to, remembered
// until the expiry tick.
type frogUnreachable struct {
	target *world.EntityHandle
	expiry int64
}

// frogTongue ports the FrogAttackablesSensor and ShootTongue behaviour: a frog
// walks up to a size 1 slime or magma cube within 10 blocks, pulls it in with
// its tongue and eats it.
type frogTongue struct {
	target                      *world.EntityHandle
	state                       frogTongueState
	timer, pathCounter, running int
	scanCooldown                int
	unreachable                 []frogUnreachable
	navigator                   Navigator
}

// frogPrey is an entity a frog can bite.
type frogPrey interface {
	world.Entity
	CubeSize() int
	Dead() bool
	Hurt(float64, world.DamageSource) (float64, bool)
}

// frogEdible ports Frog.canEat: a living slime or magma cube of size 1.
func frogEdible(e world.Entity) bool {
	switch e.H().Type().EncodeEntity() {
	case "minecraft:slime", "minecraft:magma_cube":
	default:
		return false
	}
	prey, ok := e.(frogPrey)
	return ok && prey.CubeSize() == 1 && !prey.Dead()
}

// active reports whether the frog is busy with its tongue.
func (t *frogTongue) active() bool { return t.state != frogTongueDone }

// tick runs the sensor and behaviour for the tick, returning the horizontal
// velocity of the frog while it walks to its prey.
func (t *frogTongue) tick(e *Ent, tx *world.Tx, now int64) (mgl64.Vec3, bool) {
	t.forgetExpired(now)
	if !t.active() {
		if t.scanCooldown--; t.scanCooldown > 0 {
			return mgl64.Vec3{}, false
		}
		t.scanCooldown = 20
		target := t.nearestPrey(e, tx)
		if target == nil {
			return mgl64.Vec3{}, false
		}
		start := cube.PosFromVec3(e.Position())
		if !t.navigator.MoveTo(tx, start, cube.PosFromVec3(target.Position()), frogGroundProfile, navigatorTick(e)) {
			t.markUnreachable(target.H(), now)
			return mgl64.Vec3{}, false
		}
		t.target, t.state, t.pathCounter, t.running = target.H(), frogTongueMove, 10, 0
	}
	target, ok := t.target.Entity(tx)
	if t.running++; !ok || t.running > 100 || !frogEdible(target) && t.state == frogTongueMove {
		t.stop()
		return mgl64.Vec3{}, false
	}
	switch t.state {
	case frogTongueMove:
		if target.Position().Sub(e.Position()).Len() < 1.75 {
			tx.PlaySound(e.Position(), sound.Custom{Name: "mob.frog.tongue", Volume: 2, Pitch: 1})
			if v, ok := target.(interface{ SetVelocity(mgl64.Vec3) }); ok {
				if pull := e.Position().Sub(target.Position()); pull.Len() > 0 {
					v.SetVelocity(pull.Normalize().Mul(0.75))
				}
			}
			t.timer, t.state = 0, frogTongueCatch
			return mgl64.Vec3{}, false
		}
		if t.pathCounter <= 0 {
			t.navigator.MoveTo(tx, cube.PosFromVec3(e.Position()), cube.PosFromVec3(target.Position()), frogGroundProfile, navigatorTick(e))
			t.pathCounter = 10
		} else {
			t.pathCounter--
		}
		waypoint, ok := t.navigator.Tick(e.Position(), navigatorTick(e), 0.5)
		if !ok {
			waypoint = target.Position()
		}
		d := waypoint.Sub(e.Position())
		d[1] = 0
		if d.Len() < 0.05 {
			return mgl64.Vec3{}, true
		}
		return d.Normalize().Mul(0.15), true
	case frogTongueCatch:
		if t.timer++; t.timer > 6 {
			t.state = frogTongueEat
			t.eat(e, tx, target)
		}
	case frogTongueEat:
		if t.timer >= 10 {
			t.stop()
		} else {
			t.timer++
		}
	}
	return mgl64.Vec3{}, true
}

// eat ports ShootTongue.eatEntity: the frog bites its prey for its 10 attack
// damage, and prey it kills disappears at once.
func (t *frogTongue) eat(e *Ent, tx *world.Tx, target world.Entity) {
	tx.PlaySound(e.Position(), sound.Custom{Name: "mob.frog.eat", Volume: 2, Pitch: 1})
	prey, ok := target.(frogPrey)
	if !ok || prey.Dead() {
		return
	}
	prey.Hurt(10, AttackDamageSource{Attacker: e.living()})
	if prey.Dead() {
		_ = target.Close()
	}
}

// stop ends the behaviour.
func (t *frogTongue) stop() {
	t.target, t.state, t.timer, t.running = nil, frogTongueDone, 0, 0
	t.navigator = Navigator{}
}

// nearestPrey ports FrogAttackablesSensor: the nearest visible edible mob
// within 10 blocks that is not remembered as unreachable.
func (t *frogTongue) nearestPrey(e *Ent, tx *world.Tx) world.Entity {
	var best world.Entity
	bestDist := 10.0
	eye := EyePosition(e)
	for candidate := range tx.EntitiesWithin(cube.Box(-10, -10, -10, 10, 10, 10).Translate(e.Position())) {
		if !frogEdible(candidate) || t.isUnreachable(candidate.H()) {
			continue
		}
		if dist := candidate.Position().Sub(e.Position()).Len(); dist < bestDist && dragonLineOfSight(tx, eye, EyePosition(candidate)) {
			best, bestDist = candidate, dist
		}
	}
	return best
}

// markUnreachable ports addUnreachableTargetToMemory: up to five targets are
// remembered for 100 ticks.
func (t *frogTongue) markUnreachable(h *world.EntityHandle, now int64) {
	if t.isUnreachable(h) {
		return
	}
	if len(t.unreachable) == 5 {
		t.unreachable = t.unreachable[1:]
	}
	t.unreachable = append(t.unreachable, frogUnreachable{target: h})
	for i := range t.unreachable {
		t.unreachable[i].expiry = now + 100
	}
}

func (t *frogTongue) isUnreachable(h *world.EntityHandle) bool {
	for _, u := range t.unreachable {
		if u.target == h {
			return true
		}
	}
	return false
}

func (t *frogTongue) forgetExpired(now int64) {
	if len(t.unreachable) > 0 && now >= t.unreachable[0].expiry {
		t.unreachable = nil
	}
}

// frogGroundProfile is the walking route of a frog.
var frogGroundProfile = GroundProfile{MaxVisitedNodes: 160, StepHeight: 1, MaxFall: 3, Width: 0.5, Height: 0.5, FollowRange: 16, AllowWater: true}
