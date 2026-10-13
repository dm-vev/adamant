package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"math"
	"math/rand/v2"
)

// NewGoat creates a normal adult goat.
func NewGoat(o world.EntitySpawnOpts) *world.EntityHandle { return o.New(GoatType, GoatConfig{}) }

// GoatType is the registered Java goat type.
var GoatType goatType

type goatType struct{}

func (goatType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		GoatConfig{}.Apply(d)
	}
	return &Goat{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (goatType) EncodeEntity() string          { return "minecraft:goat" }
func (goatType) BBox(e world.Entity) cube.BBox { return cube.Box(-.45, 0, -.45, .45, 1.3, .45) }
func (goatType) DecodeNBT(m map[string]any, d *world.EntityData) {
	s := ReadNativeMobJavaState(m)
	if s == nil {
		s = m
	}
	c := GoatConfig{Age: int(nbtconv.Int32(s, "Age")), Screaming: nbtconv.Bool(s, "IsScreamingGoat"), LeftHorn: nbtconv.Bool(s, "HasLeftHorn"), RightHorn: nbtconv.Bool(s, "HasRightHorn")}
	if a, ok := nativeMobAttribute(m, "minecraft:health"); ok {
		c.Health, c.MaxHealth = a.Current, a.Base
	}
	d.Data = c.New()
}
func (goatType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*GoatBehaviour)
	a := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{{Name: "minecraft:health", Base: b.maxHealth, Current: b.health, DefaultMin: 0, DefaultMax: b.maxHealth, Min: 0, Max: b.maxHealth}})
	return MergeNativeMobJavaState(map[string]any{"Attributes": a}, MergeNativeMobNBT(ReadNativeMobJavaState(d.NativeNBT), map[string]any{"Age": int32(b.age), "IsScreamingGoat": byte(boolByte(b.screaming)), "HasLeftHorn": byte(boolByte(b.leftHorn)), "HasRightHorn": byte(boolByte(b.rightHorn))}))
}

// GoatConfig configures source goat state.
type GoatConfig struct {
	Health, MaxHealth              float64
	Age                            int
	Screaming, LeftHorn, RightHorn bool
}

func (c GoatConfig) Apply(d *world.EntityData) { d.Data = c.New() }
func (c GoatConfig) New() *GoatBehaviour {
	if c.MaxHealth <= 0 {
		c.MaxHealth = 10
	}
	if c.Health <= 0 {
		c.Health = c.MaxHealth
	}
	if !c.LeftHorn && !c.RightHorn {
		c.LeftHorn, c.RightHorn = true, true
	}
	return &GoatBehaviour{health: c.Health, maxHealth: c.MaxHealth, age: c.Age, screaming: c.Screaming, leftHorn: c.LeftHorn, rightHorn: c.RightHorn, next: 40 + rand.IntN(81), speed: .25, mc: &MovementComputer{Gravity: .08, Drag: .02, DragBeforeGravity: true}}
}

// Goat models source screaming, ramming cooldown and breakable horns.
type Goat struct{ *Ent }
type GoatBehaviour struct {
	health, maxHealth                    float64
	age, next, ramCooldown               int
	screaming, leftHorn, rightHorn, dead bool
	speed                                float64
	navigator                            Navigator
	mc                                   *MovementComputer
}

