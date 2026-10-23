package entity

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// PandaType is the registered Java panda species.
var PandaType pandaType

type pandaType struct{}
type Panda struct{ *Ent }
type PandaConfig struct {
	Health, MaxHealth             float64
	Age, Variant                  int
	Gene, HiddenGene              int
	SneezeTicks, UnhappyTicks     int
	Rolling, Sitting, LyingOnBack bool
}
type pandaState struct {
	health   *HealthManager
	effects  *EffectManager
	breeding breeding
	animalAI
	age, variant, gene, hiddenGene int
	speed                          float64
	dead                           bool
	// The flags and counters of Panda: sneezing and how long for, sulking,
	// eating, rolling, sitting and lying on its back.
	sneezing                bool
	sneezeCounter, unhappy  int
	eatCounter, rollCounter int
	rolling, sitting, lying bool
	rollDelta               mgl64.Vec3
	gotBamboo, didBite      bool
	held                    item.Stack
	lookAtPlayer            *pandaLookAtPlayerGoal
	shown                   pandaShownState
}

// pandaShownState are the flags of a panda viewers were last shown.
type pandaShownState struct{ sitting, rolling, lying, sneezing, eating, scared bool }

func NewPanda(o world.EntitySpawnOpts) *world.EntityHandle { return o.New(PandaType, PandaConfig{}) }
func (pandaType) EncodeEntity() string                     { return "minecraft:panda" }
func (pandaType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		PandaConfig{}.Apply(d)
	}
	return &Panda{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (pandaType) BBox(e world.Entity) cube.BBox {
	if p, ok := e.(*Panda); ok && p.b().age < 0 {
		return cube.Box(-.325, 0, -.325, .325, .625, .325)
	}
	return cube.Box(-.65, 0, -.65, .65, 1.25, .65)
}
func (c PandaConfig) Apply(d *world.EntityData) { d.Data = c.New() }
func (c PandaConfig) New() *pandaState {
	variant := max(0, min(c.Variant, 6))
	m := c.MaxHealth
	if m <= 0 {
		m = 20
		if variant == 5 {
			m = 10
		}
	}
	h := c.Health
	if h <= 0 {
		h = m
	}
	s := &pandaState{health: NewHealthManager(h, m), effects: NewEffectManager(), age: c.Age, variant: variant, gene: max(0, min(c.Gene, 6)), hiddenGene: max(0, min(c.HiddenGene, 6)), sneezeCounter: c.SneezeTicks, unhappy: c.UnhappyTicks, rolling: c.Rolling, sitting: c.Sitting, lying: c.LyingOnBack, speed: .15}
	if variant == 1 {
		s.speed = .07
	}
	return s
}

// setAttributes ports Panda.setAttributes: weak pandas have 10 health and lazy
// ones walk at less than half the speed.
func (s *pandaState) setAttributes() {
	if s.variant == 5 {
		s.health.SetMaxHealth(10)
	}
	if s.variant == 1 {
		s.speed = .07
	}
}
func (pandaType) EncodeNBT(d *world.EntityData) map[string]any {
	s := d.Data.(*pandaState)
	m := s.health.MaxHealth()
	a := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{{Name: "minecraft:health", Base: m, Current: s.health.Health(), DefaultMin: 0, DefaultMax: m, Min: 0, Max: m}})
	state := s.breeding.encodeNBT(map[string]any{"Age": int32(s.age), "Variant": int32(s.variant), "MainGene": int32(s.gene), "HiddenGene": int32(s.hiddenGene), "SneezeTicks": int32(s.sneezeCounter), "UnhappyTicks": int32(s.unhappy), "Rolling": s.rolling, "Sitting": s.sitting, "LyingOnBack": s.lying})
	return MergeNativeMobJavaState(map[string]any{"Attributes": a}, MergeNativeMobNBT(ReadNativeMobJavaState(d.NativeNBT), state))
}
func (pandaType) DecodeNBT(n map[string]any, d *world.EntityData) {
	s := ReadNativeMobJavaState(n)
	if s == nil {
		s = n
	}
	c := PandaConfig{Age: int(nbtconv.Int32(s, "Age")), Variant: int(nbtconv.Int32(s, "Variant")), Gene: int(nbtconv.Int32(s, "MainGene")), HiddenGene: int(nbtconv.Int32(s, "HiddenGene")), SneezeTicks: int(nbtconv.Int32(s, "SneezeTicks")), UnhappyTicks: int(nbtconv.Int32(s, "UnhappyTicks")), Rolling: nbtconv.Bool(s, "Rolling"), Sitting: nbtconv.Bool(s, "Sitting"), LyingOnBack: nbtconv.Bool(s, "LyingOnBack")}
	if a, ok := nativeMobAttribute(n, "minecraft:health"); ok {
		c.Health, c.MaxHealth = a.Current, a.Base
	}
	d.Data = c.New()
	d.Data.(*pandaState).breeding.decodeNBT(s)
}
func (s *pandaState) Tick(e *Ent, tx *world.Tx) *Movement {
	p := &Panda{Ent: e}
	ai := &s.ai
	// Panda.BABY_DIMENSIONS lowers the eyes of a cub to 0.28125.
	ai.MovementSpeed, ai.EyeHeight = s.speed, 1.0625
	if s.age < 0 {
		ai.EyeHeight = .28125
	}
	mv := animalTick(e, tx, p, &s.animalAI, s.registerGoals)
	if !p.Dead() {
		s.afterTick(ai.Mob(e, p))
	}
	if shown := (pandaShownState{s.sitting, s.rolling, s.lying, s.sneezing, s.eating(), s.scared(tx)}); shown != s.shown {
		s.shown = shown
		p.updateState()
	}
	return mv
}

