package entity

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// llamaState is the state of Llama's caravan and spitting.
type llamaState struct {
	// caravanHead is the llama this one follows and caravanTail the llama
	// following this one, as Llama keeps them.
	caravanHead, caravanTail *world.EntityHandle
	// didSpit is set once the llama spat, which ends LlamaHurtByTargetGoal.
	didSpit bool
}

// leashed reports whether the llama is on a lead.
func (m *Mount) leashed() bool { return m.HolderLeashID() != 0 }

// leashedToKnot reports whether the llama is tied to a fence.
func (m *Mount) leashedToKnot(tx *world.Tx) bool {
	id := m.HolderLeashID()
	if id == 0 {
		return false
	}
	for e := range tx.Entities() {
		if e.H().UniqueID() == id {
			return e.H().Type() == LeashKnotType
		}
	}
	return false
}

// llamaMount returns the llama behind a caravan link, or nil if it is gone.
func llamaMount(tx *world.Tx, h *world.EntityHandle) *Mount {
	if h == nil {
		return nil
	}
	e, ok := h.Entity(tx)
	if !ok {
		return nil
	}
	m := mountFromEntity(e)
	if m == nil || !m.b().llama() || m.Dead() {
		return nil
	}
	return m
}

// leaveCaravan ports Llama.leaveCaravan.
func (b *MountBehaviour) leaveCaravan(tx *world.Tx) {
	s := &b.llamaState
	if head := llamaMount(tx, s.caravanHead); head != nil {
		head.b().llamaState.caravanTail = nil
	}
	s.caravanHead = nil
}

// caravanLeashed ports LlamaFollowCaravanGoal.firstIsLeashed: whether a
// leashed llama leads the caravan of m within eight links.
func caravanLeashed(tx *world.Tx, m *Mount, depth int) bool {
	for ; depth <= 8; depth++ {
		head := llamaMount(tx, m.b().llamaState.caravanHead)
		if head == nil {
			return false
		}
		if head.leashed() {
			return true
		}
		m = head
	}
	return false
}

// combatTargetable ports TargetingConditions.forCombat's rule on targets:
// living and, for players, in a game mode that takes damage.
func combatTargetable(e world.Entity) bool {
	if l, ok := e.(Living); ok && l.Dead() {
		return false
	}
	g, ok := e.(interface{ GameMode() world.GameMode })
	return !ok || g.GameMode().AllowsTakingDamage()
}

// spit ports Llama.spit: a llama spit flies from in front of the llama at a
// third of the target's height with an uncertainty of 10.
func (m *Mount) spit(tx *world.Tx, target world.Entity) {
	yaw := m.Rotation().Yaw() * math.Pi / 180
	width := m.H().Type().BBox(m).Width()
	origin := mgl64.Vec3{m.Position()[0] - (width+1)*.5*math.Sin(yaw), EyePosition(m)[1] - .1, m.Position()[2] + (width+1)*.5*math.Cos(yaw)}
	box := target.H().Type().BBox(target)
	d := mgl64.Vec3{target.Position()[0] - m.Position()[0], target.Position()[1] + box.Height()/3 - origin[1], target.Position()[2] - m.Position()[2]}
	d[1] += math.Hypot(d[0], d[2]) * .2
	if d.Len() == 0 {
		return
	}
	tx.AddEntity(NewLlamaSpit(world.EntitySpawnOpts{Position: origin, Velocity: shootVelocity(d, 1.5, 10)}, m))
	tx.PlaySound(m.Position(), sound.NativeEvent{SoundType: "spit", EntityType: m.H().Type().EncodeEntity(), ExtraData: -1})
	m.b().llamaState.didSpit = true
}

// shootVelocity ports Projectile.getMovementToShoot: the direction, spread
// by a triangular distribution growing with the uncertainty, at the power
// passed.
func shootVelocity(d mgl64.Vec3, power, uncertainty float64) mgl64.Vec3 {
	spread := .0172275 * uncertainty
	triangle := func() float64 { return spread * (rand.Float64() - rand.Float64()) }
	return d.Normalize().Add(mgl64.Vec3{triangle(), triangle(), triangle()}).Mul(power)
}
