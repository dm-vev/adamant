package entity

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/player/bossbar"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// WardenType is the registered Java Warden type.
// trySpawnWitherPattern recognises the Java T-shaped soul-sand and three-skull
// construction in every orientation accepted by the shared 24-way matcher.
func trySpawnWitherPattern(tx *world.Tx, head cube.Pos) bool {
	skull, ok := tx.Block(head).(block.Skull)
	if !ok || skull.Type != block.WitherSkeletonSkull() {
		return false
	}
	return forEachGolemPatternOrientation(func(right, up, _ cube.Pos) bool {
		base := head.Sub(golemScale(up, 2))
		if !witherSoulMaterial(tx.Block(base)) || !witherSoulMaterial(tx.Block(base.Add(up))) {
			return false
		}
		for _, side := range []cube.Pos{right, golemScale(right, -1)} {
			if !witherSoulMaterial(tx.Block(base.Add(up).Add(side))) {
				return false
			}
			sideSkull, ok := tx.Block(head.Add(side)).(block.Skull)
			if !ok || sideSkull.Type != block.WitherSkeletonSkull() {
				return false
			}
		}
		for _, pos := range []cube.Pos{head, head.Add(right), head.Sub(right), base, base.Add(up), base.Add(up).Add(right), base.Add(up).Sub(right)} {
			tx.SetBlock(pos, nil, nil)
		}
		tx.AddEntity(world.EntitySpawnOpts{Position: base.Vec3Middle(), SpawnReason: "construction"}.New(WitherType, witherConfig{invulnerable: 220}))
		return true
	})
}
func witherSoulMaterial(b world.Block) bool {
	switch b.(type) {
	case block.SoulSand, block.SoulSoil:
		return true
	default:
		return false
	}
}

const (
	wardenBaseMovementSpeed  = float64(float32(0.3))
	phantomBaseMovementSpeed = 0.7
	witherBaseMovementSpeed  = float64(float32(0.6))
)

var WardenType wardenType

type wardenType struct{}

