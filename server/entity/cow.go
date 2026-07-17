package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// NewCow creates a cow entity.
func NewCow(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(CowType, CowConfig{})
}

// CowType is a world.EntityType implementation for cows.
var CowType cowType

type cowType struct{}

func (cowType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &Cow{Ent: &Ent{tx: tx, handle: handle, data: data}}
}

func (cowType) EncodeEntity() string { return "minecraft:cow" }

func (cowType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.45, 0, -0.45, 0.45, 1.4, 0.45)
}

func (cowType) DecodeNBT(m map[string]any, data *world.EntityData) {
	conf := CowConfig{}
	if _, ok := m["Health"]; ok {
		conf.Health = float64(nbtconv.Float32(m, "Health"))
		conf.healthSet = true
	}
	if _, ok := m["MaxHealth"]; ok {
		conf.MaxHealth = float64(nbtconv.Float32(m, "MaxHealth"))
	}
	if _, ok := m["Variant"]; ok {
		conf.Variant = int(nbtconv.Int32(m, "Variant"))
	}
	data.Data = conf.New()
}

func (cowType) EncodeNBT(data *world.EntityData) map[string]any {
	cow := data.Data.(*CowBehaviour)
	return map[string]any{
		"Health":    float32(cow.health.Health()),
		"MaxHealth": float32(cow.health.MaxHealth()),
		"Variant":   int32(cow.variant),
	}
}

// CowConfig configures a cow.
type CowConfig struct {
	Health, MaxHealth float64
	Variant           int
	healthSet         bool
}

// Apply applies the cow configuration to entity data.
func (conf CowConfig) Apply(data *world.EntityData) {
	data.Data = conf.New()
}

// New creates a cow behaviour from the configuration.
func (conf CowConfig) New() *CowBehaviour {
	if conf.MaxHealth <= 0 {
		conf.MaxHealth = 10
	}
	if !conf.healthSet && conf.Health <= 0 {
		conf.Health = conf.MaxHealth
	}
	return &CowBehaviour{
		health:  NewHealthManager(conf.Health, conf.MaxHealth),
		effects: NewEffectManager(),
		mc:      &MovementComputer{Gravity: 0.08, Drag: 0.02, DragBeforeGravity: true},
		variant: conf.Variant,
		speed:   0.2,
	}
}

// Cow is a living cow entity.
type Cow struct{ *Ent }

// CowBehaviour holds cow state shared between transaction-bound Cow values.
type CowBehaviour struct {
	health  *HealthManager
	effects *EffectManager
	mc      *MovementComputer
	variant int
	speed   float64
	dead    bool
}

func (c *Cow) behaviour() *CowBehaviour { return c.Behaviour().(*CowBehaviour) }

// Tick applies effects and inert movement physics.
func (c *Cow) Tick(tx *world.Tx, current int64) {
	c.behaviour().effects.Tick(c, tx)
	c.Ent.Tick(tx, current)
}

func (b *CowBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	m := b.mc.TickMovement(e, e.data.Pos, e.data.Vel, e.data.Rot, e.data.Rot, tx)
	e.data.Pos, e.data.Vel = m.Position(), m.Velocity()
	return m
}

// Health returns the cow's current health.
func (c *Cow) Health() float64 { return c.behaviour().health.Health() }

// MaxHealth returns the cow's maximum health.
func (c *Cow) MaxHealth() float64 { return c.behaviour().health.MaxHealth() }

// SetMaxHealth sets the cow's maximum health.
func (c *Cow) SetMaxHealth(v float64) { c.behaviour().health.SetMaxHealth(v) }

// Dead reports whether the cow has died.
func (c *Cow) Dead() bool { return c.behaviour().dead || c.Health() <= 0 }

// Hurt damages the cow and kills it if its health is exhausted.
func (c *Cow) Hurt(damage float64, src world.DamageSource) (float64, bool) {
	if c.Dead() || damage <= 0 || (src.Fire() && c.hasEffect(effect.FireResistance)) {
		return 0, false
	}
	if resistance, ok := c.behaviour().effects.Effect(effect.Resistance); ok {
		damage *= effect.Resistance.Multiplier(src, resistance.Level())
	}
	c.behaviour().health.AddHealth(-damage)
	c.viewAction(HurtAction{})
	if c.Dead() {
		c.die()
	}
	return damage, true
}

// Heal restores the cow's health up to its maximum.
func (c *Cow) Heal(health float64, _ world.HealingSource) {
	if !c.Dead() && health > 0 {
		c.behaviour().health.AddHealth(health)
	}
}

// KnockBack applies velocity away from the source position.
func (c *Cow) KnockBack(src mgl64.Vec3, force, height float64) {
	if c.Dead() {
		return
	}
	velocity := c.Position().Sub(src)
	velocity[1] = 0
	if velocity.Len() != 0 {
		velocity = velocity.Normalize().Mul(force)
	}
	velocity[1] = height
	c.SetVelocity(velocity)
}

// AddEffect adds an effect to the cow.
func (c *Cow) AddEffect(e effect.Effect) { c.behaviour().effects.Add(e, c) }

// RemoveEffect removes an effect from the cow.
func (c *Cow) RemoveEffect(t effect.Type) { c.behaviour().effects.Remove(t, c) }

// Effects returns the cow's active effects.
func (c *Cow) Effects() []effect.Effect { return c.behaviour().effects.Effects() }

// Speed returns the cow's movement speed.
func (c *Cow) Speed() float64 { return c.behaviour().speed }

// SetSpeed changes the cow's movement speed.
func (c *Cow) SetSpeed(speed float64) { c.behaviour().speed = speed }

func (c *Cow) hasEffect(t effect.Type) bool {
	_, ok := c.behaviour().effects.Effect(t)
	return ok
}

func (c *Cow) viewAction(action world.EntityAction) {
	viewers := c.tx.Viewers(c.Position())
	for _, viewer := range viewers {
		viewer.ViewEntityAction(c, action)
	}
	c.tx.ReleaseViewers(viewers)
}

func (c *Cow) die() {
	b := c.behaviour()
	if b.dead {
		return
	}
	b.dead = true
	c.viewAction(DeathAction{})
	tx := c.tx
	tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: c.Position()}, item.NewStack(item.Leather{}, 1)))
	tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: c.Position()}, item.NewStack(item.Beef{Cooked: c.OnFireDuration() > 0}, 1)))
	_ = c.CloseIn(tx)
}
