package entity

import (
	"hash/fnv"
	"math"

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
	carried                                world.Block
	angerTicks                             int
	angerTarget                            uuid.UUID
	effects                                []effect.Effect
	native                                 map[string]any
}
type speciesState struct {
	kind                                     speciesKind
	health                                   *HealthManager
	effects                                  *EffectManager
	mc                                       *MovementComputer
	random                                   vanilla.Random
	speed, attack                            float64
	fuse, swell, cooldown, jumpDelay, size   int
	charged, ignited, climbing, dead, loaded bool
	carried                                  world.Block
	passenger                                *world.EntityHandle
	ai                                       MobAI
	swellDir                                 int
	friends                                  *silverfishWakeUpFriendsGoal
	cubeAI                                   cubeAI

	// The anger of an enderman as a NeutralMob, and the state it screams
	// and stares in.
	neutralAnger
	creepy, staredAt                 bool
	targetChangeTime, lastStareSound int
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
		s.random = vanilla.NewLegacyRandom(int64(hash.Sum64()))
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
		c.carried = decodeCarriedBlock(j["CarriedBlock"], "Name", "Properties")
		c.angerTicks = int(nbtconv.Int32(j, "AngerTime"))
		c.angerTarget, _ = uuid.Parse(golemUUIDString(j["AngryAt"]))
	}
	if b := decodeCarriedBlock(n["carriedBlock"], "name", "states"); b != nil {
		c.carried = b
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
		att = 2
	case caveSpiderSpecies:
		health = 12
		speed = .3
		att = 2
	case creeperSpecies:
		health = 20
		speed = .25
		att = 2
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
		health = float64(s.size * s.size)
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
	if k == endermanSpecies {
		j["AngerTime"] = int32(s.angerTicks)
		if s.angerTarget != uuid.Nil {
			j["AngryAt"] = golemUUIDNBT(s.angerTarget.String())
		}
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
		health, maxHealth, speed, attack = 16, 16, .3, 2
	case caveSpiderSpecies:
		health, maxHealth, speed, attack = 12, 12, .3, 2
	case creeperSpecies:
		health, maxHealth, speed, attack = 20, 20, .25, 2
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
		health, maxHealth, speed, attack = float64(size*size), float64(size*size), .2+.1*float64(size), float64(size)
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
	d.Data = &speciesState{kind: c.kind, health: NewHealthManager(health, maxHealth), effects: NewEffectManager(), mc: &MovementComputer{Gravity: .08, Drag: .02, DragBeforeGravity: true}, random: vanilla.NewLegacyRandom(0), speed: speed, attack: attack, fuse: fuse, swell: c.swell, cooldown: c.attackCooldown, size: size, charged: c.charged, ignited: c.ignited, climbing: c.climbing, loaded: c.loaded, carried: c.carried, neutralAnger: neutralAnger{angerTicks: c.angerTicks, angerTarget: c.angerTarget}, lastStareSound: math.MinInt32}
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

// NewSlimeWithSize returns a slime of a size: 1, 2 or 4 when it spawns
// naturally.
func NewSlimeWithSize(o world.EntitySpawnOpts, size int) *world.EntityHandle {
	return o.New(SlimeType, speciesConfig{kind: slimeSpecies, size: size})
}
func (m *speciesMob) s() *speciesState { return m.Behaviour().(*speciesState) }

// Size returns the size of the slime: 1, 2 or 4 when it spawned naturally.
func (m *Slime) Size() int { return m.s().size }
func (m *speciesMob) Tick(tx *world.Tx, n int64) {
	m.s().effects.Tick(m, tx)
	m.Ent.Tick(tx, n)
	m.updateJockey(tx)
}
func (s *speciesState) Tick(e *Ent, tx *world.Tx) *Movement { return s.aiTick(e, tx) }
func (m *speciesMob) kill()                                 { s := m.s(); s.dead = true; m.view(DeathAction{}); _ = m.CloseIn(m.tx) }

// CubeSize returns the Java Slime.getSize used by the cube_mob loot predicate.
func (m *speciesMob) CubeSize() int { return m.s().size }
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

// IsSensitiveToWater reports whether water hurts the mob, as it hurts
// endermen.
func (m *speciesMob) IsSensitiveToWater() bool { return m.s().kind == endermanSpecies }
func (m *speciesMob) Hurt(d float64, src world.DamageSource) (float64, bool) {
	s := m.s()
	if m.Dead() || d <= 0 {
		return 0, false
	}
	if _, ok := src.(LightningDamageSource); ok && s.kind == creeperSpecies {
		s.charged = true
	}
	if s.kind == endermanSpecies {
		after, ok := m.endermanHurt(src)
		if after != nil {
			defer after()
		}
		if !ok {
			return 0, false
		}
	}
	if s.kind == silverfishSpecies && (damageAttacker(src) != nil || silverfishAlwaysTriggered(src)) {
		// Java registers the goal when the silverfish is made, so it
		// hears of damage taken before its first tick.
		if s.friends == nil {
			s.friends = &silverfishWakeUpFriendsGoal{}
		}
		s.friends.notifyHurt()
	}
	d, ok := s.ai.DamageCooldown(m.tx, d)
	if !ok {
		return 0, false
	}
	s.health.AddHealth(-d)
	s.ai.Hurt(src)
	m.view(HurtAction{})
	onMobHurt(m.tx, m)
	if m.Dead() {
		if s.kind == slimeSpecies {
			splitCube(s.ai.Mob(m.Ent, speciesSelf(m.Ent, s.kind)), s.size, NewSlimeWithSize)
		}
		m.kill()
		dropAllDeathLoot(m.tx, m, src)
	}
	return d, true
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
	knockBack(m, src, f, h)
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
func (m *speciesMob) FinalizeSpawn(reason string, r *vanilla.Random) {
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
	if s.kind == slimeSpecies && m.tx != nil {
		// AbstractCubeMob.setSpawnSize: larger slimes are likelier on
		// harder local difficulties.
		scale := r.NextInt(3)
		if scale < 2 && r.NextFloat() < .5*m.tx.LocalDifficulty(cube.PosFromVec3(m.Position())).SpecialMultiplier() {
			scale++
		}
		s.size = 1 << scale
		maximum := float64(s.size * s.size)
		s.health.SetMaxHealth(maximum)
		s.health.AddHealth(maximum)
		s.speed = .2 + .1*float64(s.size)
		s.attack = float64(s.size)
		m.updateState()
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
