package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// BatType is the source minecraft:bat ambient species.
var BatType batType

type batType struct{}

func (batType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		batBehaviourConfig{}.Apply(d)
	}
	return &Bat{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (batType) EncodeEntity() string        { return "minecraft:bat" }
func (batType) BBox(world.Entity) cube.BBox { return cube.Box(-.25, 0, -.25, .25, .9, .25) }
func (batType) DecodeNBT(m map[string]any, d *world.EntityData) {
	c := batBehaviourConfig{}
	s := ReadNativeMobJavaState(m)
	if s == nil {
		s = m
	}
	// Java keeps whether a bat rests in BatFlags.
	c.Resting = nbtconv.Bool(s, "Resting") || nbtconv.Uint8(s, "BatFlags")&1 != 0
	if _, ok := s["Health"]; ok {
		c.Health = float64(nbtconv.Float32(s, "Health"))
		c.healthSet = true
	} else if h, ok := nativeMobAttribute(m, "minecraft:health"); ok {
		c.Health, c.healthSet = h.Current, true
	}
	if _, ok := s["MaxHealth"]; ok {
		c.MaxHealth = float64(nbtconv.Float32(s, "MaxHealth"))
	} else if h, ok := nativeMobAttribute(m, "minecraft:health"); ok {
		c.MaxHealth = h.Base
	}
	if c.MaxHealth <= 0 {
		c.MaxHealth = 6
	}
	if c.Health <= 0 {
		c.Health = c.MaxHealth
	}
	c.healthSet = true
	c.Variant = int(nbtconv.Int32(m, "Variant"))
	c.variantSet = true
	c.Apply(d)
}
func (batType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*batBehaviour)
	h := b.health
	attrs := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{{Name: "minecraft:health", Base: h.MaxHealth(), Current: h.Health(), DefaultMin: 0, DefaultMax: h.MaxHealth(), Min: 0, Max: h.MaxHealth()}, {Name: "minecraft:movement", Base: .6, Current: .6, DefaultMin: 0, DefaultMax: .6, Min: 0, Max: .6}})
	m := MergeNativeMobNBT(d.NativeNBT, map[string]any{"Attributes": attrs, "Variant": int32(b.variant)})
	return MergeNativeMobJavaState(m, map[string]any{"Health": float32(h.Health()), "MaxHealth": float32(h.MaxHealth()), "Resting": byte(batBool(b.resting)), "BatFlags": byte(batBool(b.resting))})
}

type batBehaviourConfig struct {
	Health, MaxHealth     float64
	Resting               bool
	Variant               int
	healthSet, variantSet bool
}

func (c batBehaviourConfig) Apply(d *world.EntityData) { d.Data = c.New() }
func (c batBehaviourConfig) New() *batBehaviour {
	if c.MaxHealth <= 0 {
		c.MaxHealth = 6
	}
	if !c.healthSet && c.Health <= 0 {
		c.Health = c.MaxHealth
	}
	return &batBehaviour{health: NewHealthManager(c.Health, c.MaxHealth), effects: NewEffectManager(), resting: c.Resting, variant: c.Variant, speed: .6}
}

type batBehaviour struct {
	health        *HealthManager
	effects       *EffectManager
	ai            MobAI
	resting, dead bool
	variant       int
	speed         float64
	// targetPosition is the block the bat flies towards.
	targetPosition *cube.Pos
}
type Bat struct{ *Ent }

// NewBat creates a bat, which hangs resting until it ticks, as the
// constructor of Java's Bat leaves it.
func NewBat(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(BatType, batBehaviourConfig{Resting: true})
}
func (*Bat) SpeciesType() world.EntityType { return BatType }
func (b *Bat) state() *batBehaviour        { return b.Behaviour().(*batBehaviour) }
func (b *Bat) Tick(tx *world.Tx, n int64)  { b.state().effects.Tick(b, tx); b.Ent.Tick(tx, n) }

func (b *batBehaviour) mobAI() *MobAI { return &b.ai }

// Tick runs a tick of the bat on its MobAI. A bat has no goals: its
// customServerAiStep flies it about and hangs it from blocks.
func (b *batBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	b.ai.EyeHeight, b.ai.MovementSpeed = .45, b.speed
	return b.ai.Tick(e, &Bat{Ent: e}, nil)
}

// setResting changes whether the bat hangs resting and shows viewers.
func (b *batBehaviour) setResting(e *Ent, resting bool) {
	if b.resting != resting {
		b.resting = resting
		e.updateState()
	}
}

// batTakeOff plays the sound of a bat taking off, as level event 1025.
func batTakeOff(m *Mob) {
	r := m.Random()
	m.tx.PlaySound(m.BlockPos().Vec3(), sound.Custom{Name: "entity.bat.takeoff", Volume: .05, Pitch: float64((r.NextFloat()-r.NextFloat())*.2 + 1)})
}

