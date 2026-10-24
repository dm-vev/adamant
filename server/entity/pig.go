package entity

import (
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// NewPig creates an adult, unsaddled Java pig.
func NewPig(o world.EntitySpawnOpts) *world.EntityHandle { return o.New(PigType, PigConfig{}) }

// PigType is the registered Java pig type.
var PigType pigType

type pigType struct{}

func (pigType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		PigConfig{}.Apply(d)
	}
	return &Pig{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (pigType) EncodeEntity() string { return "minecraft:pig" }
func (pigType) BBox(e world.Entity) cube.BBox {
	if p, ok := e.(*Pig); ok && p.behaviour().age < 0 {
		return cube.Box(-.225, 0, -.225, .225, .5, .225)
	}
	return cube.Box(-.45, 0, -.45, .45, .9, .45)
}
func (pigType) DecodeNBT(m map[string]any, d *world.EntityData) {
	s := ReadNativeMobJavaState(m)
	if s == nil {
		s = m
	}
	c := PigConfig{Age: int(nbtconv.Int32(s, "Age")), Saddled: nbtconv.Bool(s, "Saddle")}
	if v, ok := s["variant"].(string); ok {
		c.Variant = pigVariant(v)
	} else if v, ok := m["Variant"]; ok {
		c.Variant = int(nbtconv.Int32(map[string]any{"Variant": v}, "Variant"))
	}
	if v, ok := s["sound_variant"].(string); ok {
		c.SoundVariant = v
	}
	if a, ok := nativeMobAttribute(m, "minecraft:health"); ok {
		c.Health, c.MaxHealth = a.Current, a.Base
	}
	if v, ok := s["Health"]; ok {
		c.Health = float64(nbtconv.Float32(map[string]any{"Health": v}, "Health"))
	}
	if v, ok := s["MaxHealth"]; ok {
		c.MaxHealth = float64(nbtconv.Float32(map[string]any{"MaxHealth": v}, "MaxHealth"))
	}
	d.Data = c.New()
	d.Data.(*PigBehaviour).breeding.decodeNBT(s)
}
func pigVariant(value string) int {
	switch strings.TrimPrefix(value, "minecraft:") {
	case "warm":
		return 1
	case "cold":
		return 2
	default:
		return 0
	}
}
func (pigType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*PigBehaviour)
	a := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{{Name: "minecraft:health", Base: b.maxHealth, Current: b.health, DefaultMin: 0, DefaultMax: b.maxHealth, Min: 0, Max: b.maxHealth}})
	native := MergeNativeMobNBT(d.NativeNBT, map[string]any{"Attributes": a, "Variant": int32(b.variant)})
	return MergeNativeMobJavaState(native, MergeNativeMobNBT(ReadNativeMobJavaState(d.NativeNBT), b.breeding.encodeNBT(map[string]any{"Age": int32(b.age), "Saddle": byte(boolByte(b.saddled)), "variant": "minecraft:" + [...]string{"temperate", "warm", "cold"}[b.variant], "sound_variant": "minecraft:" + b.soundVariant})))
}

// PigConfig supplies a pig's persistent Java state.
type PigConfig struct {
	Health, MaxHealth float64
	Age               int
	Saddled           bool
	Variant           int
	SoundVariant      string
}

func (c PigConfig) Apply(d *world.EntityData) { d.Data = c.New() }
func (c PigConfig) New() *PigBehaviour {
	if c.MaxHealth <= 0 {
		c.MaxHealth = 10
	}
	if c.Health <= 0 {
		c.Health = c.MaxHealth
	}
	sv := strings.TrimPrefix(c.SoundVariant, "minecraft:")
	if sv != "big" && sv != "mini" {
		sv = "classic"
	}
	return &PigBehaviour{health: c.Health, maxHealth: c.MaxHealth, age: c.Age, saddled: c.Saddled, variant: max(0, min(c.Variant, 2)), soundVariant: sv, speed: .25, effects: NewEffectManager()}
}

// Pig is a concrete passive pig with growth, saddle and ridden steering state.
type Pig struct{ *Ent }
type PigBehaviour struct {
	effects           *EffectManager
	health, maxHealth float64
	age, variant      int
	soundVariant      string
	saddled, dead     bool
	speed             float64
	passenger         *world.EntityHandle
	lastLightningBolt string
	breeding          breeding
	animalAI
	steering itemBasedSteering
}

func (p *PigBehaviour) Variant() int32 { return int32(p.variant) }
func (p *PigBehaviour) Baby() bool     { return p.age < 0 }

// SoundVariant returns the name of the sounds the pig makes.
func (p *PigBehaviour) SoundVariant() string { return p.soundVariant }

