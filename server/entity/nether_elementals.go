package entity

import (
	"hash/fnv"
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

type ghastType struct{}
type blazeType struct{}
type magmaCubeType struct{}

var GhastType ghastType
var BlazeType blazeType
var MagmaCubeType magmaCubeType

type netherKind uint8

const (
	ghastKind netherKind = iota
	blazeKind
	magmaKind
)

type netherConfig struct {
	kind               netherKind
	health             float64
	size, charge, jump int
	seed               int64
}
type netherState struct {
	kind                                                              netherKind
	health                                                            *HealthManager
	effects                                                           *EffectManager
	mc                                                                *MovementComputer
	random                                                            vanilla.Random
	slime                                                             *SlimeCore
	size, charge, attackStep, attackTime, lastSeen, jumpDelay, wander int
	heightOffset                                                      float64
	heightTimer                                                       int
	speed, attack                                                     float64
	charging, dead                                                    bool
	path                                                              []cube.Pos
	pathIndex                                                         int
}
type Ghast struct{ *Ent }
type Blaze struct{ *Ent }
type MagmaCube struct{ *Ent }

func (ghastType) EncodeEntity() string     { return "minecraft:ghast" }
func (blazeType) EncodeEntity() string     { return "minecraft:blaze" }
func (magmaCubeType) EncodeEntity() string { return "minecraft:magma_cube" }
func (t ghastType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	return openNether(tx, h, d, ghastKind)
}
func (t blazeType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	return openNether(tx, h, d, blazeKind)
}
func (t magmaCubeType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	return openNether(tx, h, d, magmaKind)
}
func (t ghastType) BBox(world.Entity) cube.BBox { return cube.Box(-2, 0, -2, 2, 4, 2) }
func (t blazeType) BBox(world.Entity) cube.BBox { return cube.Box(-.3, 0, -.3, .3, 1.8, .3) }
func (t magmaCubeType) BBox(e world.Entity) cube.BBox {
	s := 1
	if m, ok := e.(*MagmaCube); ok {
		s = m.n().size
	}
	w := float64(s) * .255
	return cube.Box(-w, 0, -w, w, float64(s)*.51, w)
}
func openNether(tx *world.Tx, h *world.EntityHandle, d *world.EntityData, k netherKind) world.Entity {
	if d.Data == nil {
		size := 1
		if k == magmaKind {
			size = 1
		}
		(netherConfig{kind: k, size: size}).Apply(d)
	}
	s := d.Data.(*netherState)
	if h != nil {
		u := h.UUID()
		r := fnv.New64a()
		_, _ = r.Write(u[:])
		s.random = vanilla.NewLegacyRandom(int64(r.Sum64()))
	}
	e := &Ent{tx: tx, handle: h, data: d}
	switch k {
	case ghastKind:
		return &Ghast{e}
	case blazeKind:
		return &Blaze{e}
	default:
		return &MagmaCube{e}
	}
}
func decodeNether(k netherKind, n map[string]any, d *world.EntityData) {
	c := netherConfig{kind: k, size: 1}
	for _, a := range ReadNativeMobAttributes(n) {
		if a.Name == "minecraft:health" {
			c.health = a.Current
		}
	}
	if j := ReadNativeMobJavaState(n); j != nil {
		c.size = int(nbtconv.Int32(j, "Size"))
		c.charge = int(nbtconv.Int32(j, "Charge"))
		c.jump = int(nbtconv.Int32(j, "JumpDelay"))
	}
	switch k {
	case ghastKind:
		c.charge = int(nbtconv.Int32(n, "ExplosionPower"))
	case magmaKind:
		size, _ := nbtconv.Integer(n, "Size")
		c.size = int(size) + 1
	}
	d.NativeNBT = CloneNativeMobNBT(n)
	c.Apply(d)
}
func encodeNether(k netherKind, d *world.EntityData) map[string]any {
	s := d.Data.(*netherState)
	maxHP, speed, attack := 10., .06, 0.
	switch k {
	case blazeKind:
		maxHP, speed, attack = 20, .23, 6
	case magmaKind:
		maxHP, speed, attack = float64(s.size*s.size), .2+.1*float64(s.size), float64(s.size)
	}
	attrs := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{{Name: "minecraft:health", Base: maxHP, Current: s.health.Health(), DefaultMin: 0, DefaultMax: maxHP, Min: 0, Max: maxHP}, {Name: "minecraft:movement", Base: speed, Current: s.speed, DefaultMin: 0, DefaultMax: 3, Min: 0, Max: 3}, {Name: "minecraft:attack_damage", Base: attack, Current: attack, DefaultMin: 0, DefaultMax: 2048, Min: 0, Max: 2048}})
	// BDS saves positions, rotations and motion as floats.
	n := map[string]any{"identifier": speciesIDNether(k), "Pos": []float32{float32(d.Pos[0]), float32(d.Pos[1]), float32(d.Pos[2])}, "Rotation": []float32{float32(d.Rot[0]), float32(d.Rot[1])}, "Motion": []float32{float32(d.Vel[0]), float32(d.Vel[1]), float32(d.Vel[2])}, "Attributes": attrs}
	switch k {
	case ghastKind:
		n["ExplosionPower"] = int32(s.charge)
	case magmaKind:
		n["Size"] = uint8(s.size - 1)
	}
	return MergeNativeMobJavaState(MergeNativeMobNBT(d.NativeNBT, n), map[string]any{"Size": int32(s.size), "Charge": int32(s.charge), "JumpDelay": int32(s.slime.JumpDelay)})
}
func speciesIDNether(k netherKind) string {
	switch k {
	case ghastKind:
		return "minecraft:ghast"
	case blazeKind:
		return "minecraft:blaze"
	default:
		return "minecraft:magma_cube"
	}
}
func (ghastType) DecodeNBT(n map[string]any, d *world.EntityData)     { decodeNether(ghastKind, n, d) }
func (blazeType) DecodeNBT(n map[string]any, d *world.EntityData)     { decodeNether(blazeKind, n, d) }
func (magmaCubeType) DecodeNBT(n map[string]any, d *world.EntityData) { decodeNether(magmaKind, n, d) }
func (ghastType) EncodeNBT(d *world.EntityData) map[string]any        { return encodeNether(ghastKind, d) }
func (blazeType) EncodeNBT(d *world.EntityData) map[string]any        { return encodeNether(blazeKind, d) }
func (magmaCubeType) EncodeNBT(d *world.EntityData) map[string]any    { return encodeNether(magmaKind, d) }
func (c netherConfig) Apply(d *world.EntityData) {
	hp, maxHP, speed, attack := 10., 10., .06, 0.
	if c.kind == blazeKind {
		hp, maxHP, speed, attack = 20, 20, .23, 6
	}
	if c.kind == magmaKind {
		if c.size < 1 {
			c.size = 1
		}
		hp, maxHP, speed, attack = float64(c.size*c.size), float64(c.size*c.size), .2+.1*float64(c.size), float64(c.size)
	}
	if c.health > 0 {
		hp = c.health
	}
	gravity, drag := .08, .02
	if c.kind == ghastKind {
		gravity, drag = 0, .01
	}
	s := &netherState{kind: c.kind, health: NewHealthManager(hp, maxHP), effects: NewEffectManager(), mc: &MovementComputer{Gravity: gravity, Drag: drag, DragBeforeGravity: true}, random: vanilla.NewLegacyRandom(c.seed), size: c.size, slime: NewSlimeCore(c.size), charge: c.charge, jumpDelay: c.jump, heightOffset: .5, speed: speed, attack: attack}
	if c.jump > 0 {
		s.slime.JumpDelay = c.jump
	}
	d.Data = s
}
func NewGhast(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(GhastType, netherConfig{kind: ghastKind, size: 1})
}
func NewBlaze(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(BlazeType, netherConfig{kind: blazeKind, size: 1})
}
func NewMagmaCube(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(MagmaCubeType, netherConfig{kind: magmaKind, size: 1})
}
func (m *Ghast) n() *netherState                { return m.Behaviour().(*netherState) }
func (m *Blaze) n() *netherState                { return m.Behaviour().(*netherState) }
func (m *MagmaCube) n() *netherState            { return m.Behaviour().(*netherState) }
func (m *MagmaCube) Size() int                  { return m.n().size }
func (m *Ghast) Tick(tx *world.Tx, n int64)     { m.n().effects.Tick(m, tx); m.Ent.Tick(tx, n) }
func (m *Blaze) Tick(tx *world.Tx, n int64)     { m.n().effects.Tick(m, tx); m.Ent.Tick(tx, n) }