// customServerAiStep ports Bat.customServerAiStep: a resting bat hangs from
// the solid block above it, now and then turning its head, until a player
// comes within four blocks or the block is gone. A flying bat steers
// towards a random air block nearby, picking another when it gets close,
// at times or when that block fills, and now and then hangs from a solid
// block it flies under.
func (b *batBehaviour) customServerAiStep(m *Mob) {
	pos := m.BlockPos()
	above := pos.Side(cube.FaceUp)
	r := m.Random()
	if b.resting {
		if !world.RedstoneFullPowerConductor(above, m.tx.Block(above), m.tx) {
			b.setResting(m.Ent, false)
			batTakeOff(m)
			return
		}
		if r.NextInt(200) == 0 {
			m.AI.headYaw = float64(r.NextInt(360))
		}
		c := forNonCombat()
		c.rangeLimit = 4
		if nearestPlayer(m, c) != nil {
			b.setResting(m.Ent, false)
			batTakeOff(m)
		}
		return
	}
	if t := b.targetPosition; t != nil && (!isAir(m.tx.Block(*t)) || t[1] <= m.tx.Range()[0]) {
		b.targetPosition = nil
	}
	if t := b.targetPosition; t == nil || r.NextInt(30) == 0 || t.Vec3Centre().Sub(m.data.Pos).LenSqr() < 4 {
		x := m.data.Pos[0] + float64(r.NextInt(7)) - float64(r.NextInt(7))
		y := m.data.Pos[1] + float64(r.NextInt(6)) - 2
		z := m.data.Pos[2] + float64(r.NextInt(7)) - float64(r.NextInt(7))
		t := cube.PosFromVec3(mgl64.Vec3{x, y, z})
		b.targetPosition = &t
	}
	t := *b.targetPosition
	dx := float64(t[0]) + .5 - m.data.Pos[0]
	dy := float64(t[1]) + .1 - m.data.Pos[1]
	dz := float64(t[2]) + .5 - m.data.Pos[2]
	const tenth, upward = float64(float32(.1)), float64(float32(.7))
	v := m.data.Vel
	v = v.Add(mgl64.Vec3{(signum(dx)*.5 - v[0]) * tenth, (signum(dy)*upward - v[1]) * tenth, (signum(dz)*.5 - v[2]) * tenth})
	m.data.Vel = v
	yaw := float64(float32(math.Atan2(v[2], v[0])*180/math.Pi) - 90)
	m.SetZza(.5)
	m.SetYaw(m.Yaw() + wrapDegrees(yaw-m.Yaw()))
	if r.NextInt(100) == 0 && world.RedstoneFullPowerConductor(above, m.tx.Block(above), m.tx) {
		b.setResting(m.Ent, true)
	}
}

// afterAiStep ports what Bat.tick does after the tick of a Mob: a resting
// bat hangs still from the top of its block, and a flying one sinks and
// rises slowly.
func (b *batBehaviour) afterAiStep(m *Mob) {
	if b.resting {
		m.data.Vel = mgl64.Vec3{}
		m.data.Pos[1] = math.Floor(m.data.Pos[1]) + 1 - .9
		return
	}
	m.data.Vel[1] *= .6
}

// signum ports Math.signum.
func signum(v float64) float64 {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return v
}
func (b *Bat) Health() float64        { return b.state().health.Health() }
func (b *Bat) MaxHealth() float64     { return b.state().health.MaxHealth() }
func (b *Bat) SetMaxHealth(h float64) { b.state().health.SetMaxHealth(h) }
func (b *Bat) Dead() bool             { return b.state().dead || b.Health() <= 0 }
func (b *Bat) Heal(h float64, _ world.HealingSource) float64 {
	if b.Dead() || h <= 0 {
		return 0
	}
	old := b.Health()
	b.state().health.AddHealth(h)
	return b.Health() - old
}

// Hurt ports Bat.hurtServer: a hurt bat stops resting.
func (b *Bat) Hurt(d float64, s world.DamageSource) (float64, bool) {
	st := b.state()
	if b.Dead() || d <= 0 || (s.Fire() && b.hasEffect(effect.FireResistance)) {
		return 0, false
	}
	st.setResting(b.Ent, false)
	d, ok := st.ai.DamageCooldown(b.tx, d)
	if !ok {
		return 0, false
	}
	if r, ok := st.effects.Effect(effect.Resistance); ok {
		d *= effect.Resistance.Multiplier(s, r.Level())
	}
	st.health.AddHealth(-d)
	st.ai.Hurt(s)
	onMobHurt(b.tx, b)
	if b.Dead() {
		st.dead = true
		b.viewAction(DeathAction{})
		dropAllDeathLoot(b.tx, b, s)
		_ = b.CloseIn(b.tx)
	} else {
		b.viewAction(HurtAction{})
	}
	return d, true
}
func (b *Bat) KnockBack(src mgl64.Vec3, f, h float64) {
	d := b.Position().Sub(src)
	if d.Len() != 0 {
		d = d.Normalize().Mul(f)
	}
	d[1] = h
	b.SetVelocity(d)
}
func (b *Bat) AddEffect(e effect.Effect)    { b.state().effects.Add(e, b) }
func (b *Bat) RemoveEffect(t effect.Type)   { b.state().effects.Remove(t, b) }
func (b *Bat) Effects() []effect.Effect     { return b.state().effects.Effects() }
func (b *Bat) Speed() float64               { return b.state().speed }
func (b *Bat) SetSpeed(v float64)           { b.state().speed = v }
func (b *Bat) hasEffect(t effect.Type) bool { _, ok := b.state().effects.Effect(t); return ok }
func (b *Bat) Resting() bool                { return b.state().resting }
func (b *Bat) SetResting(v bool)            { b.state().setResting(b.Ent, v) }
func (b *Bat) viewAction(a world.EntityAction) {
	vs := b.tx.Viewers(b.Position())
	for _, v := range vs {
		v.ViewEntityAction(b, a)
	}
	b.tx.ReleaseViewers(vs)
}

var _ Living = (*Bat)(nil)

func batBool(v bool) byte {
	if v {
		return 1
	}
	return 0
}
