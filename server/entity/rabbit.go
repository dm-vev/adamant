package entity

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// NewRabbit creates a common brown rabbit.
func NewRabbit(o world.EntitySpawnOpts) *world.EntityHandle { return o.New(RabbitType, RabbitConfig{}) }

// RabbitType is the registered Java rabbit type.
var RabbitType rabbitType

type rabbitType struct{}

func (rabbitType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		RabbitConfig{}.Apply(d)
	}
	return &Rabbit{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (rabbitType) EncodeEntity() string { return "minecraft:rabbit" }
func (rabbitType) BBox(e world.Entity) cube.BBox {
	if r, ok := e.(*Rabbit); ok && r.behaviour().age < 0 {
		return cube.Box(-.125, 0, -.125, .125, .25, .125)
	}
	return cube.Box(-.25, 0, -.25, .25, .5, .25)
}
func (rabbitType) DecodeNBT(m map[string]any, d *world.EntityData) {
	s := ReadNativeMobJavaState(m)
	if s == nil {
		s = m
	}
	c := RabbitConfig{Variant: int(nbtconv.Int32(s, "RabbitType")), Age: int(nbtconv.Int32(s, "Age")), MoreCarrotTicks: int(nbtconv.Int32(s, "MoreCarrotTicks"))}
	if a, ok := nativeMobAttribute(m, "minecraft:health"); ok {
		c.Health, c.MaxHealth = a.Current, a.Base
	}
	d.Data = c.New()
	d.Data.(*RabbitBehaviour).breeding.decodeNBT(s)
}
func (rabbitType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*RabbitBehaviour)
	a := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{{Name: "minecraft:health", Base: b.maxHealth, Current: b.health, DefaultMin: 0, DefaultMax: b.maxHealth, Min: 0, Max: b.maxHealth}})
	return MergeNativeMobJavaState(map[string]any{"Attributes": a}, MergeNativeMobNBT(ReadNativeMobJavaState(d.NativeNBT), b.breeding.encodeNBT(map[string]any{"Age": int32(b.age), "RabbitType": int32(b.variant), "MoreCarrotTicks": int32(b.moreCarrotTicks)})))
}

// RabbitConfig controls variant and age.
type RabbitConfig struct {
	Health, MaxHealth             float64
	Variant, Age, MoreCarrotTicks int
}

func (c RabbitConfig) Apply(d *world.EntityData) { d.Data = c.New() }
func (c RabbitConfig) New() *RabbitBehaviour {
	if c.MaxHealth <= 0 {
		c.MaxHealth = 3
	}
	if c.Health <= 0 {
		c.Health = c.MaxHealth
	}
	return &RabbitBehaviour{health: c.Health, maxHealth: c.MaxHealth, variant: max(0, min(c.Variant, 99)), age: c.Age, killer: c.Variant == 99, moreCarrotTicks: c.MoreCarrotTicks, speed: .3, effects: NewEffectManager()}
}

// Rabbit is a small prey animal with source variant, crop-raiding and killer-bunny state.
type Rabbit struct{ *Ent }
type RabbitBehaviour struct {
	effects           *EffectManager
	health, maxHealth float64
	variant, age      int
	dead, killer      bool
	speed             float64
	breeding          breeding
	animalAI
	// The hops of the rabbit: jumpTicks counts up to jumpDuration during a
	// hop and jumpDelayTicks down after landing.
	jumpTicks, jumpDuration, jumpDelayTicks int
	wasOnGround                             bool
	// nextJumpSpeed is the speed modifier RabbitMoveControl hops at.
	nextJumpSpeed float64
	// moreCarrotTicks counts down until the rabbit wants to eat carrots
	// again.
	moreCarrotTicks int
}

