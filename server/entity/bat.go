package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/world"
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
	c.Resting = nbtconv.Bool(s, "Resting")
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
	return MergeNativeMobJavaState(m, map[string]any{"Health": float32(h.Health()), "MaxHealth": float32(h.MaxHealth()), "Resting": byte(batBool(b.resting))})
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
	return &batBehaviour{health: NewHealthManager(c.Health, c.MaxHealth), effects: NewEffectManager(), mc: &MovementComputer{Gravity: .08, Drag: .06, DragBeforeGravity: true}, resting: c.Resting, variant: c.Variant, speed: .6, random: java26.NewLegacyRandom(0), nextMove: 1}
}

type batBehaviour struct {
	health            *HealthManager
	effects           *EffectManager
	mc                *MovementComputer
	resting, dead     bool
	variant, nextMove int
	speed             float64
	random            java26.Random
	target            mgl64.Vec3
	hasTarget         bool
}
type Bat struct{ *Ent }

func NewBat(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(BatType, batBehaviourConfig{})
}
func (*Bat) SpeciesType() world.EntityType { return BatType }
func (b *Bat) state() *batBehaviour        { return b.Behaviour().(*batBehaviour) }
func (b *Bat) Tick(tx *world.Tx, n int64)  { b.state().effects.Tick(b, tx); b.Ent.Tick(tx, n) }
func (b *batBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	p := cube.PosFromVec3(e.Position())
	ceiling := p.Add(cube.Pos{0, 1, 0})
	attached := tx.Block(ceiling).Model().FaceSolid(ceiling, cube.FaceDown, tx)
	timeOfDay := tx.World().Time() % 24000
	night := timeOfDay >= 13000 && timeOfDay < 23000
	if !attached || night || e.data.Vel[1] > .05 {
		b.resting = false
	}
	if attached && !night && e.data.Vel.Len() < .04 {
		b.resting = true
		b.hasTarget = false
	}
	if b.resting {
		b.mc.Gravity = 0
		return b.mc.TickMovement(e, e.data.Pos, mgl64.Vec3{}, e.data.Rot, e.data.Rot, tx)
	}
	b.mc.Gravity = .08
	if !b.hasTarget || b.nextMove <= 0 {
		b.nextMove = 20 + int(b.random.NextInt(31))
		b.target = e.Position().Add(mgl64.Vec3{float64(b.random.NextInt(17) - 8), float64(b.random.NextInt(9) - 4), float64(b.random.NextInt(17) - 8)})
		b.hasTarget = true
	}
	if b.nextMove > 0 {
		b.nextMove--
	}
	d := b.target.Sub(e.Position())
	v := e.Velocity()
	speed := b.speed / 6
	if d.Len() < .5 {
		b.hasTarget = false
		v[0], v[2] = 0, 0
	} else {
		d = d.Normalize()
		v[0], v[1], v[2] = d[0]*speed, d[1]*speed, d[2]*speed
	}
	m := b.mc.TickMovement(e, e.data.Pos, v, e.data.Rot, e.data.Rot, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	return m
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
func (b *Bat) Hurt(d float64, s world.DamageSource) (float64, bool) {
	st := b.state()
	if b.Dead() || d <= 0 || (s.Fire() && b.hasEffect(effect.FireResistance)) {
		return 0, false
	}
	if r, ok := st.effects.Effect(effect.Resistance); ok {
		d *= effect.Resistance.Multiplier(s, r.Level())
	}
	st.health.AddHealth(-d)
	if b.Dead() {
		st.dead = true
		b.viewAction(DeathAction{})
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
func (b *Bat) SetResting(v bool)            { b.state().resting = v; b.state().hasTarget = false }
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
