package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Each nether beast has a distinct native type and concrete public entity.
type netherBeastKind uint8

const (
	hoglinKind netherBeastKind = iota
	zoglinKind
)

type netherBeastState struct {
	health                          *HealthManager
	kind                            netherBeastKind
	effects                         *EffectManager
	mc                              *MovementComputer
	age, attackCooldown, conversion int
	immune, dead                    bool
	targetUUID                      string
	speed                           float64
	navigator                       Navigator
	wander, repellentScan           int
	repellent                       *cube.Pos
	breeding                        breeding
	// pacified counts down the ticks a hoglin stays peaceful after it saw a
	// repellent, as BecomePassiveIfMemoryPresent remembers.
	pacified int
}
type netherBeastConfig struct {
	kind                       netherBeastKind
	health                     float64
	age                        int
	immune                     bool
	attackCooldown, conversion int
}

func (c netherBeastConfig) Apply(d *world.EntityData) {
	h, speed := 40.0, .3
	if c.health > 0 {
		h = c.health
	}
	d.Data = &netherBeastState{kind: c.kind, health: NewHealthManager(h, h), effects: NewEffectManager(), mc: &MovementComputer{Gravity: .08, Drag: .02, DragBeforeGravity: true}, age: c.age, immune: c.immune, attackCooldown: c.attackCooldown, conversion: c.conversion, speed: speed}
}
func (t netherBeastType) EncodeEntity() string { return t.id }
func (t netherBeastType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		netherBeastConfig{kind: t.kind}.Apply(d)
	}
	e := &Ent{tx: tx, handle: h, data: d}
	if t.kind == zoglinKind {
		return &Zoglin{e}
	}
	return &Hoglin{e}
}
func (t netherBeastType) BBox(e world.Entity) cube.BBox {
	small := false
	if a, ok := e.(interface{ beast() *netherBeastState }); ok {
		small = a.beast().age < 0
	}
	if small {
		return cube.Box(-.375, 0, -.375, .375, .85, .375)
	}
	return cube.Box(-.7, 0, -.7, .7, 1.4, .7)
}
func (t netherBeastType) AttributeDefaults() []NativeMobAttribute {
	health, speed, damage := 40.0, .3, 6.0
	return []NativeMobAttribute{{Name: "minecraft:health", Base: health, Current: health, DefaultMin: 0, DefaultMax: health, Min: 0, Max: health}, {Name: "minecraft:movement", Base: speed, Current: speed, DefaultMin: 0, DefaultMax: speed, Min: 0, Max: speed}, {Name: "minecraft:attack_damage", Base: damage, Current: damage, DefaultMin: 0, DefaultMax: damage, Min: 0, Max: damage}}
}

type Hoglin struct{ *Ent }
type Zoglin struct{ *Ent }