func (r *Rabbit) behaviour() *RabbitBehaviour { return r.Behaviour().(*RabbitBehaviour) }
func (r *Rabbit) Tick(tx *world.Tx, t int64) {
	r.behaviour().effects.Tick(r, tx)
	r.Ent.Tick(tx, t)
}
func (b *RabbitBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	rabbit := &Rabbit{Ent: e}
	b.ai.MovementSpeed, b.ai.EyeHeight = b.speed, .59
	if b.age < 0 {
		b.ai.EyeHeight = .39
	}
	mv := animalTick(e, tx, rabbit, &b.animalAI, b.registerGoals)
	// Rabbit.aiStep: a hop ends after its 15 ticks.
	if b.jumpTicks != b.jumpDuration {
		b.jumpTicks++
	} else if b.jumpDuration != 0 {
		b.jumpTicks, b.jumpDuration = 0, 0
		b.ai.jumping = false
	}
	return mv
}

// doHurtTarget ports Mob.doHurtTarget for the killer bunny, which bites for
// 8 damage.
func (b *RabbitBehaviour) doHurtTarget(m *Mob, target world.Entity) bool {
	l, ok := target.(Living)
	if !ok {
		return false
	}
	if _, hurt := l.Hurt(8, AttackDamageSource{Attacker: m.Self}); !hurt {
		return false
	}
	l.KnockBack(m.data.Pos, .4, .4)
	return true
}

// rabbitFood is the rabbit food tag the rabbit's TemptGoal follows.
func rabbitFood(stack item.Stack) bool {
	return itemNamed(stack, "minecraft:carrot", "minecraft:golden_carrot", "minecraft:dandelion")
}

// monsterClass holds the mobs that extend Java's Monster.
var monsterClass = map[string]bool{
	"minecraft:blaze": true, "minecraft:bogged": true, "minecraft:breeze": true, "minecraft:cave_spider": true, "minecraft:creaking": true,
	"minecraft:creeper": true, "minecraft:drowned": true, "minecraft:elder_guardian": true, "minecraft:enderman": true, "minecraft:endermite": true,
	"minecraft:evoker": true, "minecraft:giant": true, "minecraft:guardian": true, "minecraft:husk": true, "minecraft:illusioner": true,
	"minecraft:parched": true, "minecraft:piglin": true, "minecraft:piglin_brute": true, "minecraft:pillager": true, "minecraft:ravager": true,
	"minecraft:silverfish": true, "minecraft:skeleton": true, "minecraft:spider": true, "minecraft:stray": true, "minecraft:vex": true,
	"minecraft:vindicator": true, "minecraft:warden": true, "minecraft:witch": true, "minecraft:wither": true, "minecraft:wither_skeleton": true,
	"minecraft:zoglin": true, "minecraft:zombie": true, "minecraft:zombie_villager": true, "minecraft:zombified_piglin": true,
}

func (r *Rabbit) Health() float64    { return r.behaviour().health }
func (r *Rabbit) MaxHealth() float64 { return r.behaviour().maxHealth }
func (r *Rabbit) SetMaxHealth(v float64) {
	b := r.behaviour()
	b.maxHealth = v
	b.health = math.Min(v, b.health)
}
func (r *Rabbit) Dead() bool { return r.behaviour().dead || r.Health() <= 0 }

// Panicking reports whether the rabbit runs from danger.
func (r *Rabbit) Panicking() bool { return r.behaviour().panicking() }
func (r *Rabbit) Hurt(v float64, src world.DamageSource) (float64, bool) {
	if r.Dead() || v <= 0 {
		return 0, false
	}
	b := r.behaviour()
	var ok bool
	if v, ok = b.ai.DamageCooldown(r.tx, v); !ok {
		return 0, false
	}
	if b.killer {
		// Rabbit.setVariant: the killer bunny wears 8 armour.
		v = damageAfterArmour(v, src, 8, 0)
	}
	if v, ok = mobEffectDamage(b.effects, v, src); !ok {
		return 0, false
	}
	b.health -= v
	b.ai.Hurt(src)
	resetLove(r)
	r.viewAction(HurtAction{})
	onMobHurt(r.tx, r)
	if r.Dead() {
		b.dead = true
		r.viewAction(DeathAction{})
		dropAllDeathLoot(r.tx, r, src)
		_ = r.CloseIn(r.tx)
	}
	return v, true
}
func (r *Rabbit) Heal(v float64, _ world.HealingSource) float64 {
	if r.Dead() || v <= 0 {
		return 0
	}
	b := r.behaviour()
	old := b.health
	b.health = math.Min(b.maxHealth, b.health+v)
	return b.health - old
}
func (r *Rabbit) KnockBack(src mgl64.Vec3, f, h float64) {
	knockBack(r, src, f, h)
}
func (r *Rabbit) viewAction(a world.EntityAction) {
	vs := r.tx.Viewers(r.Position())
	for _, v := range vs {
		v.ViewEntityAction(r, a)
	}
	r.tx.ReleaseViewers(vs)
}
func (r *Rabbit) Variant() int { return r.behaviour().variant }
func (r *Rabbit) Killer() bool { return r.behaviour().killer }

