package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// PolarBearType is the registered Java polar bear species.
var PolarBearType polarBearType

type polarBearType struct{}
type PolarBear struct{ *Ent }
type PolarBearConfig struct {
	Health, MaxHealth float64
	Age               int
	Standing          bool
	AttackCooldown    int
}
type polarBearState struct {
	health         *HealthManager
	effects        *EffectManager
	age, cooldown  int
	standing, dead bool
	speed          float64
	ai             MobAI
	panic          *PanicGoal
	neutralAnger
	warningSoundTicks int
	shownStanding     bool
}

func NewPolarBear(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(PolarBearType, PolarBearConfig{})
}
func (polarBearType) EncodeEntity() string { return "minecraft:polar_bear" }
func (polarBearType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		PolarBearConfig{}.Apply(d)
	}
	return &PolarBear{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (polarBearType) BBox(e world.Entity) cube.BBox {
	if p, ok := e.(*PolarBear); ok && p.b().age < 0 {
		return cube.Box(-.35, 0, -.35, .35, 1.4, .35)
	}
	return cube.Box(-.7, 0, -.7, .7, 1.4, .7)
}
func (c PolarBearConfig) Apply(d *world.EntityData) { d.Data = c.New() }
func (c PolarBearConfig) New() *polarBearState {
	m := c.MaxHealth
	if m <= 0 {
		m = 30
	}
	h := c.Health
	if h <= 0 {
		h = m
	}
	return &polarBearState{health: NewHealthManager(h, m), effects: NewEffectManager(), age: c.Age, cooldown: c.AttackCooldown, standing: c.Standing, speed: .25}
}
func (polarBearType) DecodeNBT(n map[string]any, d *world.EntityData) {
	s := ReadNativeMobJavaState(n)
	if s == nil {
		s = n
	}
	c := PolarBearConfig{Age: int(nbtconv.Int32(s, "Age")), Standing: nbtconv.Bool(s, "Standing"), AttackCooldown: int(nbtconv.Int32(s, "AttackCooldown"))}
	if a, ok := nativeMobAttribute(n, "minecraft:health"); ok {
		c.Health, c.MaxHealth = a.Current, a.Base
	}
	d.Data = c.New()
}
func (polarBearType) EncodeNBT(d *world.EntityData) map[string]any {
	s := d.Data.(*polarBearState)
	m := s.health.MaxHealth()
	a := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{{Name: "minecraft:health", Base: m, Current: s.health.Health(), DefaultMin: 0, DefaultMax: m, Min: 0, Max: m}})
	return MergeNativeMobJavaState(map[string]any{"Attributes": a}, MergeNativeMobNBT(ReadNativeMobJavaState(d.NativeNBT), map[string]any{"Age": int32(s.age), "Standing": s.standing, "AttackCooldown": int32(s.cooldown)}))
}
func (p *PolarBear) b() *polarBearState         { return p.Behaviour().(*polarBearState) }
func (p *PolarBear) Tick(tx *world.Tx, n int64) { p.b().effects.Tick(p, tx); p.Ent.Tick(tx, n) }
func (s *polarBearState) Tick(e *Ent, tx *world.Tx) *Movement {
	p := &PolarBear{e}
	if s.age < 0 {
		s.age = min(0, s.age+1)
	}
	ai := &s.ai
	ai.MovementSpeed, ai.EyeHeight = s.speed, 1.19
	if s.age < 0 {
		ai.EyeHeight = .595
	}
	mv := ai.Tick(e, p, s.registerGoals)
	// PolarBear.tick: the growl cools down and the anger runs out.
	if s.warningSoundTicks > 0 {
		s.warningSoundTicks--
	}
	if !s.dead {
		s.updatePersistentAnger(ai.Mob(e, p), true)
	}
	if s.standing != s.shownStanding {
		s.shownStanding = s.standing
		p.updateState()
	}
	return mv
}

// ageTicks returns the age of the polar bear, negative for cubs.
func (p *PolarBear) ageTicks() *int { return &p.b().age }

// Standing reports if the polar bear rears up to swipe.
func (p *PolarBear) Standing() bool { return p.b().standing }

func (p *PolarBear) Health() float64        { return p.b().health.Health() }
func (p *PolarBear) MaxHealth() float64     { return p.b().health.MaxHealth() }
func (p *PolarBear) SetMaxHealth(v float64) { p.b().health.SetMaxHealth(v) }
func (p *PolarBear) Dead() bool             { return p.b().dead || p.Health() <= 0 }

// Panicking reports whether the polar bear runs from danger.
func (p *PolarBear) Panicking() bool { s := p.b(); return s.panic != nil && s.panic.running }
func (p *PolarBear) Hurt(d float64, src world.DamageSource) (float64, bool) {
	if p.Dead() || d <= 0 {
		return 0, false
	}
	s := p.b()
	d, ok := s.ai.DamageCooldown(p.tx, d)
	if !ok {
		return 0, false
	}
	if r, ok := s.effects.Effect(effect.Resistance); ok {
		d *= effect.Resistance.Multiplier(src, r.Level())
	}
	s.health.AddHealth(-d)
	s.ai.Hurt(src)
	p.view(HurtAction{})
	onMobHurt(p.tx, p)
	if p.Dead() {
		p.b().dead = true
		p.view(DeathAction{})
		dropAllDeathLoot(p.tx, p, src)
		_ = p.CloseIn(p.tx)
	}
	return d, true
}
func (p *PolarBear) Heal(d float64, _ world.HealingSource) float64 {
	if p.Dead() || d <= 0 {
		return 0
	}
	old := p.Health()
	p.b().health.AddHealth(d)
	return p.Health() - old
}
func (p *PolarBear) AddEffect(e effect.Effect)  { p.b().effects.Add(e, p) }
func (p *PolarBear) RemoveEffect(t effect.Type) { p.b().effects.Remove(t, p) }
func (p *PolarBear) Effects() []effect.Effect   { return p.b().effects.Effects() }
func (p *PolarBear) Speed() float64             { return p.b().speed }
func (p *PolarBear) SetSpeed(v float64)         { p.b().speed = v }
func (p *PolarBear) KnockBack(src mgl64.Vec3, k, h float64) {
	d := p.Position().Sub(src)
	d[1] = 0
	if d.Len() > 0 {
		d = d.Normalize().Mul(k)
	}
	d[1] = h
	p.SetVelocity(d)
}
func (p *PolarBear) view(a world.EntityAction) {
	v := p.tx.Viewers(p.Position())
	for _, x := range v {
		x.ViewEntityAction(p, a)
	}
	p.tx.ReleaseViewers(v)
}

var _ Living = (*PolarBear)(nil)
var _ world.EntityType = polarBearType{}