func (h *Hoglin) beast() *netherBeastState { return h.Behaviour().(*netherBeastState) }
func (z *Zoglin) beast() *netherBeastState { return z.Behaviour().(*netherBeastState) }
func (t netherBeastType) DecodeNBT(n map[string]any, d *world.EntityData) {
	j := ReadNativeMobJavaState(n)
	if j == nil {
		j = n
	}
	age := int(nbtconv.Int32(j, "Age"))
	if age == 0 && nbtconv.Bool(j, "IsBaby") {
		age = -24000
	}
	c := netherBeastConfig{kind: t.kind, age: age, immune: nbtconv.Bool(n, "IsImmuneToZombification"), attackCooldown: int(nbtconv.Int32(j, "AttackCooldown")), conversion: int(nbtconv.Int32(n, "TimeInOverworld"))}
	if x, ok := nativeMobAttribute(n, "minecraft:health"); ok {
		c.health = x.Current
	} else if _, ok := j["Health"]; ok {
		c.health = float64(nbtconv.Float32(j, "Health"))
	}
	c.Apply(d)
	s := d.Data.(*netherBeastState)
	s.breeding.decodeNBT(j)
}
func (t netherBeastType) EncodeNBT(d *world.EntityData) map[string]any {
	s := d.Data.(*netherBeastState)
	attrs := t.AttributeDefaults()
	attrs[0].Current = s.health.Health()
	attrs[1].Current = s.speed
	native := map[string]any{"Attributes": EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), attrs), "IsBaby": byte(boolByte(s.age < 0))}
	if t.kind == hoglinKind {
		native["IsImmuneToZombification"] = byte(boolByte(s.immune))
		native["TimeInOverworld"] = int32(s.conversion)
	}
	out := MergeNativeMobNBT(d.NativeNBT, native)
	j := ReadNativeMobJavaState(d.NativeNBT)
	if j == nil {
		j = make(map[string]any)
	}
	j["Age"], j["AttackCooldown"] = int32(s.age), int32(s.attackCooldown)
	if t.kind != zoglinKind {
		s.breeding.encodeNBT(j)
	}
	return MergeNativeMobJavaState(out, j)
}
func (h *Hoglin) SpeciesType() world.EntityType { return HoglinType }
func (z *Zoglin) SpeciesType() world.EntityType { return ZoglinType }
func (h *Hoglin) Tick(tx *world.Tx, n int64)    { h.beast().effects.Tick(h, tx); h.Ent.Tick(tx, n) }
func (z *Zoglin) Tick(tx *world.Tx, n int64)    { z.beast().effects.Tick(z, tx); z.Ent.Tick(tx, n) }
func (s *netherBeastState) Tick(e *Ent, tx *world.Tx) *Movement {
	// Hoglins are animals that grow up and breed; zoglins stay as they
	// were born.
	var partner mgl64.Vec3
	var inLove bool
	if s.kind == hoglinKind {
		partner, inLove = tickBreeding(&Hoglin{Ent: e}, tx)
	}
	if s.attackCooldown > 0 {
		s.attackCooldown--
	}
	if s.kind == hoglinKind {
		if s.repellentScan > 0 {
			s.repellentScan--
		}
		if s.repellentScan == 0 {
			s.repellent = nearestHoglinRepellent(tx, e.Position(), 8, 4)
			s.repellentScan = 10
		}
		if s.repellent != nil {
			s.pacified = 200
		} else if s.pacified > 0 {
			s.pacified--
		}
	}
	if s.kind == hoglinKind {
		if !s.immune && tx.World().Dimension() == world.Overworld {
			s.conversion++
			if s.conversion > 300 {
				new := world.EntitySpawnOpts{Position: e.Position(), Rotation: e.Rotation(), SpawnReason: "conversion"}.New(ZoglinType, netherBeastConfig{kind: zoglinKind, age: s.age})
				tx.AddEntity(new)
				_ = e.CloseIn(tx)
				return nil
			}
		} else if tx.World().Dimension() != world.Overworld {
			s.conversion = 0
		}
	}
	repellent := s.repellent
	target, dist := beastTarget(e, tx, s.kind)
	if s.kind == hoglinKind && s.targetUUID != "" {
		for candidate := range tx.Entities() {
			if candidate.H().UUID().String() == s.targetUUID {
				if l, ok := candidate.(Living); ok && !l.Dead() {
					target = candidate
					dist = candidate.Position().Sub(e.Position()).Len()
				}
				break
			}
		}
	}
	if s.kind == hoglinKind && target != nil && dist < 8 && repellent != nil {
		target = nil
	}
	if s.kind == zoglinKind && target == nil {
		target, dist = beastTarget(e, tx, s.kind)
	}
	if s.kind == hoglinKind && inLove {
		// AnimalMakeLove: a breeding hoglin forgets its attack target and
		// walks to its partner at 0.6 times its speed.
		beastMoveAt(e, partner, groundSpeed(.3, .6))
		return beastMoveFinish(s, e, tx)
	}
	if target != nil {
		if dist < 2.25 && s.attackCooldown == 0 {
			damage := 6.0
			if s.age < 0 {
				damage = 0.5
			}
			if l, ok := target.(Living); ok {
				l.Hurt(damage, AttackDamageSource{Attacker: e})
				s.attackCooldown = 20
				if s.kind == hoglinKind {
					if s.age < 0 {
						s.attackCooldown = 15
					} else {
						s.attackCooldown = 40
					}
				}
				if s.age >= 0 {
					d := target.Position().Sub(e.Position())
					d[1] = .5
					if d.Len() > 0 {
						d = d.Normalize().Mul(1.2)
					}
					if knock, ok := target.(interface{ SetVelocity(mgl64.Vec3) }); ok {
						knock.SetVelocity(d)
					}
				}
				if s.kind == hoglinKind {
					hoglinAlert(e, tx, target)
				}
			}
		} else {
			beastMove(s, e, tx, target.Position())
		}
		return beastMoveFinish(s, e, tx)
	}
	if s.kind == hoglinKind && repellent != nil {
		away := e.Position().Sub(repellent.Vec3().Add(mgl64.Vec3{.5, 0, .5}))
		away[1] = 0
		if away.Len() > 0 {
			beastMove(s, e, tx, e.Position().Add(away.Normalize().Mul(8)))
			return beastMoveFinish(s, e, tx)
		}
	}
	return beastWander(s, e, tx)
}
func beastMove(s *netherBeastState, e *Ent, tx *world.Tx, p mgl64.Vec3) {
	beastMoveAt(e, p, s.speed)
}

