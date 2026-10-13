package entity

import (
	"math"
	"math/rand/v2"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
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
	return &PigBehaviour{health: c.Health, maxHealth: c.MaxHealth, age: c.Age, saddled: c.Saddled, variant: max(0, min(c.Variant, 2)), soundVariant: sv, speed: .25, mc: &MovementComputer{Gravity: .08, Drag: .02, DragBeforeGravity: true}, next: 40 + rand.IntN(81), effects: NewEffectManager()}
}

// Pig is a concrete passive pig with growth, saddle and ridden steering state.
type Pig struct{ *Ent }
type PigBehaviour struct {
	effects           *EffectManager
	health, maxHealth float64
	age, variant      int
	soundVariant      string
	saddled, dead     bool
	speed, steer      float64
	passenger         *world.EntityHandle
	lastLightningBolt string
	navigator         Navigator
	next              int
	mc                *MovementComputer
	breeding          breeding
	panic             panicState
}

func (p *PigBehaviour) Variant() int32  { return int32(p.variant) }
func (p *PigBehaviour) Baby() bool      { return p.age < 0 }
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
	if b.next > 0 {
		b.next--
	}
	panicking := b.panic.run(e, tx, &b.navigator)
	speed := b.speed
	if partner, ok := tickBreeding(&Pig{Ent: e}, tx); panicking {
		speed = groundSpeed(.25, 1.25)
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
	rot, vel := e.data.Rot, e.data.Vel
	if b.steer != 0 {
		rot[0] += b.steer * 4
		d := rot.Vec3()
		vel[0], vel[2] = d[0]*b.speed, d[2]*b.speed
	} else if target, ok := b.navigator.Tick(e.data.Pos, navigatorTick(e), .9); ok {
		d := target.Sub(e.data.Pos)
		d[1] = 0
		if d.Len() < .25 {
			b.navigator.Index++
		} else {
			d = d.Normalize()
			vel[0], vel[2] = d[0]*speed, d[2]*speed
		}
	} else {
		vel[0], vel[2] = 0, 0
	}
	m := b.mc.TickMovement(e, e.data.Pos, vel, e.data.Rot, rot, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	return m
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
func (p *Pig) Panicking() bool { return p.behaviour().panic.active }
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
	var ok bool
	if v, ok = mobEffectDamage(p.behaviour().effects, v, s); !ok {
		return 0, false
	}
	b := p.behaviour()
	b.health -= v
	b.panic.hurt(s, p.tx.CurrentTick())
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
func (p *Pig) SetVehicleInput(strafe, forward float64) {
	b := p.behaviour()
	if b.saddled {
		b.steer = math.Max(-1, math.Min(1, forward))
	}
}
func (p *Pig) SetVehicleInputFrom(passenger world.Entity, strafe, forward float64) {
	if p.ControlsVehicle(passenger) {
		p.SetVehicleInput(strafe, forward)
		r := p.Rotation()
		r[0] = passenger.Rotation()[0]
		p.data.Rot = r
	}
}
func (p *Pig) ControlsVehicle(passenger world.Entity) bool {
	r, ok := passenger.(Rider)
	return ok && p.behaviour().saddled && r.Riding() == p.H()
}
func (p *Pig) Mount(tx *world.Tx, passenger world.Entity) bool {
	if passenger == nil || p.Dead() {
		return false
	}
	r, ok := passenger.(Rider)
	b := p.behaviour()
	if !ok || r.Riding() != nil || !b.saddled || b.passenger != nil {
		return false
	}
	b.passenger = passenger.H()
	r.SetRiding(p.H())
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
func (p *Pig) FinalizeSpawn(_ string, r *java26.Random) {
	if r != nil {
		p.behaviour().age = 0
	}
}

var _ VehicleInput = (*Pig)(nil)
var _ VehicleController = (*Pig)(nil)
var _ PassengerDismounter = (*Pig)(nil)
var _ Linker = (*Pig)(nil)
var _ Interactable = (*Pig)(nil)
