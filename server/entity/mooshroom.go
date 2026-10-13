package entity

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// NewMooshroom creates a red, adult mooshroom.
func NewMooshroom(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(MooshroomType, MooshroomConfig{})
}

// MooshroomType is the registered Java mushroom cow entity type.
var MooshroomType mooshroomType

type mooshroomType struct{}

func (mooshroomType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		MooshroomConfig{}.Apply(d)
	}
	return &Mooshroom{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (mooshroomType) EncodeEntity() string { return "minecraft:mooshroom" }
func (mooshroomType) BBox(e world.Entity) cube.BBox {
	if m, ok := e.(*Mooshroom); ok && m.behaviour().age < 0 {
		return cube.Box(-.225, 0, -.225, .225, .7, .225)
	}
	return cube.Box(-.45, 0, -.45, .45, 1.4, .45)
}
func (mooshroomType) DecodeNBT(m map[string]any, d *world.EntityData) {
	s := ReadNativeMobJavaState(m)
	if s == nil {
		s = m
	}
	c := MooshroomConfig{Variant: 0, Age: int(nbtconv.Int32(s, "Age")), LastLightningBolt: nbtconv.String(s, "LastLightningBolt")}
	if n, ok := s["Type"].(string); ok && n == "brown" {
		c.Variant = 1
	}
	if a, ok := nativeMobAttribute(m, "minecraft:health"); ok {
		c.Health, c.MaxHealth = a.Current, a.Base
	}
	d.Data = c.New()
	d.Data.(*MooshroomBehaviour).breeding.decodeNBT(s)
}
func (mooshroomType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*MooshroomBehaviour)
	a := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{{Name: "minecraft:health", Base: b.maxHealth, Current: b.health, DefaultMin: 0, DefaultMax: b.maxHealth, Min: 0, Max: b.maxHealth}})
	variant := "red"
	if b.variant == 1 {
		variant = "brown"
	}
	return MergeNativeMobJavaState(map[string]any{"Attributes": a}, MergeNativeMobNBT(ReadNativeMobJavaState(d.NativeNBT), b.breeding.encodeNBT(map[string]any{"Age": int32(b.age), "Type": variant, "LastLightningBolt": b.lastLightningBolt})))
}

// MooshroomConfig configures variant and age.
type MooshroomConfig struct {
	Health, MaxHealth float64
	Variant, Age      int
	LastLightningBolt string
}

func (c MooshroomConfig) Apply(d *world.EntityData) { d.Data = c.New() }
func (c MooshroomConfig) New() *MooshroomBehaviour {
	if c.MaxHealth <= 0 {
		c.MaxHealth = 10
	}
	if c.Health <= 0 {
		c.Health = c.MaxHealth
	}
	return &MooshroomBehaviour{health: c.Health, maxHealth: c.MaxHealth, variant: max(0, min(c.Variant, 1)), age: c.Age, lastLightningBolt: c.LastLightningBolt, speed: .2, next: 40 + rand.IntN(81), mc: &MovementComputer{Gravity: .08, Drag: .02, DragBeforeGravity: true}, effects: NewEffectManager()}
}

// Mooshroom is a separately modeled mushroom-field bovine with red/brown type and bowl milking.
type Mooshroom struct{ *Ent }
type MooshroomBehaviour struct {
	effects            *EffectManager
	health, maxHealth  float64
	variant, age, next int
	dead               bool
	lastLightningBolt  string
	speed              float64
	navigator          Navigator
	mc                 *MovementComputer
	breeding           breeding
	panic              panicState
}

