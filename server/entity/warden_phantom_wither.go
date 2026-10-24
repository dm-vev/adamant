package entity

import (
	"fmt"
	"hash/fnv"
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/player/bossbar"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

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
		// WitherSkullBlock.checkSpawn: the wither appears at the bottom of the
		// pattern summoning, with a third of its health.
		yaw := 90.0
		if right[0] == 0 {
			yaw = 0
		}
		opts := world.EntitySpawnOpts{Position: base.Vec3Middle().Add(mgl64.Vec3{0, .55}), Rotation: cube.Rotation{yaw, 0}, SpawnReason: "construction"}
		tx.AddEntity(opts.New(WitherType, witherConfig{invulnerable: 220, health: 100}))
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
		s.random = vanilla.NewLegacyRandom(int64(hash.Sum64()))
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
	d.Data = &phantomState{health: NewHealthManager(health, maxHealth), effects: NewEffectManager(), speed: phantomBaseMovementSpeed, mc: &MovementComputer{Gravity: 0, Drag: .02, DragBeforeGravity: true}, random: vanilla.NewLegacyRandom(0), size: size, attack: c.attack, target: c.target}
}

type phantomState struct {
	health       *HealthManager
	effects      *EffectManager
	speed        float64
	mc           *MovementComputer
	random       vanilla.Random
	size, attack int
	target       int64
	anchor       mgl64.Vec3
	anchorSet    bool
	dead         bool

	ai         MobAI
	phase      phantomPhase
	moveTarget mgl64.Vec3
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
func (p *Phantom) FinalizeSpawn(_ string, _ *vanilla.Random) {
	s := p.state()
	s.anchor = cube.PosFromVec3(p.Position()).Add(cube.Pos{0, 5, 0}).Vec3()
	s.anchorSet = true
}
func (s *phantomState) Tick(e *Ent, tx *world.Tx) *Movement { return s.aiTick(e, tx) }
func (p *Phantom) Hurt(d float64, src world.DamageSource) (float64, bool) {
	if p.Dead() || d <= 0 {
		return 0, false
	}
	s := p.state()
	d, ok := s.ai.DamageCooldown(p.tx, d)
	if !ok {
		return 0, false
	}
	s.health.AddHealth(-d)
	s.ai.Hurt(src)
	p.viewAction(HurtAction{})
	onMobHurt(p.tx, p)
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
		s.random = vanilla.NewLegacyRandom(int64(hash.Sum64()))
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
	return encodeBossNBT(d, attrs, map[string]any{"Invulnerability": int32(s.invulnerable), "Charge": int32(s.charge), "Death": int32(s.death)})
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
	d.Data = &witherState{health: NewHealthManager(h, 300), effects: NewEffectManager(), speed: witherBaseMovementSpeed, invulnerable: max(0, c.invulnerable), charge: max(0, c.charge), death: max(0, c.death), bar: bossbar.New("Wither"), random: vanilla.NewLegacyRandom(0), viewers: make(map[*world.EntityHandle]bool)}
}

type witherState struct {
	health                      *HealthManager
	effects                     *EffectManager
	ai                          MobAI
	speed                       float64
	random                      vanilla.Random
	invulnerable, charge, death int
	// heads are the targets of the middle and side heads, the alternative
	// targets of WitherBoss.
	heads                           [3]*world.EntityHandle
	nextHeadUpdate, idleHeadUpdates [2]int
	destroyBlocksTick               int
	dead, barShown                  bool
	bar                             bossbar.BossBar
	viewers                         map[*world.EntityHandle]bool
}
type Wither struct{ *Ent }

func NewWither(o world.EntitySpawnOpts) *world.EntityHandle { return o.New(WitherType, witherConfig{}) }
func (w *Wither) state() *witherState                       { return w.Behaviour().(*witherState) }
func (w *Wither) Tick(tx *world.Tx, n int64)                { w.state().effects.Tick(w, tx); w.Ent.Tick(tx, n) }
func (w *Wither) updateBossBar() {
	s := w.state()
	progress := w.Health() / w.MaxHealth()
	if s.invulnerable > 0 {
		// The bar fills up while the wither is summoned.
		progress = 1 - float64(s.invulnerable)/220
	}
	bar := s.bar.WithHealthPercentage(max(0, min(1, progress)))
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
func (w *Wither) Health() float64        { return w.state().health.Health() }
func (w *Wither) MaxHealth() float64     { return w.state().health.MaxHealth() }
func (w *Wither) SetMaxHealth(v float64) { w.state().health.SetMaxHealth(v) }
func (w *Wither) Dead() bool             { return w.state().dead || w.state().death >= 220 }

// Hurt ports WitherBoss.hurtServer: drowning, other withers and its undead
// friends never hurt the wither, nor anything while it is summoned but the
// void, nor arrows and wind charges once it is powered. A hit makes it break
// the blocks around it a second later and its idle heads restless.
func (w *Wither) Hurt(d float64, src world.DamageSource) (float64, bool) {
	s := w.state()
	if w.Dead() || d <= 0 {
		return 0, false
	}
	if _, drown := src.(DrowningDamageSource); drown {
		return 0, false
	}
	killer := mobKiller(src)
	if killer != nil && (killer.H().Type() == WitherType || witherFriend(killer)) {
		return 0, false
	}
	if _, void := src.(VoidDamageSource); s.invulnerable > 0 && !void {
		return 0, false
	}
	if w.Powered() {
		if p, ok := src.(ProjectileDamageSource); ok && p.Projectile != nil {
			switch p.Projectile.H().Type().EncodeEntity() {
			case "minecraft:arrow", "minecraft:wind_charge_projectile", "minecraft:trident", "minecraft:spectral_arrow":
				return 0, false
			}
		}
		if _, ok := src.(WindChargeDamageSource); ok {
			return 0, false
		}
	}
	if s.destroyBlocksTick <= 0 {
		s.destroyBlocksTick = 20
	}
	for i := range s.idleHeadUpdates {
		s.idleHeadUpdates[i] += 3
	}
	d, ok := s.ai.DamageCooldown(w.tx, d)
	if !ok {
		return 0, false
	}
	if src.ReducedByArmour() {
		// The 4 points of armour of the wither, as CombatRules.getDamageAfterAbsorb.
		d *= 1 - math.Min(20, math.Max(4.0/5, 4-d/2))/25
	}
	s.health.AddHealth(-d)
	s.ai.Hurt(src)
	w.updateBossBar()
	w.viewAction(HurtAction{})
	onMobHurt(w.tx, w)
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

// AddEffect does nothing: WitherBoss.addEffect refuses every effect.
func (w *Wither) AddEffect(effect.Effect)    {}
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
	case SonicBoomDamageSource:
		return s.Attacker
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
