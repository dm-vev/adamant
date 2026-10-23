package entity

import (
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
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
	data.Data.(*CowBehaviour).breeding.decodeNBT(source)
}
func (cowType) EncodeNBT(data *world.EntityData) map[string]any {
	cow := data.Data.(*CowBehaviour)
	health := cow.health
	attributes := EncodeNativeMobAttributes(ReadNativeMobAttributes(data.NativeNBT), []NativeMobAttribute{{
		Name: "minecraft:health", Base: health.MaxHealth(), Current: health.Health(),
		DefaultMin: 0, DefaultMax: health.MaxHealth(), Min: 0, Max: health.MaxHealth(),
	}})
	native := MergeNativeMobNBT(data.NativeNBT, map[string]any{"Attributes": attributes})
	javaState := MergeNativeMobNBT(ReadNativeMobJavaState(data.NativeNBT), cow.breeding.encodeNBT(map[string]any{
		"Age": int32(cow.ageTicks), "variant": "minecraft:" + [...]string{"temperate", "warm", "cold"}[min(cow.variant, 2)],
		"sound_variant": "minecraft:" + cow.soundVariant,
	}))
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
	SoundVariant      string
	healthSet         bool
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
		BaseBehaviour: NewBaseBehaviour(),
		health:        NewHealthManager(conf.Health, conf.MaxHealth),
		effects:       NewEffectManager(),
		variant:       max(0, min(conf.Variant, 2)),
		ageTicks:      conf.AgeTicks,
		soundVariant:  soundVariant,
		speed:         0.2,
	}
}

// Cow is a living cow entity.
type Cow struct{ *Ent }

type CowBehaviour struct {
	BaseBehaviour
	health            *HealthManager
	effects           *EffectManager
	variant, ageTicks int
	soundVariant      string
	speed             float64
	breeding          breeding
	animalAI
	dead bool
}

func (c *CowBehaviour) Variant() int32 { return int32(c.variant) }
func (c *CowBehaviour) Baby() bool     { return c.ageTicks < 0 }

// SoundVariant returns the name of the sounds the cow makes.
func (c *CowBehaviour) SoundVariant() string { return c.soundVariant }

// ClimateVariant returns the name of the climate the cow is coloured for.
func (c *CowBehaviour) ClimateVariant() string {
	return [...]string{"temperate", "warm", "cold"}[min(c.variant, 2)]
}

func (c *Cow) behaviour() *CowBehaviour { return c.Behaviour().(*CowBehaviour) }

// Tick applies the cow's passive wandering goal and movement physics.
func (c *Cow) Tick(tx *world.Tx, current int64) {
	c.behaviour().effects.Tick(c, tx)
	c.Ent.Tick(tx, current)
}

func (b *CowBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	cow := &Cow{Ent: e}
	b.ai.MovementSpeed, b.ai.EyeHeight = b.speed, animalEyeHeight(1.3, b.ageTicks < 0)
	return animalTick(e, tx, cow, &b.animalAI, func(m *Mob) {
		registerAnimalGoals(m, &b.animalAI, animalSpec{panic: 2, breed: 1, tempt: 1.25, tempting: cow.breedingFood, parent: 1.25, stroll: 1}, nil)
	})
}

// Health returns the cow's current health.
func (c *Cow) Health() float64 { return c.behaviour().health.Health() }

// MaxHealth returns the cow's maximum health.
func (c *Cow) MaxHealth() float64 { return c.behaviour().health.MaxHealth() }

// SetMaxHealth sets the cow's maximum health.
func (c *Cow) SetMaxHealth(v float64) { c.behaviour().health.SetMaxHealth(v) }

// Dead reports whether the cow has died.
func (c *Cow) Dead() bool { return c.behaviour().dead || c.Health() <= 0 }

// Panicking reports whether the cow runs from danger.
func (c *Cow) Panicking() bool { return c.behaviour().panicking() }

// Hurt damages the cow and kills it if its health is exhausted.
func (c *Cow) Hurt(damage float64, src world.DamageSource) (float64, bool) {
	if c.Dead() || damage <= 0 || (src.Fire() && c.hasEffect(effect.FireResistance)) {
		return 0, false
	}
	b := c.behaviour()
	damage, ok := b.ai.DamageCooldown(c.tx, damage)
	if !ok {
		return 0, false
	}
	if resistance, ok := b.effects.Effect(effect.Resistance); ok {
		damage *= effect.Resistance.Multiplier(src, resistance.Level())
	}
	b.health.AddHealth(-damage)
	b.ai.Hurt(src)
	resetLove(c)
	c.viewAction(HurtAction{})
	onMobHurt(c.tx, c)
	if c.Dead() {
		c.die(src)
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

func (c *Cow) die(src world.DamageSource) {
	b := c.behaviour()
	if b.dead {
		return
	}
	b.dead = true
	c.viewAction(DeathAction{})
	dropAllDeathLoot(c.tx, c, src)
	_ = c.CloseIn(c.tx)
}

// Baby reports whether the cow is a calf.
func (c *Cow) Baby() bool { return c.behaviour().ageTicks < 0 }

// Milk returns whether an empty bucket can be used on the cow.
func (c *Cow) Milk(_ *world.Tx) bool {
	return !c.Dead() && c.behaviour().ageTicks >= 0
}

// MilkSound returns the vanilla cow milking sound.
func (c *Cow) MilkSound() world.Sound {
	return sound.Custom{Name: "entity.cow.milk", Volume: 1, Pitch: 1}
}

var _ Milkable = (*Cow)(nil)

// Interact feeds the cow wheat to breed it or let a calf grow up faster.
func (c *Cow) Interact(_ *world.Tx, user item.User, ctx *item.UseContext) bool {
	return feedBreeder(c, user, ctx)
}

// InLove reports whether the cow is in love.
func (c *Cow) InLove() bool { return c.behaviour().breeding.InLove() }

func (c *Cow) breedingState() *breeding { return &c.behaviour().breeding }
func (c *Cow) ageTicks() *int           { return &c.behaviour().ageTicks }

func (c *Cow) breedingFood(stack item.Stack) bool { return itemNamed(stack, "minecraft:wheat") }

// breedOffspring ports Cow.getBreedOffspring: the calf takes the variant of
// either parent.
func (c *Cow) breedOffspring(_ *world.Tx, partner breeder) *world.EntityHandle {
	return babyAt(c).New(CowType, CowConfig{AgeTicks: babyStartAge, Variant: randomParent(c.behaviour().variant, partner.(*Cow).behaviour().variant)})
}

// FinalizeSpawn ports Cow.finalizeSpawn: the biome selects the variant and
// the sound variant is random.
func (c *Cow) FinalizeSpawn(_ string, random *vanilla.Random) {
	if random == nil {
		return
	}
	c.behaviour().variant = farmVariantIndex(templateFarmTemperature(spawnBiome(c.Ent)), "temperate", "warm", "cold")
	if random.NextInt(2) == 0 {
		c.behaviour().soundVariant = "classic"
	} else {
		c.behaviour().soundVariant = "moody"
	}
}