func (g *Goat) behaviour() *GoatBehaviour  { return g.Behaviour().(*GoatBehaviour) }
func (g *Goat) Tick(tx *world.Tx, t int64) { g.Ent.Tick(tx, t) }
func (b *GoatBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if b.age < 0 {
		b.age = min(0, b.age+1)
	}
	if b.ramCooldown > 0 {
		b.ramCooldown--
	}
	if b.next > 0 {
		b.next--
	}
	if b.navigator.Index >= len(b.navigator.Path) && b.next == 0 {
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
			v[0], v[2] = d[0]*b.speed, d[2]*b.speed
		}
	} else {
		v[0], v[2] = 0, 0
	}
	m := b.mc.TickMovement(e, e.data.Pos, v, e.data.Rot, rot, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	return m
}
func (g *Goat) Health() float64    { return g.behaviour().health }
func (g *Goat) MaxHealth() float64 { return g.behaviour().maxHealth }
func (g *Goat) SetMaxHealth(v float64) {
	b := g.behaviour()
	b.maxHealth = v
	b.health = math.Min(v, b.health)
}
func (g *Goat) Dead() bool { return g.behaviour().dead || g.Health() <= 0 }
func (g *Goat) Hurt(v float64, src world.DamageSource) (float64, bool) {
	if g.Dead() || v <= 0 {
		return 0, false
	}
	b := g.behaviour()
	b.health -= v
	g.viewAction(HurtAction{})
	if g.Dead() {
		b.dead = true
		g.viewAction(DeathAction{})
		dropMobLoot(g.tx, g.Position(), "minecraft:goat", src)
		_ = g.CloseIn(g.tx)
	}
	return v, true
}
func (g *Goat) Heal(v float64, _ world.HealingSource) float64 {
	if g.Dead() || v <= 0 {
		return 0
	}
	b := g.behaviour()
	old := b.health
	b.health = math.Min(b.maxHealth, b.health+v)
	return b.health - old
}
func (g *Goat) KnockBack(src mgl64.Vec3, f, h float64) {
	d := g.Position().Sub(src)
	d[1] = 0
	if d.Len() > 0 {
		d = d.Normalize().Mul(f)
	}
	d[1] = h
	g.SetVelocity(d)
}
func (g *Goat) viewAction(a world.EntityAction) {
	vs := g.tx.Viewers(g.Position())
	for _, v := range vs {
		v.ViewEntityAction(g, a)
	}
	g.tx.ReleaseViewers(vs)
}
func (g *Goat) Screaming() bool     { return g.behaviour().screaming }
func (g *Goat) Horns() (bool, bool) { b := g.behaviour(); return b.leftHorn, b.rightHorn }

// Ram starts the source charge, consuming a horn on collision with a hard obstacle.
func (g *Goat) Ram(target world.Entity) bool {
	b := g.behaviour()
	if g.Dead() || b.ramCooldown > 0 || target == nil {
		return false
	}
	b.ramCooldown = 100
	delta := target.Position().Sub(g.Position())
	delta[1] = 0
	if delta.Len() > 0 {
		g.SetVelocity(delta.Normalize().Mul(1.5))
	}
	return true
}
func (g *Goat) BreakHorn(left bool) bool {
	b := g.behaviour()
	if left && b.leftHorn {
		b.leftHorn = false
		return true
	}
	if !left && b.rightHorn {
		b.rightHorn = false
		return true
	}
	return false
}
func (g *Goat) Feed(s item.Stack) bool {
	b := g.behaviour()
	if s.Empty() || b.age >= 0 {
		return false
	}
	name, _ := s.Item().EncodeItem()
	if name != "minecraft:wheat" {
		return false
	}
	b.age = min(0, b.age+1200)
	return true
}
func (g *Goat) Interact(_ *world.Tx, u item.User, c *item.UseContext) bool {
	if u == nil || c == nil {
		return false
	}
	held, _ := u.HeldItems()
	if g.Feed(held) {
		c.SubtractFromCount(1)
		return true
	}
	return false
}

var _ Interactable = (*Goat)(nil)

func (g *Goat) FinalizeSpawn(_ string, r *java26.Random) {
	if r == nil {
		return
	}
	b := g.behaviour()
	b.screaming = r.NextInt(50) == 0
	if b.age >= 0 && r.NextInt(10) == 0 {
		if r.NextInt(2) == 0 {
			b.leftHorn = false
		} else {
			b.rightHorn = false
		}
	}
}

var _ = item.Wheat{}
