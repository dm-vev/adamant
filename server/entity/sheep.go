package entity

import (
	"math"
	"math/rand/v2"
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// NewSheep creates a sheep with Java's default white, adult state.
func NewSheep(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(SheepType, SheepConfig{})
}

// SheepType is the registered Java sheep actor.
var SheepType sheepType

type sheepType struct{}

func (sheepType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		SheepConfig{}.Apply(d)
	}
	return &Sheep{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (sheepType) EncodeEntity() string { return "minecraft:sheep" }
func (sheepType) BBox(e world.Entity) cube.BBox {
	if s, ok := e.(*Sheep); ok && s.behaviour().ageTicks < 0 {
		return cube.Box(-.225, 0, -.225, .225, .65, .225)
	}
	return cube.Box(-.45, 0, -.45, .45, 1.3, .45)
}
func (sheepType) DecodeNBT(m map[string]any, d *world.EntityData) {
	s := ReadNativeMobJavaState(m)
	if s == nil {
		s = m
	}
	c := SheepConfig{WoolColor: sheepNBTColor(s["Color"]), Sheared: nbtconv.Bool(s, "Sheared"), RegrowTicks: int(nbtconv.Int32(s, "RegrowTicks")), AgeTicks: int(nbtconv.Int32(s, "Age"))}
	if a, ok := nativeMobAttribute(m, "minecraft:health"); ok {
		c.Health, c.MaxHealth = a.Current, a.Base
	}
	if h, ok := s["Health"]; ok {
		c.Health = float64(nbtconv.Float32(map[string]any{"h": h}, "h"))
	}
	if h, ok := s["MaxHealth"]; ok {
		c.MaxHealth = float64(nbtconv.Float32(map[string]any{"h": h}, "h"))
	}
	d.Data = c.New()
}
func (sheepType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*SheepBehaviour)
	attr := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{{Name: "minecraft:health", Base: b.maxHealth, Current: b.health, DefaultMin: 0, DefaultMax: b.maxHealth, Min: 0, Max: b.maxHealth}})
	native := MergeNativeMobNBT(d.NativeNBT, map[string]any{"Attributes": attr})
	return MergeNativeMobJavaState(native, MergeNativeMobNBT(ReadNativeMobJavaState(d.NativeNBT), map[string]any{"Age": int32(b.ageTicks), "Color": byte(b.color), "Sheared": byte(boolByte(b.sheared)), "RegrowTicks": int32(b.regrowTicks)}))
}

// SheepConfig configures a sheep's saved source state.
type SheepConfig struct {
	Health, MaxHealth                float64
	AgeTicks, WoolColor, RegrowTicks int
	Sheared                          bool
}

func (c SheepConfig) Apply(d *world.EntityData) { d.Data = c.New() }
func (c SheepConfig) New() *SheepBehaviour {
	if c.MaxHealth <= 0 {
		c.MaxHealth = 8
	}
	if c.Health <= 0 {
		c.Health = c.MaxHealth
	}
	return &SheepBehaviour{health: c.Health, maxHealth: c.MaxHealth, ageTicks: c.AgeTicks, color: max(0, min(c.WoolColor, 15)), sheared: c.Sheared, regrowTicks: c.RegrowTicks, speed: .23, nextWander: 40 + rand.IntN(81), mc: &MovementComputer{Gravity: .08, Drag: .02, DragBeforeGravity: true}}
}

// Sheep is a concrete Java sheep, including wool and grazing state.
type Sheep struct{ *Ent }
type SheepBehaviour struct {
	health, maxHealth            float64
	ageTicks, color, regrowTicks int
	sheared, dead                bool
	speed                        float64
	navigator                    Navigator
	nextWander                   int
	mc                           *MovementComputer
}