// ClimateVariant returns the name of the climate the pig is coloured for.
func (p *PigBehaviour) ClimateVariant() string {
	return [...]string{"temperate", "warm", "cold"}[min(p.variant, 2)]
}

func (p *Pig) behaviour() *PigBehaviour { return p.Behaviour().(*PigBehaviour) }
func (p *Pig) Tick(tx *world.Tx, t int64) {
	p.behaviour().effects.Tick(p, tx)
	p.Ent.Tick(tx, t)
	b := p.behaviour()
	if b.passenger != nil {
		if rider, ok := b.passenger.Entity(tx); ok {
			if mover, ok := rider.(interface{ Teleport(mgl64.Vec3) }); ok {
				mover.Teleport(p.Position().Add(mgl64.Vec3{0, .9, 0}))
			}
		} else {
			b.passenger = nil
		}
	}
}
func (b *PigBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	pig := &Pig{Ent: e}
	b.ai.MovementSpeed, b.ai.EyeHeight = b.speed, animalEyeHeight(.765, b.age < 0)
	return animalTick(e, tx, pig, &b.animalAI, func(m *Mob) { b.registerGoals(m, pig) })
}

// registerGoals ports Pig.registerGoals: a pig follows players holding a
// carrot on a stick as it follows those holding its food.
func (b *PigBehaviour) registerGoals(m *Mob, pig *Pig) {
	ai := m.AI
	b.panic = NewPanicGoal(1.25)
	ai.Goals.Add(0, NewFloatGoal(m))
	ai.Goals.Add(1, b.panic)
	ai.Goals.Add(3, NewBreedGoal(1))
	ai.Goals.Add(4, NewTemptGoal(1.2, func(s item.Stack) bool { return itemNamed(s, "minecraft:carrot_on_a_stick") }, false))
	ai.Goals.Add(4, NewTemptGoal(1.2, pig.breedingFood, false))
	ai.Goals.Add(5, NewFollowParentGoal(1.1))
	ai.Goals.Add(6, NewWaterAvoidingRandomStrollGoal(1))
	ai.Goals.Add(7, NewLookAtPlayerGoal(6))
	ai.Goals.Add(8, NewRandomLookAroundGoal())
}

// travel ports LivingEntity.travelRidden for a pig a player steers with a
// carrot on a stick: Pig.tickRidden turns the pig the way the player looks,
// and the pig walks forward at 0.225 times its speed, faster while boosted.
func (b *PigBehaviour) travel(m *Mob, _ mgl64.Vec3) bool {
	c := (&Pig{Ent: m.Ent}).controller(m.tx)
	if c == nil || !m.Alive() {
		return false
	}
	rot := c.Rotation()
	m.data.Rot = cube.Rotation{rot.Yaw(), rot.Pitch() * .5}
	m.AI.headYaw, m.AI.bodyYaw = rot.Yaw(), rot.Yaw()
	b.steering.tickBoost()
	m.AI.travelRidden(m, mgl64.Vec3{0, 0, 1}, b.speed*.225*b.steering.boostFactor())
	return true
}

// itemBasedSteering ports Java's ItemBasedSteering: the boost of a mob a
// player steers with an item on a stick, boostTime counting up to
// boostTimeTotal while it lasts.
type itemBasedSteering struct {
	boosting                  bool
	boostTime, boostTimeTotal int
}

// boost ports ItemBasedSteering.boost: the mob boosts for 7 to 49 seconds,
// unless it is boosted already.
func (s *itemBasedSteering) boost(r *vanilla.Random) bool {
	if s.boosting {
		return false
	}
	s.boosting, s.boostTime = true, 0
	s.boostTimeTotal = int(r.NextInt(841)) + 140
	return true
}

// tickBoost ports ItemBasedSteering.tickBoost.
func (s *itemBasedSteering) tickBoost() {
	if s.boosting {
		if s.boostTime++; s.boostTime-1 > s.boostTimeTotal {
			s.boosting = false
		}
	}
}

// boostFactor ports ItemBasedSteering.boostFactor.
func (s *itemBasedSteering) boostFactor() float64 {
	if !s.boosting {
		return 1
	}
	return 1 + 1.15*float64(vanilla.Sin(float64(float32(s.boostTime)/float32(s.boostTimeTotal)*math.Pi)))
}

// Boost ports Pig.boost: a carrot on a stick used by the player steering
// the pig boosts it, unless it is boosted already.
func (p *Pig) Boost(_ *world.Tx, rider item.User) bool {
	b := p.behaviour()
	if c := p.controller(p.tx); c == nil || c.H() != rider.H() {
		return false
	}
	return b.steering.boost(b.ai.Mob(p.Ent, p).Random())
}

