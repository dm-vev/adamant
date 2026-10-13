package entity

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// NewArmadillo creates an unrolled adult armadillo.
func NewArmadillo(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(ArmadilloType, ArmadilloConfig{})
}

// ArmadilloType is the registered Java armadillo type.
var ArmadilloType armadilloType

type armadilloType struct{}

func (armadilloType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		ArmadilloConfig{}.Apply(d)
	}
	return &Armadillo{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (armadilloType) EncodeEntity() string { return "minecraft:armadillo" }
func (armadilloType) BBox(e world.Entity) cube.BBox {
	if a, ok := e.(*Armadillo); ok && a.behaviour().age < 0 {
		return cube.Box(-.21, 0, -.21, .21, .39, .21)
	}
	return cube.Box(-.35, 0, -.35, .35, .65, .35)
}
func (armadilloType) DecodeNBT(m map[string]any, d *world.EntityData) {
	s := ReadNativeMobJavaState(m)
	if s == nil {
		s = m
	}
	c := ArmadilloConfig{Age: int(nbtconv.Int32(s, "Age")), ScuteTicks: int(nbtconv.Int32(s, "ScuteTime")), RollTicks: int(nbtconv.Int32(s, "RollTicks")), ThreatCooldown: int(nbtconv.Int32(s, "ThreatCooldown")), Rolling: nbtconv.Bool(s, "Rolling")}
	if a, ok := nativeMobAttribute(m, "minecraft:health"); ok {
		c.Health, c.MaxHealth = a.Current, a.Base
	}
	d.Data = c.New()
	d.Data.(*ArmadilloBehaviour).breeding.decodeNBT(s)
}
func (armadilloType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*ArmadilloBehaviour)
	a := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{{Name: "minecraft:health", Base: b.maxHealth, Current: b.health, DefaultMin: 0, DefaultMax: b.maxHealth, Min: 0, Max: b.maxHealth}})
	return MergeNativeMobJavaState(map[string]any{"Attributes": a}, MergeNativeMobNBT(ReadNativeMobJavaState(d.NativeNBT), b.breeding.encodeNBT(map[string]any{"Age": int32(b.age), "ScuteTime": int32(b.scuteTicks), "Rolling": byte(boolByte(b.rolling)), "RollTicks": int32(b.rollTicks), "ThreatCooldown": int32(b.threatCooldown)})))
}
func (c ArmadilloConfig) New() *ArmadilloBehaviour {
	if c.MaxHealth <= 0 {
		c.MaxHealth = 12
	}
	if c.Health <= 0 {
		c.Health = c.MaxHealth
	}
	if c.ScuteTicks == 0 {
		c.ScuteTicks = 6000 + rand.IntN(6001)
	}
	return &ArmadilloBehaviour{health: c.Health, maxHealth: c.MaxHealth, effects: NewEffectManager(), age: c.Age, scuteTicks: c.ScuteTicks, rollTicks: c.RollTicks, threatCooldown: c.ThreatCooldown, rolling: c.Rolling, next: 40 + rand.IntN(81), speed: .15, mc: &MovementComputer{Gravity: .08, Drag: .02, DragBeforeGravity: true}}
}
func (a *Armadillo) FinalizeSpawn(_ string, r *java26.Random) {
	if r != nil {
		a.behaviour().scuteTicks = 6000 + int(r.NextInt(6001))
	}
}

// ArmadilloConfig configures age and saved scute/turtle-like state.
type ArmadilloConfig struct {
	Health, MaxHealth                          float64
	Age, ScuteTicks, RollTicks, ThreatCooldown int
	Rolling                                    bool
}

func (c ArmadilloConfig) Apply(d *world.EntityData) { d.Data = c.New() }

// Armadillo has threat-triggered rolling, scute shed timers and brush interaction state.
type Armadillo struct{ *Ent }
type ArmadilloBehaviour struct {
	health, maxHealth                                float64
	effects                                          *EffectManager
	age, scuteTicks, rollTicks, threatCooldown, next int
	rolling, dead                                    bool
	speed                                            float64
	navigator                                        Navigator
	mc                                               *MovementComputer
	breeding                                         breeding
}