func (p *Panda) b() *pandaState { return p.Behaviour().(*pandaState) }

// ageTicks returns the age of the panda, negative for cubs.
func (p *Panda) ageTicks() *int { return &p.b().age }
func (p *Panda) Tick(tx *world.Tx, n int64) {
	p.b().effects.Tick(p, tx)
	p.Ent.Tick(tx, n)
}
func (p *Panda) Health() float64        { return p.b().health.Health() }
func (p *Panda) MaxHealth() float64     { return p.b().health.MaxHealth() }
func (p *Panda) SetMaxHealth(v float64) { p.b().health.SetMaxHealth(v) }
func (p *Panda) Dead() bool             { return p.b().dead || p.Health() <= 0 }

// Panicking reports whether the panda runs from fire or other hazards.
func (p *Panda) Panicking() bool { return p.b().panicking() }

// Hurt ports Panda.hurtServer: a hurt panda stands up.
func (p *Panda) Hurt(d float64, src world.DamageSource) (float64, bool) {
	s := p.b()
	if p.Dead() || d <= 0 {
		return 0, false
	}
	s.sitting = false
	d, ok := s.ai.DamageCooldown(p.tx, d)
	if !ok {
		return 0, false
	}
	if r, ok := s.effects.Effect(effect.Resistance); ok {
		d *= effect.Resistance.Multiplier(src, r.Level())
	}
	s.health.AddHealth(-d)
	s.ai.Hurt(src)
	resetLove(p)
	p.view(HurtAction{})
	onMobHurt(p.tx, p)
	if p.Dead() {
		s.dead = true
		p.view(DeathAction{})
		if !s.held.Empty() {
			// Panda.pickUpItem guarantees the drop of what it picked up.
			p.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: p.Position()}, s.held))
			s.held = item.Stack{}
		}
		dropAllDeathLoot(p.tx, p, src)
		_ = p.CloseIn(p.tx)
	}
	return d, true
}
func (p *Panda) Heal(d float64, _ world.HealingSource) float64 {
	if p.Dead() || d <= 0 {
		return 0
	}
	old := p.Health()
	p.b().health.AddHealth(d)
	return p.Health() - old
}
func (p *Panda) AddEffect(e effect.Effect)  { p.b().effects.Add(e, p) }
func (p *Panda) RemoveEffect(t effect.Type) { p.b().effects.Remove(t, p) }
func (p *Panda) Effects() []effect.Effect   { return p.b().effects.Effects() }
func (p *Panda) Speed() float64             { return p.b().speed }
func (p *Panda) SetSpeed(v float64)         { p.b().speed = v }
func (p *Panda) KnockBack(src mgl64.Vec3, k, h float64) {
	d := p.Position().Sub(src)
	d[1] = 0
	if d.Len() > 0 {
		d = d.Normalize().Mul(k)
	}
	d[1] = h
	p.SetVelocity(d)
}
func (p *Panda) view(a world.EntityAction) {
	v := p.tx.Viewers(p.Position())
	for _, x := range v {
		x.ViewEntityAction(p, a)
	}
	p.tx.ReleaseViewers(v)
}
func (p *Panda) Variant() int    { return p.b().variant }
func (p *Panda) Gene() int       { return p.b().gene }
func (p *Panda) HiddenGene() int { return p.b().hiddenGene }
func (p *Panda) Age() int        { return p.b().age }

// Sitting, Rolling, LyingOnBack, Sneezing, Eating and Scared report what
// the panda does.
func (p *Panda) Sitting() bool     { return p.b().sitting }
func (p *Panda) Rolling() bool     { return p.b().rolling }
func (p *Panda) LyingOnBack() bool { return p.b().lying }
func (p *Panda) Sneezing() bool    { return p.b().sneezing }
func (p *Panda) Eating() bool      { return p.b().eating() }
func (p *Panda) Scared() bool      { return p.b().scared(p.tx) }

// HeldItems returns the food the panda holds.
func (p *Panda) HeldItems() (item.Stack, item.Stack) { return p.b().held, item.Stack{} }