// controller ports Pig.getControllingPassenger: a player riding a saddled
// pig steers it while holding a carrot on a stick.
func (p *Pig) controller(tx *world.Tx) world.Entity {
	b := p.behaviour()
	if !b.saddled || b.passenger == nil {
		return nil
	}
	e, ok := b.passenger.Entity(tx)
	if !ok || !isPlayerEntity(e) {
		return nil
	}
	u, ok := e.(item.User)
	if !ok {
		return nil
	}
	main, off := u.HeldItems()
	if !itemNamed(main, "minecraft:carrot_on_a_stick") && !itemNamed(off, "minecraft:carrot_on_a_stick") {
		return nil
	}
	return e
}
func (p *Pig) Health() float64    { return p.behaviour().health }
func (p *Pig) MaxHealth() float64 { return p.behaviour().maxHealth }
func (p *Pig) SetMaxHealth(v float64) {
	b := p.behaviour()
	b.maxHealth = v
	b.health = math.Min(v, b.health)
}
func (p *Pig) Dead() bool { return p.behaviour().dead || p.Health() <= 0 }

// Panicking reports whether the pig runs from danger.
func (p *Pig) Panicking() bool { return p.behaviour().panicking() }
func (p *Pig) Hurt(v float64, s world.DamageSource) (float64, bool) {
	if p.Dead() || v <= 0 {
		return 0, false
	}
	if bolt, ok := s.(LightningDamageSource); ok {
		b := p.behaviour()
		if bolt.BoltUUID == "" || bolt.BoltUUID != b.lastLightningBolt {
			b.lastLightningBolt = bolt.BoltUUID
			opts := world.EntitySpawnOpts{Position: p.Position(), Rotation: p.Rotation()}
			p.tx.AddEntity(NewZombifiedPiglin(opts))
			b.dead = true
			_ = p.CloseIn(p.tx)
			return 0, true
		}
	}
	b := p.behaviour()
	var ok bool
	if v, ok = b.ai.DamageCooldown(p.tx, v); !ok {
		return 0, false
	}
	if v, ok = mobEffectDamage(b.effects, v, s); !ok {
		return 0, false
	}
	b.health -= v
	b.ai.Hurt(s)
	resetLove(p)
	p.viewAction(HurtAction{})
	onMobHurt(p.tx, p)
	if p.Dead() {
		b.dead = true
		p.viewAction(DeathAction{})
		dropAllDeathLoot(p.tx, p, s)
		_ = p.CloseIn(p.tx)
	}
	return v, true
}
func (p *Pig) Heal(v float64, _ world.HealingSource) float64 {
	if p.Dead() || v <= 0 {
		return 0
	}
	b := p.behaviour()
	old := b.health
	b.health = math.Min(b.maxHealth, b.health+v)
	return b.health - old
}
func (p *Pig) KnockBack(src mgl64.Vec3, f, h float64) {
	d := p.Position().Sub(src)
	d[1] = 0
	if d.Len() > 0 {
		d = d.Normalize().Mul(f)
	}
	d[1] = h
	p.SetVelocity(d)
}
func (p *Pig) viewAction(a world.EntityAction) {
	vs := p.tx.Viewers(p.Position())
	for _, v := range vs {
		v.ViewEntityAction(p, a)
	}
	p.tx.ReleaseViewers(vs)
}

// dropCustomDeathLoot drops a pig's saddle, a preserved equipment item.
func (p *Pig) dropCustomDeathLoot(bool, int) {
	if p.behaviour().saddled {
		dropMobEquipment(p.tx, p, item.NewStack(item.Saddle{}, 1), 2, false, 0)
	}
}
func (p *Pig) Age() int      { return p.behaviour().age }
func (p *Pig) Saddled() bool { return p.behaviour().saddled }
func (p *Pig) Saddle() bool {
	b := p.behaviour()
	if p.Dead() || b.age < 0 || b.saddled {
		return false
	}
	b.saddled = true
	return true
}

// ControlsVehicle reports if the passenger steers the pig.
func (p *Pig) ControlsVehicle(passenger world.Entity) bool {
	c := p.controller(p.tx)
	return c != nil && passenger != nil && c.H() == passenger.H()
}

// RiderSeat returns the seat of Bedrock's pig, which seats riders lower
// once saddled.
func (p *Pig) RiderSeat(rider world.Entity) world.RiderSeat {
	y := 0.7
	if p.behaviour().saddled {
		y = 0.63
	}
	return seat{pos: mgl64.Vec3{0, y, 0}}.riderSeat(rider)
}

