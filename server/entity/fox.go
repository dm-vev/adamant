package entity

import (
	"math"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
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
	age, variant                       int
	sleeping, crouching, sitting, dead bool
	trusted                            []string
	held                               item.Stack
	speed                              float64
	breeding                           breeding
	animalAI
	// The other flags of Fox, its ticksSinceEaten and its crouchAmount,
	// which reaches 5 a second after it crouched.
	interested, pouncing, faceplanted, defending bool
	ticksSinceEaten                              int
	crouchAmount                                 float64
	shown                                        foxShownState
}

// foxShownState are the flags of a fox viewers were last shown.
type foxShownState struct {
	sleeping, crouching, sitting, interested bool
	held                                     bool
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
	return &foxState{health: NewHealthManager(health, maxHealth), effects: NewEffectManager(), age: c.Age, variant: max(0, min(c.Variant, 1)), sleeping: c.Sleeping, crouching: c.Crouching, sitting: c.Sitting, trusted: append([]string(nil), c.Trusted...), speed: .3}
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
	state.breeding.decodeNBT(s)
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
	return MergeNativeMobJavaState(canonical, MergeNativeMobNBT(ReadNativeMobJavaState(d.NativeNBT), s.breeding.encodeNBT(map[string]any{"Age": int32(s.age), "Type": int32(s.variant), "Sleeping": s.sleeping, "Crouching": s.crouching, "Sitting": s.sitting, "Trusted": append([]string(nil), s.trusted...)})))
}
func (f *Fox) b() *foxState { return f.Behaviour().(*foxState) }

// ageTicks returns the age of the fox, negative for kits.
func (f *Fox) ageTicks() *int { return &f.b().age }
func (f *Fox) Tick(tx *world.Tx, n int64) {
	f.b().effects.Tick(f, tx)
	f.Ent.Tick(tx, n)
}

func (s *foxState) Tick(e *Ent, tx *world.Tx) *Movement {
	f := &Fox{e}
	ai := &s.ai
	ai.MovementSpeed, ai.EyeHeight = s.speed, .4
	if s.age < 0 {
		ai.EyeHeight = .34375
	}
	m := ai.Mob(e, f)
	if ai.started && !f.Dead() {
		s.beforeAiStep(m)
	}
	mv := animalTick(e, tx, f, &s.animalAI, s.registerGoals)
	if !f.Dead() {
		s.afterTick(m)
	}
	return mv
}

// beforeAiStep ports what Fox.aiStep does before the AI: a fox eats the
// food in its mouth half a minute after it took it, forgets crouching and
// interest without a target, and lies still while it sleeps.
func (s *foxState) beforeAiStep(m *Mob) {
	s.ticksSinceEaten++
	if foxFood(s.held) && m.Target() == nil && m.OnGround() && !s.sleeping {
		if s.ticksSinceEaten > 600 {
			s.held = s.held.Grow(-1)
			s.ticksSinceEaten = 0
			m.updateState()
		} else if s.ticksSinceEaten > 560 && m.Random().NextFloat() < .1 {
			m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.fox.eat", Volume: 1, Pitch: 1})
			(&Fox{m.Ent}).view(EatAction{})
		}
	}
	if target := m.Target(); target == nil || !entityAlive(target) {
		s.crouching, s.interested = false, false
	}
	if s.sleeping {
		m.AI.jumping, m.AI.xxa, m.AI.zza = false, 0, 0
	}
}

// afterTick ports what Fox.tick does after the AI: a fox in water, with a
// target or in a thunderstorm wakes up, and its crouch deepens over a
// second.
func (s *foxState) afterTick(m *Mob) {
	inWater := m.InWater()
	if inWater || m.Target() != nil || m.tx.ThunderingAt(m.BlockPos()) {
		s.sleeping = false
	}
	if inWater || s.sleeping {
		s.sitting = false
	}
	if s.crouching {
		s.crouchAmount = math.Min(s.crouchAmount+.2, 5)
	} else {
		s.crouchAmount = 0
	}
	shown := foxShownState{s.sleeping, s.crouching, s.sitting, s.interested, !s.held.Empty()}
	if shown != s.shown {
		if shown.held != s.shown.held || !s.held.Empty() {
			viewers := m.tx.Viewers(m.data.Pos)
			for _, v := range viewers {
				v.ViewEntityItems(m.Self)
			}
			m.tx.ReleaseViewers(viewers)
		}
		s.shown = shown
		m.updateState()
	}
}

