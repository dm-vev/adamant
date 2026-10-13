package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// OcelotType is the source minecraft:ocelot species. Trusting an ocelot never
// converts it into a tameable cat: it only stops fleeing from that player.
var OcelotType ocelotType

type ocelotType struct{}

func (ocelotType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		ocelotBehaviourConfig{}.Apply(d)
	}
	return &Ocelot{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (ocelotType) EncodeEntity() string        { return "minecraft:ocelot" }
func (ocelotType) BBox(world.Entity) cube.BBox { return cube.Box(-.3, 0, -.3, .3, .7, .3) }
func (ocelotType) DecodeNBT(m map[string]any, d *world.EntityData) {
	c := ocelotBehaviourConfig{}
	s := ReadNativeMobJavaState(m)
	if s == nil {
		s = m
	}
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
		c.MaxHealth = 10
	}
	if c.Health <= 0 {
		c.Health = c.MaxHealth
	}
	c.healthSet = true
	c.Trusting = nbtconv.String(s, "Trusting")
	c.Apply(d)
}
func (ocelotType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*ocelotBehaviour)
	h := b.health
	attrs := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{{Name: "minecraft:health", Base: h.MaxHealth(), Current: h.Health(), DefaultMin: 0, DefaultMax: h.MaxHealth(), Min: 0, Max: h.MaxHealth()}, {Name: "minecraft:movement", Base: .3, Current: .3, DefaultMin: 0, DefaultMax: .3, Min: 0, Max: .3}, {Name: "minecraft:attack_damage", Base: 3, Current: 3, DefaultMin: 0, DefaultMax: 3, Min: 0, Max: 3}})
	m := MergeNativeMobNBT(d.NativeNBT, map[string]any{"Attributes": attrs})
	return MergeNativeMobJavaState(m, map[string]any{"Health": float32(h.Health()), "MaxHealth": float32(h.MaxHealth()), "Trusting": b.trusting})
}

type ocelotBehaviourConfig struct {
	Health, MaxHealth float64
	Trusting          string
	healthSet         bool
}

func (c ocelotBehaviourConfig) Apply(d *world.EntityData) { d.Data = c.New() }
func (c ocelotBehaviourConfig) New() *ocelotBehaviour {
	if c.MaxHealth <= 0 {
		c.MaxHealth = 10
	}
	if !c.healthSet && c.Health <= 0 {
		c.Health = c.MaxHealth
	}
	return &ocelotBehaviour{health: NewHealthManager(c.Health, c.MaxHealth), effects: NewEffectManager(), mc: &MovementComputer{Gravity: .08, Drag: .02, DragBeforeGravity: true}, random: java26.NewLegacyRandom(0), trusting: c.Trusting, nextWander: 1}
}

type ocelotBehaviour struct {
	health     *HealthManager
	effects    *EffectManager
	mc         *MovementComputer
	random     java26.Random
	trusting   string
	dead       bool
	nextWander int
	navigator  Navigator
}
type Ocelot struct{ *Ent }

