package entity

import (
	"math"
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

// Each nether beast has a distinct native type and concrete public entity.
type netherBeastKind uint8

const (
	hoglinKind netherBeastKind = iota
	zoglinKind
	striderKind
)

type netherBeastState struct {
	health                                           *HealthManager
	kind                                             netherBeastKind
	effects                                          *EffectManager
	mc                                               *MovementComputer
	age, attackCooldown, conversion, boost, boostMax int
	immune, saddled, dead, suffocating               bool
	targetUUID                                       string
	passenger                                        *world.EntityHandle
	steer, steerYaw, speed                           float64
	navigator                                        Navigator
	wander, repellentScan                            int
	repellent                                        *cube.Pos
	panic                                            panicState
	parent                                           followParentGoal
	breeding                                         breeding
	// pacified counts down the ticks a hoglin stays peaceful after it saw a
	// repellent, as BecomePassiveIfMemoryPresent remembers.
	pacified int
}
type netherBeastConfig struct {
	kind                       netherBeastKind
	health                     float64
	age                        int
	immune, saddled            bool
	attackCooldown, conversion int
}

func (c netherBeastConfig) Apply(d *world.EntityData) {
	h, speed := 40.0, .3
	if c.kind == striderKind {
		h, speed = 20, .175
	}
	if c.health > 0 {
		h = c.health
	}
	d.Data = &netherBeastState{kind: c.kind, health: NewHealthManager(h, h), effects: NewEffectManager(), mc: &MovementComputer{Gravity: .08, Drag: .02, DragBeforeGravity: true}, age: c.age, immune: c.immune, saddled: c.saddled, attackCooldown: c.attackCooldown, conversion: c.conversion, speed: speed}
}
func (t netherBeastType) EncodeEntity() string { return t.id }
func (t netherBeastType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		netherBeastConfig{kind: t.kind}.Apply(d)
	}
	e := &Ent{tx: tx, handle: h, data: d}
	switch t.kind {
	case hoglinKind:
		return &Hoglin{e}
	case zoglinKind:
		return &Zoglin{e}
	default:
		return &Strider{e}
	}
}
func (t netherBeastType) BBox(e world.Entity) cube.BBox {
	small := false
	if a, ok := e.(interface{ beast() *netherBeastState }); ok {
		small = a.beast().age < 0
	}
	switch t.kind {
	case hoglinKind, zoglinKind:
		if small {
			return cube.Box(-.375, 0, -.375, .375, .85, .375)
		}
		return cube.Box(-.7, 0, -.7, .7, 1.4, .7)
	default:
		if small {
			return cube.Box(-.225, 0, -.225, .225, .85, .225)
		}
		return cube.Box(-.45, 0, -.45, .45, .9, .45)
	}
}
func (t netherBeastType) AttributeDefaults() []NativeMobAttribute {
	health, speed, damage := 40.0, .3, 6.0
	if t.kind == striderKind {
		health, speed, damage = 20, .175, 0
	}
	return []NativeMobAttribute{{Name: "minecraft:health", Base: health, Current: health, DefaultMin: 0, DefaultMax: health, Min: 0, Max: health}, {Name: "minecraft:movement", Base: speed, Current: speed, DefaultMin: 0, DefaultMax: speed, Min: 0, Max: speed}, {Name: "minecraft:attack_damage", Base: damage, Current: damage, DefaultMin: 0, DefaultMax: damage, Min: 0, Max: damage}}
}

type Hoglin struct{ *Ent }
type Zoglin struct{ *Ent }
type Strider struct{ *Ent }

func (h *Hoglin) beast() *netherBeastState  { return h.Behaviour().(*netherBeastState) }
func (z *Zoglin) beast() *netherBeastState  { return z.Behaviour().(*netherBeastState) }
func (s *Strider) beast() *netherBeastState { return s.Behaviour().(*netherBeastState) }

