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
	"github.com/df-mc/dragonfly/server/world/particle"
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
	c := SheepConfig{WoolColor: sheepNBTColor(s["Color"]), Sheared: nbtconv.Bool(s, "Sheared"), AgeTicks: int(nbtconv.Int32(s, "Age"))}
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
	d.Data.(*SheepBehaviour).breeding.decodeNBT(s)
}
func (sheepType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*SheepBehaviour)
	attr := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{{Name: "minecraft:health", Base: b.maxHealth, Current: b.health, DefaultMin: 0, DefaultMax: b.maxHealth, Min: 0, Max: b.maxHealth}})
	native := MergeNativeMobNBT(d.NativeNBT, map[string]any{"Attributes": attr})
	return MergeNativeMobJavaState(native, MergeNativeMobNBT(ReadNativeMobJavaState(d.NativeNBT), b.breeding.encodeNBT(map[string]any{"Age": int32(b.ageTicks), "Color": byte(b.color), "Sheared": byte(boolByte(b.sheared))})))
}

// SheepConfig configures a sheep's saved source state.
type SheepConfig struct {
	Health, MaxHealth   float64
	AgeTicks, WoolColor int
	Sheared             bool
}

func (c SheepConfig) Apply(d *world.EntityData) { d.Data = c.New() }
func (c SheepConfig) New() *SheepBehaviour {
	if c.MaxHealth <= 0 {
		c.MaxHealth = 8
	}
	if c.Health <= 0 {
		c.Health = c.MaxHealth
	}
	return &SheepBehaviour{health: c.Health, maxHealth: c.MaxHealth, ageTicks: c.AgeTicks, color: max(0, min(c.WoolColor, 15)), sheared: c.Sheared, speed: .23, mc: &MovementComputer{Gravity: .08, Drag: .02, DragBeforeGravity: true}, effects: NewEffectManager()}
}

// Sheep is a concrete Java sheep, including wool and grazing state.
type Sheep struct{ *Ent }
type SheepBehaviour struct {
	effects           *EffectManager
	health, maxHealth float64
	ageTicks, color   int
	sheared, dead     bool
	speed             float64
	mc                *MovementComputer
	breeding          breeding
	goals             animalGoals
	// eatTicks is the eatAnimationTick of the running EatBlockGoal.
	eatTicks int
	sheep    *Sheep
}

func (s *SheepBehaviour) Baby() bool        { return s.ageTicks < 0 }
func (s *Sheep) behaviour() *SheepBehaviour { return s.Behaviour().(*SheepBehaviour) }
func (s *Sheep) Tick(tx *world.Tx, current int64) {
	s.behaviour().effects.Tick(s, tx)
	s.Ent.Tick(tx, current)
}
func (b *SheepBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	b.sheep = &Sheep{Ent: e}
	vel, rot := b.goals.tick(e, tx, b.sheep, animalGoalSpec{speed: b.speed, panic: 1.25, breed: 1, tempt: 1.1, tempting: b.sheep.breedingFood, parent: 1.1, species: b, stroll: 1, reach: .9})
	return b.mc.TickMovement(e, e.data.Pos, vel, rot, e.data.Rot, tx).apply(e)
}

// run ports EatBlockGoal: now and then a sheep, far more often a lamb,
// lowers its head for 40 ticks and eats the grass it stands in or the grass
// block it stands on, which regrows its wool and lets a lamb grow up faster.
func (b *SheepBehaviour) run(e *Ent, tx *world.Tx) bool {
	if b.eatTicks == 0 {
		chance := 1000
		if b.ageTicks < 0 {
			chance = 50
		}
		pos := cube.PosFromVec3(e.Position())
		if rand.IntN(chance) != 0 || !blockInTag(tx.Block(pos), "edible_for_sheep") && !sheepGrass(tx, pos.Side(cube.FaceDown)) {
			return false
		}
		b.eatTicks = 40
		b.sheep.viewAction(EatGrassAction{})
		return true
	}
	if b.eatTicks--; b.eatTicks == 4 {
		b.eat(e, tx)
	}
	return b.eatTicks > 0
}

// stop ends EatBlockGoal when a goal of higher priority runs.
func (b *SheepBehaviour) stop() { b.eatTicks = 0 }

