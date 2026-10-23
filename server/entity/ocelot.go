package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// OcelotType is the source minecraft:ocelot species. Trusting an ocelot never
// converts it into a tameable cat: it only stops fleeing from players.
var OcelotType ocelotType

type ocelotType struct{}

func (ocelotType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		ocelotBehaviourConfig{}.Apply(d)
	}
	return &Ocelot{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (ocelotType) EncodeEntity() string { return "minecraft:ocelot" }
func (ocelotType) BBox(e world.Entity) cube.BBox {
	if o, ok := e.(*Ocelot); ok && o.b().age < 0 {
		return cube.Box(-.15, 0, -.15, .15, .35, .15)
	}
	return cube.Box(-.3, 0, -.3, .3, .7, .3)
}
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
	c.Trusting = nbtconv.Bool(s, "Trusting")
	c.Age = int(nbtconv.Int32(s, "Age"))
	c.Apply(d)
	d.Data.(*ocelotBehaviour).breeding.decodeNBT(s)
}
func (ocelotType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*ocelotBehaviour)
	h := b.health
	attrs := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{{Name: "minecraft:health", Base: h.MaxHealth(), Current: h.Health(), DefaultMin: 0, DefaultMax: h.MaxHealth(), Min: 0, Max: h.MaxHealth()}, {Name: "minecraft:movement", Base: .3, Current: .3, DefaultMin: 0, DefaultMax: .3, Min: 0, Max: .3}, {Name: "minecraft:attack_damage", Base: 3, Current: 3, DefaultMin: 0, DefaultMax: 3, Min: 0, Max: 3}})
	m := MergeNativeMobNBT(d.NativeNBT, map[string]any{"Attributes": attrs})
	return MergeNativeMobJavaState(m, b.breeding.encodeNBT(map[string]any{"Health": float32(h.Health()), "MaxHealth": float32(h.MaxHealth()), "Trusting": boolByte(b.trusting), "Age": int32(b.age)}))
}

type ocelotBehaviourConfig struct {
	Health, MaxHealth float64
	Trusting          bool
	Age               int
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
	return &ocelotBehaviour{health: NewHealthManager(c.Health, c.MaxHealth), effects: NewEffectManager(), trusting: c.Trusting, age: c.Age}
}

type ocelotBehaviour struct {
	health   *HealthManager
	effects  *EffectManager
	trusting bool
	dead     bool
	age      int
	breeding breeding
	animalAI
	tempt *TemptGoal
	// crouching and sprinting are the pose the speed of the ocelot gives
	// it.
	crouching, sprinting bool
	shown                [2]bool
}
type Ocelot struct{ *Ent }

func NewOcelot(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(OcelotType, ocelotBehaviourConfig{})
}
func (*Ocelot) SpeciesType() world.EntityType { return OcelotType }
func (o *Ocelot) b() *ocelotBehaviour         { return o.Behaviour().(*ocelotBehaviour) }
func (o *Ocelot) Tick(tx *world.Tx, n int64)  { o.b().effects.Tick(o, tx); o.Ent.Tick(tx, n) }
func (b *ocelotBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	o := &Ocelot{Ent: e}
	b.ai.MovementSpeed, b.ai.EyeHeight = .3, .595
	if b.age < 0 {
		b.ai.EyeHeight = .34375
	}
	mv := animalTick(e, tx, o, &b.animalAI, b.registerGoals)
	if shown := [2]bool{b.crouching, b.sprinting}; shown != b.shown {
		b.shown = shown
		o.updateState()
	}
	return mv
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

// Trusting reports whether the ocelot trusts players and no longer flees.
func (o *Ocelot) Trusting() bool { return o.b().trusting }

// Baby reports whether the ocelot is a kitten.
func (o *Ocelot) Baby() bool             { return o.b().age < 0 }
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
	d, ok := b.ai.DamageCooldown(o.tx, d)
	if !ok {
		return 0, false
	}
	if r, ok := b.effects.Effect(effect.Resistance); ok {
		d *= effect.Resistance.Multiplier(s, r.Level())
	}
	b.health.AddHealth(-d)
	b.ai.Hurt(s)
	resetLove(o)
	o.viewAction(HurtAction{})
	onMobHurt(o.tx, o)
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
func (o *Ocelot) AddEffect(e effect.Effect)  { o.b().effects.Add(e, o) }
func (o *Ocelot) RemoveEffect(t effect.Type) { o.b().effects.Remove(t, o) }
func (o *Ocelot) Effects() []effect.Effect   { return o.b().effects.Effects() }
func (o *Ocelot) Speed() float64             { return .3 }

// Crouching reports if the ocelot creeps, as it does while tempted.
func (o *Ocelot) Crouching() bool              { return o.b().crouching }
func (o *Ocelot) SetSpeed(float64)             {}
func (o *Ocelot) hasEffect(t effect.Type) bool { _, ok := o.b().effects.Effect(t); return ok }

// Interact ports Ocelot.mobInteract: an ocelot that does not trust players
// yet takes fish from a player within 3 blocks and starts trusting players
// one in three times. Fish otherwise breeds it or lets a kitten grow up
// faster.
func (o *Ocelot) Interact(tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	if u == nil || ctx == nil {
		return false
	}
	main, _ := u.HeldItems()
	b := o.b()
	// Ocelot.mobInteract: only while its TemptGoal runs.
	if (b.tempt == nil || b.tempt.Running()) && !b.trusting && !main.Empty() && ocelotFish(main.Item()) && u.Position().Sub(o.Position()).LenSqr() < 9 {
		ctx.SubtractFromCount(1)
		if b.ai.Mob(o.Ent, o).Random().NextInt(3) == 0 {
			b.trusting = true
			o.updateState()
			o.viewAction(TameAction{Succeeded: true})
		} else {
			o.viewAction(TameAction{})
		}
		return true
	}
	return feedBreeder(o, u, ctx)
}

// InLove reports whether the ocelot is in love.
func (o *Ocelot) InLove() bool { return o.b().breeding.InLove() }

func (o *Ocelot) breedingState() *breeding { return &o.b().breeding }
func (o *Ocelot) ageTicks() *int           { return &o.b().age }

// breedingFood reports whether the stack is in #ocelot_food.
func (o *Ocelot) breedingFood(stack item.Stack) bool {
	return !stack.Empty() && ocelotFish(stack.Item())
}

func (o *Ocelot) breedOffspring(_ *world.Tx, _ breeder) *world.EntityHandle {
	return babyAt(o).New(OcelotType, ocelotBehaviourConfig{Age: babyStartAge})
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