// IsSensitiveToWater reports that water hurts blazes, as Blaze does.
func (m *Blaze) IsSensitiveToWater() bool { return true }
func (m *MagmaCube) Tick(tx *world.Tx, n int64) { m.n().effects.Tick(m, tx); m.Ent.Tick(tx, n) }
func (s *netherState) Tick(e *Ent, tx *world.Tx) *Movement {
	switch s.kind {
	case ghastKind:
		return s.tickGhast(e, tx)
	case blazeKind:
		return s.tickBlaze(e, tx)
	default:
		return s.tickMagma(e, tx)
	}
}
func (s *netherState) tickGhast(e *Ent, tx *world.Tx) *Movement {
	target, dist := netherTarget(e, tx, 100)
	if target != nil && dist < 64 && lineVisible(e, tx, target) {
		s.charge++
		if s.charge == 10 {
			tx.PlaySound(e.Position(), sound.Custom{Name: "entity.ghast.warn", Volume: 5, Pitch: 1})
		}
		if s.charge == 20 {
			d := target.Position().Add(mgl64.Vec3{0, .5, 0}).Sub(e.Position().Add(mgl64.Vec3{0, .5, 0}))
			if d.Len() > 0 {
				pos := e.Position().Add(mgl64.Vec3{0, .5, 0}).Add(e.Rotation().Vec3().Mul(4))
				tx.AddEntity(NewGhastFireball(world.EntitySpawnOpts{Position: pos, Velocity: d.Normalize().Mul(.5)}, &Ghast{e}))
				tx.PlaySound(e.Position(), sound.Custom{Name: "entity.ghast.shoot", Volume: 5, Pitch: 1})
			}
			s.charge = -40
		}
	} else if s.charge > 0 {
		s.charge--
	}
	s.charging = s.charge > 10
	return s.wanderFly(e, tx)
}
func (s *netherState) tickBlaze(e *Ent, tx *world.Tx) *Movement {
	if fluid, ok := tx.Liquid(cube.PosFromVec3(e.Position())); ok && fluid.LiquidType() == "water" {
		(&Blaze{e}).Hurt(1, DrowningDamageSource{})
	}
	if s.attackTime > 0 {
		s.attackTime--
	}
	if s.heightTimer > 0 {
		s.heightTimer--
	}
	if s.heightTimer == 0 {
		s.heightTimer = 100
		s.heightOffset = float64((s.random.NextFloat() - .5) * 2)
	}
	target, dist := netherTarget(e, tx, 48)
	if target != nil {
		if target.Position()[1] > e.Position()[1]+s.heightOffset {
			v := e.Velocity()
			v[1] += (.3 - v[1]) * .3
			e.SetVelocity(v)
		}
		if dist < 2 {
			if lineVisible(e, tx, target) && s.attackTime <= 0 {
				s.attackTime = 20
				hurtMob(target, s.attack, e)
			}
			return s.groundMove(e, tx, target.Position())
		}
		if dist < 48 && lineVisible(e, tx, target) {
			if s.attackTime <= 0 {
				s.attackStep++
				switch {
				case s.attackStep == 1:
					s.attackTime, s.charging = 60, true
				case s.attackStep <= 4:
					s.attackTime = 6
				default:
					s.attackTime, s.attackStep, s.charging = 100, 0, false
				}
				if s.attackStep > 1 {
					d := target.Position().Add(mgl64.Vec3{0, .5, 0}).Sub(e.Position().Add(mgl64.Vec3{0, .5, 0}))
					spread := math.Sqrt(math.Sqrt(dist)) * .5
					d[0] += float64(s.random.NextFloat()-.5) * 4.594 * spread
					d[2] += float64(s.random.NextFloat()-.5) * 4.594 * spread
					if d.Len() > 0 {
						opts := world.EntitySpawnOpts{Position: e.Position().Add(mgl64.Vec3{0, 1, 0}), Velocity: d.Normalize().Mul(.5)}
						tx.AddEntity(tx.World().EntityRegistry().Config().SmallFireball(opts, e))
					}
					tx.PlaySound(e.Position(), sound.Custom{Name: "entity.blaze.shoot", Volume: 1, Pitch: 1})
				}
			}
			return s.groundMove(e, tx, target.Position())
		}
	}
	s.charging = false
	return s.wanderGround(e, tx)
}
func (s *netherState) tickMagma(e *Ent, tx *world.Tx) *Movement {
	fluid, _ := tx.Liquid(cube.PosFromVec3(e.Position()))
	if fluid != nil && fluid.LiquidType() == "lava" {
		if s.slime.JumpDelay > 0 {
			s.slime.JumpDelay--
		} else {
			s.slime.JumpDelay = 10 + int(s.random.NextInt(20))
			v := e.Velocity()
			v[1] = .22 + .05*float64(s.size)
			e.SetVelocity(v)
		}
	} else if s.slime.Jump(&s.random, s.mc.OnGround()) {
		v := e.Velocity()
		v[1] = .42 + .08*float64(s.size)
		e.SetVelocity(v)
		if s.size > 1 {
			tx.PlaySound(e.Position(), sound.Custom{Name: "entity.slime.jump", Volume: 1, Pitch: 1})
		}
	}
	s.jumpDelay = s.slime.JumpDelay
	if s.attackTime > 0 {
		s.attackTime--
	}
	target, _ := netherTarget(e, tx, 16)
	for other := range tx.EntitiesWithin(cube.Box(-float64(s.size), 0, -float64(s.size), float64(s.size), 1, float64(s.size)).Translate(e.Position())) {
		if other == e {
			continue
		}
		id := other.H().Type().EncodeEntity()
		if l, ok := other.(Living); ok && !l.Dead() && (id == "minecraft:player" || id == "minecraft:iron_golem") && s.attackTime == 0 {
			l.Hurt(float64(s.size+2), AttackDamageSource{Attacker: &MagmaCube{e}})
			s.attackTime = 20
		}
	}
	if target != nil {
		return s.groundMove(e, tx, target.Position())
	}
	return s.wanderGround(e, tx)
}
func (s *netherState) fly(e *Ent, tx *world.Tx, p mgl64.Vec3) *Movement {
	d := p.Sub(e.Position())
	if d.Len() > 0 {
		d = d.Normalize()
	}
	v := e.Velocity().Add(d.Mul(.02))
	pos := e.Position().Add(v)
	if len(tx.Block(cube.PosFromVec3(pos)).Model().BBox(cube.PosFromVec3(pos), tx)) != 0 {
		v = v.Mul(0)
	}
	r := e.Rotation()
	r[0] = math.Atan2(-d[0], d[2]) * 180 / math.Pi
	m := s.mc.TickMovement(e, e.Position(), v, e.Rotation(), r, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	return m
}
func (s *netherState) wanderFly(e *Ent, tx *world.Tx) *Movement {
	if s.wander > 0 {
		s.wander--
	}
	if s.pathIndex >= len(s.path) && s.wander == 0 {
		s.wander = 40
		p := e.Position().Add(mgl64.Vec3{float64(s.random.NextFloat()*2-1) * 16, float64(s.random.NextFloat()*2-1) * 16, float64(s.random.NextFloat()*2-1) * 16})
		s.path = []cube.Pos{cube.PosFromVec3(p)}
		s.pathIndex = 0
	}
	if len(s.path) > 0 {
		return s.fly(e, tx, s.path[len(s.path)-1].Vec3())
	}
	return s.fly(e, tx, e.Position())
}
func (s *netherState) groundMove(e *Ent, tx *world.Tx, p mgl64.Vec3) *Movement {
	d := p.Sub(e.Position())
	d[1] = 0
	if d.Len() > 0 {
		d = d.Normalize()
	}
	v := e.Velocity()
	v[0], v[2] = d[0]*s.speed, d[2]*s.speed
	if s.kind == magmaKind && s.mc.OnGround() {
		v[1] = e.Velocity()[1]
	}
	r := e.Rotation()
	if d.Len() > 0 {
		r[0] = math.Atan2(-d[0], d[2]) * 180 / math.Pi
	}
	m := s.mc.TickMovement(e, e.Position(), v, e.Rotation(), r, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	return m
}
func (s *netherState) wanderGround(e *Ent, tx *world.Tx) *Movement {
	if s.wander > 0 {
		s.wander--
	}
	if s.wander == 0 {
		s.wander = 40
		s.path = []cube.Pos{cube.PosFromVec3(e.Position().Add(mgl64.Vec3{float64(s.random.NextInt(11)) - 5, 0, float64(s.random.NextInt(11)) - 5}))}
	}
	return s.groundMove(e, tx, s.path[len(s.path)-1].Vec3())
}
func netherTarget(e *Ent, tx *world.Tx, r float64) (world.Entity, float64) {
	area := cube.Box(-r, -4, -r, r, 4, r).Translate(e.Position())
	var best world.Entity
	dist := r
	for x := range tx.EntitiesWithin(area) {
		if x == e {
			continue
		}
		id := x.H().Type().EncodeEntity()
		if id != "minecraft:player" && !(e.H().Type().EncodeEntity() == "minecraft:magma_cube" && id == "minecraft:iron_golem") {
			continue
		}
		l, ok := x.(Living)
		if !ok || l.Dead() {
			continue
		}
		d := x.Position().Sub(e.Position()).Len()
		if d < dist {
			best, dist = x, d
		}
	}
	return best, dist
}
func lineVisible(e *Ent, tx *world.Tx, t world.Entity) bool {
	d := t.Position().Sub(e.Position())
	length := d.Len()
	if length == 0 {
		return true
	}
	for traveled := .25; traveled < length; traveled += .25 {
		p := cube.PosFromVec3(e.Position().Add(d.Mul(traveled / length)))
		b := tx.Block(p)
		if len(b.Model().BBox(p, tx)) > 0 {
			return false
		}
	}
	return true
}
func hurtMob(e world.Entity, d float64, source world.Entity) {
	if l, ok := e.(Living); ok {
		l.Hurt(d, AttackDamageSource{Attacker: source})
	}
}
func (s *netherState) Hurt(e *Ent, self world.Entity, d float64, src world.DamageSource) (float64, bool) {
	if s.dead || d <= 0 {
		return 0, false
	}
	if src.Fire() && (s.kind == blazeKind || s.kind == magmaKind) {
		return 0, false
	}
	s.health.AddHealth(-d)
	viewers := e.tx.Viewers(e.Position())
	for _, viewer := range viewers {
		viewer.ViewEntityAction(e, HurtAction{})
	}
	e.tx.ReleaseViewers(viewers)
	onMobHurt(e.tx, self)
	if s.health.Health() <= 0 {
		s.dead = true
		viewers = e.tx.Viewers(e.Position())
		for _, viewer := range viewers {
			viewer.ViewEntityAction(e, DeathAction{})
		}
		e.tx.ReleaseViewers(viewers)
		dropAllDeathLoot(e.tx, self, src)
		e.CloseIn(e.tx)
	}
	return d, true
}
func (m *Ghast) Health() float64        { return m.n().health.Health() }
func (m *Ghast) MaxHealth() float64     { return m.n().health.MaxHealth() }
func (m *Ghast) SetMaxHealth(v float64) { m.n().health.SetMaxHealth(v) }
func (m *Ghast) Dead() bool             { return m.n().dead || m.Health() <= 0 }
func (m *Ghast) Hurt(d float64, s world.DamageSource) (float64, bool) {
	if p, ok := s.(ProjectileDamageSource); ok && p.Projectile != nil && p.Projectile.H().Type().EncodeEntity() == "minecraft:fireball" && p.Owner != nil && p.Owner.H().Type().EncodeEntity() == "minecraft:player" {
		return m.n().Hurt(m.Ent, m, 1000, s)
	}
	return m.n().Hurt(m.Ent, m, d, s)
}
func (m *Blaze) Health() float64                                      { return m.n().health.Health() }
func (m *Blaze) MaxHealth() float64                                   { return m.n().health.MaxHealth() }
func (m *Blaze) SetMaxHealth(v float64)                               { m.n().health.SetMaxHealth(v) }
func (m *Blaze) Dead() bool                                           { return m.n().dead || m.Health() <= 0 }
func (m *Blaze) Hurt(d float64, s world.DamageSource) (float64, bool) { return m.n().Hurt(m.Ent, m, d, s) }
func (m *MagmaCube) Health() float64                                  { return m.n().health.Health() }
func (m *MagmaCube) MaxHealth() float64                               { return m.n().health.MaxHealth() }
func (m *MagmaCube) SetMaxHealth(v float64)                           { m.n().health.SetMaxHealth(v) }
func (m *MagmaCube) Dead() bool                                       { return m.n().dead || m.Health() <= 0 }
func (m *MagmaCube) Hurt(d float64, s world.DamageSource) (float64, bool) {
	b := m.n()
	actual, ok := b.Hurt(m.Ent, m, d, s)
	if ok && m.Dead() && b.size > 1 {
		for i := range 2 + int(b.random.NextInt(3)) {
			size := b.slime.ChildSize()
			offset := float64(i)*.5 - float64(b.size)/4
			pos := m.Position().Add(mgl64.Vec3{offset, 0, offset})
			m.tx.AddEntity(NewMagmaCubeWithSize(world.EntitySpawnOpts{Position: pos, SpawnReason: "slime_split"}, size))
		}
	}
	return actual, ok
}
func NewMagmaCubeWithSize(o world.EntitySpawnOpts, size int) *world.EntityHandle {
	return o.New(MagmaCubeType, netherConfig{kind: magmaKind, size: size})
}

// CubeSize returns the Java Slime.getSize of the magma cube.
func (m *MagmaCube) CubeSize() int { return m.n().size }
func (m *Ghast) Heal(h float64, _ world.HealingSource) float64     { return healNether(m.n(), h) }
func (m *Blaze) Heal(h float64, _ world.HealingSource) float64     { return healNether(m.n(), h) }
func (m *MagmaCube) Heal(h float64, _ world.HealingSource) float64 { return healNether(m.n(), h) }
func healNether(s *netherState, h float64) float64 {
	if h <= 0 || s.dead {
		return 0
	}
	old := s.health.Health()
	s.health.AddHealth(h)
	return s.health.Health() - old
}
func (m *Ghast) KnockBack(src mgl64.Vec3, f, h float64)     { netherKnock(m.Ent, src, f, h) }
func (m *Blaze) KnockBack(src mgl64.Vec3, f, h float64)     { netherKnock(m.Ent, src, f, h) }
func (m *MagmaCube) KnockBack(src mgl64.Vec3, f, h float64) { netherKnock(m.Ent, src, f, h) }
func netherKnock(e *Ent, src mgl64.Vec3, f, h float64) {
	d := e.Position().Sub(src)
	if d.Len() > 0 {
		d = d.Normalize().Mul(f)
	}
	d[1] = h
	e.SetVelocity(d)
}
func (m *Ghast) AddEffect(e effect.Effect)      { m.n().effects.Add(e, m) }
func (m *Blaze) AddEffect(e effect.Effect)      { m.n().effects.Add(e, m) }
func (m *MagmaCube) AddEffect(e effect.Effect)  { m.n().effects.Add(e, m) }
func (m *Ghast) RemoveEffect(t effect.Type)     { m.n().effects.Remove(t, m) }
func (m *Blaze) RemoveEffect(t effect.Type)     { m.n().effects.Remove(t, m) }
func (m *MagmaCube) RemoveEffect(t effect.Type) { m.n().effects.Remove(t, m) }
func (m *Ghast) Effects() []effect.Effect       { return m.n().effects.Effects() }
func (m *Blaze) Effects() []effect.Effect       { return m.n().effects.Effects() }
func (m *MagmaCube) Effects() []effect.Effect   { return m.n().effects.Effects() }
func (m *Ghast) Speed() float64                 { return m.n().speed }
func (m *Blaze) Speed() float64                 { return m.n().speed }
func (m *MagmaCube) Speed() float64             { return m.n().speed }
func (m *Ghast) SetSpeed(v float64)             { m.n().speed = v }
func (m *Blaze) SetSpeed(v float64)             { m.n().speed = v }
func (m *MagmaCube) SetSpeed(v float64)         { m.n().speed = v }
func (m *Ghast) FinalizeSpawn(_ string, r *vanilla.Random) {
	if r != nil {
		m.n().charge = 0
	}
}
func (m *Blaze) FinalizeSpawn(_ string, r *vanilla.Random) {
	if r != nil {
		m.n().charge = 0
	}
}

type ghastFireballBehaviour struct{ *ProjectileBehaviour }
type ghastFireballType struct{}

func (b *ghastFireballBehaviour) Apply(d *world.EntityData) { d.Data = b }
func (b *ghastFireballBehaviour) Attack(e *Ent, attacker world.Entity, tx *world.Tx) {
	if attacker == nil || attacker.H().Type().EncodeEntity() != "minecraft:player" {
		return
	}
	direction := attacker.Position().Add(mgl64.Vec3{0, 1.5, 0}).Sub(e.Position())
	if direction.Len() == 0 {
		return
	}
	b.conf.Owner = attacker.H()
	e.SetVelocity(direction.Normalize().Mul(1.5))
	tx.PlaySound(e.Position(), sound.Custom{Name: "entity.ghast.shoot", Volume: 1, Pitch: 1})
}

var GhastFireballType ghastFireballType

func ghastFireballHit(e *Ent, tx *world.Tx, target trace.Result) {
	if entityResult, ok := target.(trace.EntityResult); ok {
		if ghast, ok := entityResult.Entity().(*Ghast); ok {
			if behaviour, ok := e.Behaviour().(*ghastFireballBehaviour); ok && behaviour.Owner() != nil {
				if owner, ok := behaviour.Owner().Entity(tx); ok && owner.H().Type().EncodeEntity() == "minecraft:player" {
					ghast.Hurt(1000, ProjectileDamageSource{Projectile: e, Owner: owner})
					return
				}
			}
		}
	}
	(block.ExplosionConfig{SpawnFire: true, NoBlockDamage: mobGriefingDisabled(tx)}).Explode(tx, world.NewEntityExplosionSource(e, 1))
	tx.PlaySound(e.Position(), sound.Custom{Name: "entity.generic.explode", Volume: 1, Pitch: 1})
}
func (ghastFireballType) EncodeEntity() string { return "minecraft:fireball" }
func (ghastFireballType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		d.Data = (&ghastFireballBehaviour{ProjectileBehaviour: (&ProjectileBehaviourConfig{Gravity: 0, Drag: .01, Damage: -1, Hit: ghastFireballHit}).New()})
	}
	return &Ent{tx: tx, handle: h, data: d}
}
func (ghastFireballType) BBox(world.Entity) cube.BBox { return cube.Box(-.25, 0, -.25, .25, .5, .25) }
func (ghastFireballType) DecodeNBT(_ map[string]any, d *world.EntityData) {
	d.Data = &ghastFireballBehaviour{ProjectileBehaviour: (&ProjectileBehaviourConfig{Gravity: 0, Drag: .01, Damage: -1, Hit: ghastFireballHit}).New()}
}
func (ghastFireballType) EncodeNBT(*world.EntityData) map[string]any { return nil }
func NewGhastFireball(o world.EntitySpawnOpts, owner world.Entity) *world.EntityHandle {
	var ownerHandle *world.EntityHandle
	if owner != nil {
		ownerHandle = owner.H()
	}
	conf := ProjectileBehaviourConfig{Owner: ownerHandle, Gravity: 0, Drag: .01, Damage: -1, Hit: ghastFireballHit}
	return o.New(GhastFireballType, &ghastFireballBehaviour{ProjectileBehaviour: conf.New()})
}

var _ world.EntityType = ghastType{}
var _ world.EntityType = blazeType{}
var _ world.EntityType = magmaCubeType{}
var _ world.EntityType = ghastFireballType{}
var _ Living = (*Ghast)(nil)
var _ Living = (*Blaze)(nil)
var _ Living = (*MagmaCube)(nil)