func (s *SheepBehaviour) Baby() bool              { return s.ageTicks < 0 }
func (s *Sheep) behaviour() *SheepBehaviour       { return s.Behaviour().(*SheepBehaviour) }
func (s *Sheep) Tick(tx *world.Tx, current int64) { s.Ent.Tick(tx, current) }
func (b *SheepBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if b.ageTicks < 0 {
		b.ageTicks = min(0, b.ageTicks+1)
	}
	// EatBlockGoal: a sheared sheep regrows only after eating a grass block.
	if b.sheared && b.regrowTicks > 0 {
		b.regrowTicks--
	}
	if b.sheared && b.regrowTicks == 0 {
		p := cube.PosFromVec3(e.Position()).Side(cube.FaceDown)
		name, _ := tx.Block(p).EncodeBlock()
		if name == "minecraft:grass_block" {
			tx.SetBlock(p, block.Dirt{}, nil)
			b.sheared = false
		}
	}
	if b.nextWander > 0 {
		b.nextWander--
	}
	if b.navigator.Index >= len(b.navigator.Path) && b.nextWander == 0 {
		b.choosePath(e, tx)
	}
	rot, vel := e.data.Rot, e.data.Vel
	if target, ok := b.navigator.Tick(e.data.Pos, navigatorTick(e), .9); ok {
		delta := target.Sub(e.data.Pos)
		delta[1] = 0
		if delta.Len() < .25 {
			b.navigator.Index++
		} else {
			d := delta.Normalize()
			vel[0], vel[2] = d[0]*b.speed, d[2]*b.speed
			rot[0] = math.Atan2(-d[0], d[2]) * 180 / math.Pi
		}
	} else {
		vel[0], vel[2] = 0, 0
	}
	m := b.mc.TickMovement(e, e.data.Pos, vel, e.data.Rot, rot, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	return m
}
func (b *SheepBehaviour) choosePath(e *Ent, tx *world.Tx) {
	b.nextWander = 40 + rand.IntN(81)
	start := cube.PosFromVec3(e.Position())
	for range 4 {
		goal := start.Add(cube.Pos{rand.IntN(13) - 6, 0, rand.IntN(13) - 6})
		if b.navigator.MoveTo(tx, start, goal, GroundProfile{MaxVisitedNodes: 128}, navigatorTick(e)) {
			return
		}
	}
	b.navigator.Path, b.navigator.Index = nil, 0
}
func (s *Sheep) Health() float64    { return s.behaviour().health }
func (s *Sheep) MaxHealth() float64 { return s.behaviour().maxHealth }
func (s *Sheep) SetMaxHealth(v float64) {
	b := s.behaviour()
	b.maxHealth = v
	b.health = math.Min(b.health, v)
}
func (s *Sheep) Dead() bool { return s.behaviour().dead || s.Health() <= 0 }
func (s *Sheep) Hurt(v float64, src world.DamageSource) (float64, bool) {
	if s.Dead() || v <= 0 {
		return 0, false
	}
	b := s.behaviour()
	b.health -= v
	s.viewAction(HurtAction{})
	if s.Dead() {
		b.dead = true
		s.viewAction(DeathAction{})
		_ = s.CloseIn(s.tx)
	}
	return v, true
}
func (s *Sheep) Heal(v float64, _ world.HealingSource) float64 {
	if s.Dead() || v <= 0 {
		return 0
	}
	b := s.behaviour()
	old := b.health
	b.health = math.Min(b.maxHealth, b.health+v)
	return b.health - old
}
func (s *Sheep) KnockBack(src mgl64.Vec3, force, height float64) {
	v := s.Position().Sub(src)
	v[1] = 0
	if v.Len() > 0 {
		v = v.Normalize().Mul(force)
	}
	v[1] = height
	s.SetVelocity(v)
}
func (s *Sheep) viewAction(a world.EntityAction) {
	vs := s.tx.Viewers(s.Position())
	for _, v := range vs {
		v.ViewEntityAction(s, a)
	}
	s.tx.ReleaseViewers(vs)
}

// WoolColor reports the inherited fleece dye index (white is zero).
func (s *Sheep) WoolColor() int { return s.behaviour().color }

// Sheared reports whether the sheep currently has no fleece.
func (s *Sheep) Sheared() bool { return s.behaviour().sheared }

// Shear returns the source wool drop and removes the sheep's fleece once.
func (s *Sheep) Shear(_ *world.Tx) item.Stack {
	b := s.behaviour()
	if s.Dead() || b.sheared {
		return item.Stack{}
	}
	b.sheared = true
	b.regrowTicks = 200 + rand.IntN(1001)
	stacks, err := loot.Generate("minecraft:shearing/sheep/"+item.Colours()[b.color].String(), rand.Int64(), loot.Context{Entity: "minecraft:sheep", Origin: s.Position()})
	if err != nil || len(stacks) == 0 {
		return item.Stack{}
	}
	return stacks[0]
}
func (s *Sheep) Interact(_ *world.Tx, user item.User, ctx *item.UseContext) bool {
	if user == nil || ctx == nil {
		return false
	}
	held, _ := user.HeldItems()
	if held.Empty() {
		return false
	}
	if dye, ok := held.Item().(item.Dye); ok {
		s.behaviour().color = sheepColor(dye.Colour.String())
		ctx.SubtractFromCount(1)
		return true
	}
	if name, _ := held.Item().EncodeItem(); name == "minecraft:wheat" && s.behaviour().ageTicks < 0 {
		s.behaviour().ageTicks = min(0, s.behaviour().ageTicks+1200)
		ctx.SubtractFromCount(1)
		return true
	}
	return false
}

var _ Interactable = (*Sheep)(nil)
var _ interface{ Shear(*world.Tx) item.Stack } = (*Sheep)(nil)

// FinalizeSpawn chooses the source fleece color, including rare pink sheep.
func (s *Sheep) FinalizeSpawn(_ string, r *java26.Random) {
	if r == nil {
		return
	}
	b := s.behaviour()
	roll := r.NextInt(100)
	switch {
	case roll < 5:
		b.color = 15
	case roll < 10:
		b.color = 7
	case roll < 15:
		b.color = 8
	case roll < 18:
		b.color = 12
	default:
		if r.NextInt(500) == 0 {
			b.color = 6
		} else {
			b.color = 0
		}
	}
}
func sheepColor(s string) int {
	switch strings.TrimPrefix(s, "minecraft:") {
	case "orange":
		return 1
	case "magenta":
		return 2
	case "light_blue":
		return 3
	case "yellow":
		return 4
	case "lime":
		return 5
	case "pink":
		return 6
	case "gray":
		return 7
	case "light_gray":
		return 8
	case "cyan":
		return 9
	case "purple":
		return 10
	case "blue":
		return 11
	case "brown":
		return 12
	case "green":
		return 13
	case "red":
		return 14
	case "black":
		return 15
	}
	return 0
}
func sheepNBTColor(v any) int {
	switch color := v.(type) {
	case byte:
		return int(color)
	case int8:
		return int(color)
	case int32:
		return int(color)
	case int:
		return color
	default:
		return 0
	}
}
