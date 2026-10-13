package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"math"
	"math/rand/v2"
	"strings"
)

// FoxType is the registered Java fox species.
var FoxType foxType

type foxType struct{}
type Fox struct{ *Ent }
type FoxConfig struct {
	Health, MaxHealth            float64
	Age, Variant                 int
	Sleeping, Crouching, Sitting bool
	Trusted                      []string
}
type foxState struct {
	health                             *HealthManager
	effects                            *EffectManager
	age, variant, inLove               int
	sleeping, crouching, sitting, dead bool
	trusted                            []string
	held                               item.Stack
	speed                              float64
	next                               int
	navigator                          Navigator
	mc                                 *MovementComputer
}

func NewFox(o world.EntitySpawnOpts) *world.EntityHandle { return o.New(FoxType, FoxConfig{}) }
func (foxType) EncodeEntity() string                     { return "minecraft:fox" }
func (foxType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		FoxConfig{}.Apply(d)
	}
	return &Fox{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (foxType) BBox(e world.Entity) cube.BBox {
	if f, ok := e.(*Fox); ok && f.b().age < 0 {
		return cube.Box(-.2, 0, -.2, .2, .35, .2)
	}
	return cube.Box(-.4, 0, -.4, .4, .7, .4)
}
func (c FoxConfig) Apply(d *world.EntityData) { d.Data = c.New() }
func (c FoxConfig) New() *foxState {
	maxHealth := c.MaxHealth
	if maxHealth <= 0 {
		maxHealth = 10
	}
	health := c.Health
	if health <= 0 {
		health = maxHealth
	}
	return &foxState{health: NewHealthManager(health, maxHealth), effects: NewEffectManager(), age: c.Age, variant: max(0, min(c.Variant, 1)), sleeping: c.Sleeping, crouching: c.Crouching, sitting: c.Sitting, trusted: append([]string(nil), c.Trusted...), speed: .3, next: 40 + rand.IntN(81), mc: &MovementComputer{Gravity: .08, Drag: .02, DragBeforeGravity: true}}
}
func (foxType) DecodeNBT(n map[string]any, d *world.EntityData) {
	s := ReadNativeMobJavaState(n)
	if s == nil {
		s = n
	}
	c := FoxConfig{Age: int(nbtconv.Int32(s, "Age")), Variant: int(nbtconv.Int32(s, "Type")), Sleeping: nbtconv.Bool(s, "Sleeping"), Crouching: nbtconv.Bool(s, "Crouching"), Sitting: nbtconv.Bool(s, "Sitting")}
	if t, ok := s["Trusted"].([]string); ok {
		c.Trusted = append([]string(nil), t...)
	}
	if a, ok := nativeMobAttribute(n, "minecraft:health"); ok {
		c.Health, c.MaxHealth = a.Current, a.Base
	}
	d.Data = c.New()
	state := d.Data.(*foxState)
	state.inLove = int(nbtconv.Int32(s, "InLove"))
	if equipment := ReadNativeMobEquipment(n); len(equipment.Mainhand) > 0 {
		state.held = item.ReadNBT(equipment.Mainhand[0].Raw, nil)
	}
}
func (foxType) EncodeNBT(d *world.EntityData) map[string]any {
	s := d.Data.(*foxState)
	maxHealth := s.health.MaxHealth()
	attrs := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{{Name: "minecraft:health", Base: maxHealth, Current: s.health.Health(), DefaultMin: 0, DefaultMax: maxHealth, Min: 0, Max: maxHealth}})
	canonical := map[string]any{"Attributes": attrs}
	if !s.held.Empty() {
		name, damage := s.held.Item().EncodeItem()
		equipment := NativeMobEquipment{Mainhand: []NativeMobItem{{Name: name, Count: byte(s.held.Count()), Damage: int32(damage), WasPickedUp: 1, Raw: item.WriteNBT(s.held, true)}}}
		for key, value := range EncodeNativeMobEquipment(equipment) {
			canonical[key] = value
		}
	}
	return MergeNativeMobJavaState(canonical, MergeNativeMobNBT(ReadNativeMobJavaState(d.NativeNBT), map[string]any{"Age": int32(s.age), "Type": int32(s.variant), "Sleeping": s.sleeping, "Crouching": s.crouching, "Sitting": s.sitting, "InLove": int32(s.inLove), "Trusted": append([]string(nil), s.trusted...)}))
}
func (f *Fox) b() *foxState { return f.Behaviour().(*foxState) }
func (f *Fox) Tick(tx *world.Tx, n int64) {
	f.b().effects.Tick(f, tx)
	f.Ent.Tick(tx, n)
	s := f.b()
	threat := nearestFoxThreat(f.Ent, tx, 12)
	pos := cube.PosFromVec3(f.Position())
	if s.sleeping && (threat != nil || tx.SkyLight(pos) < 8) {
		s.sleeping = false
	} else if !s.sleeping && threat == nil && tx.SkyLight(pos) > 8 && rand.IntN(200) == 0 {
		s.sleeping = true
		s.sitting = false
		s.crouching = false
		f.SetVelocity(mgl64.Vec3{})
	}
	if s.inLove > 0 {
		s.inLove--
		for e := range tx.EntitiesWithin(cube.Box(-8, -4, -8, 8, 4, 8).Translate(f.Position())) {
			partner, ok := e.(*Fox)
			if !ok || partner == f || !partner.CanBreed() {
				continue
			}
			if h := f.BreedWith(tx, partner, ptrJavaRandom(rand.Int64())); h != nil {
				tx.AddEntity(h)
				break
			}
		}
	}
}
func (s *foxState) Tick(e *Ent, tx *world.Tx) *Movement {
	f := &Fox{e}
	if s.age < 0 {
		s.age = min(0, s.age+1)
	}
	if s.sleeping && nearestFoxThreat(e, tx, 12) != nil {
		s.sleeping = false
	}
	if s.sleeping || s.sitting {
		v := e.Velocity()
		v[0], v[2] = 0, 0
		e.data.Vel = v
	} else {
		if target := nearestFoxPrey(e, tx, 12); target != nil {
			delta := target.Position().Sub(e.Position())
			if delta.Len() < 1.2 {
				if living, ok := target.(Living); ok {
					living.Hurt(4, AttackDamageSource{Attacker: f})
				}
				s.sitting = false
				s.crouching = false
			} else {
				s.crouching = true
				s.sitting = false
				f.moveToward(target.Position())
				if delta.Len() < 4 && e.data.Vel[1] <= 0 {
					v := e.Velocity()
					v[1] = .42
					e.data.Vel = v
				}
			}
		}
		if s.next > 0 {
			s.next--
		}
		if s.navigator.Index >= len(s.navigator.Path) && s.next == 0 {
			s.next = 40 + rand.IntN(81)
			start := cube.PosFromVec3(e.Position())
			for range 4 {
				goal := start.Add(cube.Pos{rand.IntN(13) - 6, 0, rand.IntN(13) - 6})
				if s.navigator.MoveTo(tx, start, goal, GroundProfile{MaxVisitedNodes: 128}, navigatorTick(e)) {
					break
				}
			}
		}
		if !s.crouching {
			if target, ok := s.navigator.Tick(e.data.Pos, navigatorTick(e), .8); ok {
				f.moveToward(target)
			}
		}
	}
	if s.sleeping {
		e.data.Vel[0], e.data.Vel[2] = 0, 0
	}
	m := s.mc.TickMovement(e, e.data.Pos, e.data.Vel, e.data.Rot, e.data.Rot, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	return m
}
func nearestFoxThreat(e *Ent, tx *world.Tx, r float64) world.Entity {
	trusted := e.Behaviour().(*foxState).trusted
	area := cube.Box(-r, -6, -r, r, 6, r).Translate(e.Position())
	for other := range tx.EntitiesWithin(area) {
		id := other.H().Type().EncodeEntity()
		threat := id == "minecraft:polar_bear" || id == "minecraft:wolf"
		if id == "minecraft:player" {
			threat = true
			for _, uuid := range trusted {
				if uuid == other.H().UUID().String() {
					threat = false
					break
				}
			}
		}
		if threat && other.Position().Sub(e.Position()).Len() < r {
			return other
		}
	}
	return nil
}
func nearestFoxPrey(e *Ent, tx *world.Tx, r float64) world.Entity {
	area := cube.Box(-r, -6, -r, r, 6, r).Translate(e.Position())
	var nearest world.Entity
	best := r
	for other := range tx.EntitiesWithin(area) {
		id := other.H().Type().EncodeEntity()
		prey := id == "minecraft:chicken" || id == "minecraft:rabbit" || id == "minecraft:cod" || id == "minecraft:salmon" || id == "minecraft:tropical_fish" || id == "minecraft:pufferfish"
		if id == "minecraft:turtle" {
			if turtle, ok := other.(interface{ Age() int }); ok {
				prey = turtle.Age() < 0
			}
		}
		if !prey {
			continue
		}
		if living, ok := other.(Living); ok && living.Dead() {
			continue
		}
		distance := other.Position().Sub(e.Position()).Len()
		if distance < best {
			nearest, best = other, distance
		}
	}
	return nearest
}
func (f *Fox) moveToward(p mgl64.Vec3) {
	d := p.Sub(f.Position())
	d[1] = 0
	if d.Len() == 0 {
		return
	}
	d = d.Normalize()
	v := f.Velocity()
	v[0], v[2] = d[0]*f.b().speed, d[2]*f.b().speed
	f.SetVelocity(v)
	r := f.Rotation()
	r[0] = math.Atan2(-d[0], d[2]) * 180 / math.Pi
	f.data.Rot = r
}
func (f *Fox) Health() float64        { return f.b().health.Health() }
func (f *Fox) MaxHealth() float64     { return f.b().health.MaxHealth() }
func (f *Fox) SetMaxHealth(v float64) { f.b().health.SetMaxHealth(v) }
func (f *Fox) Dead() bool             { return f.b().dead || f.Health() <= 0 }
func (f *Fox) Hurt(d float64, _ world.DamageSource) (float64, bool) {
	if f.Dead() || d <= 0 {
		return 0, false
	}
	f.b().health.AddHealth(-d)
	f.view(HurtAction{})
	if f.Dead() {
		s := f.b()
		s.dead = true
		f.view(DeathAction{})
		if !s.held.Empty() {
			f.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: f.Position()}, s.held))
			s.held = item.Stack{}
		}
		_ = f.CloseIn(f.tx)
	}
	return d, true
}
func (f *Fox) Heal(d float64, _ world.HealingSource) float64 {
	if f.Dead() || d <= 0 {
		return 0
	}
	old := f.Health()
	f.b().health.AddHealth(d)
	return f.Health() - old
}
func (f *Fox) AddEffect(e effect.Effect)  { f.b().effects.Add(e, f) }
func (f *Fox) RemoveEffect(t effect.Type) { f.b().effects.Remove(t, f) }
func (f *Fox) Effects() []effect.Effect   { return f.b().effects.Effects() }
func (f *Fox) Speed() float64             { return f.b().speed }
func (f *Fox) SetSpeed(v float64)         { f.b().speed = v }
func (f *Fox) KnockBack(src mgl64.Vec3, k, h float64) {
	d := f.Position().Sub(src)
	d[1] = 0
	if d.Len() > 0 {
		d = d.Normalize().Mul(k)
	}
	d[1] = h
	f.SetVelocity(d)
}
func (f *Fox) view(a world.EntityAction) {
	v := f.tx.Viewers(f.Position())
	for _, x := range v {
		x.ViewEntityAction(f, a)
	}
	f.tx.ReleaseViewers(v)
}
func (f *Fox) Variant() int    { return f.b().variant }
func (f *Fox) Sleeping() bool  { return f.b().sleeping }
func (f *Fox) Sitting() bool   { return f.b().sitting }
func (f *Fox) Crouching() bool { return f.b().crouching }
func (f *Fox) Interact(_ *world.Tx, u item.User, ctx *item.UseContext) bool {
	if u == nil || ctx == nil || f.Dead() {
		return false
	}
	held, _ := u.HeldItems()
	if !f.Feed(held) {
		return false
	}
	ctx.SubtractFromCount(1)
	return true
}
func (f *Fox) Feed(food item.Stack) bool {
	if food.Empty() {
		return false
	}
	name, _ := food.Item().EncodeItem()
	if name != "minecraft:sweet_berries" && name != "minecraft:glow_berries" {
		return false
	}
	s := f.b()
	if s.age < 0 {
		s.age = min(0, s.age+1200)
		return true
	}
	if s.inLove == 0 {
		s.inLove = 600
		return true
	}
	return false
}
func (f *Fox) CanBreed() bool { return !f.Dead() && f.b().age == 0 && f.b().inLove > 0 }
func (f *Fox) BreedWith(tx *world.Tx, partner *Fox, r *java26.Random) *world.EntityHandle {
	if partner == nil || partner == f || r == nil || !f.CanBreed() || !partner.CanBreed() {
		return nil
	}
	a, b := f.b(), partner.b()
	a.age, b.age = 6000, 6000
	a.inLove, b.inLove = 0, 0
	variant := a.variant
	if r.NextInt(2) == 1 {
		variant = b.variant
	}
	trusted := append([]string(nil), a.trusted...)
	for _, id := range b.trusted {
		if len(trusted) < 2 && (len(trusted) == 0 || trusted[0] != id) {
			trusted = append(trusted, id)
		}
	}
	pos := f.Position().Add(partner.Position()).Mul(.5)
	return world.EntitySpawnOpts{Position: pos}.New(FoxType, FoxConfig{Age: -24000, Variant: variant, Trusted: trusted})
}
func (f *Fox) FinalizeSpawn(_ string, r *java26.Random) {
	if r == nil {
		return
	}
	s := f.b()
	variant := 0
	biome := f.tx.Biome(cube.PosFromVec3(f.Position())).String()
	if strings.Contains(biome, "snowy_taiga") || strings.Contains(biome, "grove") {
		variant = 1
	}
	g := f.H().SpawnGroup()
	if g != nil {
		if cohort, ok := g.Data.(foxSpawnCohort); ok {
			s.variant = cohort.variant
			if cohort.count > 0 {
				s.age = -24000
			}
			cohort.count++
			g.Data = cohort
			return
		}
		s.variant = variant
		g.Data = foxSpawnCohort{variant: s.variant, count: 1}
		return
	}
	s.variant = variant
}
func ptrJavaRandom(seed int64) *java26.Random { r := java26.NewLegacyRandom(seed); return &r }

type foxSpawnCohort struct{ variant, count int }

func (f *Fox) Collect(stack item.Stack) (int, bool) {
	s := f.b()
	if f.Dead() || stack.Empty() || !s.held.Empty() {
		return 0, false
	}
	s.held = item.NewStack(stack.Item(), stack.Count())
	return stack.Count(), true
}

var _ Living = (*Fox)(nil)
var _ Interactable = (*Fox)(nil)
var _ world.EntityType = foxType{}
var _ Collector = (*Fox)(nil)
