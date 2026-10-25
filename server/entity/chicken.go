package entity

import (
	"math"
	"math/rand/v2"
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

// NewChicken creates a worldless chicken handle with the supplied variant and age.
func NewChicken(opts world.EntitySpawnOpts, conf ChickenBehaviourConfig) *world.EntityHandle {
	return opts.New(ChickenType, conf)
}

var ChickenType chickenType

type chickenType struct{}

func (chickenType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	if data.Data == nil {
		ChickenBehaviourConfig{}.Apply(data)
	}
	return &Chicken{Ent: &Ent{tx: tx, handle: handle, data: data}}
}
func (chickenType) EncodeEntity() string { return "minecraft:chicken" }
func (chickenType) BBox(e world.Entity) cube.BBox {
	if c, ok := e.(*Chicken); ok && c.behaviour().ageTicks < 0 {
		return cube.Box(-0.1, 0, -0.1, 0.1, 0.35, 0.1)
	}
	return cube.Box(-0.2, 0, -0.2, 0.2, 0.7, 0.2)
}
func (chickenType) DecodeNBT(m map[string]any, data *world.EntityData) {
	conf := ChickenBehaviourConfig{}
	if _, ok := m["Health"]; ok {
		conf.Health = float64(nbtconv.Float32(m, "Health"))
		conf.healthSet = true
	}
	if _, ok := m["MaxHealth"]; ok {
		conf.MaxHealth = float64(nbtconv.Float32(m, "MaxHealth"))
	}
	if _, ok := m["Age"]; ok {
		conf.AgeTicks = int(nbtconv.Int32(m, "Age"))
	}
	if _, ok := m["EggLayTime"]; ok {
		conf.EggLayTime = int(nbtconv.Int32(m, "EggLayTime"))
		conf.eggTimerSet = true
	}
	if _, ok := m["Variant"]; ok {
		conf.Variant = int(nbtconv.Int32(m, "Variant"))
	}
	if v, ok := m["variant"].(string); ok {
		switch strings.TrimPrefix(v, "minecraft:") {
		case "cold":
			conf.Variant = 1
		case "warm":
			conf.Variant = 2
		}
	}
	if v, ok := m["sound_variant"].(string); ok {
		conf.SoundVariant = v
	}
	conf.Jockey = nbtconv.Bool(m, "IsChickenJockey")
	data.Data = conf.New()
	data.Data.(*ChickenBehaviour).breeding.decodeNBT(m)
}
func (chickenType) EncodeNBT(data *world.EntityData) map[string]any {
	b := data.Data.(*ChickenBehaviour)
	variant := []string{"temperate", "cold", "warm"}[min(b.variant, 2)]
	return b.breeding.encodeNBT(map[string]any{"Health": float32(b.health.Health()), "MaxHealth": float32(b.health.MaxHealth()), "Age": int32(b.ageTicks), "EggLayTime": int32(b.eggTimer), "variant": "minecraft:" + variant, "sound_variant": "minecraft:" + b.soundVariant, "IsChickenJockey": boolByte(b.jockey)})
}

type ChickenBehaviourConfig struct {
	Health, MaxHealth             float64
	AgeTicks, Variant, EggLayTime int
	SoundVariant                  string
	Jockey                        bool
	healthSet, eggTimerSet        bool
}

func (c ChickenBehaviourConfig) Apply(data *world.EntityData) { data.Data = c.New() }
func (c ChickenBehaviourConfig) New() *ChickenBehaviour {
	if c.MaxHealth <= 0 {
		c.MaxHealth = 4
	}
	if !c.healthSet && c.Health <= 0 {
		c.Health = c.MaxHealth
	}
	c.Variant = max(0, min(c.Variant, 2))
	soundVariant := strings.TrimPrefix(c.SoundVariant, "minecraft:")
	if soundVariant != "picky" {
		soundVariant = "classic"
	}
	if !c.eggTimerSet {
		c.EggLayTime = 6000 + rand.IntN(6000)
	}
	return &ChickenBehaviour{health: NewHealthManager(c.Health, c.MaxHealth), effects: NewEffectManager(), speed: .25, ageTicks: c.AgeTicks, eggTimer: c.EggLayTime, variant: c.Variant, soundVariant: soundVariant, jockey: c.Jockey}
}

type Chicken struct{ *Ent }
type ChickenBehaviour struct {
	health             *HealthManager
	effects            *EffectManager
	speed              float64
	ageTicks, eggTimer int
	variant            int
	soundVariant       string
	dead, jockey       bool
	breeding           breeding
	animalAI
}

func (c *Chicken) behaviour() *ChickenBehaviour { return c.Behaviour().(*ChickenBehaviour) }
func (c *Chicken) Tick(tx *world.Tx, current int64) {
	c.behaviour().effects.Tick(c, tx)
	c.Ent.Tick(tx, current)
}
func (b *ChickenBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	chicken := &Chicken{Ent: e}
	b.ai.MovementSpeed, b.ai.EyeHeight = b.speed, animalEyeHeight(.644, b.ageTicks < 0)
	mv := animalTick(e, tx, chicken, &b.animalAI, func(m *Mob) {
		registerAnimalGoals(m, &b.animalAI, animalSpec{panic: 1.4, breed: 1, tempt: 1, tempting: chicken.breedingFood, parent: 1.1, stroll: 1}, nil)
	})
	// Chicken.aiStep: a chicken flaps its wings and falls slowly.
	if !e.OnGround() && e.data.Vel[1] < 0 {
		e.data.Vel[1] *= .6
	}
	if b.ageTicks >= 0 && !b.jockey {
		if b.eggTimer--; b.eggTimer <= 0 {
			b.eggTimer = 6000 + rand.IntN(6000)
			egg := item.Egg{}
			switch b.variant {
			case 1:
				egg.Variant = item.EggBlue
			case 2:
				egg.Variant = item.EggBrown
			}
			tx.PlaySound(e.Position(), sound.NativeEvent{SoundType: "plop", EntityType: "minecraft:chicken", ExtraData: -1})
			tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: e.Position()}, item.NewStack(egg, 1)))
			tx.EmitGameEvent(world.GameEventEntityPlace, e.Position(), world.GameEventContext{Source: chicken})
		}
	}
	return mv
}
func (c *Chicken) Health() float64        { return c.behaviour().health.Health() }
func (c *Chicken) MaxHealth() float64     { return c.behaviour().health.MaxHealth() }
func (c *Chicken) SetMaxHealth(v float64) { c.behaviour().health.SetMaxHealth(v) }
func (c *Chicken) Dead() bool             { return c.behaviour().dead || c.Health() <= 0 }

