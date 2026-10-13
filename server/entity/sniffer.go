package entity

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// NewSniffer creates an adult sniffer with no active dig memory.
func NewSniffer(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(SnifferType, SnifferConfig{})
}

// SnifferType is the registered Java sniffer type.
var SnifferType snifferType

type snifferType struct{}

func (snifferType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		SnifferConfig{}.Apply(d)
	}
	return &Sniffer{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (snifferType) EncodeEntity() string { return "minecraft:sniffer" }
func (snifferType) BBox(e world.Entity) cube.BBox {
	if s, ok := e.(*Sniffer); ok && s.behaviour().age < 0 {
		return cube.Box(-.45, 0, -.45, .45, .75, .45)
	}
	return cube.Box(-.7, 0, -.7, .7, 1.4, .7)
}
func (snifferType) DecodeNBT(m map[string]any, d *world.EntityData) {
	s := ReadNativeMobJavaState(m)
	if s == nil {
		s = m
	}
	c := SnifferConfig{Age: int(nbtconv.Int32(s, "Age")), SniffTicks: int(nbtconv.Int32(s, "SniffTicks")), DigTicks: int(nbtconv.Int32(s, "DigTicks")), Next: int(nbtconv.Int32(s, "Next")), DigPos: cube.Pos{int(nbtconv.Int32(s, "DigX")), int(nbtconv.Int32(s, "DigY")), int(nbtconv.Int32(s, "DigZ"))}}
	if v, ok := s["Sniffing"].(byte); ok {
		c.Sniffing = v != 0
	}
	if positions, ok := s["ExploredPositions"].([]int32); ok {
		for i := 0; i+2 < len(positions); i += 3 {
			c.Explored = append(c.Explored, cube.Pos{int(positions[i]), int(positions[i+1]), int(positions[i+2])})
		}
	}
	if a, ok := nativeMobAttribute(m, "minecraft:health"); ok {
		c.Health, c.MaxHealth = a.Current, a.Base
	}
	d.Data = c.New()
	d.Data.(*SnifferBehaviour).breeding.decodeNBT(s)
}
func (snifferType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*SnifferBehaviour)
	a := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{{Name: "minecraft:health", Base: b.maxHealth, Current: b.health, DefaultMin: 0, DefaultMax: b.maxHealth, Min: 0, Max: b.maxHealth}})
	positions := make([]int32, 0, len(b.explored)*3)
	for _, p := range b.explored {
		positions = append(positions, int32(p[0]), int32(p[1]), int32(p[2]))
	}
	return MergeNativeMobJavaState(map[string]any{"Attributes": a}, MergeNativeMobNBT(ReadNativeMobJavaState(d.NativeNBT), b.breeding.encodeNBT(map[string]any{"Age": int32(b.age), "SniffTicks": int32(b.sniffTicks), "DigTicks": int32(b.digTicks), "Sniffing": byte(boolByte(b.sniffing)), "Next": int32(b.next), "DigX": int32(b.digPos[0]), "DigY": int32(b.digPos[1]), "DigZ": int32(b.digPos[2]), "ExploredPositions": positions})))
}

// SnifferConfig restores sniff/dig memory and age.
type SnifferConfig struct {
	Health, MaxHealth               float64
	Age, SniffTicks, DigTicks, Next int
	Sniffing                        bool
	DigPos                          cube.Pos
	Explored                        []cube.Pos
}

func (c SnifferConfig) Apply(d *world.EntityData) { d.Data = c.New() }
func (c SnifferConfig) New() *SnifferBehaviour {
	if c.MaxHealth <= 0 {
		c.MaxHealth = 14
	}
	if c.Health <= 0 {
		c.Health = c.MaxHealth
	}
	next := c.Next
	if next == 0 {
		next = 80 + rand.IntN(81)
	}
	return &SnifferBehaviour{health: c.Health, maxHealth: c.MaxHealth, age: c.Age, sniffTicks: c.SniffTicks, digTicks: c.DigTicks, sniffing: c.Sniffing, next: next, digPos: c.DigPos, explored: append([]cube.Pos(nil), c.Explored...), speed: .1, mc: &MovementComputer{Gravity: .08, Drag: .02, DragBeforeGravity: true}, effects: NewEffectManager()}
}