// ageTicks returns the age of the strider, negative while it is young.
func (s *Strider) ageTicks() *int { return &s.beast().age }
func (t netherBeastType) DecodeNBT(n map[string]any, d *world.EntityData) {
	j := ReadNativeMobJavaState(n)
	if j == nil {
		j = n
	}
	age := int(nbtconv.Int32(j, "Age"))
	if age == 0 && nbtconv.Bool(j, "IsBaby") {
		age = -24000
	}
	c := netherBeastConfig{kind: t.kind, age: age, immune: nbtconv.Bool(n, "IsImmuneToZombification"), saddled: nbtconv.Uint8(n, "Saddle") != 0, attackCooldown: int(nbtconv.Int32(j, "AttackCooldown")), conversion: int(nbtconv.Int32(n, "TimeInOverworld"))}
	if x, ok := nativeMobAttribute(n, "minecraft:health"); ok {
		c.health = x.Current
	} else if _, ok := j["Health"]; ok {
		c.health = float64(nbtconv.Float32(j, "Health"))
	}
	c.Apply(d)
	s := d.Data.(*netherBeastState)
	s.breeding.decodeNBT(j)
	s.suffocating = nbtconv.Bool(j, "Suffocating")
	s.boost = int(nbtconv.Int32(j, "BoostTime"))
	s.boostMax = int(nbtconv.Int32(j, "BoostTimeMax"))
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
	if t.kind == striderKind {
		native["Saddle"] = byte(boolByte(s.saddled))
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
	j["Suffocating"], j["BoostTime"], j["BoostTimeMax"] = byte(boolByte(s.suffocating)), int32(s.boost), int32(s.boostMax)
	return MergeNativeMobJavaState(out, j)
}
func (h *Hoglin) SpeciesType() world.EntityType  { return HoglinType }
func (z *Zoglin) SpeciesType() world.EntityType  { return ZoglinType }
func (s *Strider) SpeciesType() world.EntityType { return StriderType }
func (h *Hoglin) Tick(tx *world.Tx, n int64)     { h.beast().effects.Tick(h, tx); h.Ent.Tick(tx, n) }
func (z *Zoglin) Tick(tx *world.Tx, n int64)     { z.beast().effects.Tick(z, tx); z.Ent.Tick(tx, n) }
func (s *Strider) Tick(tx *world.Tx, n int64)    { s.beast().effects.Tick(s, tx); s.Ent.Tick(tx, n) }
func (s *netherBeastState) Tick(e *Ent, tx *world.Tx) *Movement {
	// Hoglins and striders are animals that grow up and breed; zoglins stay
	// as they were born.
	var partner mgl64.Vec3
	var inLove bool
	switch s.kind {
	case hoglinKind:
		partner, inLove = tickBreeding(&Hoglin{Ent: e}, tx)
	case striderKind:
		partner, inLove = tickBreeding(&Strider{Ent: e}, tx)
	}
	if s.attackCooldown > 0 {
		s.attackCooldown--
	}
	if s.boost > 0 {
		s.boost--
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
	switch s.kind {
	case hoglinKind:
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
	case striderKind:
		s.suffocating = !striderWarm(tx, e.Position())
		if striderInWater(tx, e.Position()) && tx.CurrentTick()%20 == 0 {
			if ent, ok := e.handle.Entity(tx); ok {
				if living, ok := ent.(Living); ok {
					living.Hurt(1, DrowningDamageSource{})
				}
			}
		}
		if s.passenger == nil {
			for rider := range tx.EntitiesWithin(cube.Box(-2, 0, -2, 2, 3, 2).Translate(e.Position())) {
				if r, ok := rider.(Rider); ok && r.Riding() == e.H() {
					s.passenger = rider.H()
					break
				}
			}
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
	if s.kind == striderKind && s.passenger != nil && s.saddled && s.steer != 0 {
		v := e.data.Vel
		rot := e.data.Rot
		rot[0] = s.steerYaw
		e.data.Rot = rot
		dir := rot.Vec3()
		speed := s.speed
		if s.suffocating {
			speed *= .35
		} else {
			speed *= .55
		}
		if s.boost > 0 && s.boostMax > 0 {
			speed *= 1 + .15*math.Sin(math.Pi*float64(s.boost)/float64(s.boostMax))
		}
		v[0], v[2] = dir[0]*speed, dir[2]*speed
		e.data.Vel = v
		if rider, ok := s.passenger.Entity(tx); ok {
			if mover, ok := rider.(interface{ Teleport(mgl64.Vec3) }); ok {
				mover.Teleport(e.Position().Add(mgl64.Vec3{0, 1.05, 0}))
			}
		}
		return beastMoveFinish(s, e, tx)
	}
	if s.kind == hoglinKind && inLove {
		// AnimalMakeLove: a breeding hoglin forgets its attack target and
		// walks to its partner at 0.6 times its speed.
		beastMoveAt(e, partner, groundSpeed(.3, .6))
		return beastMoveFinish(s, e, tx)
	}
	if target != nil && s.kind != striderKind {
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
	if s.kind == hoglinKind || s.kind == zoglinKind {
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
	if s.kind == striderKind {
		if s.panic.run(e, tx, &s.navigator) {
			if p, ok := s.navigator.Tick(e.Position(), navigatorTick(e), .9); ok {
				beastMoveAt(e, p, groundSpeed(.175, 1.65))
			}
			return beastMoveFinish(s, e, tx)
		}
		if inLove {
			// BreedGoal: the strider walks to its partner.
			beastMove(s, e, tx, partner)
			return beastMoveFinish(s, e, tx)
		}
		if target := beastTemptPlayer(tx, e.Position()); target != nil {
			beastMove(s, e, tx, target.Position())
			return beastMoveFinish(s, e, tx)
		}
		if parent, ok := s.parent.next(e, tx, &Strider{Ent: e}, parentRange{}.orDefault()); ok {
			// FollowParentGoal: young striders keep close to adults.
			s.parent.follow(e, tx, &s.navigator, parent)
			if p, ok := s.navigator.Tick(e.Position(), navigatorTick(e), .9); ok {
				beastMoveAt(e, p, groundSpeed(.175, 1))
			}
			return beastMoveFinish(s, e, tx)
		}
		if s.suffocating {
			if p := nearestLava(tx, e.Position(), 8); p != nil {
				beastMove(s, e, tx, p.Vec3().Add(mgl64.Vec3{.5, 0, .5}))
				return beastMoveFinish(s, e, tx)
			}
		}
		return beastWander(s, e, tx)
	}
	return beastWander(s, e, tx)
}
func beastMove(s *netherBeastState, e *Ent, tx *world.Tx, p mgl64.Vec3) {
	speed := s.speed
	if s.kind == striderKind && s.suffocating {
		speed *= .6
	}
	beastMoveAt(e, p, speed)
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
	if s.kind == striderKind && inLava(tx, e.Position()) {
		v := e.data.Vel
		v[1] = .05
		e.data.Vel = v
	}
	m := s.mc.TickMovement(e, e.data.Pos, e.data.Vel, e.data.Rot, e.data.Rot, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	if s.kind == striderKind && s.passenger != nil {
		if rider, ok := s.passenger.Entity(tx); ok {
			if mover, ok := rider.(interface{ Teleport(mgl64.Vec3) }); ok {
				mover.Teleport(e.Position().Add(mgl64.Vec3{0, 1.05, 0}))
			}
		}
	}
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
			if s.kind == striderKind && s.suffocating && !nearBlock(tx, goal.Vec3(), "minecraft:lava", 8, 3) {
				continue
			}
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
func nearBlock(tx *world.Tx, p mgl64.Vec3, name string, h, v int) bool {
	return nearestBlock(tx, p, name, h, v) != nil
}
func nearestBlock(tx *world.Tx, p mgl64.Vec3, name string, h, v int) *cube.Pos {
	start := cube.PosFromVec3(p)
	var found *cube.Pos
	best := h*h + v*v
	for x := -h; x <= h; x++ {
		for y := -v; y <= v; y++ {
			for z := -h; z <= h; z++ {
				q := start.Add(cube.Pos{x, y, z})
				id, _ := tx.Block(q).EncodeBlock()
				if id == name && x*x+y*y+z*z < best {
					c := q
					found = &c
					best = x*x + y*y + z*z
				}
			}
		}
	}
	return found
}
func hoglinRepellent(tx *world.Tx, p mgl64.Vec3) bool {
	return nearestHoglinRepellent(tx, p, 8, 4) != nil
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
func striderWarm(tx *world.Tx, p mgl64.Vec3) bool {
	return inLava(tx, p) || inLava(tx, p.Add(mgl64.Vec3{0, -1, 0})) || nearBlock(tx, p, "minecraft:magma", 0, 1) || nearBlock(tx, p, "minecraft:fire", 0, 1) || nearBlock(tx, p, "minecraft:soul_fire", 0, 1) || nearBlock(tx, p, "minecraft:campfire", 0, 1) || nearBlock(tx, p, "minecraft:soul_campfire", 0, 1)
}
func inLava(tx *world.Tx, p mgl64.Vec3) bool {
	id, _ := tx.Block(cube.PosFromVec3(p)).EncodeBlock()
	return id == "minecraft:lava" || id == "minecraft:flowing_lava"
}
func nearestLava(tx *world.Tx, p mgl64.Vec3, r int) *cube.Pos {
	return nearestBlock(tx, p, "minecraft:lava", r, 4)
}
func striderInWater(tx *world.Tx, p mgl64.Vec3) bool {
	id, _ := tx.Block(cube.PosFromVec3(p)).EncodeBlock()
	return id == "minecraft:water" || id == "minecraft:flowing_water"
}
func beastTemptPlayer(tx *world.Tx, p mgl64.Vec3) world.Entity {
	var chosen world.Entity
	best := 10.0
	for x := range tx.EntitiesWithin(cube.Box(-10, -4, -10, 10, 4, 10).Translate(p)) {
		if x.H().Type().EncodeEntity() != "minecraft:player" {
			continue
		}
		u, ok := x.(item.User)
		if !ok {
			continue
		}
		a, b := u.HeldItems()
		tempt := func(s item.Stack) bool {
			if s.Empty() {
				return false
			}
			n, _ := s.Item().EncodeItem()
			return n == "minecraft:warped_fungus"
		}
		if !tempt(a) && !tempt(b) {
			continue
		}
		d := x.Position().Sub(p).Len()
		if d < best {
			best, chosen = d, x
		}
	}
	return chosen
}
func (h *Hoglin) Health() float64         { return h.beast().health.Health() }
func (z *Zoglin) Health() float64         { return z.beast().health.Health() }
func (s *Strider) Health() float64        { return s.beast().health.Health() }
func (h *Hoglin) MaxHealth() float64      { return h.beast().health.MaxHealth() }
func (z *Zoglin) MaxHealth() float64      { return z.beast().health.MaxHealth() }
func (s *Strider) MaxHealth() float64     { return s.beast().health.MaxHealth() }
func (h *Hoglin) SetMaxHealth(v float64)  { h.beast().health.SetMaxHealth(v) }
func (z *Zoglin) SetMaxHealth(v float64)  { z.beast().health.SetMaxHealth(v) }
func (s *Strider) SetMaxHealth(v float64) { s.beast().health.SetMaxHealth(v) }
func (h *Hoglin) Dead() bool              { return h.beast().dead || h.Health() <= 0 }
func (z *Zoglin) Dead() bool              { return z.beast().dead || z.Health() <= 0 }
func (s *Strider) Dead() bool             { return s.beast().dead || s.Health() <= 0 }

// IsSensitiveToWater reports that water hurts striders, as Strider does.
func (s *Strider) IsSensitiveToWater() bool { return true }
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
	b.panic.hurt(src, e.tx.CurrentTick())
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
func (h *Hoglin) viewAction(a world.EntityAction)  { beastViewAction(h.Ent, a) }
func (z *Zoglin) viewAction(a world.EntityAction)  { beastViewAction(z.Ent, a) }
func (s *Strider) viewAction(a world.EntityAction) { beastViewAction(s.Ent, a) }
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
func (h *Hoglin) Heal(v float64, _ world.HealingSource) float64  { return beastHeal(h.beast(), v) }
func (z *Zoglin) Heal(v float64, _ world.HealingSource) float64  { return beastHeal(z.beast(), v) }
func (s *Strider) Heal(v float64, _ world.HealingSource) float64 { return beastHeal(s.beast(), v) }
func beastHeal(b *netherBeastState, v float64) float64 {
	if v <= 0 {
		return 0
	}
	old := b.health.Health()
	b.health.AddHealth(v)
	return b.health.Health() - old
}
func (h *Hoglin) KnockBack(p mgl64.Vec3, f, k float64)  { beastKnock(h.Ent, p, f, k) }
func (z *Zoglin) KnockBack(p mgl64.Vec3, f, k float64)  { beastKnock(z.Ent, p, f, k) }
func (s *Strider) KnockBack(p mgl64.Vec3, f, k float64) { beastKnock(s.Ent, p, f, k) }
func (h *Hoglin) Hurt(d float64, src world.DamageSource) (float64, bool) {
	return beastHurt(h.Ent, h, h.beast(), d, src)
}
func (z *Zoglin) Hurt(d float64, src world.DamageSource) (float64, bool) {
	return beastHurt(z.Ent, z, z.beast(), d, src)
}
func (s *Strider) Hurt(d float64, src world.DamageSource) (float64, bool) {
	return beastHurt(s.Ent, s, s.beast(), d, src)
}

// Panicking reports whether the strider runs from danger.
func (s *Strider) Panicking() bool { return s.beast().panic.active }

// dropCustomDeathLoot drops a strider's saddle, a preserved equipment item.
func (s *Strider) dropCustomDeathLoot(bool, int) {
	if s.beast().saddled {
		dropMobEquipment(s.tx, s, item.NewStack(item.Saddle{}, 1), 2, false, 0)
	}
}
func (h *Hoglin) AddEffect(e effect.Effect)   { h.beast().effects.Add(e, h) }
func (z *Zoglin) AddEffect(e effect.Effect)   { z.beast().effects.Add(e, z) }
func (s *Strider) AddEffect(e effect.Effect)  { s.beast().effects.Add(e, s) }
func (h *Hoglin) RemoveEffect(t effect.Type)  { h.beast().effects.Remove(t, h) }
func (z *Zoglin) RemoveEffect(t effect.Type)  { z.beast().effects.Remove(t, z) }
func (s *Strider) RemoveEffect(t effect.Type) { s.beast().effects.Remove(t, s) }
func (h *Hoglin) Effects() []effect.Effect    { return h.beast().effects.Effects() }
func (z *Zoglin) Effects() []effect.Effect    { return z.beast().effects.Effects() }
func (s *Strider) Effects() []effect.Effect   { return s.beast().effects.Effects() }
func (h *Hoglin) Speed() float64              { return h.beast().speed }
func (z *Zoglin) Speed() float64              { return z.beast().speed }
func (s *Strider) Speed() float64             { return s.beast().speed }
func (h *Hoglin) SetSpeed(v float64)          { h.beast().speed = v }
func (z *Zoglin) SetSpeed(v float64)          { z.beast().speed = v }
func (s *Strider) SetSpeed(v float64)         { s.beast().speed = v }
func beastKnock(e *Ent, p mgl64.Vec3, f, k float64) {
	d := e.Position().Sub(p)
	d[1] = 0
	if d.Len() > 0 {
		d = d.Normalize().Mul(f)
	}
	d[1] = k
	e.SetVelocity(d)
}
func (h *Hoglin) FinalizeSpawn(reason string, r *java26.Random) {
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
func (z *Zoglin) FinalizeSpawn(_ string, _ *java26.Random) {}
func (s *Strider) FinalizeSpawn(reason string, r *java26.Random) {
	if reason == "jockey" || r == nil {
		return
	}
	b := s.beast()
	b.age = 0
	if r.NextInt(30) == 0 {
		opts := world.EntitySpawnOpts{Position: s.Position(), Rotation: s.Rotation(), SpawnReason: "jockey"}
		jockey := opts.New(ZombifiedPiglinType{}, zombifiedPiglinConfig{main: item.NewStack(item.WarpedFungusOnAStick{}, 1)})
		b.saddled = true
		if s.tx != nil {
			rider := s.tx.AddEntity(jockey)
			s.Mount(s.tx, rider)
		}
		return
	}
	if r.NextInt(10) == 0 {
		baby := world.EntitySpawnOpts{Position: s.Position(), Rotation: s.Rotation(), SpawnReason: "jockey"}.New(StriderType, netherBeastConfig{kind: striderKind, age: -24000})
		if s.tx != nil {
			s.tx.AddEntity(baby)
		}
	}
}

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

// InLove reports whether the strider is in love.
func (s *Strider) InLove() bool { return s.beast().breeding.InLove() }

func (s *Strider) breedingState() *breeding { return &s.beast().breeding }

// breedingFood reports whether the stack is in #strider_food.
func (s *Strider) breedingFood(stack item.Stack) bool {
	return itemNamed(stack, "minecraft:warped_fungus")
}

func (s *Strider) breedOffspring(*world.Tx, breeder) *world.EntityHandle {
	return babyAt(s).New(StriderType, netherBeastConfig{kind: striderKind, age: babyStartAge})
}
func (s *Strider) SetVehicleInput(strafe, forward float64) { s.beast().steer = forward }
func (s *Strider) SetVehicleInputFrom(passenger world.Entity, strafe, forward float64) {
	s.SetVehicleInput(strafe, forward)
	if passenger != nil {
		s.beast().steerYaw = passenger.Rotation()[0]
	}
}
func (s *Strider) ControlsVehicle(passenger world.Entity) bool {
	return passenger != nil && s.beast().passenger == passenger.H() && striderController(passenger)
}
func striderController(p world.Entity) bool {
	u, ok := p.(item.User)
	if !ok {
		return false
	}
	a, b := u.HeldItems()
	for _, s := range []item.Stack{a, b} {
		if !s.Empty() {
			n, _ := s.Item().EncodeItem()
			if n == "minecraft:warped_fungus_on_a_stick" {
				return true
			}
		}
	}
	return false
}

// Mount seats p on the strider as Entity.startRiding does when forced, as
// /ride does: unlike riding it by hand, which Interact allows only once
// saddled, the strider need not wear a saddle.
func (s *Strider) Mount(tx *world.Tx, p world.Entity) bool {
	b := s.beast()
	if p == nil || b.passenger != nil {
		return false
	}
	r, ok := p.(Rider)
	if !ok || r.Riding() != nil {
		return false
	}
	b.passenger = p.H()
	seatRider(r, s)
	if v, ok := p.(interface{ Teleport(mgl64.Vec3) }); ok {
		v.Teleport(s.Position().Add(mgl64.Vec3{0, 1.05, 0}))
	}
	s.broadcast(tx, p.H(), true)
	return true
}
func (s *Strider) Dismount(tx *world.Tx, p world.Entity) {
	if p == nil || s.beast().passenger != p.H() {
		return
	}
	if r, ok := p.(Rider); ok {
		r.SetRiding(nil)
	}
	s.broadcast(tx, p.H(), false)
	s.beast().passenger = nil
	s.beast().steer = 0
}

// RiderSeat returns the seat of Bedrock's strider, which centres a baby
// strider riding its parent.
func (s *Strider) RiderSeat(rider world.Entity) world.RiderSeat {
	pos := mgl64.Vec3{0, 1.7, -0.2}
	if rider.H().Type() == s.H().Type() {
		pos[2] = 0
	}
	return seat{pos: pos}.riderSeat(rider)
}
func (s *Strider) broadcast(tx *world.Tx, h *world.EntityHandle, link bool) {
	vs := tx.Viewers(s.Position())
	for _, v := range vs {
		if lv, ok := v.(interface {
			ViewEntityLinkType(*world.EntityHandle, *world.EntityHandle, LinkType, bool)
		}); ok {
			lv.ViewEntityLinkType(s.H(), h, LinkRider, link)
		} else if lv, ok := v.(interface {
			ViewEntityLink(*world.EntityHandle, *world.EntityHandle, bool)
		}); ok {
			lv.ViewEntityLink(s.H(), h, link)
		}
	}
	tx.ReleaseViewers(vs)
}

// Interact ports Strider.mobInteract: a saddled strider carries a player who
// is not holding its food or sneaking, warped fungus feeds it, and a saddle
// saddles an adult.
func (s *Strider) Interact(tx *world.Tx, u item.User, c *item.UseContext) bool {
	if u == nil {
		return false
	}
	b := s.beast()
	held, _ := u.HeldItems()
	food := s.breedingFood(held)
	sneaking, ok := u.(interface{ Sneaking() bool })
	if !food && b.saddled && b.passenger == nil && (!ok || !sneaking.Sneaking()) {
		return s.Mount(tx, u)
	}
	if feedBreeder(s, u, c) {
		if food {
			tx.PlaySound(s.Position(), sound.Custom{Name: "entity.strider.eat", Volume: 1, Pitch: 1 + (rand.Float64()-rand.Float64())*.2})
		}
		return true
	}
	if itemNamed(held, "minecraft:saddle") && !b.saddled && b.age >= 0 {
		b.saddled = true
		if c != nil {
			c.SubtractFromCount(1)
		}
		return true
	}
	return false
}
func (s *Strider) Saddled() bool     { return s.beast().saddled }
func (s *Strider) Suffocating() bool { return s.beast().suffocating }

// Interact ports Animal.mobInteract for hoglins: crimson fungus feeds them.
func (h *Hoglin) Interact(_ *world.Tx, u item.User, c *item.UseContext) bool {
	return u != nil && feedBreeder(h, u, c)
}
func (s *Strider) Dash(_ *world.Tx, passenger world.Entity) bool {
	b := s.beast()
	if passenger == nil || !s.ControlsVehicle(passenger) || b.boost > 0 {
		return false
	}
	b.boostMax = 100
	b.boost = 100
	return true
}
func (s *Strider) DismountAll(_ *Ent, tx *world.Tx) {
	b := s.beast()
	if b.passenger != nil {
		if p, ok := b.passenger.Entity(tx); ok {
			if r, ok := p.(Rider); ok {
				r.SetRiding(nil)
			}
			s.broadcast(tx, b.passenger, false)
		}
		b.passenger = nil
	}
}
func (h *Hoglin) Age() int  { return h.beast().age }
func (z *Zoglin) Age() int  { return z.beast().age }
func (s *Strider) Age() int { return s.beast().age }

var _ Interactable = (*Hoglin)(nil)
var _ interface {
	Dash(*world.Tx, world.Entity) bool
} = (*Strider)(nil)
var HoglinType = netherBeastType{id: "minecraft:hoglin", kind: hoglinKind}
var ZoglinType = netherBeastType{id: "minecraft:zoglin", kind: zoglinKind}
var StriderType = netherBeastType{id: "minecraft:strider", kind: striderKind}

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
func NewStrider(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(StriderType, netherBeastConfig{kind: striderKind})
}

var _ world.EntityType = netherBeastType{}
var _ Living = (*Hoglin)(nil)
var _ Living = (*Zoglin)(nil)
var _ Living = (*Strider)(nil)
var _ Interactable = (*Hoglin)(nil)
var _ Interactable = (*Strider)(nil)
var _ VehicleInput = (*Strider)(nil)
var _ interface {
	SetVehicleInputFrom(world.Entity, float64, float64)
} = (*Strider)(nil)
var _ VehicleController = (*Strider)(nil)
var _ PassengerDismounter = (*Strider)(nil)
var _ interface {
	Dash(*world.Tx, world.Entity) bool
} = (*Strider)(nil)
var _ = item.WarpedFungusOnAStick{}