// Panicking reports whether the chicken runs from danger.
func (c *Chicken) Panicking() bool { return c.behaviour().panicking() }
func (c *Chicken) Hurt(damage float64, src world.DamageSource) (float64, bool) {
	if c.Dead() || damage <= 0 || (src.Fire() && c.hasEffect(effect.FireResistance)) {
		return 0, false
	}
	b := c.behaviour()
	damage, ok := b.ai.DamageCooldown(c.tx, damage)
	if !ok {
		return 0, false
	}
	if res, ok := b.effects.Effect(effect.Resistance); ok {
		damage *= effect.Resistance.Multiplier(src, res.Level())
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
func (c *Chicken) Heal(h float64, _ world.HealingSource) float64 {
	if c.Dead() || h <= 0 {
		return 0
	}
	old := c.Health()
	c.behaviour().health.AddHealth(h)
	return c.Health() - old
}
func (c *Chicken) KnockBack(src mgl64.Vec3, force, height float64) {
	knockBack(c, src, force, height)
}
func (c *Chicken) AddEffect(e effect.Effect)    { c.behaviour().effects.Add(e, c) }
func (c *Chicken) RemoveEffect(t effect.Type)   { c.behaviour().effects.Remove(t, c) }
func (c *Chicken) Effects() []effect.Effect     { return c.behaviour().effects.Effects() }
func (c *Chicken) Speed() float64               { return c.behaviour().speed }
func (c *Chicken) SetSpeed(speed float64)       { c.behaviour().speed = speed }
func (c *Chicken) hasEffect(t effect.Type) bool { _, ok := c.behaviour().effects.Effect(t); return ok }
func (c *Chicken) PassengerPosition(passenger world.Entity) mgl64.Vec3 {
	offset := mgl64.Vec3{0, .7, -.1}
	if c.behaviour().ageTicks < 0 {
		offset = mgl64.Vec3{0, .375, 0}
	}
	sin, cos := math.Sincos(-c.Rotation().Yaw() * math.Pi / 180)
	offset[0], offset[2] = offset[0]*cos+offset[2]*sin, offset[2]*cos-offset[0]*sin
	return c.Position().Add(offset).Sub(vehicleAttachmentPoint(passenger))
}
func (c *Chicken) viewAction(a world.EntityAction) {
	vs := c.tx.Viewers(c.Position())
	for _, v := range vs {
		v.ViewEntityAction(c, a)
	}
	c.tx.ReleaseViewers(vs)
}
func (c *Chicken) die(src world.DamageSource) {
	b := c.behaviour()
	if b.dead {
		return
	}
	b.dead = true
	c.viewAction(DeathAction{})
	dropAllDeathLoot(c.tx, c, src)
	_ = c.CloseIn(c.tx)
}

// Baby reports whether the chicken is a chick.
func (c *Chicken) Baby() bool { return c.behaviour().ageTicks < 0 }

// SoundVariant returns the name of the sounds the chicken makes.
func (c *Chicken) SoundVariant() string { return c.behaviour().soundVariant }

// FinalizeSpawn ports Chicken.finalizeSpawn: the biome selects the variant and
// the sound variant is random.
func (c *Chicken) FinalizeSpawn(_ string, r *vanilla.Random) {
	if r == nil {
		return
	}
	b := c.behaviour()
	b.variant = farmVariantIndex(templateFarmTemperature(spawnBiome(c.Ent)), "temperate", "cold", "warm")
	b.soundVariant = [...]string{"classic", "picky"}[r.NextInt(2)]
}

// ClimateVariant returns the name of the climate the chicken is coloured for.
func (c *Chicken) ClimateVariant() string {
	return []string{"temperate", "cold", "warm"}[min(c.behaviour().variant, 2)]
}

// LootComponents exposes the chicken variant to loot predicates.
func (c *Chicken) LootComponents() map[string]any {
	return map[string]any{"minecraft:chicken/variant": "minecraft:" + []string{"temperate", "cold", "warm"}[min(c.behaviour().variant, 2)]}
}

// Interact feeds the chicken seeds to breed it or let a chick grow up
// faster.
func (c *Chicken) Interact(_ *world.Tx, user item.User, ctx *item.UseContext) bool {
	return feedBreeder(c, user, ctx)
}

// InLove reports whether the chicken is in love.
func (c *Chicken) InLove() bool { return c.behaviour().breeding.InLove() }

func (c *Chicken) breedingState() *breeding { return &c.behaviour().breeding }
func (c *Chicken) ageTicks() *int           { return &c.behaviour().ageTicks }

// breedingFood reports whether the stack is in #chicken_food.
func (c *Chicken) breedingFood(stack item.Stack) bool {
	return itemNamed(stack, "minecraft:wheat_seeds", "minecraft:melon_seeds", "minecraft:pumpkin_seeds", "minecraft:beetroot_seeds", "minecraft:torchflower_seeds", "minecraft:pitcher_pod")
}

// breedOffspring ports Chicken.getBreedOffspring: the chick takes the
// variant of either parent.
func (c *Chicken) breedOffspring(_ *world.Tx, partner breeder) *world.EntityHandle {
	return babyAt(c).New(ChickenType, ChickenBehaviourConfig{AgeTicks: babyStartAge, Variant: randomParent(c.behaviour().variant, partner.(*Chicken).behaviour().variant)})
}