// Sniffer owns source sniff-to-dig continuation memory, not just a wandering profile.
type Sniffer struct{ *Ent }
type SnifferBehaviour struct {
	effects                         *EffectManager
	health, maxHealth               float64
	age, sniffTicks, digTicks, next int
	sniffing, dead                  bool
	digPos                          cube.Pos
	explored                        []cube.Pos
	speed                           float64
	navigator                       Navigator
	mc                              *MovementComputer
	breeding                        breeding
}

func (s *Sniffer) behaviour() *SnifferBehaviour { return s.Behaviour().(*SnifferBehaviour) }
func (s *Sniffer) Tick(tx *world.Tx, t int64) {
	s.behaviour().effects.Tick(s, tx)
	s.Ent.Tick(tx, t)
}
func (b *SnifferBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	partner, inLove := tickBreeding(&Sniffer{Ent: e}, tx)
	if b.digTicks > 0 {
		b.digTicks--
		if b.digTicks == 0 {
			b.sniffing = false
			snifferSeedDrop(e, tx, b.digPos)
			b.explored = append(b.explored, b.digPos)
			if len(b.explored) > 20 {
				b.explored = b.explored[1:]
			}
			b.next = 6000 + rand.IntN(1201)
		}
	}
	if b.sniffTicks > 0 {
		b.sniffTicks--
		if b.sniffTicks == 0 {
			b.sniffing = false
			b.digPos = cube.PosFromVec3(e.Position()).Side(cube.FaceDown)
			if snifferDiggable(tx, b.digPos) && !snifferExplored(b.explored, b.digPos) {
				b.digTicks = 120
			}
		}
	}
	if b.next > 0 {
		b.next--
	}
	if inLove && b.digTicks == 0 {
		b.sniffing, b.sniffTicks = false, 0
		navigateToPartner(&b.navigator, e, tx, partner)
	} else if b.navigator.Index >= len(b.navigator.Path) && b.next == 0 && b.digTicks == 0 {
		b.sniffing = true
		b.sniffTicks = 40 + rand.IntN(41)
		b.next = 1200
		start := cube.PosFromVec3(e.Position())
		for range 4 {
			goal := start.Add(cube.Pos{rand.IntN(17) - 8, 0, rand.IntN(17) - 8})
			if b.navigator.MoveTo(tx, start, goal, GroundProfile{MaxVisitedNodes: 160}, navigatorTick(e)) {
				break
			}
		}
	}
	rot, v := e.data.Rot, e.data.Vel
	if b.digTicks > 0 || b.sniffing {
		v[0], v[2] = 0, 0
	} else if target, ok := b.navigator.Tick(e.data.Pos, navigatorTick(e), 1.4); ok {
		d := target.Sub(e.data.Pos)
		d[1] = 0
		if d.Len() < .3 {
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
func snifferDiggable(tx *world.Tx, pos cube.Pos) bool {
	name, _ := tx.Block(pos).EncodeBlock()
	switch name {
	case "minecraft:dirt", "minecraft:coarse_dirt", "minecraft:rooted_dirt", "minecraft:grass_block", "minecraft:podzol", "minecraft:mud", "minecraft:moss_block", "minecraft:muddy_mangrove_roots":
		return true
	}
	return false
}
func snifferExplored(positions []cube.Pos, pos cube.Pos) bool {
	for _, p := range positions {
		if p == pos {
			return true
		}
	}
	return false
}
func snifferSeedDrop(e *Ent, tx *world.Tx, pos cube.Pos) {
	stacks, err := loot.Generate("minecraft:gameplay/sniffer_digging", rand.Int64(), loot.Context{Entity: "minecraft:sniffer", Origin: pos.Vec3()})
	if err != nil {
		return
	}
	for _, stack := range stacks {
		tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: pos.Vec3()}, stack))
	}
}
func (s *Sniffer) Health() float64    { return s.behaviour().health }
func (s *Sniffer) MaxHealth() float64 { return s.behaviour().maxHealth }
func (s *Sniffer) SetMaxHealth(v float64) {
	b := s.behaviour()
	b.maxHealth = v
	b.health = math.Min(v, b.health)
}
func (s *Sniffer) Dead() bool { return s.behaviour().dead || s.Health() <= 0 }
func (s *Sniffer) Hurt(v float64, src world.DamageSource) (float64, bool) {
	if s.Dead() || v <= 0 {
		return 0, false
	}
	var ok bool
	if v, ok = mobEffectDamage(s.behaviour().effects, v, src); !ok {
		return 0, false
	}
	b := s.behaviour()
	b.health -= v
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
func (s *Sniffer) Heal(v float64, _ world.HealingSource) float64 {
	if s.Dead() || v <= 0 {
		return 0
	}
	b := s.behaviour()
	old := b.health
	b.health = math.Min(b.maxHealth, b.health+v)
	return b.health - old
}
func (s *Sniffer) KnockBack(src mgl64.Vec3, f, h float64) {
	d := s.Position().Sub(src)
	d[1] = 0
	if d.Len() > 0 {
		d = d.Normalize().Mul(f)
	}
	d[1] = h
	s.SetVelocity(d)
}
func (s *Sniffer) viewAction(a world.EntityAction) {
	vs := s.tx.Viewers(s.Position())
	for _, v := range vs {
		v.ViewEntityAction(s, a)
	}
	s.tx.ReleaseViewers(vs)
}
func (s *Sniffer) Sniffing() bool { return s.behaviour().sniffing }
func (s *Sniffer) Digging() bool  { return s.behaviour().digTicks > 0 }

// SetAge sets the age in ticks; negative ages are babies growing up.
func (s *Sniffer) SetAge(age int) { s.behaviour().age = age }

// Interact feeds the sniffer torchflower seeds to breed it or let a snifflet
// grow up faster.
func (s *Sniffer) Interact(_ *world.Tx, u item.User, c *item.UseContext) bool {
	return feedBreeder(s, u, c)
}

// InLove reports whether the sniffer is in love.
func (s *Sniffer) InLove() bool { return s.behaviour().breeding.InLove() }

func (s *Sniffer) breedingState() *breeding { return &s.behaviour().breeding }
func (s *Sniffer) ageTicks() *int           { return &s.behaviour().age }

func (s *Sniffer) breedingFood(stack item.Stack) bool {
	return itemNamed(stack, "minecraft:torchflower_seeds")
}

func (s *Sniffer) breedOffspring(_ *world.Tx, _ breeder) *world.EntityHandle {
	return babyAt(s).New(SnifferType, SnifferConfig{Age: babyStartAge})
}

// spawnChildFromBreeding ports Sniffer.spawnChildFromBreeding: instead of a
// snifflet, the sniffer lays a sniffer egg item with a plop.
func (s *Sniffer) spawnChildFromBreeding(tx *world.Tx, partner breeder) {
	finishBreeding(s, partner, tx)
	tx.PlaySound(s.Position(), sound.NativeEvent{SoundType: "plop", EntityType: "minecraft:sniffer", ExtraData: -1})
	tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: s.Position()}, item.NewStack(block.SnifferEgg{}, 1)))
}

var _ Interactable = (*Sniffer)(nil)

func (s *Sniffer) FinalizeSpawn(_ string, r *java26.Random) {
	if r != nil {
		s.behaviour().next = int(r.NextInt(200))
	}
}