func NewOcelot(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(OcelotType, ocelotBehaviourConfig{})
}
func (*Ocelot) SpeciesType() world.EntityType { return OcelotType }
func (o *Ocelot) b() *ocelotBehaviour         { return o.Behaviour().(*ocelotBehaviour) }
func (o *Ocelot) Tick(tx *world.Tx, n int64)  { o.b().effects.Tick(o, tx); o.Ent.Tick(tx, n) }
func (b *ocelotBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	var nearest world.Entity
	distance := 8.0
	for p := range tx.Players() {
		d := p.Position().Sub(e.Position()).Len()
		if d < distance {
			nearest, distance = p, d
		}
	}
	trusted := nearest != nil && nearest.H().UUID().String() == b.trusting
	tempting := false
	if nearest != nil && !trusted {
		if u, ok := nearest.(item.User); ok {
			main, off := u.HeldItems()
			tempting = ocelotFish(main.Item()) || ocelotFish(off.Item())
		}
	}
	start := cube.PosFromVec3(e.Position())
	if nearest != nil && !trusted && distance < 8 && !tempting {
		away := e.Position().Sub(nearest.Position())
		away[1] = 0
		if away.Len() == 0 {
			away = mgl64.Vec3{1, 0, 0}
		}
		goal := cube.PosFromVec3(e.Position().Add(away.Normalize().Mul(7)))
		b.navigator.MoveTo(tx, start, goal, GroundProfile{MaxVisitedNodes: 128}, navigatorTick(e))
	} else if nearest != nil && !trusted && tempting && distance < 10 {
		b.navigator.MoveTo(tx, start, cube.PosFromVec3(nearest.Position()), GroundProfile{MaxVisitedNodes: 128}, navigatorTick(e))
	} else if b.navigator.Index >= len(b.navigator.Path) {
		if b.nextWander > 0 {
			b.nextWander--
		}
		if b.nextWander == 0 {
			b.nextWander = 40 + int(b.random.NextInt(61))
			goal := start.Add(cube.Pos{int(b.random.NextInt(11)) - 5, 0, int(b.random.NextInt(11)) - 5})
			b.navigator.MoveTo(tx, start, goal, GroundProfile{MaxVisitedNodes: 128}, navigatorTick(e))
		}
	}
	v, r := e.data.Vel, e.data.Rot
	if p, ok := b.navigator.Tick(e.data.Pos, navigatorTick(e), .6); ok {
		d := p.Sub(e.data.Pos)
		d[1] = 0
		if d.Len() < .25 {
			b.navigator.Index++
		} else {
			d = d.Normalize()
			speed := .23
			if nearest != nil && !trusted {
				speed = .31
			}
			v[0], v[2] = d[0]*speed, d[2]*speed
			r[0] = math.Atan2(-d[0], d[2]) * 180 / math.Pi
		}
	} else {
		v[0], v[2] = 0, 0
	}
	return b.mc.TickMovement(e, e.data.Pos, v, r, e.data.Rot, tx)
}
func ocelotFish(i world.Item) bool {
	switch v := i.(type) {
	case item.Cod:
		return !v.Cooked
	case item.Salmon:
		return !v.Cooked
	}
	return false
}
func (o *Ocelot) TrustingPlayer() string { return o.b().trusting }
func (o *Ocelot) Health() float64        { return o.b().health.Health() }
func (o *Ocelot) MaxHealth() float64     { return o.b().health.MaxHealth() }
func (o *Ocelot) SetMaxHealth(h float64) { o.b().health.SetMaxHealth(h) }
func (o *Ocelot) Dead() bool             { return o.b().dead || o.Health() <= 0 }
func (o *Ocelot) Heal(h float64, _ world.HealingSource) float64 {
	if o.Dead() || h <= 0 {
		return 0
	}
	old := o.Health()
	o.b().health.AddHealth(h)
	return o.Health() - old
}
func (o *Ocelot) Hurt(d float64, s world.DamageSource) (float64, bool) {
	b := o.b()
	if o.Dead() || d <= 0 || (s.Fire() && o.hasEffect(effect.FireResistance)) {
		return 0, false
	}
	if r, ok := b.effects.Effect(effect.Resistance); ok {
		d *= effect.Resistance.Multiplier(s, r.Level())
	}
	b.health.AddHealth(-d)
	o.viewAction(HurtAction{})
	if o.Dead() {
		b.dead = true
		o.viewAction(DeathAction{})
		dropAllDeathLoot(o.tx, o, s)
		_ = o.CloseIn(o.tx)
	}
	return d, true
}
func (o *Ocelot) KnockBack(s mgl64.Vec3, f, h float64) {
	d := o.Position().Sub(s)
	d[1] = 0
	if d.Len() != 0 {
		d = d.Normalize().Mul(f)
	}
	d[1] = h
	o.SetVelocity(d)
}
func (o *Ocelot) AddEffect(e effect.Effect)    { o.b().effects.Add(e, o) }
func (o *Ocelot) RemoveEffect(t effect.Type)   { o.b().effects.Remove(t, o) }
func (o *Ocelot) Effects() []effect.Effect     { return o.b().effects.Effects() }
func (o *Ocelot) Speed() float64               { return .23 }
func (o *Ocelot) SetSpeed(float64)             {}
func (o *Ocelot) hasEffect(t effect.Type) bool { _, ok := o.b().effects.Effect(t); return ok }
func (o *Ocelot) Interact(tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	main, _ := u.HeldItems()
	if !ocelotFish(main.Item()) {
		return false
	}
	b := o.b()
	if b.trusting == u.H().UUID().String() {
		return false
	}
	ctx.SubtractFromCount(1)
	if b.random.NextInt(3) == 0 {
		b.trusting = u.H().UUID().String()
		tx.PlaySound(o.Position(), sound.Custom{Name: "entity.ocelot.ambient", Volume: 1, Pitch: 1})
	} else {
		tx.PlaySound(o.Position(), sound.Custom{Name: "entity.ocelot.hurt", Volume: 1, Pitch: 1})
	}
	return true
}
func (o *Ocelot) viewAction(a world.EntityAction) {
	vs := o.tx.Viewers(o.Position())
	for _, v := range vs {
		v.ViewEntityAction(o, a)
	}
	o.tx.ReleaseViewers(vs)
}

var _ Living = (*Ocelot)(nil)
var _ Interactable = (*Ocelot)(nil)