// Collect ports Panda.pickUpItem: a panda holding nothing picks up food it
// eats off the ground.
func (p *Panda) Collect(stack item.Stack) (int, bool) {
	s := p.b()
	if p.Dead() || stack.Empty() || !s.held.Empty() || !pandaEatsFromGround(stack) || mobGriefingDisabled(p.tx) {
		return 0, false
	}
	s.held = stack
	s.viewHeld(s.ai.Mob(p.Ent, p))
	return stack.Count(), true
}

// Interact ports Panda.mobInteract. Bamboo grows a cub, puts an adult in
// love, or else makes it sit down to eat; a golden dandelion pauses a cub's
// growth. A worried panda scared by a thunderstorm ignores players, and one
// lying on its back only rolls over.
func (p *Panda) Interact(tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	if u == nil || ctx == nil || p.Dead() {
		return false
	}
	s := p.b()
	if s.scared(tx) {
		return false
	}
	if s.lying {
		s.lying = false
		return true
	}
	held, _ := u.HeldItems()
	if !pandaFood(held) {
		return s.age < 0 && feedBreeder(p, u, ctx)
	}
	m := s.ai.Mob(p.Ent, p)
	if m.Target() != nil {
		s.gotBamboo = true
	}
	if s.age < 0 && !s.breeding.ageLocked || s.age == 0 && !s.breeding.InLove() {
		return feedBreeder(p, u, ctx)
	}
	if s.age < 0 || s.sitting || m.InWater() {
		return false
	}
	// tryToSit and eat: the panda sits down to munch on the bamboo,
	// dropping what it held.
	s.tryToSit(m)
	s.eatCounter = 1
	if !s.held.Empty() {
		if c, ok := u.(interface{ GameMode() world.GameMode }); !ok || !c.GameMode().CreativeInventory() {
			tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: p.Position()}, s.held))
		}
	}
	s.held = held.Grow(1 - held.Count())
	s.viewHeld(m)
	ctx.SubtractFromCount(1)
	return true
}

// InLove reports whether the panda is in love.
func (p *Panda) InLove() bool { return p.b().breeding.InLove() }

func (p *Panda) breedingState() *breeding { return &p.b().breeding }

// breedingFood reports whether the stack is in #panda_food.
func (p *Panda) breedingFood(stack item.Stack) bool { return pandaFood(stack) }

// breedOffspring ports Panda.getBreedOffspring and setGeneFromParents: the
// cub inherits a random gene of each parent, either of which mutates with a
// chance of 1 in 32.
func (p *Panda) breedOffspring(_ *world.Tx, partner breeder) *world.EntityHandle {
	a, b := p.b(), partner.(*Panda).b()
	r := vanilla.NewLegacyRandom(rand.Int64())
	oneOf := func(s *pandaState) int {
		if r.NextBoolean() {
			return s.gene
		}
		return s.hiddenGene
	}
	var main, hidden int
	if r.NextBoolean() {
		main, hidden = oneOf(a), oneOf(b)
	} else {
		main, hidden = oneOf(b), oneOf(a)
	}
	if r.NextInt(32) == 0 {
		main = pandaRandomGene(&r)
	}
	if r.NextInt(32) == 0 {
		hidden = pandaRandomGene(&r)
	}
	return babyAt(p).New(PandaType, PandaConfig{Age: babyStartAge, Variant: pandaVariant(main, hidden), Gene: main, HiddenGene: hidden})
}

// pandaGroup is the AgeableMobGroupData of naturally spawned pandas.
type pandaGroup struct{ size int }

// FinalizeSpawn ports Panda.finalizeSpawn: each panda rolls its genes, and
// after the first panda of a group each has a 20% chance to be a cub.
func (p *Panda) FinalizeSpawn(_ string, r *vanilla.Random) {
	if r == nil {
		return
	}
	b := p.b()
	b.gene, b.hiddenGene = pandaRandomGene(r), pandaRandomGene(r)
	b.variant = pandaVariant(b.gene, b.hiddenGene)
	b.setAttributes()
	g := p.H().SpawnGroup()
	if g == nil {
		return
	}
	group, _ := g.Data.(pandaGroup)
	if group.size > 0 && r.NextFloat() <= .2 {
		b.age = babyStartAge
	}
	group.size++
	g.Data = group
}

// pandaRandomGene ports Panda.Gene.getRandom.
func pandaRandomGene(r *vanilla.Random) int {
	switch n := r.NextInt(16); {
	case n == 0:
		return 1
	case n == 1:
		return 2
	case n == 2:
		return 3
	case n == 4:
		return 6
	case n < 9:
		return 5
	case n < 11:
		return 4
	default:
		return 0
	}
}
func pandaVariant(main, hidden int) int {
	if (main == 4 || main == 5) && main != hidden {
		return 0
	}
	return main
}

var _ Living = (*Panda)(nil)
var _ Interactable = (*Panda)(nil)
var _ world.EntityType = pandaType{}