func (wardenType) EncodeEntity() string        { return "minecraft:warden" }
func (wardenType) BBox(world.Entity) cube.BBox { return cube.Box(-.49, 0, -.49, .49, 2.9, .49) }
func (wardenType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		wardenConfig{}.Apply(d)
	}
	s := d.Data.(*wardenState)
	if h != nil {
		hash := fnv.New64a()
		id := h.UUID()
		_, _ = hash.Write(id[:])
		s.random = java26.NewLegacyRandom(int64(hash.Sum64()))
	}
	return &Warden{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (wardenType) DecodeNBT(n map[string]any, d *world.EntityData) {
	c := wardenConfig{health: nativeBossHealth(n, 500), anger: int(nbtconv.Int32(n, "Anger")), dig: int(nbtconv.Int32(n, "Digging")), emerging: int(nbtconv.Int32(n, "Emerging")), sniff: int(nbtconv.Int32(n, "Sniffing")), boom: int(nbtconv.Int32(n, "SonicBoom"))}
	if java := ReadNativeMobJavaState(n); java != nil {
		c.anger = int(nbtconv.Int32(java, "Anger"))
		c.dig = int(nbtconv.Int32(java, "Digging"))
		c.emerging = int(nbtconv.Int32(java, "Emerging"))
		c.sniff = int(nbtconv.Int32(java, "Sniffing"))
		c.boom = int(nbtconv.Int32(java, "SonicBoom"))
		c.target = nbtconv.Int64(java, "Target")
	}
	c.Apply(d)
	d.Data.(*wardenState).speed = nativeBossMovementSpeed(n, wardenBaseMovementSpeed)
}
func (wardenType) EncodeNBT(d *world.EntityData) map[string]any {
	s := d.Data.(*wardenState)
	attrs := []NativeMobAttribute{
		{Name: "minecraft:health", Base: 500, Current: s.health.Health(), DefaultMin: 0, DefaultMax: 500, Min: 0, Max: 500},
		{Name: "minecraft:movement", Base: wardenBaseMovementSpeed, Current: s.speed, DefaultMin: 0, DefaultMax: 1024, Min: 0, Max: 1024},
		{Name: "minecraft:follow_range", Base: 16, Current: 16, DefaultMin: 0, DefaultMax: 2048, Min: 0, Max: 2048},
		{Name: "minecraft:attack_damage", Base: 30, Current: 30, DefaultMin: 0, DefaultMax: 2048, Min: 0, Max: 2048},
	}
	return encodeBossNBT(d, attrs, map[string]any{"Anger": int32(s.anger), "Digging": int32(s.dig), "Emerging": int32(s.emerging), "Sniffing": int32(s.sniff), "SonicBoom": int32(s.boom), "Target": s.target})
}

type wardenConfig struct {
	health                            float64
	anger, dig, emerging, sniff, boom int
	target                            int64
}

func (c wardenConfig) Apply(d *world.EntityData) {
	h := c.health
	if h <= 0 {
		h = 500
	}
	d.Data = &wardenState{health: NewHealthManager(h, 500), effects: NewEffectManager(), speed: wardenBaseMovementSpeed, mc: &MovementComputer{Gravity: .08, Drag: .02}, random: java26.NewLegacyRandom(0), anger: max(0, c.anger), dig: max(0, c.dig), emerging: max(0, c.emerging), sniff: max(0, c.sniff), boom: max(0, c.boom), target: c.target}
}

type wardenState struct {
	health                            *HealthManager
	effects                           *EffectManager
	speed                             float64
	mc                                *MovementComputer
	random                            java26.Random
	anger, dig, emerging, sniff, boom int
	target                            int64
	pulse                             int
	sonicCooldown                     int
	scent, lastSensed                 mgl64.Vec3
	scentSet, sensed                  bool
	dead                              bool
}
type Warden struct{ *Ent }

func NewWarden(o world.EntitySpawnOpts) *world.EntityHandle { return o.New(WardenType, wardenConfig{}) }
func (*Warden) SpeciesType() world.EntityType               { return WardenType }
func (w *Warden) state() *wardenState                       { return w.Behaviour().(*wardenState) }
func (w *Warden) Tick(tx *world.Tx, n int64)                { w.state().effects.Tick(w, tx); w.Ent.Tick(tx, n) }
func (s *wardenState) Tick(e *Ent, tx *world.Tx) *Movement {
	if s.sonicCooldown > 0 {
		s.sonicCooldown--
	}
	if s.anger > 0 {
		s.anger--
	}
	if s.dig > 0 {
		s.dig--
		if s.dig == 0 {
			s.emerging, s.sniff = 0, 0
			if !s.scentSet {
				_ = e.CloseIn(tx)
				return nil
			}
		}
	}
	if s.emerging > 0 {
		s.emerging--
	}
	if s.sniff > 0 {
		s.sniff--
	}
	if s.pulse > 0 {
		s.pulse--
	}
	if s.pulse == 0 {
		s.pulse = 10
		box := cube.Box(-32, -16, -32, 32, 16, 32).Translate(e.Position())
		for candidate := range tx.EntitiesWithin(box) {
			if candidate == e {
				continue
			}
			pos := candidate.Position()
			if pos.Sub(e.Position()).LenSqr() >= 1024 {
				continue
			}
			moved := !s.sensed || pos.Sub(s.lastSensed).LenSqr() > .01
			s.lastSensed, s.sensed = pos, true
			if moved {
				id := candidate.H().UUID()
				s.target = int64(binary.LittleEndian.Uint64(id[8:]))
				s.scent, s.scentSet = pos, true
				s.anger = min(150, s.anger+20)
			}
		}
	}
	if s.pulse%4 == 0 {
		for player := range tx.EntitiesWithin(cube.Box(-20, -12, -20, 20, 12, 20).Translate(e.Position())) {
			if player.H().Type().EncodeEntity() == "minecraft:player" {
				if living, ok := player.(Living); ok {
					living.AddEffect(effect.New(effect.Darkness, 1, time.Second*4))
				}
			}
		}
	}
	if !s.scentSet {
		s.sniff++
		if s.sniff > 1200 {
			s.dig = 120
			s.sniff = 0
		}
		return s.stop(e, tx)
	}
	d := s.scent.Sub(e.Position())
	dist := d.Len()
	if dist < 1.8 {
		for target := range tx.EntitiesWithin(cube.Box(-2, -2, -2, 2, 2, 2).Translate(e.Position())) {
			if target.H().Type().EncodeEntity() == "minecraft:player" {
				if living, ok := target.(Living); ok {
					living.Hurt(30, AttackDamageSource{Attacker: &Warden{Ent: e}})
				}
			}
		}
		return s.stop(e, tx)
	}
	if dist > 48 {
		s.scentSet = false
		s.dig = 120
		return s.stop(e, tx)
	}
	if dist < 15 && s.sonicCooldown == 0 {
		s.boom = 34
		s.sonicCooldown = 60
		return s.sonic(e, tx, s.scent)
	}
	return s.move(e, tx, s.scent, s.speed)
}
func (s *wardenState) sonic(e *Ent, tx *world.Tx, target mgl64.Vec3) *Movement {
	delta := target.Sub(e.Position().Add(mgl64.Vec3{0, 1.6, 0}))
	if delta.Len() == 0 {
		return s.stop(e, tx)
	}
	dir := delta.Normalize()
	hit := make(map[*world.EntityHandle]struct{})
	for i := range 15 {
		p := e.Position().Add(mgl64.Vec3{0, 1.6, 0}).Add(dir.Mul(float64(i + 1)))
		for victim := range tx.EntitiesWithin(cube.Box(-.7, -.7, -.7, .7, .7, .7).Translate(p)) {
			if victim == e {
				continue
			}
			if _, seen := hit[victim.H()]; seen {
				continue
			}
			living, ok := victim.(Living)
			if !ok || living.Dead() {
				continue
			}
			hit[victim.H()] = struct{}{}
			living.Hurt(10, AttackDamageSource{Attacker: &Warden{Ent: e}})
		}
	}
	tx.PlaySound(e.Position(), sound.Custom{Name: "entity.warden.sonic_boom", Volume: 3, Pitch: 1})
	return s.stop(e, tx)
}
func (s *wardenState) move(e *Ent, tx *world.Tx, p mgl64.Vec3, speed float64) *Movement {
	d := p.Sub(e.Position())
	d[1] = 0
	if d.Len() > 0 {
		d = d.Normalize()
	}
	v := e.Velocity()
	v[0], v[2] = d[0]*speed, d[2]*speed
	r := e.Rotation()
	if d.Len() > 0 {
		r[0] = math.Atan2(-d[0], d[2]) * 180 / math.Pi
	}
	m := s.mc.TickMovement(e, e.Position(), v, e.Rotation(), r, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	return m
}
func (s *wardenState) stop(e *Ent, tx *world.Tx) *Movement {
	v := e.Velocity()
	v[0], v[2] = 0, 0
	m := s.mc.TickMovement(e, e.Position(), v, e.Rotation(), e.Rotation(), tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	return m
}
func (w *Warden) Health() float64        { return w.state().health.Health() }
func (w *Warden) MaxHealth() float64     { return w.state().health.MaxHealth() }
func (w *Warden) SetMaxHealth(v float64) { w.state().health.SetMaxHealth(v) }
func (w *Warden) Dead() bool             { return w.state().dead || w.Health() <= 0 }
func (w *Warden) Hurt(d float64, src world.DamageSource) (float64, bool) {
	if w.Dead() || d <= 0 {
		return 0, false
	}
	s := w.state()
	s.anger = min(150, s.anger+20)
	s.scentSet = false
	if r, ok := s.effects.Effect(effect.Resistance); ok {
		d *= effect.Resistance.Multiplier(src, r.Level())
	}
	s.health.AddHealth(-d)
	w.viewAction(HurtAction{})
	if w.Dead() {
		s.dead = true
		w.viewAction(DeathAction{})
		w.dropSourceLoot(src)
		_ = w.CloseIn(w.tx)
	}
	return d, true
}
func (w *Warden) Heal(v float64, _ world.HealingSource) float64 {
	if v <= 0 || w.Dead() {
		return 0
	}
	a := w.Health()
	w.state().health.AddHealth(v)
	return w.Health() - a
}
func (w *Warden) KnockBack(p mgl64.Vec3, f, h float64) {
	d := w.Position().Sub(p)
	d[1] = 0
	if d.Len() > 0 {
		d = d.Normalize().Mul(f)
	}
	d[1] = h
	w.SetVelocity(d)
}
func (w *Warden) AddEffect(e effect.Effect) {
	if e.Type() != effect.Poison && e.Type() != effect.FatalPoison {
		w.state().effects.Add(e, w)
	}
}
func (w *Warden) RemoveEffect(t effect.Type) { w.state().effects.Remove(t, w) }
func (w *Warden) Effects() []effect.Effect   { return w.state().effects.Effects() }
func (w *Warden) Speed() float64             { return w.state().speed }
func (w *Warden) SetSpeed(v float64)         { w.state().speed = max(0, min(1024, v)) }
func (w *Warden) viewAction(a world.EntityAction) {
	v := w.tx.Viewers(w.Position())
	for _, x := range v {
		x.ViewEntityAction(w, a)
	}
	w.tx.ReleaseViewers(v)
}

// PhantomType is the insomnia-spawned Phantom.
var PhantomType phantomType

type phantomType struct{}

func (phantomType) EncodeEntity() string { return "minecraft:phantom" }
func (phantomType) BBox(e world.Entity) cube.BBox {
	size := 0
	if phantom, ok := e.(*Phantom); ok {
		size = phantom.state().size
	}
	scale := 1 + .15*float64(size)
	return cube.Box(-.3*scale, 0, -.3*scale, .3*scale, .9*scale, .3*scale)
}
func (phantomType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		phantomConfig{}.Apply(d)
	}
	s := d.Data.(*phantomState)
	if h != nil {
		hash := fnv.New64a()
		u := h.UUID()
		_, _ = hash.Write(u[:])
		s.random = java26.NewLegacyRandom(int64(hash.Sum64()))
	}
	return &Phantom{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (phantomType) DecodeNBT(n map[string]any, d *world.EntityData) {
	size := int(nbtconv.Int32(n, "Size"))
	if java := ReadNativeMobJavaState(n); java != nil {
		size = int(nbtconv.Int32(java, "Size"))
	}
	phantomConfig{health: nativeBossHealth(n, 20), size: size, attack: int(nbtconv.Int32(n, "Attack")), target: nbtconv.Int64(n, "Target")}.Apply(d)
	d.Data.(*phantomState).speed = nativeBossMovementSpeed(n, phantomBaseMovementSpeed)
	if _, ok := n["AnchorX"]; ok {
		s := d.Data.(*phantomState)
		s.anchor = mgl64.Vec3{nbtconv.Float64(n, "AnchorX"), nbtconv.Float64(n, "AnchorY"), nbtconv.Float64(n, "AnchorZ")}
		s.anchorSet = true
	}
}
func (phantomType) EncodeNBT(d *world.EntityData) map[string]any {
	s := d.Data.(*phantomState)
	maxHealth := s.health.MaxHealth()
	attrs := []NativeMobAttribute{
		{Name: "minecraft:health", Base: maxHealth, Current: s.health.Health(), DefaultMin: 0, DefaultMax: maxHealth, Min: 0, Max: maxHealth},
		{Name: "minecraft:movement", Base: phantomBaseMovementSpeed, Current: s.speed, DefaultMin: 0, DefaultMax: 1024, Min: 0, Max: 1024},
		{Name: "minecraft:follow_range", Base: 64, Current: 64, DefaultMin: 0, DefaultMax: 2048, Min: 0, Max: 2048},
		{Name: "minecraft:attack_damage", Base: float64(6 + s.size), Current: float64(6 + s.size), DefaultMin: 0, DefaultMax: 2048, Min: 0, Max: 2048},
	}
	n := encodeBossNBT(d, attrs, map[string]any{"Size": int32(s.size), "Attack": int32(s.attack), "Target": s.target})
	if s.anchorSet {
		n["AnchorX"], n["AnchorY"], n["AnchorZ"] = float64(s.anchor[0]), float64(s.anchor[1]), float64(s.anchor[2])
	}
	return n
}

type phantomConfig struct {
	health       float64
	size, attack int
	target       int64
}

func (c phantomConfig) Apply(d *world.EntityData) {
	size := max(0, min(64, c.size))
	const maxHealth = 20
	health := c.health
	if health <= 0 {
		health = maxHealth
	}
	d.Data = &phantomState{health: NewHealthManager(health, maxHealth), effects: NewEffectManager(), speed: phantomBaseMovementSpeed, mc: &MovementComputer{Gravity: 0, Drag: .02, DragBeforeGravity: true}, random: java26.NewLegacyRandom(0), size: size, attack: c.attack, target: c.target}
}

type phantomState struct {
	health       *HealthManager
	effects      *EffectManager
	speed        float64
	mc           *MovementComputer
	random       java26.Random
	size, attack int
	target       int64
	orbit        float64
	cooldown     int
	sunTicks     int
	anchor       mgl64.Vec3
	anchorSet    bool
	dead         bool
}
type Phantom struct{ *Ent }

func NewPhantom(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(PhantomType, phantomConfig{})
}
func (*Phantom) SpeciesType() world.EntityType { return PhantomType }
func (p *Phantom) state() *phantomState        { return p.Behaviour().(*phantomState) }
func (p *Phantom) Tick(tx *world.Tx, n int64)  { p.state().effects.Tick(p, tx); p.Ent.Tick(tx, n) }
func (p *Phantom) Health() float64             { return p.state().health.Health() }
func (p *Phantom) MaxHealth() float64          { return p.state().health.MaxHealth() }
func (p *Phantom) SetMaxHealth(v float64)      { p.state().health.SetMaxHealth(v) }
func (p *Phantom) Dead() bool                  { return p.state().dead || p.Health() <= 0 }
func (p *Phantom) Heal(v float64, _ world.HealingSource) float64 {
	if v <= 0 || p.Dead() {
		return 0
	}
	old := p.Health()
	p.state().health.AddHealth(v)
	return p.Health() - old
}
func (p *Phantom) KnockBack(src mgl64.Vec3, f, h float64) {
	d := p.Position().Sub(src)
	if d.Len() > 0 {
		d = d.Normalize().Mul(f)
	}
	d[1] = h
	p.SetVelocity(d)
}
func (p *Phantom) AddEffect(e effect.Effect) {
	if e.Type() != effect.Poison && e.Type() != effect.FatalPoison {
		p.state().effects.Add(e, p)
	}
}
func (p *Phantom) RemoveEffect(t effect.Type) { p.state().effects.Remove(t, p) }
func (p *Phantom) Effects() []effect.Effect   { return p.state().effects.Effects() }
func (p *Phantom) Speed() float64             { return p.state().speed }
func (p *Phantom) SetSpeed(v float64)         { p.state().speed = max(0, min(1024, v)) }
func (p *Phantom) viewAction(a world.EntityAction) {
	v := p.tx.Viewers(p.Position())
	for _, x := range v {
		x.ViewEntityAction(p, a)
	}
	p.tx.ReleaseViewers(v)
}
func (p *Phantom) FinalizeSpawn(_ string, _ *java26.Random) {
	s := p.state()
	s.anchor = p.Position().Add(mgl64.Vec3{0, 5, 0})
	s.anchorSet = true
}
func (s *phantomState) Tick(e *Ent, tx *world.Tx) *Movement {
	if s.cooldown > 0 {
		s.cooldown--
	}
	pos := cube.PosFromVec3(e.Position())
	if tx.World().Time()%24000 < 12000 && tx.SkyLight(pos) >= 12 {
		s.sunTicks++
		if s.sunTicks >= 20 {
			if flammable, ok := any(&Phantom{Ent: e}).(Flammable); ok {
				flammable.SetOnFire(time.Second * 8)
			}
			s.sunTicks = 0
		}
	} else {
		s.sunTicks = 0
	}
	var target world.Entity
	best := math.MaxFloat64
	for candidate := range tx.EntitiesWithin(cube.Box(-32, -24, -32, 32, 24, 32).Translate(e.Position())) {
		id := candidate.H().Type().EncodeEntity()
		if id == "minecraft:cat" {
			delta := e.Position().Sub(candidate.Position())
			if delta.LenSqr() < 256 && delta.LenSqr() > 0 {
				away := delta.Normalize().Mul(16)
				return s.circle(e, tx, e.Position().Add(away))
			}
			continue
		}
		if id != "minecraft:player" || math.Abs(candidate.Position()[1]-e.Position()[1]) > 24 {
			continue
		}
		dist := candidate.Position().Sub(e.Position()).LenSqr()
		if dist < best && dist < 4096 {
			target, best = candidate, dist
		}
	}
	if target == nil {
		anchor := e.Position().Add(mgl64.Vec3{0, 3, 0})
		if s.anchorSet {
			anchor = s.anchor.Add(mgl64.Vec3{math.Cos(s.orbit) * 5, 0, math.Sin(s.orbit) * 5})
			s.orbit += .05
		}
		return s.circle(e, tx, anchor)
	}
	id := target.H().UUID()
	s.target = int64(binary.LittleEndian.Uint64(id[8:]))
	d := target.Position().Sub(e.Position())
	flat := math.Hypot(d[0], d[2])
	if flat < 4 && math.Abs(d[1]) < 2 && s.cooldown == 0 {
		s.attack = 10
		s.cooldown = 40
		if living, ok := target.(Living); ok {
			living.Hurt(float64(6+s.size), AttackDamageSource{Attacker: &Phantom{Ent: e}})
		}
		tx.PlaySound(e.Position(), sound.Custom{Name: "entity.phantom.bite", Volume: 1, Pitch: 1})
		return s.circle(e, tx, e.Position().Add(mgl64.Vec3{0, 6, 0}))
	}
	if flat < 12 {
		s.orbit += .09
		return s.circle(e, tx, target.Position().Add(mgl64.Vec3{math.Cos(s.orbit) * 12, 4, math.Sin(s.orbit) * 12}))
	}
	return s.circle(e, tx, target.Position().Add(mgl64.Vec3{0, 5, 0}))
}
func (s *phantomState) circle(e *Ent, tx *world.Tx, p mgl64.Vec3) *Movement {
	d := p.Sub(e.Position())
	if d.Len() > 0 {
		d = d.Normalize()
	}
	v := e.Velocity().Mul(.8).Add(d.Mul(.1))
	rot := e.Rotation()
	rot[0] = math.Atan2(-v[0], v[2]) * 180 / math.Pi
	m := s.mc.TickMovement(e, e.Position(), v, e.Rotation(), rot, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	return m
}
func (p *Phantom) Hurt(d float64, src world.DamageSource) (float64, bool) {
	if p.Dead() || d <= 0 {
		return 0, false
	}
	s := p.state()
	s.health.AddHealth(-d)
	p.viewAction(HurtAction{})
	if p.Dead() {
		s.dead = true
		p.viewAction(DeathAction{})
		p.dropSourceLoot(src)
		_ = p.CloseIn(p.tx)
	}
	return d, true
}

// WitherType is the constructible three-headed Wither boss.
var WitherType witherType

type witherType struct{}

func (witherType) EncodeEntity() string        { return "minecraft:wither" }
func (witherType) BBox(world.Entity) cube.BBox { return cube.Box(-.49, 0, -.49, .49, 3.5, .49) }
func (witherType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		witherConfig{}.Apply(d)
	}
	s := d.Data.(*witherState)
	if h != nil {
		id := h.UUID()
		s.bar = bossbar.New("Wither").WithID(fmt.Sprintf("adamant:wither:%x", id[8:]))
		hash := fnv.New64a()
		_, _ = hash.Write(id[:])
		s.random = java26.NewLegacyRandom(int64(hash.Sum64()))
	}
	return &Wither{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (witherType) DecodeNBT(n map[string]any, d *world.EntityData) {
	c := witherConfig{health: nativeBossHealth(n, 300), invulnerable: int(nbtconv.Int32(n, "Invul")), charge: int(nbtconv.Int32(n, "Charge")), death: int(nbtconv.Int32(n, "Death"))}
	if j := ReadNativeMobJavaState(n); j != nil {
		c.invulnerable = int(nbtconv.Int32(j, "Invulnerability"))
		c.charge = int(nbtconv.Int32(j, "Charge"))
		c.death = int(nbtconv.Int32(j, "Death"))
	}
	c.Apply(d)
	d.Data.(*witherState).speed = nativeBossMovementSpeed(n, witherBaseMovementSpeed)
}
func (witherType) EncodeNBT(d *world.EntityData) map[string]any {
	s := d.Data.(*witherState)
	attrs := []NativeMobAttribute{
		{Name: "minecraft:health", Base: 300, Current: s.health.Health(), DefaultMin: 0, DefaultMax: 300, Min: 0, Max: 300},
		{Name: "minecraft:movement", Base: witherBaseMovementSpeed, Current: s.speed, DefaultMin: 0, DefaultMax: 1024, Min: 0, Max: 1024},
		{Name: "minecraft:follow_range", Base: 40, Current: 40, DefaultMin: 0, DefaultMax: 2048, Min: 0, Max: 2048},
		{Name: "minecraft:attack_damage", Base: 8, Current: 8, DefaultMin: 0, DefaultMax: 2048, Min: 0, Max: 2048},
	}
	return encodeBossNBT(d, attrs, map[string]any{"Invulnerability": int32(s.invulnerable), "Charge": int32(s.charge), "Death": int32(s.death), "Targets": s.targets})
}

type witherConfig struct {
	health                      float64
	invulnerable, charge, death int
}

func (c witherConfig) Apply(d *world.EntityData) {
	h := c.health
	if h <= 0 {
		h = 300
	}
	d.Data = &witherState{health: NewHealthManager(h, 300), effects: NewEffectManager(), speed: witherBaseMovementSpeed, mc: &MovementComputer{Gravity: 0, Drag: .02}, invulnerable: max(0, c.invulnerable), charge: max(0, c.charge), death: max(0, c.death), targets: make([]int64, 3), bar: bossbar.New("Wither"), random: java26.NewLegacyRandom(0), viewers: make(map[*world.EntityHandle]bool)}
}

type witherState struct {
	health                      *HealthManager
	effects                     *EffectManager
	speed                       float64
	mc                          *MovementComputer
	random                      java26.Random
	invulnerable, charge, death int
	targets                     []int64
	heads                       [3]int
	shoot                       int
	dead, barShown              bool
	bar                         bossbar.BossBar
	destroyTicks                int
	viewers                     map[*world.EntityHandle]bool
}
type Wither struct{ *Ent }

func NewWither(o world.EntitySpawnOpts) *world.EntityHandle { return o.New(WitherType, witherConfig{}) }
func (w *Wither) state() *witherState                       { return w.Behaviour().(*witherState) }
func (w *Wither) Tick(tx *world.Tx, n int64)                { w.state().effects.Tick(w, tx); w.Ent.Tick(tx, n) }
func (s *witherState) Tick(e *Ent, tx *world.Tx) *Movement {
	if s.dead {
		s.death++
		if s.death >= 220 {
			_ = e.CloseIn(tx)
		}
		return s.stop(e, tx)
	}
	w := &Wither{Ent: e}
	w.updateBossBar()
	if s.invulnerable > 0 {
		s.invulnerable--
		if s.invulnerable%10 == 0 {
			s.health.AddHealth(10)
		}
		if s.invulnerable == 0 {
			// WitherBoss.customServerAiStep: the summoning finishes with a
			// power 7 mob explosion and the global wither spawn sound.
			block.ExplosionConfig{NoBlockDamage: mobGriefingDisabled(tx)}.Explode(tx, world.NewEntityExplosionSource(e, 7))
			tx.PlayGlobalSound(e.Position(), sound.Custom{Name: "mob.wither.spawn", Volume: 1, Pitch: 1})
		}
		return s.stop(e, tx)
	}
	if s.shoot > 0 {
		s.shoot--
	}
	var targets [3]world.Entity
	for i := range targets {
		targets[i] = witherNearest(tx, e.Position(), e, targets[:i])
		if targets[i] != nil {
			id := targets[i].H().UUID()
			s.targets[i] = int64(binary.LittleEndian.Uint64(id[8:]))
		}
	}
	target := targets[0]
	if target == nil {
		return s.stop(e, tx)
	}
	d := target.Position().Sub(e.Position())
	v := e.Velocity()
	v[0] = v[0]*.8 + math.Copysign(.05, d[0])
	v[1] = v[1]*.8 + math.Copysign(.03, d[1])
	v[2] = v[2]*.8 + math.Copysign(.05, d[2])
	m := s.mc.TickMovement(e, e.Position(), v, e.Rotation(), cube.Rotation{math.Atan2(-d[0], d[2]) * 180 / math.Pi, 0}, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	if s.shoot == 0 {
		for i, t := range targets {
			if t != nil {
				s.fireSkull(e, tx, t, i > 0)
			}
		}
		s.shoot = 40
	}
	s.destroyTicks++
	if s.health.Health() <= s.health.MaxHealth()/2 {
		if s.destroyTicks >= 20 {
			s.destroyTicks = 0
			s.destroyBlocks(tx, e.Position())
		}
		if tx.CurrentTick()%20 == 0 {
			s.health.AddHealth(1)
		}
	}
	return m
}
func (s *witherState) fireSkull(e *Ent, tx *world.Tx, target world.Entity, side bool) {
	src := e.Position().Add(mgl64.Vec3{0, 2, 0})
	delta := target.Position().Add(mgl64.Vec3{0, 1, 0}).Sub(src)
	if delta.Len() == 0 {
		return
	}
	charged := side && tx.World().Difficulty() == world.DifficultyHard && s.random.NextFloat() < .1
	opts := world.EntitySpawnOpts{Position: src, Velocity: delta.Normalize().Mul(.9)}
	tx.AddEntity(NewWitherSkull(opts, e, charged))
	tx.PlaySound(src, sound.Custom{Name: "mob.wither.shoot", Volume: 1, Pitch: 1})
}
func witherNearest(tx *world.Tx, p mgl64.Vec3, exclude world.Entity, used []world.Entity) world.Entity {
	var target world.Entity
	best := math.MaxFloat64
	for candidate := range tx.EntitiesWithin(cube.Box(-40, -24, -40, 40, 24, 40).Translate(p)) {
		if candidate == exclude {
			continue
		}
		id := candidate.H().Type().EncodeEntity()
		if id != "minecraft:player" && id != "minecraft:iron_golem" {
			continue
		}
		living, ok := candidate.(Living)
		if !ok || living.Dead() {
			continue
		}
		found := false
		for _, previous := range used {
			if previous.H() == candidate.H() {
				found = true
				break
			}
		}
		if found {
			continue
		}
		dist := candidate.Position().Sub(p).LenSqr()
		if dist < best {
			best, target = dist, candidate
		}
	}
	return target
}
func (s *witherState) destroyBlocks(tx *world.Tx, p mgl64.Vec3) {
	center := cube.PosFromVec3(p)
	for x := -1; x <= 1; x++ {
		for y := 0; y <= 2; y++ {
			for z := -1; z <= 1; z++ {
				pos := center.Add(cube.Pos{x, y, z})
				breakable, ok := tx.Block(pos).(block.Breakable)
				if !ok || breakable.BreakInfo().Hardness <= 0 {
					continue
				}
				tx.SetBlock(pos, nil, nil)
			}
		}
	}
}
func (w *Wither) updateBossBar() {
	s := w.state()
	bar := s.bar.WithHealthPercentage(max(0, min(1, w.Health()/w.MaxHealth())))
	changed := !s.barShown || bar.HealthPercentage() != s.bar.HealthPercentage()
	s.bar, s.barShown = bar, true
	for handle := range s.viewers {
		s.viewers[handle] = false
	}
	for viewer := range w.tx.EntitiesWithin(cube.Box(-64, -64, -64, 64, 64, 64).Translate(w.Position())) {
		player, ok := viewer.(interface{ SendBossBar(bossbar.BossBar) })
		if !ok {
			continue
		}
		handle := viewer.H()
		if changed || !s.viewers[handle] {
			player.SendBossBar(bar)
		}
		s.viewers[handle] = true
	}
	for handle, visible := range s.viewers {
		if visible {
			continue
		}
		if entity, ok := handle.Entity(w.tx); ok {
			if player, ok := entity.(interface{ RemoveBossBarID(string) }); ok {
				player.RemoveBossBarID(bar.ID())
			}
		}
		delete(s.viewers, handle)
	}
}
func (w *Wither) removeBossBar() {
	s := w.state()
	id := s.bar.ID()
	for handle := range s.viewers {
		if entity, ok := handle.Entity(w.tx); ok {
			if player, ok := entity.(interface{ RemoveBossBarID(string) }); ok {
				player.RemoveBossBarID(id)
			}
		}
		delete(s.viewers, handle)
	}
}
func (s *witherState) stop(e *Ent, tx *world.Tx) *Movement {
	m := s.mc.TickMovement(e, e.Position(), mgl64.Vec3{}, e.Rotation(), e.Rotation(), tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	return m
}
func (w *Wither) Health() float64        { return w.state().health.Health() }
func (w *Wither) MaxHealth() float64     { return w.state().health.MaxHealth() }
func (w *Wither) SetMaxHealth(v float64) { w.state().health.SetMaxHealth(v) }
func (w *Wither) Dead() bool             { return w.state().dead || w.state().death >= 220 }
func (w *Wither) Hurt(d float64, src world.DamageSource) (float64, bool) {
	if w.Dead() || d <= 0 || w.state().invulnerable > 0 {
		return 0, false
	}
	s := w.state()
	if s.health.Health() <= s.health.MaxHealth()/2 {
		if _, ok := src.(ProjectileDamageSource); ok {
			return 0, false
		}
	}
	s.health.AddHealth(-d)
	w.updateBossBar()
	w.viewAction(HurtAction{})
	if s.health.Health() <= 0 {
		s.dead = true
		s.death = 1
		w.viewAction(DeathAction{})
		w.removeBossBar()
		w.dropSourceLoot(src)
		w.tx.PlaySound(w.Position(), sound.Custom{Name: "mob.wither.death", Volume: 1, Pitch: 1})
	}
	return d, true
}
func (w *Wither) Heal(v float64, _ world.HealingSource) float64 {
	if v <= 0 || w.Dead() {
		return 0
	}
	a := w.Health()
	w.state().health.AddHealth(v)
	w.updateBossBar()
	return w.Health() - a
}
func (w *Wither) KnockBack(mgl64.Vec3, float64, float64) {}
func (w *Wither) AddEffect(e effect.Effect) {
	if e.Type() != effect.Poison && e.Type() != effect.FatalPoison {
		w.state().effects.Add(e, w)
	}
}
func (w *Wither) RemoveEffect(t effect.Type) { w.state().effects.Remove(t, w) }
func (w *Wither) Effects() []effect.Effect   { return w.state().effects.Effects() }
func (w *Wither) Speed() float64             { return w.state().speed }
func (w *Wither) SetSpeed(v float64)         { w.state().speed = max(0, min(1024, v)) }
func (w *Wither) viewAction(a world.EntityAction) {
	v := w.tx.Viewers(w.Position())
	for _, x := range v {
		x.ViewEntityAction(w, a)
	}
	w.tx.ReleaseViewers(v)
}

func (w *Warden) dropSourceLoot(src world.DamageSource)  { dropAllDeathLoot(w.tx, w, src) }
func (p *Phantom) dropSourceLoot(src world.DamageSource) { dropAllDeathLoot(p.tx, p, src) }
func (w *Wither) dropSourceLoot(src world.DamageSource)  { dropAllDeathLoot(w.tx, w, src) }

// DamageSourceKiller returns the entity credited with a kill: the attacker of
// a melee source or the owner of a projectile.
func DamageSourceKiller(src world.DamageSource) world.Entity { return mobKiller(src) }

func mobKiller(src world.DamageSource) world.Entity {
	switch s := src.(type) {
	case AttackDamageSource:
		return s.Attacker
	case SpearDamageSource:
		return s.Attacker
	case MaceSmashDamageSource:
		return s.Attacker
	case ProjectileDamageSource:
		return s.Owner
	}
	return nil
}
func mobKilledByPlayer(src world.DamageSource) bool {
	killer := mobKiller(src)
	return killer != nil && killer.H().Type().EncodeEntity() == "minecraft:player"
}

func mobGriefingDisabled(tx *world.Tx) bool {
	return !tx.World().GameRuleBool("mob_griefing")
}
func nativeBossMovementSpeed(n map[string]any, fallback float64) float64 {
	attributes, _ := n["Attributes"].([]any)
	for _, value := range attributes {
		attribute, ok := value.(map[string]any)
		if !ok || attribute["Name"] != "minecraft:movement" {
			continue
		}
		if current, ok := attribute["Current"]; ok {
			return max(0, min(1024, nativeNBTNumber(current)))
		}
	}
	return fallback
}

func nativeBossHealth(n map[string]any, fallback float64) float64 {
	for _, attr := range ReadNativeMobAttributes(n) {
		if attr.Name == "minecraft:health" && attr.Current > 0 {
			return attr.Current
		}
	}
	if _, ok := n["Health"]; ok {
		h := float64(nbtconv.Float32(n, "Health"))
		if h > 0 {
			return h
		}
	}
	return fallback
}
func encodeBossNBT(d *world.EntityData, attrs []NativeMobAttribute, state map[string]any) map[string]any {
	native := CloneNativeMobNBT(d.NativeNBT)
	if native == nil {
		native = make(map[string]any)
	}
	native["Attributes"] = EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), attrs)
	return MergeNativeMobJavaState(native, state)
}

var _ world.EntityType = wardenType{}
var _ world.EntityType = phantomType{}
var _ world.EntityType = witherType{}
var _ Living = (*Warden)(nil)
var _ Living = (*Phantom)(nil)
var _ Living = (*Wither)(nil)
