package player

import (
	"fmt"
	"slices"

	"github.com/df-mc/dragonfly/server/block/cube"
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
	// its look, and camera the direction its camera looks in, zero if it
	// did not say.
	aim, camera mgl64.Vec3
}

// refuseAttack judges an attack on e the client of the player asks for,
// which ac checks: Attack bounds the attacks by a token bucket of PerSecond
// (20) and Burst (10), and the living targets of a tick by TargetsPerTick
// (1); AttackAim holds the aim of the client, its interaction ray or its
// look, to the direction of the nearest point of the box of e, a cosine of
// MinCos (0.85) at least, beyond a block, and, with LineOfSight, the eyes
// to a sight of the centre of e past full blocks. It reports whether the
// attack is refused. The role of the player may refuse it first, as
// roleRefusesAttack says.
func (p *Player) refuseAttack(ac *acState, e world.Entity) bool {
	if p.roleRefusesAttack(e) {
		return true
	}
	if ac == nil {
		return false
	}
	if p.checks(ac, anticheat.Attack) {
		conf := ac.conf.Attack
		a := &ac.attack
		if !a.bucket.Take(p.inputArrival(ac), conf.PerSecond, conf.Burst) {
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
		if cos, aimed := p.aimedAt(ac, eyes, box); !aimed {
			if p.violate(ac, anticheat.AttackAim, "aim", ac.conf.AttackAim.MinCos-cos, fmt.Sprintf("cos %.2f < %.2f", cos, ac.conf.AttackAim.MinCos)) != anticheat.Log {
				return true
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

// aimedAt reports whether an attack of the client on the box passed, from
// the eyes passed, is aimed at it, and the best cosine of the angle between
// an aim and the direction to the nearest point of the box. The client may
// aim along the look of the player; along the ray of its interaction, as a
// tap on a touch screen or the crosshair of third person gives; or along
// the look of its camera. An aim is true to the target if its ray passes
// through the box, grown by AttackAim.HitboxMargin, or if its angle to the
// nearest point of the box has a cosine of AttackAim.MinCos at least. A
// target within a block is always aimed at. An attack on a target none of
// the rays comes near, as one behind the player, is not.
func (p *Player) aimedAt(ac *acState, eyes mgl64.Vec3, box cube.BBox) (cos float64, aimed bool) {
	var nearest mgl64.Vec3
	for i := range 3 {
		nearest[i] = max(box.Min()[i], min(eyes[i], box.Max()[i]))
	}
	to := nearest.Sub(eyes)
	dist := to.Len()
	if dist <= 1 {
		return 1, true
	}
	to = to.Mul(1 / dist)
	conf := ac.conf.AttackAim
	cos = -1
	rays := [...]mgl64.Vec3{p.Rotation().Vec3(), ac.attack.aim, ac.attack.camera}
	for _, ray := range rays {
		if ray.Len() < 1e-6 {
			continue
		}
		cos = max(cos, ray.Normalize().Dot(to))
		if cos >= conf.MinCos || anticheat.RayHitsBox(eyes, ray, box, conf.HitboxMargin, dist+conf.HitboxMargin+box.Min().Sub(box.Max()).Len()) {
			return cos, true
		}
	}
	return cos, false
}
