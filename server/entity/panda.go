package entity

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
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
	health                                          *HealthManager
	effects                                         *EffectManager
	age, variant, gene, hiddenGene, sneeze, unhappy int
	rolling, sitting, lying, dead                   bool
	speed                                           float64
	next                                            int
	navigator                                       Navigator
	mc                                              *MovementComputer
	panic                                           panicState
	tempt                                           temptGoal
	avoid                                           avoidGoal
	parent                                          followParentGoal
	avoiding                                        bool
	breeding                                        breeding
	// unhappyCooldown is the tick before which a panda in love without
	// bamboo nearby does not sulk again.
	unhappyCooldown int64
}

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
		return cube.Box(-.3, 0, -.3, .3, .625, .3)
	}
	return cube.Box(-.6, 0, -.6, .6, 1.25, .6)
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
	s := &pandaState{health: NewHealthManager(h, m), effects: NewEffectManager(), age: c.Age, variant: variant, gene: max(0, min(c.Gene, 6)), hiddenGene: max(0, min(c.HiddenGene, 6)), sneeze: c.SneezeTicks, unhappy: c.UnhappyTicks, rolling: c.Rolling, sitting: c.Sitting, lying: c.LyingOnBack, speed: .15, next: 40 + rand.IntN(81), mc: &MovementComputer{Gravity: .08, Drag: .02, DragBeforeGravity: true},
		// PandaPanicGoal only reacts to fire, lava, freezing, cactus and lightning.
		panic: panicState{environmental: true}}
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
	state := s.breeding.encodeNBT(map[string]any{"Age": int32(s.age), "Variant": int32(s.variant), "MainGene": int32(s.gene), "HiddenGene": int32(s.hiddenGene), "SneezeTicks": int32(s.sneeze), "UnhappyTicks": int32(s.unhappy), "Rolling": s.rolling, "Sitting": s.sitting, "LyingOnBack": s.lying})
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
	partner, inLove := tickBreeding(p, tx)
	if s.sneeze > 0 {
		s.sneeze--
		if s.sneeze == 0 {
			tx.PlaySound(e.Position(), sound.Custom{Name: "entity.panda.sneeze", Volume: 1, Pitch: 1})
			if s.gene == 5 && rand.IntN(10) == 0 {
				tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: e.Position()}, item.NewStack(item.Slimeball{}, 1)))
			}
		}
	}
	if s.unhappy > 0 {
		if s.unhappy == 29 || s.unhappy == 14 {
			tx.PlaySound(e.Position(), sound.Custom{Name: "entity.panda.cant_breed", Volume: 1, Pitch: 1})
		}
		s.unhappy--
	}
	// PandaPanicGoal stops while the panda sits.
	panicking := !s.sitting && s.panic.run(e, tx, &s.navigator)
	modifier := 1.0
	if panicking {
		s.rolling, s.lying = false, false
		modifier = 2
	} else if inLove {
		// PandaBreedGoal: the panda gets up to walk to its partner.
		s.sitting, s.lying, s.avoiding = false, false, false
		navigateToPartner(&s.navigator, e, tx, partner)
	} else if player, ok := s.tempt.next(e, tx, func(st item.Stack) bool { return itemNamed(st, "minecraft:bamboo") }, false); ok {
		// TemptGoal: a panda follows a player holding bamboo.
		s.rolling, s.sitting, s.lying, s.avoiding = false, false, false, false
		s.tempt.follow(e, tx, &s.navigator, player, 2.5)
	} else if m, ok := s.pandaAvoid(e, tx); ok {
		modifier = m
	} else if s.rolling || s.sitting || s.lying {
		if s.sitting && s.next%120 == 0 {
			s.sitting = false
		}
		s.next--
		if s.next <= 0 {
			s.rolling = false
			s.sitting = false
			s.lying = false
			s.next = 40 + rand.IntN(81)
		}
	} else if parent, ok := s.parent.next(e, tx, p, parentRange{}.orDefault()); ok {
		// FollowParentGoal: cubs keep close to adults at 1.25 times their
		// speed.
		s.parent.follow(e, tx, &s.navigator, parent)
		modifier = 1.25
	} else {
		if s.next > 0 {
			s.next--
		}
		if s.navigator.Index >= len(s.navigator.Path) && s.next == 0 {
			if s.variant == 3 && rand.IntN(4) == 0 {
				s.rolling = true
			} else if s.variant == 1 && rand.IntN(8) == 0 {
				s.lying = true
			} else if s.variant == 2 && rand.IntN(8) == 0 {
				s.sitting = true
			}
			start := cube.PosFromVec3(e.Position())
			for range 4 {
				goal := start.Add(cube.Pos{rand.IntN(13) - 6, 0, rand.IntN(13) - 6})
				if s.navigator.MoveTo(tx, start, goal, GroundProfile{MaxVisitedNodes: 128}, navigatorTick(e)) {
					break
				}
			}
		}
	}
	speed := groundSpeed(s.speed, modifier)
	rot, v := e.data.Rot, e.data.Vel
	if target, ok := s.navigator.Tick(e.data.Pos, navigatorTick(e), 1.3); ok && !s.sitting && !s.lying {
		d := target.Sub(e.data.Pos)
		d[1] = 0
		if d.Len() < .25 {
			s.navigator.Index++
		} else {
			d = d.Normalize()
			v[0], v[2] = d[0]*speed, d[2]*speed
		}
	} else {
		v[0], v[2] = 0, 0
	}
	m := s.mc.TickMovement(e, e.data.Pos, v, e.data.Rot, rot, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	return m
}

