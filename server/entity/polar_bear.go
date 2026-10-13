package entity

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
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
	next           int
	navigator      Navigator
	mc             *MovementComputer
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
	return &polarBearState{health: NewHealthManager(h, m), effects: NewEffectManager(), age: c.Age, cooldown: c.AttackCooldown, standing: c.Standing, speed: .25, next: 40 + rand.IntN(81), mc: &MovementComputer{Gravity: .08, Drag: .02, DragBeforeGravity: true}}
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
	if s.cooldown > 0 {
		s.cooldown--
	}
	target := nearestPolarBearTarget(e, tx)
	if target != nil {
		dist := target.Position().Sub(e.Position()).Len()
		cubNearby := false
		if s.age >= 0 {
			area := cube.Box(-16, -6, -16, 16, 6, 16).Translate(e.Position())
			for other := range tx.EntitiesWithin(area) {
				if bear, ok := other.(*PolarBear); ok && bear.b().age < 0 {
					cubNearby = true
					break
				}
			}
		}
		if dist < 1.8 && s.cooldown == 0 {
			if l, ok := target.(Living); ok {
				l.Hurt(6, AttackDamageSource{Attacker: p})
			}
			s.cooldown = 20
			s.standing = false
			tx.PlaySound(p.Position(), sound.Custom{Name: "entity.polar_bear.warning", Volume: 1, Pitch: 1})
		} else if dist < 12 && (cubNearby || s.age < 0) {
			s.standing = true
			start, goal := cube.PosFromVec3(e.Position()), cube.PosFromVec3(target.Position())
			s.navigator.MoveTo(tx, start, goal, GroundProfile{MaxVisitedNodes: 128}, navigatorTick(e))
			if waypoint, ok := s.navigator.Tick(e.Position(), navigatorTick(e), 1.4); ok {
				p.moveToward(waypoint)
			}
		}
	} else {
		s.standing = false
		if s.next > 0 {
			s.next--
		}
		if s.navigator.Index >= len(s.navigator.Path) && s.next == 0 {
			s.next = 40 + rand.IntN(81)
			start := cube.PosFromVec3(e.Position())
			for range 4 {
				goal := start.Add(cube.Pos{rand.IntN(13) - 6, 0, rand.IntN(13) - 6})
				if s.navigator.MoveTo(tx, start, goal, GroundProfile{MaxVisitedNodes: 128}, navigatorTick(e)) {
					break
				}
			}
		}
		if waypoint, ok := s.navigator.Tick(e.Position(), navigatorTick(e), 1.4); ok {
			p.moveToward(waypoint)
		}
	}
	m := s.mc.TickMovement(e, e.data.Pos, e.data.Vel, e.data.Rot, e.data.Rot, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	return m
}
func (p *PolarBear) moveToward(pos mgl64.Vec3) {
	d := pos.Sub(p.Position())
	d[1] = 0
	if d.Len() == 0 {
		return
	}
	d = d.Normalize()
	v := p.Velocity()
	v[0], v[2] = d[0]*p.b().speed, d[2]*p.b().speed
	p.SetVelocity(v)
	r := p.Rotation()
	r[0] = math.Atan2(-d[0], d[2]) * 180 / math.Pi
	p.data.Rot = r
}
func nearestPolarBearTarget(e *Ent, tx *world.Tx) world.Entity {
	if e.Behaviour().(*polarBearState).age < 0 {
		return nil
	}
	area := cube.Box(-16, -6, -16, 16, 6, 16).Translate(e.Position())
	cubNearby := false
	for other := range tx.EntitiesWithin(area) {
		if bear, ok := other.(*PolarBear); ok && bear.b().age < 0 {
			cubNearby = true
			break
		}
	}
	var found world.Entity
	best := 16.0
	for other := range tx.EntitiesWithin(area) {
		id := other.H().Type().EncodeEntity()
		if id == "minecraft:player" && !cubNearby {
			continue
		}
		if id != "minecraft:player" && id != "minecraft:fox" {
			continue
		}
		if l, ok := other.(Living); !ok || l.Dead() {
			continue
		}
		d := other.Position().Sub(e.Position()).Len()
		if d < best {
			found, best = other, d
		}
	}
	return found
}
func (p *PolarBear) Health() float64        { return p.b().health.Health() }
func (p *PolarBear) MaxHealth() float64     { return p.b().health.MaxHealth() }
func (p *PolarBear) SetMaxHealth(v float64) { p.b().health.SetMaxHealth(v) }
func (p *PolarBear) Dead() bool             { return p.b().dead || p.Health() <= 0 }
func (p *PolarBear) Hurt(d float64, src world.DamageSource) (float64, bool) {
	if p.Dead() || d <= 0 {
		return 0, false
	}
	p.b().health.AddHealth(-d)
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