func (m *Mooshroom) behaviour() *MooshroomBehaviour { return m.Behaviour().(*MooshroomBehaviour) }
func (m *Mooshroom) Tick(tx *world.Tx, t int64) {
	m.behaviour().effects.Tick(m, tx)
	m.Ent.Tick(tx, t)
}
func (b *MooshroomBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if b.next > 0 {
		b.next--
	}
	panicking := b.panic.run(e, tx, &b.navigator)
	speed := b.speed
	if partner, ok := tickBreeding(&Mooshroom{Ent: e}, tx); panicking {
		// AbstractCow's PanicGoal runs at twice the movement speed.
		speed = groundSpeed(.2, 2)
	} else if ok {
		navigateToPartner(&b.navigator, e, tx, partner)
	} else if b.navigator.Index >= len(b.navigator.Path) && b.next == 0 {
		b.next = 40 + rand.IntN(81)
		start := cube.PosFromVec3(e.Position())
		for range 4 {
			goal := start.Add(cube.Pos{rand.IntN(13) - 6, 0, rand.IntN(13) - 6})
			if b.navigator.MoveTo(tx, start, goal, GroundProfile{MaxVisitedNodes: 128}, navigatorTick(e)) {
				break
			}
		}
	}
	rot, v := e.data.Rot, e.data.Vel
	if target, ok := b.navigator.Tick(e.data.Pos, navigatorTick(e), .9); ok {
		d := target.Sub(e.data.Pos)
		d[1] = 0
		if d.Len() < .25 {
			b.navigator.Index++
		} else {
			d = d.Normalize()
			v[0], v[2] = d[0]*speed, d[2]*speed
		}
	} else {
		v[0], v[2] = 0, 0
	}
	m := b.mc.TickMovement(e, e.data.Pos, v, e.data.Rot, rot, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	return m
}
func (m *Mooshroom) Health() float64    { return m.behaviour().health }
func (m *Mooshroom) MaxHealth() float64 { return m.behaviour().maxHealth }
func (m *Mooshroom) SetMaxHealth(v float64) {
	b := m.behaviour()
	b.maxHealth = v
	b.health = math.Min(v, b.health)
}
func (m *Mooshroom) Dead() bool { return m.behaviour().dead || m.Health() <= 0 }

// Panicking reports whether the mooshroom runs from danger.
func (m *Mooshroom) Panicking() bool { return m.behaviour().panic.active }
func (m *Mooshroom) Hurt(v float64, src world.DamageSource) (float64, bool) {
	if m.Dead() || v <= 0 {
		return 0, false
	}
	if bolt, ok := src.(LightningDamageSource); ok {
		b := m.behaviour()
		if bolt.BoltUUID != b.lastLightningBolt {
			b.lastLightningBolt = bolt.BoltUUID
			b.variant = 1 - b.variant
			return 0, true
		}
	}
	var ok bool
	if v, ok = mobEffectDamage(m.behaviour().effects, v, src); !ok {
		return 0, false
	}
	b := m.behaviour()
	b.health -= v
	b.panic.hurt(src, m.tx.CurrentTick())
	resetLove(m)
	m.viewAction(HurtAction{})
	onMobHurt(m.tx, m)
	if m.Dead() {
		b.dead = true
		m.viewAction(DeathAction{})
		dropAllDeathLoot(m.tx, m, src)
		_ = m.CloseIn(m.tx)
	}
	return v, true
}
func (m *Mooshroom) Heal(v float64, _ world.HealingSource) float64 {
	if m.Dead() || v <= 0 {
		return 0
	}
	b := m.behaviour()
	old := b.health
	b.health = math.Min(b.maxHealth, b.health+v)
	return b.health - old
}
func (m *Mooshroom) KnockBack(src mgl64.Vec3, f, h float64) {
	d := m.Position().Sub(src)
	d[1] = 0
	if d.Len() > 0 {
		d = d.Normalize().Mul(f)
	}
	d[1] = h
	m.SetVelocity(d)
}
func (m *Mooshroom) viewAction(a world.EntityAction) {
	vs := m.tx.Viewers(m.Position())
	for _, v := range vs {
		v.ViewEntityAction(m, a)
	}
	m.tx.ReleaseViewers(vs)
}
func (m *Mooshroom) Variant() int { return m.behaviour().variant }

// Milk allows source bowl and bucket harvesting only for adult animals.
func (m *Mooshroom) Milk(_ *world.Tx) bool { return !m.Dead() && m.behaviour().age >= 0 }
func (m *Mooshroom) MilkSound() world.Sound {
	return sound.Custom{Name: "entity.mooshroom.milk", Volume: 1, Pitch: 1}
}

// Interact fills a bowl with stew from an adult, or feeds the mooshroom wheat
// to breed it or let a calf grow up faster.
func (m *Mooshroom) Interact(_ *world.Tx, u item.User, ctx *item.UseContext) bool {
	if u == nil || ctx == nil {
		return false
	}
	held, _ := u.HeldItems()
	if _, ok := held.Item().(item.Bowl); ok && m.Milk(m.tx) {
		ctx.SubtractFromCount(1)
		ctx.NewItem = item.NewStack(item.MushroomStew{}, 1)
		return true
	}
	return feedBreeder(m, u, ctx)
}

// InLove reports whether the mooshroom is in love.
func (m *Mooshroom) InLove() bool { return m.behaviour().breeding.InLove() }

func (m *Mooshroom) breedingState() *breeding { return &m.behaviour().breeding }
func (m *Mooshroom) ageTicks() *int           { return &m.behaviour().age }

func (m *Mooshroom) breedingFood(stack item.Stack) bool { return itemNamed(stack, "minecraft:wheat") }

// breedOffspring ports MushroomCow.getOffspringVariant: the calf takes the
// type of either parent, and parents of one type have a 1 in 1024 chance of
// a calf of the other type.
func (m *Mooshroom) breedOffspring(_ *world.Tx, partner breeder) *world.EntityHandle {
	own, other := m.behaviour().variant, partner.(*Mooshroom).behaviour().variant
	variant := randomParent(own, other)
	if own == other && rand.IntN(1024) == 0 {
		variant = 1 - own
	}
	return babyAt(m).New(MooshroomType, MooshroomConfig{Age: babyStartAge, Variant: variant})
}
func (m *Mooshroom) Shear(tx *world.Tx) item.Stack {
	if m.Dead() {
		return item.Stack{}
	}
	name := "red"
	if m.behaviour().variant == 1 {
		name = "brown"
	}
	stacks, err := loot.Generate("minecraft:shearing/mooshroom/"+name, rand.Int64(), loot.Context{Entity: "minecraft:mooshroom", Origin: m.Position()})
	opts := world.EntitySpawnOpts{Position: m.Position(), Rotation: m.Rotation()}
	tx.AddEntity(NewCow(opts))
	_ = m.CloseIn(tx)
	if err != nil || len(stacks) == 0 {
		return item.Stack{}
	}
	return stacks[0]
}

// FinalizeSpawn chooses Java's rare brown mushroom-cow variant.
func (m *Mooshroom) FinalizeSpawn(_ string, r *java26.Random) {
	if r != nil && r.NextInt(1024) == 0 {
		m.behaviour().variant = 1
	}
}