// Interact feeds the rabbit to breed it or let a kit grow up faster.
func (r *Rabbit) Interact(_ *world.Tx, u item.User, c *item.UseContext) bool {
	return feedBreeder(r, u, c)
}

// InLove reports whether the rabbit is in love.
func (r *Rabbit) InLove() bool { return r.behaviour().breeding.InLove() }

func (r *Rabbit) breedingState() *breeding { return &r.behaviour().breeding }
func (r *Rabbit) ageTicks() *int           { return &r.behaviour().age }

// breedingFood reports whether the stack is in #rabbit_food.
func (r *Rabbit) breedingFood(stack item.Stack) bool {
	return itemNamed(stack, "minecraft:carrot", "minecraft:golden_carrot", "minecraft:dandelion")
}

// breedOffspring ports Rabbit.getBreedOffspring: the kit takes the variant
// of either parent, or 1 in 20 times the variant of the biome it is born in.
func (r *Rabbit) breedOffspring(tx *world.Tx, partner breeder) *world.EntityHandle {
	variant := rabbitBiomeVariant(tx, cube.PosFromVec3(r.Position()))
	if rand.IntN(20) != 0 {
		variant = randomParent(partner.(*Rabbit).behaviour().variant, r.behaviour().variant)
	}
	return babyAt(r).New(RabbitType, RabbitConfig{Age: babyStartAge, Variant: variant})
}

// rabbitBiomeVariant ports Rabbit.getRandomRabbitVariant: white or
// splotched rabbits in #spawns_white_rabbits, gold ones in
// #spawns_gold_rabbits, and brown, salt and pepper or black ones elsewhere.
func rabbitBiomeVariant(tx *world.Tx, pos cube.Pos) int {
	return rabbitVariantIn(world.JavaBiomeName(tx.Biome(pos)), rand.IntN(100))
}

// rabbitVariantIn returns the variant of a rabbit spawning in a biome for a
// roll of nextInt(100).
func rabbitVariantIn(biome string, roll int) int {
	if white, _ := gamedata.BiomeTagContains("spawns_white_rabbits", biome); white {
		if roll < 80 {
			return 1
		}
		return 3
	}
	if gold, _ := gamedata.BiomeTagContains("spawns_gold_rabbits", biome); gold {
		return 4
	}
	switch {
	case roll < 50:
		return 0
	case roll < 90:
		return 5
	}
	return 2
}

var _ Interactable = (*Rabbit)(nil)

// FinalizeSpawn chooses the biome-selected rabbit variant; rare hostile variant has its own goal state.
func (r *Rabbit) FinalizeSpawn(reason string, j *vanilla.Random) {
	if j == nil {
		return
	}
	b := r.behaviour()
	group := r.H().SpawnGroup()
	if group != nil {
		if v, ok := group.Data.(int); ok {
			b.variant = v
			b.killer = v == 99
			return
		}
	}
	if reason == "jockey" {
		b.variant = 99
		b.killer = true
	} else {
		b.variant = rabbitVariantIn(spawnBiome(r.Ent), int(j.NextInt(100)))
	}
	if group != nil {
		group.Data = b.variant
	}
}
