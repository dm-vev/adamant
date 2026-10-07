package entity

import (
	"math"
	"math/rand/v2"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
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
	if data.Data == nil {
		CowConfig{}.Apply(data)
	}
	return &Cow{Ent: &Ent{tx: tx, handle: handle, data: data}}
}

func (cowType) EncodeEntity() string { return "minecraft:cow" }

func (cowType) BBox(entity world.Entity) cube.BBox {
	if cow, ok := entity.(*Cow); ok && cow.behaviour().ageTicks < 0 {
		return cube.Box(-0.225, 0, -0.225, 0.225, 0.7, 0.225)
	}
	return cube.Box(-0.45, 0, -0.45, 0.45, 1.4, 0.45)
}

func (cowType) DecodeNBT(m map[string]any, data *world.EntityData) {
	conf := CowConfig{}
	javaState := ReadNativeMobJavaState(m)
	source := m
	if javaState != nil {
		source = javaState
	}
	if _, ok := source["Health"]; ok {
		conf.Health = float64(nbtconv.Float32(source, "Health"))
		conf.healthSet = true
	} else if health, ok := nativeMobAttribute(m, "minecraft:health"); ok {
		conf.Health, conf.healthSet = health.Current, true
	}
	if _, ok := source["MaxHealth"]; ok {
		conf.MaxHealth = float64(nbtconv.Float32(source, "MaxHealth"))
	} else if health, ok := nativeMobAttribute(m, "minecraft:health"); ok {
		conf.MaxHealth = health.Base
	}
	if _, ok := source["Age"]; ok {
		conf.AgeTicks = int(nbtconv.Int32(source, "Age"))
	}
	if value, ok := source["variant"].(string); ok {
		conf.Variant = cowVariant(value)
	}
	if value, ok := source["sound_variant"].(string); ok {
		conf.SoundVariant = value
	}
	data.Data = conf.New()
}
func (cowType) EncodeNBT(data *world.EntityData) map[string]any {
	cow := data.Data.(*CowBehaviour)
	health := cow.health
	attributes := EncodeNativeMobAttributes(ReadNativeMobAttributes(data.NativeNBT), []NativeMobAttribute{{
		Name: "minecraft:health", Base: health.MaxHealth(), Current: health.Health(),
		DefaultMin: 0, DefaultMax: health.MaxHealth(), Min: 0, Max: health.MaxHealth(),
	}})
	native := map[string]any{"Attributes": attributes}
	javaState := MergeNativeMobNBT(ReadNativeMobJavaState(data.NativeNBT), map[string]any{
		"Age": int32(cow.ageTicks), "variant": "minecraft:" + [...]string{"temperate", "warm", "cold"}[min(cow.variant, 2)],
		"sound_variant": "minecraft:" + cow.soundVariant,
	})
	return MergeNativeMobJavaState(native, javaState)
}

func nativeMobAttribute(data map[string]any, name string) (NativeMobAttribute, bool) {
	for _, attribute := range ReadNativeMobAttributes(data) {
		if attribute.Name == name {
			return attribute, true
		}
	}
	return NativeMobAttribute{}, false
}

func cowVariant(value string) int {
	switch strings.TrimPrefix(value, "minecraft:") {
	case "warm":
		return 1
	case "cold":
		return 2
	default:
		return 0
	}
}

// CowConfig configures a cow.
type CowConfig struct {
	Health, MaxHealth float64
	Variant, AgeTicks int
	SoundVariant string
	healthSet bool
}

// Apply applies the cow configuration to entity data.
func (conf CowConfig) Apply(data *world.EntityData) {
	data.Data = conf.New()
}

func (conf CowConfig) New() *CowBehaviour {
	if conf.MaxHealth <= 0 {
		conf.MaxHealth = 10
	}
	if !conf.healthSet && conf.Health <= 0 {
		conf.Health = conf.MaxHealth
	}
	soundVariant := strings.TrimPrefix(conf.SoundVariant, "minecraft:")
	if soundVariant != "moody" {
		soundVariant = "classic"
	}
	return &CowBehaviour{
		health: NewHealthManager(conf.Health, conf.MaxHealth),
		effects: NewEffectManager(),
		mc: &MovementComputer{Gravity: 0.08, Drag: 0.02, DragBeforeGravity: true},
		variant: max(0, min(conf.Variant, 2)),
		ageTicks: conf.AgeTicks,
		soundVariant: soundVariant,
		speed: 0.2,
	}
}