// eat ports the bite of EatBlockGoal.tick and Sheep.ate.
func (b *SheepBehaviour) eat(e *Ent, tx *world.Tx) {
	pos := cube.PosFromVec3(e.Position())
	griefing := tx.World().GameRuleBool("mob_griefing")
	switch {
	case blockInTag(tx.Block(pos), "edible_for_sheep"):
		if griefing {
			eaten := tx.Block(pos)
			tx.SetBlock(pos, nil, nil)
			tx.AddParticle(pos.Vec3Centre(), particle.BlockBreak{Block: eaten})
		}
	case sheepGrass(tx, pos.Side(cube.FaceDown)):
		if griefing {
			below := pos.Side(cube.FaceDown)
			tx.AddParticle(below.Vec3Centre(), particle.BlockBreak{Block: tx.Block(below)})
			tx.SetBlock(below, block.Dirt{}, nil)
		}
	default:
		return
	}
	tx.EmitGameEvent(world.GameEventEat, e.Position(), world.GameEventContext{Source: b.sheep})
	if b.sheared {
		b.sheared = false
		viewBreederState(b.sheep)
	}
	if b.ageTicks < 0 && !b.breeding.ageLocked {
		ageUp(b.sheep, 60, false)
	}
}

// sheepGrass reports whether pos holds a grass block.
func sheepGrass(tx *world.Tx, pos cube.Pos) bool {
	_, ok := tx.Block(pos).(block.Grass)
	return ok
}
func (s *Sheep) Health() float64    { return s.behaviour().health }
func (s *Sheep) MaxHealth() float64 { return s.behaviour().maxHealth }
func (s *Sheep) SetMaxHealth(v float64) {
	b := s.behaviour()
	b.maxHealth = v
	b.health = math.Min(b.health, v)
}
func (s *Sheep) Dead() bool { return s.behaviour().dead || s.Health() <= 0 }

// Panicking reports whether the sheep runs from danger.
func (s *Sheep) Panicking() bool { return s.behaviour().goals.panic.active }
func (s *Sheep) Hurt(v float64, src world.DamageSource) (float64, bool) {
	if s.Dead() || v <= 0 {
		return 0, false
	}
	var ok bool
	if v, ok = mobEffectDamage(s.behaviour().effects, v, src); !ok {
		return 0, false
	}
	b := s.behaviour()
	b.health -= v
	b.goals.hurt(src, s.tx.CurrentTick())
	resetLove(s)
	s.viewAction(HurtAction{})
	onMobHurt(s.tx, s)
	if s.Dead() {
		b.dead = true
		s.viewAction(DeathAction{})
		dropAllDeathLoot(s.tx, s, src)
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

// Baby reports whether the sheep is a lamb.
func (s *Sheep) Baby() bool { return s.behaviour().ageTicks < 0 }

// LootComponents exposes the sheep colour to loot predicates.
func (s *Sheep) LootComponents() map[string]any {
	return map[string]any{"minecraft:sheep/color": item.Colours()[s.behaviour().color].String()}
}

// Shear returns the source wool drop and removes the sheep's fleece once.
func (s *Sheep) Shear(_ *world.Tx) item.Stack {
	b := s.behaviour()
	if s.Dead() || b.sheared {
		return item.Stack{}
	}
	b.sheared = true
	viewBreederState(s)
	stacks, err := loot.Generate("minecraft:shearing/sheep/"+item.Colours()[b.color].String(), rand.Int64(), loot.Context{Entity: "minecraft:sheep", Origin: s.Position()})
	if err != nil || len(stacks) == 0 {
		return item.Stack{}
	}
	return stacks[0]
}

// Interact feeds the sheep wheat to breed it or let a lamb grow up faster,
// or dyes its fleece.
func (s *Sheep) Interact(_ *world.Tx, user item.User, ctx *item.UseContext) bool {
	if user == nil || ctx == nil {
		return false
	}
	if feedBreeder(s, user, ctx) {
		return true
	}
	held, _ := user.HeldItems()
	if dye, ok := held.Item().(item.Dye); ok {
		s.behaviour().color = sheepColor(dye.Colour.String())
		ctx.SubtractFromCount(1)
		return true
	}
	return false
}

// InLove reports whether the sheep is in love.
func (s *Sheep) InLove() bool { return s.behaviour().breeding.InLove() }

func (s *Sheep) breedingState() *breeding { return &s.behaviour().breeding }
func (s *Sheep) ageTicks() *int           { return &s.behaviour().ageTicks }

func (s *Sheep) breedingFood(stack item.Stack) bool { return itemNamed(stack, "minecraft:wheat") }

// breedOffspring ports Sheep.getBreedOffspring: the lamb's fleece has the
// colour two dyes of the parents' colours craft into, or the colour of
// either parent if they craft into none.
func (s *Sheep) breedOffspring(_ *world.Tx, partner breeder) *world.EntityHandle {
	colour := mixedDyeColour(item.Colours()[s.behaviour().color], item.Colours()[partner.(*Sheep).behaviour().color])
	return babyAt(s).New(SheepType, SheepConfig{AgeTicks: babyStartAge, WoolColor: sheepColor(colour.String())})
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