// pandaAvoid are the PandaAvoidGoals: worried pandas flee from players within
// eight blocks and monsters within four.
var pandaAvoid = []avoidSpec{
	{match: func(e world.Entity) bool { return e.H().Type().EncodeEntity() == "minecraft:player" }, distance: 8, walk: 2, sprint: 2},
	{match: func(e world.Entity) bool { return monsterClass[e.H().Type().EncodeEntity()] }, distance: 4, walk: 2, sprint: 2},
}

// pandaAvoid runs the PandaAvoidGoals of a worried panda that can perform an
// action: one that is not lying on its back, scared by a thunderstorm,
// rolling or sitting.
func (s *pandaState) pandaAvoid(e *Ent, tx *world.Tx) (float64, bool) {
	if s.variant != 2 || s.lying || s.rolling || s.sitting || tx.Thundering() {
		s.avoiding = false
		return 0, false
	}
	modifier, ok := s.avoid.next(e, tx, &s.navigator, pandaAvoid, s.avoiding)
	s.avoiding = ok
	return modifier, ok
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
func (p *Panda) Panicking() bool { return p.b().panic.active }
func (p *Panda) Hurt(d float64, src world.DamageSource) (float64, bool) {
	if p.Dead() || d <= 0 {
		return 0, false
	}
	p.b().health.AddHealth(-d)
	p.b().panic.hurt(src, p.tx.CurrentTick())
	resetLove(p)
	p.view(HurtAction{})
	onMobHurt(p.tx, p)
	if p.Dead() {
		p.b().dead = true
		p.view(DeathAction{})
		dropAllDeathLoot(p.tx, p, src)
		_ = p.CloseIn(p.tx)
	} else {
		p.b().unhappy = 80
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

// Interact ports Panda.mobInteract. Bamboo grows a cub, puts an adult in
// love, or else makes it sit down to eat; a golden dandelion pauses a cub's
// growth. A worried panda scared by a thunderstorm ignores players, and one
// lying on its back only rolls over.
func (p *Panda) Interact(tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	if u == nil || ctx == nil || p.Dead() {
		return false
	}
	b := p.b()
	if b.variant == 2 && tx.Thundering() {
		return false
	}
	if b.lying {
		b.lying = false
		return true
	}
	held, _ := u.HeldItems()
	if !p.breedingFood(held) {
		return b.age < 0 && feedBreeder(p, u, ctx)
	}
	if b.age < 0 && !b.breeding.ageLocked || b.age == 0 && !b.breeding.InLove() {
		return feedBreeder(p, u, ctx)
	}
	if b.age < 0 || b.sitting {
		return false
	}
	if _, ok := tx.Liquid(cube.PosFromVec3(p.Position())); ok {
		return false
	}
	// tryToSit and eat: the panda stops to munch on the bamboo.
	b.sitting, b.rolling, b.next = true, false, 100
	b.navigator.Path, b.navigator.Index, b.navigator.HasTarget = nil, 0, false
	ctx.SubtractFromCount(1)
	return true
}

// InLove reports whether the panda is in love.
func (p *Panda) InLove() bool { return p.b().breeding.InLove() }

func (p *Panda) breedingState() *breeding { return &p.b().breeding }

// breedingFood reports whether the stack is in #panda_food.
func (p *Panda) breedingFood(stack item.Stack) bool { return itemNamed(stack, "minecraft:bamboo") }

// moveFlagHeld reports whether the panda rolls, which PandaRollGoal does not
// let other goals interrupt.
func (p *Panda) moveFlagHeld() bool { return p.b().rolling }

// canStartBreeding ports PandaBreedGoal.canUse: a panda only breeds with
// bamboo growing within eight blocks. Without it, the panda sulks for 32
// ticks, at most once every 30 seconds.
func (p *Panda) canStartBreeding(tx *world.Tx) bool {
	b := p.b()
	if b.unhappy != 0 {
		return false
	}
	if pandaBambooNearby(tx, cube.PosFromVec3(p.Position())) {
		return true
	}
	if b.unhappyCooldown <= tx.CurrentTick() {
		b.unhappy, b.unhappyCooldown = 32, tx.CurrentTick()+600
	}
	return false
}

// pandaBambooNearby ports PandaBreedGoal.canFindBamboo: bamboo in a square of
// radius seven around the panda, up to two blocks above it.
func pandaBambooNearby(tx *world.Tx, pos cube.Pos) bool {
	for y := range 3 {
		for x := -7; x <= 7; x++ {
			for z := -7; z <= 7; z++ {
				if _, ok := tx.Block(pos.Add(cube.Pos{x, y, z})).(block.Bamboo); ok {
					return true
				}
			}
		}
	}
	return false
}

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