// Mount seats passenger on the pig as Entity.startRiding does when forced,
// as /ride does: unlike riding it by hand, which Interact allows only once
// saddled, the pig need not wear a saddle.
func (p *Pig) Mount(tx *world.Tx, passenger world.Entity) bool {
	if passenger == nil || p.Dead() {
		return false
	}
	r, ok := passenger.(Rider)
	b := p.behaviour()
	if !ok || r.Riding() != nil || b.passenger != nil {
		return false
	}
	b.passenger = passenger.H()
	seatRider(r, p)
	p.viewLink(tx, b.passenger, true)
	if t, ok := passenger.(interface{ Teleport(mgl64.Vec3) }); ok {
		t.Teleport(p.Position().Add(mgl64.Vec3{0, .9, 0}))
	}
	return true
}
func (p *Pig) Dismount(tx *world.Tx, passenger world.Entity) {
	b := p.behaviour()
	if passenger == nil || b.passenger != passenger.H() {
		return
	}
	if r, ok := passenger.(Rider); ok {
		r.SetRiding(nil)
	}
	p.viewLink(tx, b.passenger, false)
	b.passenger = nil
}
func (p *Pig) Links() []Link {
	if h := p.behaviour().passenger; h != nil {
		return []Link{{Ridden: p.H(), Rider: h, Type: LinkPassenger}}
	}
	return nil
}
func (p *Pig) viewLink(tx *world.Tx, h *world.EntityHandle, on bool) {
	vs := tx.Viewers(p.Position())
	for _, v := range vs {
		if lv, ok := v.(interface {
			ViewEntityLinkType(*world.EntityHandle, *world.EntityHandle, LinkType, bool)
		}); ok {
			lv.ViewEntityLinkType(p.H(), h, LinkPassenger, on)
		} else if lv, ok := v.(interface {
			ViewEntityLink(*world.EntityHandle, *world.EntityHandle, bool)
		}); ok {
			lv.ViewEntityLink(p.H(), h, on)
		}
	}
	tx.ReleaseViewers(vs)
}

// Interact ports Pig.mobInteract: a saddled pig is ridden unless fed or the
// player sneaks, food breeds it or lets a piglet grow, and a saddle saddles
// it.
func (p *Pig) Interact(tx *world.Tx, u item.User, c *item.UseContext) bool {
	if u == nil {
		return false
	}
	b := p.behaviour()
	held, _ := u.HeldItems()
	sneaking := false
	if s, ok := u.(interface{ Sneaking() bool }); ok {
		sneaking = s.Sneaking()
	}
	if !p.breedingFood(held) && b.saddled && b.passenger == nil && !sneaking {
		return p.Mount(tx, u)
	}
	if c != nil && feedBreeder(p, u, c) {
		return true
	}
	if _, ok := held.Item().(item.Saddle); ok && c != nil && p.Saddle() {
		c.SubtractFromCount(1)
		return true
	}
	return false
}

// InLove reports whether the pig is in love.
func (p *Pig) InLove() bool { return p.behaviour().breeding.InLove() }

func (p *Pig) breedingState() *breeding { return &p.behaviour().breeding }
func (p *Pig) ageTicks() *int           { return &p.behaviour().age }

func (p *Pig) breedingFood(stack item.Stack) bool {
	return itemNamed(stack, "minecraft:carrot", "minecraft:potato", "minecraft:beetroot")
}

// breedOffspring ports Pig.getBreedOffspring: the piglet takes the variant of
// either parent.
func (p *Pig) breedOffspring(_ *world.Tx, partner breeder) *world.EntityHandle {
	return babyAt(p).New(PigType, PigConfig{Age: babyStartAge, Variant: randomParent(p.behaviour().variant, partner.(*Pig).behaviour().variant)})
}

// FinalizeSpawn ports Pig.finalizeSpawn: the biome selects the variant and
// the sound variant is random.
func (p *Pig) FinalizeSpawn(_ string, r *vanilla.Random) {
	if r == nil {
		return
	}
	b := p.behaviour()
	b.age = 0
	b.variant = farmVariantIndex(templateFarmTemperature(spawnBiome(p.Ent)), "temperate", "warm", "cold")
	sounds := templateSpawnRegistry.PigSounds
	b.soundVariant = strings.TrimPrefix(sounds[r.NextInt(int32(len(sounds)))], "minecraft:")
}

// farmVariantIndex returns the index of a climate in the order a farm animal
// keeps its variants.
func farmVariantIndex(climate string, order ...string) int {
	for i, c := range order {
		if c == climate {
			return i
		}
	}
	return 0
}

var _ VehicleController = (*Pig)(nil)
var _ PassengerDismounter = (*Pig)(nil)
var _ Linker = (*Pig)(nil)
var _ Interactable = (*Pig)(nil)
