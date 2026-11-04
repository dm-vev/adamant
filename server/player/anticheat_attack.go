package player

import (
	"fmt"
	"slices"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// attackState is what Attack and AttackAim keep of the attacks of a client.
type attackState struct {
	bucket anticheat.Bucket
	// tick is the tick of the targets: the living entities attacked in it.
	tick    int64
	targets []*world.EntityHandle
	// aim is the direction the client last said it interacts in, zero for
	// its look.
	aim mgl64.Vec3
}

// refuseAttack judges an attack on e the client of the player asks for,
// which ac checks: Attack bounds the attacks by a token bucket of PerSecond
// (20) and Burst (10), and the living targets of a tick by TargetsPerTick
// (1); AttackAim holds the aim of the client, its interaction ray or its
// look, to the direction of the nearest point of the box of e, a cosine of
// MinCos (0.85) at least, beyond a block, and, with LineOfSight, the eyes
// to a sight of the centre of e past full blocks. It reports whether the
// attack is refused.
func (p *Player) refuseAttack(ac *acState, e world.Entity) bool {
	if ac == nil {
		return false
	}
	if p.checks(ac, anticheat.Attack) {
		conf := ac.conf.Attack
		a := &ac.attack
		if !a.bucket.Take(ac.wallNow(), conf.PerSecond, conf.Burst) {
			if p.violate(ac, anticheat.Attack, "rate", 1, fmt.Sprintf("beyond %v a second", conf.PerSecond)) != anticheat.Log {
				return true
			}
		}
		if _, living := e.(entity.Living); living {
			if a.tick != p.tickCount {
				a.tick, a.targets = p.tickCount, a.targets[:0]
			}
			if !slices.Contains(a.targets, e.H()) {
				a.targets = append(a.targets, e.H())
			}
			if n := len(a.targets); n > conf.TargetsPerTick {
				if p.violate(ac, anticheat.Attack, "targets", float64(n-conf.TargetsPerTick), fmt.Sprintf("%d targets in a tick", n)) != anticheat.Log {
					return true
				}
			}
		}
	}
	if p.checks(ac, anticheat.AttackAim) {
		eyes := entity.EyePosition(p)
		box := e.H().Type().BBox(e).Translate(e.Position())
		var nearest mgl64.Vec3
		for i := range 3 {
			nearest[i] = max(box.Min()[i], min(eyes[i], box.Max()[i]))
		}
		to := nearest.Sub(eyes)
		aim := ac.attack.aim
		if aim.Len() < 1e-6 {
			aim = p.Rotation().Vec3()
		}
		if dist := to.Len(); dist > 1 {
			if c := aim.Normalize().Dot(to.Mul(1 / dist)); c < ac.conf.AttackAim.MinCos {
				if p.violate(ac, anticheat.AttackAim, "aim", ac.conf.AttackAim.MinCos-c, fmt.Sprintf("cos %.2f < %.2f", c, ac.conf.AttackAim.MinCos)) != anticheat.Log {
					return true
				}
			}
		}
		if ac.conf.AttackAim.LineOfSight {
			centre := box.Min().Add(box.Max()).Mul(0.5)
			if blocked, at := p.sightBlocked(eyes, centre, blockOf(centre)); blocked {
				if p.violate(ac, anticheat.AttackAim, "sight", 1, fmt.Sprintf("%v hides the target", at)) != anticheat.Log {
					return true
				}
			}
		}
	}
	return false
}