// foxFood reports if a stack is food a fox eats, which has a food and a
// consumable component.
func foxFood(s item.Stack) bool {
	if s.Empty() {
		return false
	}
	switch s.Item().(type) {
	case item.Potion, item.Bucket, item.OminousBottle:
		return false
	}
	_, ok := s.Item().(item.Consumable)
	return ok
}

func (f *Fox) Health() float64        { return f.b().health.Health() }
func (f *Fox) MaxHealth() float64     { return f.b().health.MaxHealth() }
func (f *Fox) SetMaxHealth(v float64) { f.b().health.SetMaxHealth(v) }
func (f *Fox) Dead() bool             { return f.b().dead || f.Health() <= 0 }

// Panicking reports whether the fox runs from danger.
func (f *Fox) Panicking() bool { return f.b().panicking() }
func (f *Fox) Hurt(d float64, src world.DamageSource) (float64, bool) {
	if f.Dead() || d <= 0 {
		return 0, false
	}
	s := f.b()
	d, ok := s.ai.DamageCooldown(f.tx, d)
	if !ok {
		return 0, false
	}
	if r, ok := s.effects.Effect(effect.Resistance); ok {
		d *= effect.Resistance.Multiplier(src, r.Level())
	}
	s.health.AddHealth(-d)
	s.ai.Hurt(src)
	resetLove(f)
	f.view(HurtAction{})
	onMobHurt(f.tx, f)
	if f.Dead() {
		s.dead = true
		f.view(DeathAction{})
		if !s.held.Empty() {
			f.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: f.Position()}, s.held))
			s.held = item.Stack{}
		}
		dropAllDeathLoot(f.tx, f, src)
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
	if u == nil || f.Dead() {
		return false
	}
	return feedBreeder(f, u, ctx)
}

// InLove reports whether the fox is in love.
func (f *Fox) InLove() bool { return f.b().breeding.InLove() }

func (f *Fox) breedingState() *breeding { return &f.b().breeding }

// breedingFood reports whether the stack is in #fox_food.
func (f *Fox) breedingFood(stack item.Stack) bool {
	return itemNamed(stack, "minecraft:sweet_berries", "minecraft:glow_berries")
}

// breedOffspring ports Fox.getBreedOffspring and FoxBreedGoal.breed: the kit
// takes the variant of either parent and trusts the players that fed them.
func (f *Fox) breedOffspring(_ *world.Tx, partner breeder) *world.EntityHandle {
	mate := partner.(*Fox)
	var trusted []string
	for _, cause := range []uuid.UUID{f.b().breeding.loveCause, mate.b().breeding.loveCause} {
		if cause != uuid.Nil && (len(trusted) == 0 || trusted[0] != cause.String()) {
			trusted = append(trusted, cause.String())
		}
	}
	return babyAt(f).New(FoxType, FoxConfig{Age: babyStartAge, Variant: randomParent(f.b().variant, mate.b().variant), Trusted: trusted})
}
func (f *Fox) FinalizeSpawn(_ string, r *vanilla.Random) {
	if r == nil {
		return
	}
	s := f.b()
	variant := 0
	biome := world.JavaBiomeName(f.tx.Biome(cube.PosFromVec3(f.Position())))
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

type foxSpawnCohort struct{ variant, count int }

// Collect ports Fox.pickUpItem: a fox takes one item of a stack in its
// mouth, spitting out what it held, when it holds nothing or swaps
// something for food a while after it ate.
func (f *Fox) Collect(stack item.Stack) (int, bool) {
	s := f.b()
	if f.Dead() || stack.Empty() || mobGriefingDisabled(f.tx) {
		return 0, false
	}
	if !s.held.Empty() && !(s.ticksSinceEaten > 0 && foxFood(stack) && !foxFood(s.held)) {
		return 0, false
	}
	if !s.held.Empty() {
		// Fox.spitOutItem.
		look := f.Rotation().Vec3()
		pos := f.Position().Add(mgl64.Vec3{look[0], 1, look[2]})
		f.tx.AddEntity(NewItemPickupDelay(world.EntitySpawnOpts{Position: pos}, s.held, 2*time.Second))
	}
	s.held = stack.Grow(1 - stack.Count())
	s.ticksSinceEaten = 0
	return 1, true
}

// HeldItems returns the item the fox holds in its mouth.
func (f *Fox) HeldItems() (item.Stack, item.Stack) { return f.b().held, item.Stack{} }

// Interested reports if the fox tilts its head at its prey.
func (f *Fox) Interested() bool { return f.b().interested }

var _ Living = (*Fox)(nil)
var _ Interactable = (*Fox)(nil)
var _ world.EntityType = foxType{}
var _ Collector = (*Fox)(nil)