func beastMoveAt(e *Ent, p mgl64.Vec3, speed float64) {
	d := p.Sub(e.Position())
	d[1] = 0
	if d.Len() > 0 {
		d = d.Normalize()
	}
	v := e.data.Vel
	v[0], v[2] = d[0]*speed, d[2]*speed
	r := e.data.Rot
	if d.Len() > 0 {
		r[0] = math.Atan2(-d[0], d[2]) * 180 / math.Pi
	}
	e.data.Rot = r
	e.data.Vel = v
}
func beastMoveFinish(s *netherBeastState, e *Ent, tx *world.Tx) *Movement {
	m := s.mc.TickMovement(e, e.data.Pos, e.data.Vel, e.data.Rot, e.data.Rot, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	return m
}
func beastWander(s *netherBeastState, e *Ent, tx *world.Tx) *Movement {
	if s.wander > 0 {
		s.wander--
	}
	if s.navigator.Index >= len(s.navigator.Path) && s.wander == 0 {
		s.wander = 50
		start := cube.PosFromVec3(e.Position())
		for range 4 {
			goal := start.Add(cube.Pos{int(s.mcRandom(e, tx, 13)) - 6, 0, int(s.mcRandom(e, tx, 13)) - 6})
			if s.navigator.MoveTo(tx, start, goal, GroundProfile{MaxVisitedNodes: 96}, navigatorTick(e)) {
				break
			}
		}
	}
	if p, ok := s.navigator.Tick(e.Position(), navigatorTick(e), .9); ok {
		if p.Sub(e.Position()).Len() < .3 {
			s.navigator.Index++
		} else {
			beastMove(s, e, tx, p)
		}
	} else {
		v := e.data.Vel
		v[0], v[2] = 0, 0
		e.data.Vel = v
	}
	return beastMoveFinish(s, e, tx)
}
func (s *netherBeastState) mcRandom(e *Ent, tx *world.Tx, n int) int {
	return int((uint64(tx.CurrentTick()+int64(cube.PosFromVec3(e.Position()).X()*17+cube.PosFromVec3(e.Position()).Z()*31))%uint64(n) + uint64(n)) % uint64(n))
}
func beastTarget(e *Ent, tx *world.Tx, k netherBeastKind) (world.Entity, float64) {
	r := 16.0
	box := cube.Box(-r, -8, -r, r, 8, r).Translate(e.Position())
	var target world.Entity
	best := r
	for other := range tx.EntitiesWithin(box) {
		if other == e {
			continue
		}
		id := other.H().Type().EncodeEntity()
		if k == hoglinKind && (id == "minecraft:hoglin" || id == "minecraft:zoglin" || id == "minecraft:piglin" || id == "minecraft:piglin_brute" || id == "minecraft:zombified_piglin") {
			continue
		}
		if k == zoglinKind && (id == "minecraft:zoglin" || id == "minecraft:creeper") {
			continue
		}
		l, ok := other.(Living)
		if !ok || l.Dead() {
			continue
		}
		d := other.Position().Sub(e.Position()).Len()
		if d < best {
			target, best = other, d
		}
	}
	return target, best
}
func hoglinAlert(e *Ent, tx *world.Tx, target world.Entity) {
	for other := range tx.EntitiesWithin(cube.Box(-16, -8, -16, 16, 8, 16).Translate(e.Position())) {
		if h, ok := other.(*Hoglin); ok && h.H() != e.handle && h.beast().age >= 0 {
			h.beast().targetUUID = target.H().UUID().String()
		}
	}
}
func nearestHoglinRepellent(tx *world.Tx, p mgl64.Vec3, h, v int) *cube.Pos {
	start := cube.PosFromVec3(p)
	for x := -h; x <= h; x++ {
		for y := -v; y <= v; y++ {
			for z := -h; z <= h; z++ {
				q := start.Add(cube.Pos{x, y, z})
				id, _ := tx.Block(q).EncodeBlock()
				switch id {
				case "minecraft:warped_fungus", "minecraft:nether_portal", "minecraft:respawn_anchor":
					return &q
				}
			}
		}
	}
	return nil
}
func (h *Hoglin) Health() float64        { return h.beast().health.Health() }
func (z *Zoglin) Health() float64        { return z.beast().health.Health() }
func (h *Hoglin) MaxHealth() float64     { return h.beast().health.MaxHealth() }
func (z *Zoglin) MaxHealth() float64     { return z.beast().health.MaxHealth() }
func (h *Hoglin) SetMaxHealth(v float64) { h.beast().health.SetMaxHealth(v) }
func (z *Zoglin) SetMaxHealth(v float64) { z.beast().health.SetMaxHealth(v) }
func (h *Hoglin) Dead() bool             { return h.beast().dead || h.Health() <= 0 }
func (z *Zoglin) Dead() bool             { return z.beast().dead || z.Health() <= 0 }
func beastHurt(e *Ent, self world.Entity, b *netherBeastState, d float64, src world.DamageSource) (float64, bool) {
	if d <= 0 || b.dead {
		return 0, false
	}
	if src.Fire() {
		if _, ok := b.effects.Effect(effect.FireResistance); ok {
			return 0, false
		}
	}
	if r, ok := b.effects.Effect(effect.Resistance); ok {
		d *= effect.Resistance.Multiplier(src, r.Level())
	}
	b.health.AddHealth(-d)
	if a, ok := self.(breeder); ok {
		// Animal.hurtServer ends love mode, and Hoglin.wasHurtBy ends its
		// peace with the attacker.
		resetLove(a)
		b.pacified = 0
	}
	beastViewAction(e, HurtAction{})
	onMobHurt(e.tx, self)
	if b.health.Health() <= 0 {
		b.dead = true
		beastViewAction(e, DeathAction{})
		dropAllDeathLoot(e.tx, self, src)
		_ = e.CloseIn(e.tx)
	}
	return d, true
}
func (h *Hoglin) viewAction(a world.EntityAction) { beastViewAction(h.Ent, a) }
func (z *Zoglin) viewAction(a world.EntityAction) { beastViewAction(z.Ent, a) }
func beastViewAction(e *Ent, a world.EntityAction) {
	mob, ok := e.handle.Entity(e.tx)
	if !ok {
		return
	}
	v := e.tx.Viewers(e.Position())
	for _, x := range v {
		x.ViewEntityAction(mob, a)
	}
	e.tx.ReleaseViewers(v)
}
func (h *Hoglin) Heal(v float64, _ world.HealingSource) float64 { return beastHeal(h.beast(), v) }
func (z *Zoglin) Heal(v float64, _ world.HealingSource) float64 { return beastHeal(z.beast(), v) }
func beastHeal(b *netherBeastState, v float64) float64 {
	if v <= 0 {
		return 0
	}
	old := b.health.Health()
	b.health.AddHealth(v)
	return b.health.Health() - old
}
func (h *Hoglin) KnockBack(p mgl64.Vec3, f, k float64) { beastKnock(h.Ent, p, f, k) }
func (z *Zoglin) KnockBack(p mgl64.Vec3, f, k float64) { beastKnock(z.Ent, p, f, k) }
func (h *Hoglin) Hurt(d float64, src world.DamageSource) (float64, bool) {
	return beastHurt(h.Ent, h, h.beast(), d, src)
}
func (z *Zoglin) Hurt(d float64, src world.DamageSource) (float64, bool) {
	return beastHurt(z.Ent, z, z.beast(), d, src)
}
func (h *Hoglin) AddEffect(e effect.Effect)  { h.beast().effects.Add(e, h) }
func (z *Zoglin) AddEffect(e effect.Effect)  { z.beast().effects.Add(e, z) }
func (h *Hoglin) RemoveEffect(t effect.Type) { h.beast().effects.Remove(t, h) }
func (z *Zoglin) RemoveEffect(t effect.Type) { z.beast().effects.Remove(t, z) }
func (h *Hoglin) Effects() []effect.Effect   { return h.beast().effects.Effects() }
func (z *Zoglin) Effects() []effect.Effect   { return z.beast().effects.Effects() }
func (h *Hoglin) Speed() float64             { return h.beast().speed }
func (z *Zoglin) Speed() float64             { return z.beast().speed }
func (h *Hoglin) SetSpeed(v float64)         { h.beast().speed = v }
func (z *Zoglin) SetSpeed(v float64)         { z.beast().speed = v }
func beastKnock(e *Ent, p mgl64.Vec3, f, k float64) {
	d := e.Position().Sub(p)
	d[1] = 0
	if d.Len() > 0 {
		d = d.Normalize().Mul(f)
	}
	d[1] = k
	e.SetVelocity(d)
}
func (h *Hoglin) FinalizeSpawn(reason string, r *vanilla.Random) {
	if r == nil {
		return
	}
	if reason != "conversion" {
		h.beast().age = 0
		if r.NextFloat() < .2 {
			h.beast().age = -24000
		}
	}
	h.beast().immune = false
}
func (z *Zoglin) FinalizeSpawn(_ string, _ *vanilla.Random) {}

// InLove reports whether the hoglin is in love.
func (h *Hoglin) InLove() bool { return h.beast().breeding.InLove() }

func (h *Hoglin) breedingState() *breeding { return &h.beast().breeding }

// ageTicks returns the age of the hoglin, negative while it is a piglet.
func (h *Hoglin) ageTicks() *int { return &h.beast().age }

// breedingFood reports whether the stack is in #hoglin_food.
func (h *Hoglin) breedingFood(stack item.Stack) bool {
	return itemNamed(stack, "minecraft:crimson_fungus")
}

// canFallInLove ports Hoglin.canFallInLove: a hoglin pacified by a
// repellent will not fall in love.
func (h *Hoglin) canFallInLove() bool { return h.beast().pacified == 0 }

func (h *Hoglin) breedOffspring(*world.Tx, breeder) *world.EntityHandle {
	return babyAt(h).New(HoglinType, netherBeastConfig{kind: hoglinKind, age: babyStartAge})
}

// Interact ports Animal.mobInteract for hoglins: crimson fungus feeds them.
func (h *Hoglin) Interact(_ *world.Tx, u item.User, c *item.UseContext) bool {
	return u != nil && feedBreeder(h, u, c)
}
func (h *Hoglin) Age() int { return h.beast().age }
func (z *Zoglin) Age() int { return z.beast().age }

var _ Interactable = (*Hoglin)(nil)
var HoglinType = netherBeastType{id: "minecraft:hoglin", kind: hoglinKind}
var ZoglinType = netherBeastType{id: "minecraft:zoglin", kind: zoglinKind}

type netherBeastType struct {
	id   string
	kind netherBeastKind
}

func NewHoglin(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(HoglinType, netherBeastConfig{kind: hoglinKind})
}
func NewZoglin(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(ZoglinType, netherBeastConfig{kind: zoglinKind})
}

var _ world.EntityType = netherBeastType{}
var _ Living = (*Hoglin)(nil)
var _ Living = (*Zoglin)(nil)
