package entity

import (
	"math/rand/v2"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
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
	health                                                  *HealthManager
	effects                                                 *EffectManager
	age, variant, gene, hiddenGene, sneeze, unhappy, inLove int
	rolling, sitting, lying, dead                           bool
	speed                                                   float64
	next                                                    int
	path                                                    []cube.Pos
	index                                                   int
	mc                                                      *MovementComputer
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
	m := c.MaxHealth
	if m <= 0 {
		m = 20
	}
	h := c.Health
	if h <= 0 {
		h = m
	}
	return &pandaState{health: NewHealthManager(h, m), effects: NewEffectManager(), age: c.Age, variant: max(0, min(c.Variant, 6)), gene: max(0, min(c.Gene, 6)), hiddenGene: max(0, min(c.HiddenGene, 6)), sneeze: c.SneezeTicks, unhappy: c.UnhappyTicks, rolling: c.Rolling, sitting: c.Sitting, lying: c.LyingOnBack, speed: .15, next: 40 + rand.IntN(81), mc: &MovementComputer{Gravity: .08, Drag: .02, DragBeforeGravity: true}}
}
func (pandaType) EncodeNBT(d *world.EntityData) map[string]any {
	s := d.Data.(*pandaState)
	m := s.health.MaxHealth()
	a := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{{Name: "minecraft:health", Base: m, Current: s.health.Health(), DefaultMin: 0, DefaultMax: m, Min: 0, Max: m}})
	state := map[string]any{"Age": int32(s.age), "Variant": int32(s.variant), "MainGene": int32(s.gene), "HiddenGene": int32(s.hiddenGene), "SneezeTicks": int32(s.sneeze), "UnhappyTicks": int32(s.unhappy), "InLove": int32(s.inLove), "Rolling": s.rolling, "Sitting": s.sitting, "LyingOnBack": s.lying}
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
	d.Data.(*pandaState).inLove = int(nbtconv.Int32(s, "InLove"))
}
func (s *pandaState) Tick(e *Ent, tx *world.Tx) *Movement {
	if s.age < 0 {
		s.age = min(0, s.age+1)
	}
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
		s.unhappy--
	}
	if s.rolling || s.sitting || s.lying {
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
	} else {
		if s.next > 0 {
			s.next--
		}
		if s.index >= len(s.path) && s.next == 0 {
			s.next = 40 + rand.IntN(81)
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
				if p := findPath(tx, start, goal, 128, false, false); len(p) > 0 {
					s.path, s.index = p, 0
					break
				}
			}
		}
	}
	rot, v := e.data.Rot, e.data.Vel
	if s.index < len(s.path) && !s.sitting && !s.lying {
		d := s.path[s.index].Vec3().Add(mgl64.Vec3{.5, 0, .5}).Sub(e.data.Pos)
		d[1] = 0
		if d.Len() < .25 {
			s.index++
		} else {
			d = d.Normalize()
			v[0], v[2] = d[0]*s.speed, d[2]*s.speed
		}
	} else {
		v[0], v[2] = 0, 0
	}
	m := s.mc.TickMovement(e, e.data.Pos, v, e.data.Rot, rot, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	return m
}
func (p *Panda) b() *pandaState { return p.Behaviour().(*pandaState) }
func (p *Panda) Tick(tx *world.Tx, n int64) {
	p.b().effects.Tick(p, tx)
	p.Ent.Tick(tx, n)
	b := p.b()
	if b.inLove > 0 {
		b.inLove--
		for e := range tx.EntitiesWithin(cube.Box(-8, -4, -8, 8, 4, 8).Translate(p.Position())) {
			partner, ok := e.(*Panda)
			if !ok || partner == p || !partner.CanBreed() {
				continue
			}
			r := java26.NewLegacyRandom(rand.Int64())
			if h := p.BreedWith(tx, partner, &r); h != nil {
				tx.AddEntity(h)
				break
			}
		}
	}
}
func (p *Panda) Health() float64        { return p.b().health.Health() }
func (p *Panda) MaxHealth() float64     { return p.b().health.MaxHealth() }
func (p *Panda) SetMaxHealth(v float64) { p.b().health.SetMaxHealth(v) }
func (p *Panda) Dead() bool             { return p.b().dead || p.Health() <= 0 }
func (p *Panda) Hurt(d float64, src world.DamageSource) (float64, bool) {
	if p.Dead() || d <= 0 {
		return 0, false
	}
	p.b().health.AddHealth(-d)
	p.view(HurtAction{})
	if p.Dead() {
		p.b().dead = true
		p.view(DeathAction{})
		dropFamilyMobLoot(p.tx, p.Position(), "minecraft:panda", src)
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
func (p *Panda) Interact(_ *world.Tx, u item.User, ctx *item.UseContext) bool {
	if u == nil || ctx == nil || p.Dead() {
		return false
	}
	held, _ := u.HeldItems()
	if held.Empty() {
		return false
	}
	b := p.b()
	name, _ := held.Item().EncodeItem()
	switch name {
	case "minecraft:bamboo":
		if b.age >= 0 {
			b.sitting = true
			b.rolling = false
			b.next = 100
			if b.inLove == 0 {
				b.inLove = 600
			}
		}
		ctx.SubtractFromCount(1)
		return true
	case "minecraft:cake":
		if b.age < 0 {
			b.age = min(0, b.age+1200)
			ctx.SubtractFromCount(1)
			return true
		}
	}
	return false
}
func (p *Panda) CanBreed() bool { return !p.Dead() && p.b().age == 0 && p.b().inLove > 0 }
func (p *Panda) BreedWith(tx *world.Tx, partner *Panda, r *java26.Random) *world.EntityHandle {
	if partner == nil || partner == p || r == nil || !p.CanBreed() || !partner.CanBreed() {
		return nil
	}
	a, b := p.b(), partner.b()
	a.age, b.age = 6000, 6000
	a.inLove, b.inLove = 0, 0
	gene := a.gene
	if r.NextInt(2) == 1 {
		gene = b.gene
	}
	hidden := a.hiddenGene
	if r.NextInt(2) == 1 {
		hidden = b.hiddenGene
	}
	variant := gene
	if gene != hidden && (gene == 4 || gene == 5) {
		variant = 0
	}
	pos := p.Position().Add(partner.Position()).Mul(.5)
	return world.EntitySpawnOpts{Position: pos}.New(PandaType, PandaConfig{Age: -24000, Variant: variant, Gene: gene, HiddenGene: hidden})
}
func (p *Panda) FinalizeSpawn(_ string, r *java26.Random) {
	if r == nil {
		return
	}
	b := p.b()
	g := p.H().SpawnGroup()
	if g != nil {
		if v, ok := g.Data.(pandaGenePair); ok {
			b.gene, b.hiddenGene = v.main, v.hidden
			b.variant = pandaVariant(b.gene, b.hiddenGene)
			return
		}
	}
	b.gene = int(r.NextInt(7))
	b.hiddenGene = int(r.NextInt(7))
	b.variant = pandaVariant(b.gene, b.hiddenGene)
	if g != nil {
		g.Data = pandaGenePair{b.gene, b.hiddenGene}
	}
	if b.gene == 5 && r.NextInt(32) == 0 {
		b.sneeze = 20
	}
}
func pandaVariant(main, hidden int) int {
	if (main == 4 || main == 5) && main != hidden {
		return 0
	}
	return main
}

type pandaGenePair struct{ main, hidden int }

var _ Living = (*Panda)(nil)
var _ Interactable = (*Panda)(nil)
var _ world.EntityType = pandaType{}