// Cow is a living cow entity.
type Cow struct{ *Ent }

type CowBehaviour struct {
	health *HealthManager
	effects *EffectManager
	mc *MovementComputer
	variant, ageTicks int
	soundVariant string
	speed float64
	path []cube.Pos
	pathIndex, nextWander int
	dead bool
}

func (c *Cow) behaviour() *CowBehaviour { return c.Behaviour().(*CowBehaviour) }

// Tick applies the cow's passive wandering goal and movement physics.
func (c *Cow) Tick(tx *world.Tx, current int64) {
	c.behaviour().effects.Tick(c, tx)
	c.Ent.Tick(tx, current)
}

func (b *CowBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if b.ageTicks < 0 {
		b.ageTicks = min(0, b.ageTicks+1)
	}
	if b.nextWander > 0 {
		b.nextWander--
	}
	if b.pathIndex >= len(b.path) && b.nextWander == 0 {
		b.chooseWanderPath(e, tx)
	}
	rot, velocity := e.data.Rot, e.data.Vel
	if b.pathIndex < len(b.path) {
		target := b.path[b.pathIndex].Vec3().Add(mgl64.Vec3{0.5, 0, 0.5})
		delta := target.Sub(e.data.Pos)
		delta[1] = 0
		if delta.Len() < 0.25 {
			b.pathIndex++
		} else {
			direction := delta.Normalize()
			velocity[0], velocity[2] = direction[0]*b.speed, direction[2]*b.speed
			rot[0] = math.Atan2(-direction[0], direction[2]) * 180 / math.Pi
		}
	} else {
		velocity[0], velocity[2] = 0, 0
	}
	m := b.mc.TickMovement(e, e.data.Pos, velocity, e.data.Rot, rot, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	return m
}

func (b *CowBehaviour) chooseWanderPath(e *Ent, tx *world.Tx) {
	b.nextWander = 40 + rand.IntN(81)
	start := cube.PosFromVec3(e.Position())
	for range 4 {
		goal := start.Add(cube.Pos{rand.IntN(13)-6, 0, rand.IntN(13)-6})
		path := findPath(tx, start, goal, 128, false, false)
		if len(path) != 0 {
			b.path, b.pathIndex = path, 0
			return
		}
	}
	b.path, b.pathIndex = nil, 0
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

// Heal restores the cow's health up to its maximum and returns the amount restored.
func (c *Cow) Heal(health float64, _ world.HealingSource) float64 {
	if c.Dead() || health <= 0 {
		return 0
	}
	oldHealth := c.Health()
	c.behaviour().health.AddHealth(health)
	return c.Health() - oldHealth
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
	if leather := rand.IntN(3); leather > 0 {
		tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: c.Position()}, item.NewStack(item.Leather{}, leather)))
	}
	tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: c.Position()}, item.NewStack(item.Beef{Cooked: c.OnFireDuration() > 0}, rand.IntN(3)+1)))
	_ = c.CloseIn(tx)
}
// Milk returns whether an empty bucket can be used on the cow.
func (c *Cow) Milk(_ *world.Tx) bool {
	return !c.Dead() && c.behaviour().ageTicks >= 0
}

// MilkSound returns the vanilla cow milking sound.
func (c *Cow) MilkSound() world.Sound {
	return sound.Custom{Name: "entity.cow.milk", Volume: 1, Pitch: 1}
}

var _ Milkable = (*Cow)(nil)
// FinalizeSpawn assigns the randomized cow sound set used for newly spawned mobs.
func (c *Cow) FinalizeSpawn(_ string, random *java26.Random) {
	if random == nil {
		return
	}
	if random.NextInt(2) == 0 {
		c.behaviour().soundVariant = "classic"
	} else {
		c.behaviour().soundVariant = "moody"
	}
}
