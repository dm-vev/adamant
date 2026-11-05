package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// NewEyeOfEnder creates a thrown eye of ender travelling towards target,
// following Java EyeOfEnder.signalTo.
func NewEyeOfEnder(tx *world.Tx, opts world.EntitySpawnOpts, target mgl64.Vec3) *world.EntityHandle {
	delta := target.Sub(opts.Position)
	if horizontal := math.Hypot(delta[0], delta[2]); horizontal > 12 {
		target = opts.Position.Add(mgl64.Vec3{delta[0] / horizontal * 12, 8, delta[2] / horizontal * 12})
	}
	return opts.New(EyeOfEnderType, eyeOfEnderConfig{target: target, survive: randInt(tx.RegionRandom(), 5) > 0})
}

type eyeOfEnderConfig struct {
	target  mgl64.Vec3
	survive bool
}

func (c eyeOfEnderConfig) Apply(data *world.EntityData) {
	data.Data = &eyeOfEnderBehaviour{target: c.target, hasTarget: true, survive: c.survive}
}

type eyeOfEnderBehaviour struct {
	target    mgl64.Vec3
	hasTarget bool
	survive   bool
	life      int
}

// Tick advances the signal like Java EyeOfEnder.tick: steer towards the
// target, then after 80 ticks drop the eye (4 in 5) or shatter it.
func (b *eyeOfEnderBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	prev, prevVel := e.data.Pos, e.data.Vel
	next := prev.Add(prevVel)
	if b.hasTarget {
		e.data.Vel = eyeOfEnderMovement(prevVel, next, b.target)
	}
	e.data.Pos = next
	b.life++
	if b.life > 80 {
		tx.PlaySound(next, sound.Custom{Name: "entity.ender_eye.death"})
		_ = e.CloseIn(tx)
		if b.survive {
			tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: next}, item.NewStack(item.EnderEye{}, 1)))
		} else {
			// Java levelEvent 2003 at the block, which its client shows
			// at the middle of the block's floor.
			tx.AddParticle(cube.PosFromVec3(next).Vec3Middle(), particle.EyeOfEnderDeath{})
		}
		return nil
	}
	return &Movement{v: tx.Viewers(prev), releaseTx: tx, e: e,
		pos: next, vel: e.data.Vel, dpos: next.Sub(prev), dvel: e.data.Vel.Sub(prevVel), rot: e.data.Rot}
}

func eyeOfEnderMovement(old, pos, target mgl64.Vec3) mgl64.Vec3 {
	horizontal := mgl64.Vec3{target[0] - pos[0], 0, target[2] - pos[2]}
	length := horizontal.Len()
	speed := math.Hypot(old[0], old[2])
	speed += .0025 * (length - speed)
	movementY := old[1]
	if length < 1 {
		speed *= .8
		movementY *= .8
	}
	wantedY := -1.0
	if pos[1]-old[1] < target[1] {
		wantedY = 1
	}
	var result mgl64.Vec3
	if length != 0 {
		result = horizontal.Mul(speed / length)
	}
	result[1] = movementY + (wantedY-movementY)*.015
	return result
}

// EyeOfEnder is the thrown eye of ender signal entity.
type EyeOfEnder struct{ *Ent }

// Tick moves the eye and resolves its end of life.
func (e *EyeOfEnder) Tick(tx *world.Tx, _ int64) {
	e.bindTx(tx)
	if m := e.Behaviour().Tick(e.Ent, tx); m != nil {
		m.Send()
	}
}

// EyeOfEnderType is the entity type for the travelling eye signal.
var EyeOfEnderType eyeOfEnderType

type eyeOfEnderType struct{}

func (eyeOfEnderType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		// A reloaded signal has no target: like Java it flies on as it
		// was moving, and shatters.
		d.Data = &eyeOfEnderBehaviour{}
	}
	return &EyeOfEnder{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (eyeOfEnderType) EncodeEntity() string { return "minecraft:eye_of_ender_signal" }
func (eyeOfEnderType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.125, 0, -0.125, 0.125, 0.25, 0.125)
}

// The signal is transient; Java does not persist a target, so reloaded eyes
// finish their remaining flight in place.
func (eyeOfEnderType) DecodeNBT(map[string]any, *world.EntityData) {}
func (eyeOfEnderType) EncodeNBT(*world.EntityData) map[string]any  { return nil }