func (a *Armadillo) behaviour() *ArmadilloBehaviour { return a.Behaviour().(*ArmadilloBehaviour) }
func (a *Armadillo) Tick(tx *world.Tx, t int64)     { a.behaviour().effects.Tick(a, tx); a.Ent.Tick(tx, t) }
func (b *ArmadilloBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	partner, inLove := tickBreeding(&Armadillo{Ent: e}, tx)
	if b.threatCooldown > 0 {
		b.threatCooldown--
	}
	if b.scuteTicks > 0 && b.age >= 0 {
		b.scuteTicks--
		if b.scuteTicks == 0 {
			dropArmadilloShed(e, tx)
			b.scuteTicks = 6000 + rand.IntN(6001)
		}
	}
	if b.rolling {
		b.rollTicks--
		if b.rollTicks <= 0 {
			b.rolling = false
		}
	}
	if b.next > 0 {
		b.next--
	}
	if inLove && !b.rolling {
		navigateToPartner(&b.navigator, e, tx, partner)
	} else if b.navigator.Index >= len(b.navigator.Path) && b.next == 0 && !b.rolling {
		b.next = 40 + rand.IntN(81)
		start := cube.PosFromVec3(e.Position())
		for range 4 {
			goal := start.Add(cube.Pos{rand.IntN(9) - 4, 0, rand.IntN(9) - 4})
			if b.navigator.MoveTo(tx, start, goal, GroundProfile{MaxVisitedNodes: 96}, navigatorTick(e)) {
				break
			}
		}
	}
	rot, v := e.data.Rot, e.data.Vel
	if b.rolling {
		v[0], v[2] = 0, 0
	} else if target, ok := b.navigator.Tick(e.data.Pos, navigatorTick(e), .7); ok {
		d := target.Sub(e.data.Pos)
		d[1] = 0
		if d.Len() < .2 {
			b.navigator.Index++
		} else {
			d = d.Normalize()
			v[0], v[2] = d[0]*b.speed, d[2]*b.speed
		}
	} else {
		v[0], v[2] = 0, 0
	}
	m := b.mc.TickMovement(e, e.data.Pos, v, e.data.Rot, rot, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	return m
}
func dropArmadilloShed(e *Ent, tx *world.Tx) {
	stacks, err := loot.Generate("minecraft:gameplay/armadillo_shed", rand.Int64(), loot.Context{Entity: "minecraft:armadillo", Origin: e.Position()})
	if err != nil {
		return
	}
	for _, stack := range stacks {
		tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: e.Position()}, stack))
	}
}
func (a *Armadillo) Health() float64    { return a.behaviour().health }
func (a *Armadillo) MaxHealth() float64 { return a.behaviour().maxHealth }
func (a *Armadillo) SetMaxHealth(v float64) {
	b := a.behaviour()
	b.maxHealth = v
	b.health = math.Min(v, b.health)
}
func (a *Armadillo) Dead() bool { return a.behaviour().dead || a.Health() <= 0 }
func (a *Armadillo) Hurt(v float64, src world.DamageSource) (float64, bool) {
	b := a.behaviour()
	if a.Dead() || v <= 0 || (src.Fire() && a.hasEffect(effect.FireResistance)) {
		return 0, false
	}
	if resistance, ok := b.effects.Effect(effect.Resistance); ok {
		v *= effect.Resistance.Multiplier(src, resistance.Level())
	}
	if attack, ok := src.(AttackDamageSource); ok && attack.Attacker != nil {
		a.Threaten(attack.Attacker)
	}
	if b.rolling {
		v *= .5
	}
	b.health -= v
	resetLove(a)
	a.viewAction(HurtAction{})
	onMobHurt(a.tx, a)
	if a.Dead() {
		b.dead = true
		a.viewAction(DeathAction{})
		dropAllDeathLoot(a.tx, a, src)
		_ = a.CloseIn(a.tx)
	}
	return v, true
}
func (a *Armadillo) hasEffect(t effect.Type) bool {
	_, ok := a.behaviour().effects.Effect(t)
	return ok
}
func (a *Armadillo) AddEffect(e effect.Effect)  { a.behaviour().effects.Add(e, a) }
func (a *Armadillo) RemoveEffect(t effect.Type) { a.behaviour().effects.Remove(t, a) }
func (a *Armadillo) Effects() []effect.Effect   { return a.behaviour().effects.Effects() }
func (a *Armadillo) Speed() float64             { return a.behaviour().speed }
func (a *Armadillo) SetSpeed(v float64)         { a.behaviour().speed = v }
func (a *Armadillo) Heal(v float64, _ world.HealingSource) float64 {
	if a.Dead() || v <= 0 {
		return 0
	}
	b := a.behaviour()
	old := b.health
	b.health = math.Min(b.maxHealth, b.health+v)
	return b.health - old
}
func (a *Armadillo) KnockBack(src mgl64.Vec3, f, h float64) {
	b := a.behaviour()
	if b.rolling {
		return
	}
	d := a.Position().Sub(src)
	d[1] = 0
	if d.Len() > 0 {
		d = d.Normalize().Mul(f)
	}
	d[1] = h
	a.SetVelocity(d)
}
func (a *Armadillo) viewAction(x world.EntityAction) {
	vs := a.tx.Viewers(a.Position())
	for _, v := range vs {
		v.ViewEntityAction(a, x)
	}
	a.tx.ReleaseViewers(vs)
}
func (a *Armadillo) Rolling() bool { return a.behaviour().rolling }

// Threaten begins the source defensive roll against a hostile attacker.
func (a *Armadillo) Threaten(attacker world.Entity) bool {
	b := a.behaviour()
	if attacker == nil || b.threatCooldown > 0 || b.rolling {
		return false
	}
	b.rolling = true
	b.rollTicks = 200
	b.threatCooldown = 100
	return true
}

// Brush yields one scute while a scute is ready; item wear/consumption is handled by the caller.
func (a *Armadillo) Brush() item.Stack {
	if a.Dead() || a.behaviour().age < 0 {
		return item.Stack{}
	}
	stacks, err := loot.Generate("minecraft:brush/armadillo", rand.Int64(), loot.Context{Entity: "minecraft:armadillo", Origin: a.Position()})
	if err != nil || len(stacks) == 0 {
		return item.Stack{}
	}
	return stacks[0]
}

// Interact feeds the armadillo spider eyes to breed it or let a pup grow up
// faster.
func (a *Armadillo) Interact(_ *world.Tx, u item.User, c *item.UseContext) bool {
	return feedBreeder(a, u, c)
}

// InLove reports whether the armadillo is in love.
func (a *Armadillo) InLove() bool { return a.behaviour().breeding.InLove() }

func (a *Armadillo) breedingState() *breeding { return &a.behaviour().breeding }
func (a *Armadillo) ageTicks() *int           { return &a.behaviour().age }

func (a *Armadillo) breedingFood(stack item.Stack) bool {
	return itemNamed(stack, "minecraft:spider_eye")
}

func (a *Armadillo) breedOffspring(_ *world.Tx, _ breeder) *world.EntityHandle {
	return babyAt(a).New(ArmadilloType, ArmadilloConfig{Age: babyStartAge})
}

var _ Interactable = (*Armadillo)(nil)
