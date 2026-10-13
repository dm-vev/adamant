package entity

import (
	"hash/fnv"
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// Each species has its own registered type, constructor and source state. The common
// runtime below only shares living-mob plumbing; species transitions remain explicit.
type spiderType struct{}
type caveSpiderType struct{}
type creeperType struct{}
type endermanType struct{}
type silverfishType struct{}
type endermiteType struct{}
type slimeType struct{}

var SpiderType spiderType
var CaveSpiderType caveSpiderType
var CreeperType creeperType
var EndermanType endermanType
var SilverfishType silverfishType
var EndermiteType endermiteType
var SlimeType slimeType

type speciesKind uint8

const (
	spiderSpecies speciesKind = iota
	caveSpiderSpecies
	creeperSpecies
	endermanSpecies
	silverfishSpecies
	endermiteSpecies
	slimeSpecies
)

type speciesConfig struct {
	kind                                   speciesKind
	health                                 float64
	size                                   int
	fuse, swell, attackCooldown, jumpDelay int
	charged, ignited, climbing, loaded     bool
	carried                                map[string]any
	carriedSet                             bool
	effects                                []effect.Effect
	native                                 map[string]any
}
type speciesState struct {
	kind                                     speciesKind
	health                                   *HealthManager
	effects                                  *EffectManager
	mc                                       *MovementComputer
	random                                   java26.Random
	speed, attack                            float64
	fuse, swell, cooldown, jumpDelay, size   int
	charged, ignited, climbing, dead, loaded bool
	carried                                  map[string]any
	core                                     *SlimeCore
	passenger                                *world.EntityHandle
	navigator                                Navigator
	wander                                   int
}
type Spider struct{ *speciesMob }
type CaveSpider struct{ *speciesMob }
type Creeper struct{ *speciesMob }
type Enderman struct{ *speciesMob }
type Silverfish struct{ *speciesMob }
type Endermite struct{ *speciesMob }
type Slime struct{ *speciesMob }
type speciesMob struct {
	*Ent
	kind speciesKind
}

func (spiderType) EncodeEntity() string     { return "minecraft:spider" }
func (caveSpiderType) EncodeEntity() string { return "minecraft:cave_spider" }
func (creeperType) EncodeEntity() string    { return "minecraft:creeper" }
func (endermanType) EncodeEntity() string   { return "minecraft:enderman" }
func (silverfishType) EncodeEntity() string { return "minecraft:silverfish" }
func (endermiteType) EncodeEntity() string  { return "minecraft:endermite" }
func (slimeType) EncodeEntity() string      { return "minecraft:slime" }
func (t spiderType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	return openSpecies(tx, h, d, spiderSpecies)
}
func (t caveSpiderType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	return openSpecies(tx, h, d, caveSpiderSpecies)
}
func (t creeperType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	return openSpecies(tx, h, d, creeperSpecies)
}
func (t endermanType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	return openSpecies(tx, h, d, endermanSpecies)
}
func (t silverfishType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	return openSpecies(tx, h, d, silverfishSpecies)
}
func (t endermiteType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	return openSpecies(tx, h, d, endermiteSpecies)
}
func (t slimeType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	return openSpecies(tx, h, d, slimeSpecies)
}
func openSpecies(tx *world.Tx, h *world.EntityHandle, d *world.EntityData, k speciesKind) world.Entity {
	if d.Data == nil {
		(speciesConfig{kind: k}).Apply(d)
	}
	s := d.Data.(*speciesState)
	if h != nil {
		u := h.UUID()
		hash := fnv.New64a()
		_, _ = hash.Write(u[:])
		s.random = java26.NewLegacyRandom(int64(hash.Sum64()))
	}
	m := &speciesMob{Ent: &Ent{tx: tx, handle: h, data: d}, kind: k}
	switch k {
	case spiderSpecies:
		return &Spider{m}
	case caveSpiderSpecies:
		return &CaveSpider{m}
	case creeperSpecies:
		return &Creeper{m}
	case endermanSpecies:
		return &Enderman{m}
	case silverfishSpecies:
		return &Silverfish{m}
	case endermiteSpecies:
		return &Endermite{m}
	default:
		return &Slime{m}
	}
}
func (spiderType) BBox(world.Entity) cube.BBox     { return cube.Box(-.7, 0, -.7, .7, .9, .7) }
func (caveSpiderType) BBox(world.Entity) cube.BBox { return cube.Box(-.35, 0, -.35, .35, .45, .35) }
func (creeperType) BBox(world.Entity) cube.BBox    { return cube.Box(-.3, 0, -.3, .3, 1.7, .3) }
func (endermanType) BBox(world.Entity) cube.BBox   { return cube.Box(-.3, 0, -.3, .3, 2.9, .3) }
func (silverfishType) BBox(world.Entity) cube.BBox { return cube.Box(-.2, 0, -.2, .2, .3, .2) }
func (endermiteType) BBox(world.Entity) cube.BBox  { return cube.Box(-.2, 0, -.2, .2, .3, .2) }
func (slimeType) BBox(e world.Entity) cube.BBox {
	size := 2
	if m, ok := e.(*Slime); ok {
		size = m.s().size
	}
	w := float64(size) * .255
	return cube.Box(-w, 0, -w, w, float64(size)*.51, w)
}
func speciesType(k speciesKind) world.EntityType {
	switch k {
	case spiderSpecies:
		return SpiderType
	case caveSpiderSpecies:
		return CaveSpiderType
	case creeperSpecies:
		return CreeperType
	case endermanSpecies:
		return EndermanType
	case silverfishSpecies:
		return SilverfishType
	case endermiteSpecies:
		return EndermiteType
	default:
		return SlimeType
	}
}
func decodeSpecies(k speciesKind, n map[string]any, d *world.EntityData) {
	c := speciesConfig{kind: k, size: 2, fuse: int(nbtconv.Int32(n, "Fuse")), swell: int(nbtconv.Int32(n, "Swell")), charged: nbtconv.Bool(n, "powered"), ignited: nbtconv.Bool(n, "ignited")}
	for _, a := range ReadNativeMobAttributes(n) {
		if a.Name == "minecraft:health" {
			c.health = a.Current
		}
	}
	if c.health <= 0 {
		if _, ok := n["Health"]; ok {
			c.health = float64(nbtconv.Float32(n, "Health"))
		}
	}
	if j := ReadNativeMobJavaState(n); j != nil {
		c.fuse = int(nbtconv.Int32(j, "Fuse"))
		c.swell = int(nbtconv.Int32(j, "Swell"))
		c.attackCooldown = int(nbtconv.Int32(j, "AttackCooldown"))
		if c.kind == slimeSpecies {
			c.size = int(nbtconv.Int32(j, "Size"))
		}
		if b, ok := j["Charged"].(bool); ok {
			c.charged = b
		}
		c.ignited = nbtconv.Bool(j, "Ignited")
		c.climbing = nbtconv.Bool(j, "Climbing")
		if b, ok := j["CarriedBlock"].(map[string]any); ok {
			c.carried = b
			c.carriedSet = true
		}
	}
	if k == slimeSpecies {
		c.size = int(nbtconv.Int32(n, "Size")) + 1
	}
	c.Apply(d)
}
func encodeSpecies(k speciesKind, d *world.EntityData) map[string]any {
	s := d.Data.(*speciesState)
	health := 20.0
	speed := .25
	att := 3.0
	switch k {
	case spiderSpecies:
		health = 16
		speed = .3
		att = 3
	case caveSpiderSpecies:
		health = 12
		speed = .3
		att = 2
	case creeperSpecies:
		health = 20
		speed = .25
		att = 0
	case endermanSpecies:
		health = 40
		speed = .3
		att = 7
	case silverfishSpecies:
		health = 8
		speed = .25
		att = 1
	case endermiteSpecies:
		health = 8
		speed = .25
		att = 2
	case slimeSpecies:
		health = float64(s.size * s.size * 4)
		speed = .2 + .1*float64(s.size)
		att = float64(s.size)
	}
	attrs := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{{Name: "minecraft:health", Base: health, Current: s.health.Health(), DefaultMin: 0, DefaultMax: health, Min: 0, Max: health}, {Name: "minecraft:movement", Base: speed, Current: s.speed, DefaultMin: 0, DefaultMax: 4, Min: 0, Max: 4}, {Name: "minecraft:attack_damage", Base: att, Current: att, DefaultMin: 0, DefaultMax: 2048, Min: 0, Max: 2048}, {Name: "minecraft:follow_range", Base: 16, Current: 16, DefaultMin: 0, DefaultMax: 2048, Min: 0, Max: 2048}})
	m := CloneNativeMobNBT(d.NativeNBT)
	if m == nil {
		m = map[string]any{}
	}
	m["Attributes"] = attrs
	j := map[string]any{"Fuse": int32(s.fuse), "Swell": int32(s.swell), "AttackCooldown": int32(s.cooldown), "Charged": s.charged, "Ignited": s.ignited, "Climbing": s.climbing}
	if s.carried != nil {
		j["CarriedBlock"] = s.carried
	}
	if k == slimeSpecies {
		m["Size"] = int32(s.size - 1)
	}
	return MergeNativeMobJavaState(m, j)
}
func (spiderType) DecodeNBT(n map[string]any, d *world.EntityData) {
	decodeSpecies(spiderSpecies, n, d)
}
func (spiderType) EncodeNBT(d *world.EntityData) map[string]any {
	return encodeSpeciesNative(spiderSpecies, d)
}
func (caveSpiderType) DecodeNBT(n map[string]any, d *world.EntityData) {
	decodeSpecies(caveSpiderSpecies, n, d)
}
func (caveSpiderType) EncodeNBT(d *world.EntityData) map[string]any {
	return encodeSpeciesNative(caveSpiderSpecies, d)
}
func (creeperType) DecodeNBT(n map[string]any, d *world.EntityData) {
	decodeSpecies(creeperSpecies, n, d)
}
func (creeperType) EncodeNBT(d *world.EntityData) map[string]any {
	return encodeSpeciesNative(creeperSpecies, d)
}
func (endermanType) DecodeNBT(n map[string]any, d *world.EntityData) {
	decodeSpecies(endermanSpecies, n, d)
}
func (endermanType) EncodeNBT(d *world.EntityData) map[string]any {
	return encodeSpeciesNative(endermanSpecies, d)
}
func (silverfishType) DecodeNBT(n map[string]any, d *world.EntityData) {
	decodeSpecies(silverfishSpecies, n, d)
}
func (silverfishType) EncodeNBT(d *world.EntityData) map[string]any {
	return encodeSpeciesNative(silverfishSpecies, d)
}
func (endermiteType) DecodeNBT(n map[string]any, d *world.EntityData) {
	decodeSpecies(endermiteSpecies, n, d)
}
func (endermiteType) EncodeNBT(d *world.EntityData) map[string]any {
	return encodeSpeciesNative(endermiteSpecies, d)
}
func (slimeType) DecodeNBT(n map[string]any, d *world.EntityData) { decodeSpecies(slimeSpecies, n, d) }
func (slimeType) EncodeNBT(d *world.EntityData) map[string]any {
	return encodeSpeciesNative(slimeSpecies, d)
}
func (c speciesConfig) Apply(d *world.EntityData) {
	health, maxHealth, speed, attack := 20.0, 20.0, .25, 3.0
	switch c.kind {
	case spiderSpecies:
		health, maxHealth, speed, attack = 16, 16, .3, 3
	case caveSpiderSpecies:
		health, maxHealth, speed, attack = 12, 12, .3, 2
	case creeperSpecies:
		health, maxHealth, speed, attack = 20, 20, .25, 0
	case endermanSpecies:
		health, maxHealth, speed, attack = 40, 40, .3, 7
	case silverfishSpecies:
		health, maxHealth, speed, attack = 8, 8, .25, 1
	case endermiteSpecies:
		health, maxHealth, speed, attack = 8, 8, .25, 2
	case slimeSpecies:
		size := c.size
		if size < 1 {
			size = 2
		}
		health, maxHealth, speed, attack = float64(size*size*4), float64(size*size*4), .2+.1*float64(size), float64(size)
	}
	if c.health > 0 {
		health = c.health
	}
	fuse := c.fuse
	if fuse <= 0 {
		fuse = 30
	}
	size := c.size
	if size < 1 {
		size = 1
	}
	d.NativeNBT = CloneNativeMobNBT(c.native)
	d.Data = &speciesState{kind: c.kind, health: NewHealthManager(health, maxHealth), effects: NewEffectManager(), mc: &MovementComputer{Gravity: .08, Drag: .02, DragBeforeGravity: true}, random: java26.NewLegacyRandom(0), speed: speed, attack: attack, fuse: fuse, swell: c.swell, cooldown: c.attackCooldown, size: size, charged: c.charged, ignited: c.ignited, climbing: c.climbing, loaded: c.loaded, carried: c.carried, core: newSpeciesSlimeCore(c.kind, size), jumpDelay: 10}
}
func NewSpider(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(SpiderType, speciesConfig{kind: spiderSpecies, size: 1})
}
func NewCaveSpider(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(CaveSpiderType, speciesConfig{kind: caveSpiderSpecies, size: 1})
}
func NewCreeper(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(CreeperType, speciesConfig{kind: creeperSpecies, size: 1})
}
func NewEnderman(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(EndermanType, speciesConfig{kind: endermanSpecies, size: 1})
}
func NewSilverfish(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(SilverfishType, speciesConfig{kind: silverfishSpecies, size: 1})
}
func NewEndermite(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(EndermiteType, speciesConfig{kind: endermiteSpecies, size: 1})
}
func NewSlime(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(SlimeType, speciesConfig{kind: slimeSpecies, size: 2})
}
func speciesConfigFromNBT(k speciesKind, n map[string]any) speciesConfig {
	c := speciesConfig{kind: k, size: 2, fuse: int(nbtconv.Int32(n, "Fuse")), swell: int(nbtconv.Int32(n, "Swell")), charged: nbtconv.Bool(n, "powered"), ignited: nbtconv.Bool(n, "ignited"), loaded: true, native: CloneNativeMobNBT(n)}
	for _, a := range ReadNativeMobAttributes(n) {
		if a.Name == "minecraft:health" {
			c.health = a.Current
		}
	}
	if j := ReadNativeMobJavaState(n); j != nil {
		c.fuse = int(nbtconv.Int32(j, "Fuse"))
		c.swell = int(nbtconv.Int32(j, "Swell"))
		c.attackCooldown = int(nbtconv.Int32(j, "AttackCooldown"))
		c.charged = nbtconv.Bool(j, "Charged")
		c.ignited = nbtconv.Bool(j, "Ignited")
		c.climbing = nbtconv.Bool(j, "Climbing")
		c.carried, _ = j["CarriedBlock"].(map[string]any)
	}
	if k == slimeSpecies {
		c.size = int(nbtconv.Int32(n, "Size")) + 1
	}
	return c
}
func (m *speciesMob) s() *speciesState { return m.Behaviour().(*speciesState) }
func (m *speciesMob) Tick(tx *world.Tx, n int64) {
	m.s().effects.Tick(m, tx)
	m.Ent.Tick(tx, n)
	m.updateJockey(tx)
}
func (s *speciesState) Tick(e *Ent, tx *world.Tx) *Movement {
	m := &speciesMob{Ent: e, kind: s.kind}
	target, dist := nearestSpeciesTarget(e, tx, s.kind)
	switch s.kind {
	case creeperSpecies:
		if s.ignited || target != nil && dist < 3 {
			if s.swell == 0 {
				tx.PlaySound(e.Position(), sound.Custom{Name: "entity.creeper.primed", Volume: 1, Pitch: 1})
			}
			s.swell++
			if s.swell >= s.fuse {
				power := 3.0
				if s.charged {
					power = 6
				}
				(block.ExplosionConfig{SpawnFire: s.charged, NoBlockDamage: mobGriefingDisabled(tx)}).Explode(tx, world.NewEntityExplosionSource(m, power))
				m.kill()
				return nil
			}
		} else {
			s.swell = max(0, s.swell-1)
		}
	case endermanSpecies:
		if target != nil {
			if speciesEndermanGazed(e, target) {
				if s.random.NextInt(4) == 0 {
					m.teleportRandom(tx)
				}
			} else {
				target = nil
			}
		}
		if _, ok := tx.Liquid(cube.PosFromVec3(e.Position())); ok {
			m.teleportRandom(tx)
			m.Hurt(1, DrowningDamageSource{})
		}
	}
	if s.cooldown > 0 {
		s.cooldown--
	}
	if target != nil {
		attackRange := 1.6
		if s.kind == slimeSpecies {
			attackRange = float64(s.size) + .5
		}
		if dist < attackRange {
			if s.cooldown == 0 {
				if l, ok := target.(Living); ok {
					d := s.attack
					if s.kind == caveSpiderSpecies {
						difficulty := tx.LocalDifficulty(cube.PosFromVec3(e.Position())).Base
						if difficulty == world.DifficultyNormal || difficulty == world.DifficultyHard {
							l.AddEffect(effect.New(effect.Poison, 0, time.Second*7))
						}
					}
					l.Hurt(d, AttackDamageSource{Attacker: m})
				}
				s.cooldown = 20
			}
			return speciesMove(s, e, tx, e.Position())
		}
		return speciesMove(s, e, tx, target.Position())
	}
	if s.kind == slimeSpecies {
		return m.slimeTick(tx)
	}
	if s.kind == spiderSpecies || s.kind == caveSpiderSpecies {
		s.climbing = e.data.Vel[0] != 0 || e.data.Vel[2] != 0
	}
	return m.wander()
}
func nearestSpeciesTarget(e *Ent, tx *world.Tx, k speciesKind) (world.Entity, float64) {
	if (k == spiderSpecies || k == caveSpiderSpecies) && tx.SkyLight(cube.PosFromVec3(e.Position())) >= 8 {
		return nil, math.MaxFloat64
	}
	r := 16.0
	if k == endermanSpecies {
		r = 64
	}
	area := cube.Box(-r, -8, -r, r, 8, r).Translate(e.Position())
	var chosen world.Entity
	best := r
	for other := range tx.EntitiesWithin(area) {
		if other == e {
			continue
		}
		id := other.H().Type().EncodeEntity()
		ok := id == "minecraft:player" || id == "minecraft:iron_golem"
		if k == endermanSpecies && id != "minecraft:player" {
			ok = false
		}
		if (k == endermanSpecies || k == endermiteSpecies) && id == "minecraft:endermite" {
			ok = false
		}
		if !ok {
			continue
		}
		l, ok := other.(Living)
		if !ok || l.Dead() {
			continue
		}
		d := other.Position().Sub(e.Position()).Len()
		if d < best {
			chosen, best = other, d
		}
	}
	return chosen, best
}
func speciesEndermanGazed(e *Ent, target world.Entity) bool {
	d := e.Position().Sub(target.Position())
	if d.Len() == 0 {
		return true
	}
	yaw := target.Rotation()[0] * math.Pi / 180
	look := mgl64.Vec3{-math.Sin(yaw), 0, math.Cos(yaw)}
	return look.Dot(d.Normalize()) > .95 && d.Len() < 64
}
func speciesMove(s *speciesState, e *Ent, tx *world.Tx, p mgl64.Vec3) *Movement {
	d := p.Sub(e.Position())
	d[1] = 0
	if d.Len() > 0 {
		d = d.Normalize()
	}
	v := e.Velocity()
	v[0], v[2] = d[0]*s.speed, d[2]*s.speed
	r := e.Rotation()
	if d.Len() > 0 {
		r[0] = math.Atan2(-d[0], d[2]) * 180 / math.Pi
	}
	if s.kind == spiderSpecies || s.kind == caveSpiderSpecies {
		if d.Len() > 0 && s.mc.OnGround() {
			v[1] = .32
		}
	}
	m := s.mc.TickMovement(e, e.Position(), v, e.Rotation(), r, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	return m
}
func (m *speciesMob) wander() *Movement {
	s := m.s()
	if s.kind == silverfishSpecies && s.random.NextInt(100) == 0 && m.burrowInfested() {
		return nil
	}
	if s.wander > 0 {
		s.wander--
	}
	if s.navigator.Index >= len(s.navigator.Path) && s.wander == 0 {
		s.wander = 40
		start := cube.PosFromVec3(m.Position())
		goal := start.Add(cube.Pos{int(s.random.NextInt(11)) - 5, 0, int(s.random.NextInt(11)) - 5})
		s.navigator.MoveTo(m.tx, start, goal, GroundProfile{MaxVisitedNodes: 80}, navigatorTick(m.Ent))
	}
	if p, ok := s.navigator.Tick(m.Position(), navigatorTick(m.Ent), .9); ok {
		if p.Sub(m.Position()).Len() < .4 {
			s.navigator.Index++
		} else {
			return speciesMove(s, m.Ent, m.tx, p)
		}
	}
	return speciesMove(s, m.Ent, m.tx, m.Position())
}
func (m *speciesMob) burrowInfested() bool {
	origin := cube.PosFromVec3(m.Position())
	for _, offset := range []cube.Pos{{1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}, {0, 1, 0}, {0, -1, 0}} {
		pos := origin.Add(offset)
		var infested world.Block
		switch b := m.tx.Block(pos).(type) {
		case block.Stone:
			infested = block.InfestedStone{}
		case block.Cobblestone:
			infested = block.InfestedCobblestone{}
		case block.StoneBricks:
			infested = block.InfestedStoneBricks{Type: b.Type}
		case block.Deepslate:
			infested = block.InfestedDeepslate{Axis: b.Axis}
		}
		if infested != nil {
			m.tx.SetBlock(pos, infested, nil)
			m.kill()
			return true
		}
	}
	return false
}
func (m *speciesMob) slimeTick(tx *world.Tx) *Movement {
	s := m.s()
	if s.core != nil && s.core.Jump(&s.random, s.mc.OnGround()) {
		s.jumpDelay = s.core.JumpDelay
		v := m.Velocity()
		v[1] = .42 + .08*float64(s.size)
		m.SetVelocity(v)
		if s.size > 1 {
			tx.PlaySound(m.Position(), sound.Custom{Name: "entity.slime.jump", Volume: 1, Pitch: 1})
		}
	}
	for e := range tx.EntitiesWithin(cube.Box(-float64(s.size), 0, -float64(s.size), float64(s.size), 1, float64(s.size)).Translate(m.Position())) {
		if e == m {
			continue
		}
		if l, ok := e.(Living); ok && !l.Dead() && e.H().Type().EncodeEntity() == "minecraft:player" {
			l.Hurt(float64(s.size), AttackDamageSource{Attacker: m})
		}
	}
	return speciesMove(s, m.Ent, tx, m.Position())
}
func (m *speciesMob) teleportRandom(tx *world.Tx) bool {
	s := m.s()
	origin := cube.PosFromVec3(m.Position())
	for range 16 {
		p := origin.Add(cube.Pos{int(s.random.NextInt(17)) - 8, int(s.random.NextInt(17)) - 8, int(s.random.NextInt(17)) - 8})
		if len(tx.Block(p).Model().BBox(p, tx)) != 0 || len(tx.Block(p.Side(cube.FaceUp)).Model().BBox(p.Side(cube.FaceUp), tx)) != 0 {
			continue
		}
		old := m.Position()
		m.data.Pos = p.Vec3().Add(mgl64.Vec3{.5, 0, .5})
		if m.data.Pos == old {
			continue
		}
		tx.PlaySound(old, sound.Custom{Name: "entity.enderman.teleport", Volume: 1, Pitch: 1})
		tx.PlaySound(m.Position(), sound.Custom{Name: "entity.enderman.teleport", Volume: 1, Pitch: 1})
		return true
	}
	return false
}
func (m *speciesMob) wakeInfested(tx *world.Tx) {
	p := cube.PosFromVec3(m.Position())
	for _, d := range []cube.Pos{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
		q := p.Add(d)
		switch tx.Block(q).(type) {
		case block.InfestedStone, block.InfestedCobblestone, block.InfestedStoneBricks, block.InfestedDeepslate:
			tx.SetBlock(q, nil, nil)
			o := world.EntitySpawnOpts{Position: q.Vec3().Add(mgl64.Vec3{.5, 0, .5})}
			tx.AddEntity(NewSilverfish(o))
		}
	}
}
func (m *speciesMob) kill() { s := m.s(); s.dead = true; m.view(DeathAction{}); _ = m.CloseIn(m.tx) }
func (m *speciesMob) view(a world.EntityAction) {
	v := m.tx.Viewers(m.Position())
	for _, x := range v {
		x.ViewEntityAction(m, a)
	}
	m.tx.ReleaseViewers(v)
}
func (m *speciesMob) Health() float64        { return m.s().health.Health() }
func (m *speciesMob) MaxHealth() float64     { return m.s().health.MaxHealth() }
func (m *speciesMob) SetMaxHealth(v float64) { m.s().health.SetMaxHealth(v) }
func (m *speciesMob) Dead() bool             { return m.s().dead || m.Health() <= 0 }
func (m *speciesMob) Hurt(d float64, src world.DamageSource) (float64, bool) {
	s := m.s()
	if m.Dead() || d <= 0 {
		return 0, false
	}
	switch src.(type) {
	case LightningDamageSource:
		if s.kind == creeperSpecies {
			s.charged = true
		}
	case ProjectileDamageSource:
		if s.kind == endermanSpecies {
			m.teleportRandom(m.tx)
			return 0, false
		}
	}
	if s.kind == silverfishSpecies {
		m.wakeInfested(m.tx)
	}
	s.health.AddHealth(-d)
	m.view(HurtAction{})
	if m.Dead() {
		if s.kind == slimeSpecies && s.size > 1 {
			for i := range 2 {
				off := float64(i)*.4 - .2
				opts := world.EntitySpawnOpts{Position: m.Position().Add(mgl64.Vec3{off, .5, 0})}
				m.tx.AddEntity(opts.New(SlimeType, speciesConfig{kind: slimeSpecies, size: s.core.ChildSize()}))
			}
		}
		m.kill()
		m.dropSpecies(src)
	}
	return d, true
}
func (m *speciesMob) dropSpecies(src world.DamageSource) {
	id := speciesType(m.kind).EncodeEntity()
	table := "minecraft:entities/" + id[len("minecraft:"):]
	sequence, err := loot.RandomSequence(table)
	if err != nil {
		return
	}
	ctx := loot.Context{Entity: id, Origin: m.Position(), Random: m.tx.LootRandomSequence(sequence, 0)}
	if killer := mobKiller(src); killer != nil {
		ctx.Player = killer
		ctx.PlayerKilled = killer.H().Type().EncodeEntity() == "minecraft:player"
		if user, ok := killer.(interface {
			HeldItems() (item.Stack, item.Stack)
		}); ok {
			ctx.ToolStack, _ = user.HeldItems()
			if !ctx.ToolStack.Empty() {
				ctx.Tool = ctx.ToolStack.Item()
			}
		}
	}
	drops, err := loot.Generate(table, 0, ctx)
	if err != nil {
		return
	}
	for _, stack := range drops {
		m.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: m.Position()}, stack))
	}
}
func (m *speciesMob) Heal(h float64, _ world.HealingSource) float64 {
	if h <= 0 || m.Dead() {
		return 0
	}
	old := m.Health()
	m.s().health.AddHealth(h)
	return m.Health() - old
}
func (m *speciesMob) KnockBack(src mgl64.Vec3, f, h float64) {
	d := m.Position().Sub(src)
	d[1] = 0
	if d.Len() > 0 {
		d = d.Normalize().Mul(f)
	}
	d[1] = h
	m.SetVelocity(d)
}
func (m *speciesMob) AddEffect(e effect.Effect) {
	if m.kind == spiderSpecies && (e.Type() == effect.Poison || e.Type() == effect.FatalPoison) {
		return
	}
	m.s().effects.Add(e, m)
}
func (m *speciesMob) RemoveEffect(t effect.Type) { m.s().effects.Remove(t, m) }
func (m *speciesMob) Effects() []effect.Effect   { return m.s().effects.Effects() }
func (m *speciesMob) Speed() float64             { return m.s().speed }
func (m *speciesMob) SetSpeed(v float64)         { m.s().speed = v }
func (m *speciesMob) Interact(_ *world.Tx, user item.User, ctx *item.UseContext) bool {
	if m.kind != creeperSpecies || m.s().ignited {
		return false
	}
	main, _ := user.HeldItems()
	switch main.Item().(type) {
	case item.FlintAndSteel:
		m.s().ignited = true
		ctx.DamageItem(1)
		m.tx.PlaySound(m.Position(), sound.Custom{Name: "item.flintandsteel.use", Volume: 1, Pitch: 1})
		return true
	case item.FireCharge:
		m.s().ignited = true
		ctx.SubtractFromCount(1)
		m.tx.PlaySound(m.Position(), sound.Custom{Name: "item.firecharge.use", Volume: 1, Pitch: 1})
		return true
	}
	return false
}
func (m *speciesMob) FinalizeSpawn(reason string, r *java26.Random) {
	s := m.s()
	if r == nil || s.loaded {
		return
	}
	if s.kind == creeperSpecies {
		s.charged = false
	}
	if s.kind == spiderSpecies && r.NextInt(100) == 0 && m.tx != nil {
		opts := world.EntitySpawnOpts{Position: m.Position(), Rotation: m.Rotation(), SpawnReason: "jockey"}
		passenger := m.tx.AddEntity(NewSkeleton(opts))
		if passenger != nil {
			m.Mount(m.tx, passenger)
		}
	}
	if s.kind == slimeSpecies {
		sizes := [3]int{1, 2, 4}
		s.size = sizes[r.NextInt(3)]
		maximum := float64(s.size * s.size * 4)
		s.health.SetMaxHealth(maximum)
		s.health.AddHealth(maximum)
		s.core.Size = s.size
	}
}
func (m *speciesMob) SpeciesType() world.EntityType { return speciesType(m.kind) }

var _ world.EntityType = spiderType{}
var _ world.EntityType = caveSpiderType{}
var _ world.EntityType = creeperType{}
var _ world.EntityType = endermanType{}
var _ world.EntityType = silverfishType{}
var _ world.EntityType = endermiteType{}
var _ world.EntityType = slimeType{}
var _ Living = (*speciesMob)(nil)
