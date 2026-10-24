package entity

import (
	"math"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// NewShulker creates a worldless shulker entity handle.
func NewShulker(opts world.EntitySpawnOpts, conf ShulkerBehaviourConfig) *world.EntityHandle {
	return opts.New(ShulkerType, conf)
}

func shulkerFace(v any) cube.Face {
	switch value := v.(type) {
	case string:
		switch strings.ToLower(value) {
		case "down":
			return cube.FaceDown
		case "up":
			return cube.FaceUp
		case "north":
			return cube.FaceNorth
		case "south":
			return cube.FaceSouth
		case "west":
			return cube.FaceWest
		case "east":
			return cube.FaceEast
		}
	default:
		if value, ok := nativeNBTInt(v); ok && value >= 0 && value <= 5 {
			return cube.Face(value)
		}
	}
	return cube.FaceDown
}

// ShulkerType is the entity type for Java shulkers.
var ShulkerType shulkerType

type shulkerType struct{}

func (shulkerType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	if data.Data == nil {
		ShulkerBehaviourConfig{}.Apply(data)
	}
	return &Shulker{Ent: &Ent{tx: tx, handle: handle, data: data}}
}
func (shulkerType) EncodeEntity() string { return "minecraft:shulker" }

// BBox ports Shulker.makeBoundingBox: the box of a shulker grows out of the
// face it is attached to as its shell opens.
func (shulkerType) BBox(entity world.Entity) cube.BBox {
	shulker, ok := entity.(*Shulker)
	if !ok {
		return cube.Box(-0.5, 0, -0.5, 0.5, 1, 0.5)
	}
	b := shulker.behaviour()
	return shulkerProgressBox(b.attachFace.Opposite(), -1, shulkerPhysicalPeek(b.peekAmount))
}

// shulkerPhysicalPeek ports Shulker.getPhysicalPeek.
func shulkerPhysicalPeek(amount float32) float64 {
	return float64(.5 - float32(math.Sin(float64((.5+amount)*math.Pi)))*.5)
}

// shulkerProgressBox ports Shulker.getProgressDeltaAabb for a shulker of
// size one at the origin.
func shulkerProgressBox(dir cube.Face, from, to float64) cube.BBox {
	step := cube.Pos{}.Side(dir).Vec3()
	maxM, minM := math.Max(from, to), math.Min(from, to)
	lo, hi := mgl64.Vec3{-.5, 0, -.5}, mgl64.Vec3{.5, 1, .5}
	for i := range 3 {
		// AABB.expandTowards.
		if d := step[i] * maxM; d < 0 {
			lo[i] += d
		} else {
			hi[i] += d
		}
		// AABB.contract by -step*(1+min).
		if d := -step[i] * (1 + minM); d < 0 {
			lo[i] -= d
		} else {
			hi[i] -= d
		}
	}
	return cube.Box(lo[0], lo[1], lo[2], hi[0], hi[1], hi[2])
}

func (shulkerType) DecodeNBT(m map[string]any, data *world.EntityData) {
	conf := ShulkerBehaviourConfig{}
	javaState := ReadNativeMobJavaState(m)
	source := m
	if javaState != nil {
		source = javaState
	}
	if value, ok := source["AttachFace"]; ok {
		conf.AttachFace = shulkerFace(value)
	}
	if value, ok := source["Peek"]; ok {
		conf.Peek = nativeNBTByte(value)
	} else if value, ok := m["Peek"]; ok {
		conf.Peek = nativeNBTByte(value)
	}
	// Bedrock stores the shell colour in Variant, not the generic Color tag.
	if variant, ok := NativeMobVariant(m); ok && variant >= 0 && variant <= 16 {
		conf.Color, conf.colorSet = uint8(variant), true
	} else if value, ok := source["Color"]; ok {
		conf.Color, conf.colorSet = shulkerColourSwap(nativeNBTByte(value)), true
	}
	if _, ok := source["Health"]; ok {
		conf.Health = float64(nbtconv.Float32(source, "Health"))
		conf.healthSet = true
	} else if health, ok := nativeMobAttribute(m, "minecraft:health"); ok {
		conf.Health, conf.healthSet = health.Current, true
	}
	if _, ok := source["MaxHealth"]; ok {
		conf.MaxHealth = float64(nbtconv.Float32(source, "MaxHealth"))
	} else if health, ok := nativeMobAttribute(m, "minecraft:health"); ok {
		conf.MaxHealth = health.Base
	}
	data.Data = conf.New()
}
func (shulkerType) EncodeNBT(data *world.EntityData) map[string]any {
	b := data.Data.(*ShulkerBehaviour)
	attributes := EncodeNativeMobAttributes(ReadNativeMobAttributes(data.NativeNBT), []NativeMobAttribute{{
		Name: "minecraft:health", Base: b.health.MaxHealth(), Current: b.health.Health(),
		DefaultMin: 0, DefaultMax: b.health.MaxHealth(), Min: 0, Max: b.health.MaxHealth(),
	}})
	native := map[string]any{"Attributes": attributes, "Variant": int32(b.color)}
	javaState := MergeNativeMobNBT(ReadNativeMobJavaState(data.NativeNBT), map[string]any{
		"AttachFace": b.attachFace.String(), "Peek": b.peek, "Color": shulkerColourSwap(b.color),
	})
	return MergeNativeMobJavaState(native, javaState)
}

// ShulkerBehaviourConfig configures shulker health and persisted shell state.
type ShulkerBehaviourConfig struct {
	Health, MaxHealth   float64
	AttachFace          cube.Face
	Peek, Color         uint8
	healthSet, colorSet bool
}

func (conf ShulkerBehaviourConfig) Apply(data *world.EntityData) { data.Data = conf.New() }

// shulkerColourSwap converts between the Java dye colour of a shulker and
// its Bedrock variant, which number the colours in reverse. Both use 16 for
// an undyed shulker.
func shulkerColourSwap(c uint8) uint8 {
	if c > 15 {
		return 16
	}
	return 15 - c
}

func (conf ShulkerBehaviourConfig) New() *ShulkerBehaviour {
	if conf.MaxHealth <= 0 {
		conf.MaxHealth = 30
	}
	if !conf.healthSet && conf.Health <= 0 {
		conf.Health = conf.MaxHealth
	}
	if conf.AttachFace > cube.FaceEast {
		conf.AttachFace = cube.FaceDown
	}
	if !conf.colorSet && conf.Color == 0 {
		conf.Color = 16
	}
	return &ShulkerBehaviour{health: NewHealthManager(conf.Health, conf.MaxHealth), effects: NewEffectManager(), attachFace: conf.AttachFace, peek: conf.Peek, peekAmount: float32(conf.Peek) * .01, color: conf.Color}
}

// Shulker is an immobile hostile living entity.
type Shulker struct{ *Ent }
type ShulkerBehaviour struct {
	health     *HealthManager
	effects    *EffectManager
	ai         MobAI
	attachFace cube.Face
	// peek is the opening of the shell viewers see, Shulker.DATA_PEEK_ID,
	// and peekAmount how far it opened, Shulker.currentPeekAmount.
	peek       uint8
	peekAmount float32
	color      uint8
	dead       bool
}

func (s *Shulker) behaviour() *ShulkerBehaviour { return s.Behaviour().(*ShulkerBehaviour) }

// Tick ports Shulker.tick: a shulker that may no longer stay where it is
// finds another face or teleports, and its shell opens and closes a twentieth
// at a time.
func (s *Shulker) Tick(tx *world.Tx, current int64) {
	b := s.behaviour()
	face, peek, pos := b.attachFace, b.peek, s.Position()
	b.effects.Tick(s, tx)
	s.Ent.Tick(tx, current)
	if b.dead || s.tx == nil {
		return
	}
	if s.data.Riding == nil && !b.canStayAt(tx, s.H(), cube.PosFromVec3(s.Position()), b.attachFace) {
		b.findNewAttachment(s.Ent, tx)
	}
	if b.updatePeekAmount() {
		b.onPeekAmountChange(s.Ent, tx)
	}
	if b.attachFace != face || b.peek != peek || s.Position() != pos {
		s.updateState()
	}
}

func (b *ShulkerBehaviour) mobAI() *MobAI { return &b.ai }

// Tick runs the MobAI of the shulker, which never moves on its own.
func (b *ShulkerBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	s := &Shulker{Ent: e}
	// Shulker.getMaxHeadXRot and getMaxHeadYRot.
	b.ai.MaxHeadYaw, b.ai.MaxHeadPitch, b.ai.EyeHeight = 180, 180, .5
	return b.ai.Tick(e, s, func(m *Mob) {
		ai := m.AI
		ai.lookOverride, ai.bodyOverride = shulkerLookControl{b}, shulkerBodyControl{}
		look := NewLookAtPlayerGoal(8)
		look.OnlyHorizontal = true
		ai.Goals.Add(1, look)
		ai.Goals.Add(4, &shulkerAttackGoal{goalFlags: goalFlags{GoalMove | GoalLook}, b: b})
		ai.Goals.Add(7, &shulkerPeekGoal{b: b})
		ai.Goals.Add(8, NewRandomLookAroundGoal())
		hurt := NewHurtByTargetGoal()
		hurt.IgnoreDamageFrom = func(e world.Entity) bool { _, shulker := e.(*Shulker); return shulker }
		hurt.AlertOthers = true
		ai.Targets.Add(1, hurt)
		nearest := NewNearestAttackableTargetGoal(nil, true)
		nearest.Selector = func(m *Mob, _ world.Entity) bool { return m.tx.World().Difficulty() != world.DifficultyPeaceful }
		ai.Targets.Add(2, nearest)
	})
}

// travel ports Shulker.getDeltaMovement and setDeltaMovement: a shulker has
// no movement.
func (b *ShulkerBehaviour) travel(m *Mob, _ mgl64.Vec3) bool {
	m.data.Vel = mgl64.Vec3{}
	return true
}

// shulkerLookControl ports Shulker.ShulkerLookControl: a shulker turns its
// head around the axis of the face it is attached to, and never up or down.
type shulkerLookControl struct{ b *ShulkerBehaviour }

func (c shulkerLookControl) tick(m *Mob) {
	look := &m.AI.look
	m.SetPitch(0)
	if look.cooldown <= 0 {
		m.AI.headYaw = rotateTowards(m.AI.headYaw, m.AI.bodyYaw, 10)
		return
	}
	look.cooldown--
	up := cube.Pos{}.Side(c.b.attachFace.Opposite()).Vec3()
	var forward mgl64.Vec3
	switch c.b.attachFace.Opposite() {
	case cube.FaceUp:
		forward = mgl64.Vec3{0, 0, 1}
	case cube.FaceDown:
		forward = mgl64.Vec3{0, 0, -1}
	default:
		forward = mgl64.Vec3{0, -1, 0}
	}
	right := up.Cross(forward)
	out := mgl64.Vec3{look.wanted[0] - m.data.Pos[0], look.wanted[1] - m.EyeY(), look.wanted[2] - m.data.Pos[2]}
	dr, df := right.Dot(out), forward.Dot(out)
	if math.Abs(dr) > 1e-5 || math.Abs(df) > 1e-5 {
		m.AI.headYaw = rotateTowards(m.AI.headYaw, math.Atan2(-dr, df)*180/math.Pi, look.yMaxRotSpeed)
	}
}

// shulkerBodyControl ports Shulker.ShulkerBodyRotationControl: the body of a
// shulker never turns.
type shulkerBodyControl struct{}

func (shulkerBodyControl) tick(*Mob) {}

// setRawPeek ports Shulker.setRawPeekAmount: a closed shell protects the
// shulker with 20 armour.
func (b *ShulkerBehaviour) setRawPeek(m *Mob, amount uint8) {
	name := "entity.shulker.open"
	event := world.GameEventContainerOpen
	if amount == 0 {
		name, event = "entity.shulker.close", world.GameEventContainerClose
	}
	m.tx.PlaySound(m.data.Pos, sound.Custom{Name: name, Volume: 1, Pitch: 1})
	m.tx.EmitGameEvent(event, m.data.Pos, world.GameEventContext{Source: m.Self})
	b.peek = amount
}

// updatePeekAmount ports Shulker.updatePeekAmount.
func (b *ShulkerBehaviour) updatePeekAmount() bool {
	target := float32(b.peek) * .01
	if b.peekAmount == target {
		return false
	}
	if b.peekAmount > target {
		b.peekAmount = min(max(b.peekAmount-.05, target), 1)
	} else {
		b.peekAmount = min(max(b.peekAmount+.05, 0), target)
	}
	return true
}

// onPeekAmountChange ports Shulker.onPeekAmountChange: an opening shell
// pushes the entities in its way.
func (b *ShulkerBehaviour) onPeekAmountChange(e *Ent, tx *world.Tx) {
	now := shulkerPhysicalPeek(b.peekAmount)
	old := shulkerPhysicalPeek(b.peekAmount - .05)
	if b.peekAmount > float32(b.peek)*.01 {
		old = shulkerPhysicalPeek(b.peekAmount + .05)
	}
	push := now - old
	if push <= 0 {
		return
	}
	dir := b.attachFace.Opposite()
	box := shulkerProgressBox(dir, old, now).Translate(e.Position())
	step := cube.Pos{}.Side(dir).Vec3().Mul(push)
	for other := range tx.EntitiesWithin(box.Grow(1)) {
		if _, shulker := other.(*Shulker); shulker || spectator(other) || !other.H().Type().BBox(other).Translate(other.Position()).IntersectsWith(box) {
			continue
		}
		if v, ok := other.(interface {
			Velocity() mgl64.Vec3
			SetVelocity(mgl64.Vec3)
		}); ok {
			v.SetVelocity(v.Velocity().Add(step))
		}
	}
}

// shulkerAttackGoal ports Shulker.ShulkerAttackGoal: a shulker with a target
// within twenty blocks opens its shell and shoots a bullet every one to five
// and a half seconds.
type shulkerAttackGoal struct {
	goalFlags
	b          *ShulkerBehaviour
	attackTime int
}

func (g *shulkerAttackGoal) CanUse(m *Mob) bool {
	target := m.Target()
	return target != nil && entityAlive(target) && m.tx.World().Difficulty() != world.DifficultyPeaceful
}

func (g *shulkerAttackGoal) Start(m *Mob) {
	g.attackTime = 20
	g.b.setRawPeek(m, 100)
}

func (g *shulkerAttackGoal) Stop(m *Mob) { g.b.setRawPeek(m, 0) }

func (g *shulkerAttackGoal) RequiresUpdateEveryTick() bool { return true }

func (g *shulkerAttackGoal) Tick(m *Mob) {
	if m.tx.World().Difficulty() == world.DifficultyPeaceful {
		return
	}
	g.attackTime--
	target := m.Target()
	if target == nil {
		return
	}
	m.AI.look.SetLookAtEntityWith(target, 180, 180)
	if m.DistanceToSqr(target) >= 400 {
		m.SetTarget(nil)
		return
	}
	if g.attackTime > 0 {
		return
	}
	r := m.Random()
	g.attackTime = 20 + int(r.NextInt(10))*20/2
	m.tx.AddEntity(newShulkerBulletFrom(m, target, cube.Pos{}.Side(g.b.attachFace)))
	m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.shulker.shoot", Volume: 2, Pitch: float64((r.NextFloat()-r.NextFloat())*.2 + 1)})
}

// shulkerPeekGoal ports Shulker.ShulkerPeekGoal: an idle shulker peeks out of
// its shell now and then for one to three seconds.
type shulkerPeekGoal struct {
	goalFlags
	b        *ShulkerBehaviour
	peekTime int
}

func (g *shulkerPeekGoal) CanUse(m *Mob) bool {
	return m.Target() == nil && m.Random().NextInt(int32(reducedTickDelay(40))) == 0 &&
		g.b.canStayAt(m.tx, m.H(), m.BlockPos(), g.b.attachFace)
}

func (g *shulkerPeekGoal) CanContinueToUse(m *Mob) bool { return m.Target() == nil && g.peekTime > 0 }

func (g *shulkerPeekGoal) Start(m *Mob) {
	g.peekTime = adjustedTickDelay(g, 20*(1+int(m.Random().NextInt(3))))
	g.b.setRawPeek(m, 30)
}

func (g *shulkerPeekGoal) Stop(m *Mob) {
	if m.Target() == nil {
		g.b.setRawPeek(m, 0)
	}
}

func (g *shulkerPeekGoal) Tick(*Mob) { g.peekTime-- }

func (s *Shulker) Health() float64 { return s.behaviour().health.Health() }

// AttachFace returns the face of the block the shulker is attached to.
func (s *Shulker) AttachFace() cube.Face { return s.behaviour().attachFace }

// Peek returns how far the shell of the shulker opens, from 0 to 100.
func (s *Shulker) Peek() uint8 { return s.behaviour().peek }

func (s *Shulker) Color() uint8           { return s.behaviour().color }
func (s *Shulker) MaxHealth() float64     { return s.behaviour().health.MaxHealth() }
func (s *Shulker) SetMaxHealth(v float64) { s.behaviour().health.SetMaxHealth(v) }
func (s *Shulker) Dead() bool             { return s.behaviour().dead || s.Health() <= 0 }

// Hurt ports Shulker.hurtServer: arrows bounce off a closed shulker, which
// takes damage through 20 armour; a badly hurt shulker may teleport away, and
// one hit by a shulker bullet may teleport and leave a new shulker behind.
func (s *Shulker) Hurt(damage float64, src world.DamageSource) (float64, bool) {
	b := s.behaviour()
	if s.Dead() || damage <= 0 {
		return 0, false
	}
	projectile, isProjectile := src.(ProjectileDamageSource)
	if b.peek == 0 && isProjectile && projectile.Projectile != nil && strings.Contains(projectile.Projectile.H().Type().EncodeEntity(), "arrow") {
		return 0, false
	}
	var ok bool
	if damage, ok = b.ai.DamageCooldown(s.tx, damage); !ok {
		return 0, false
	}
	if b.peek == 0 {
		armour := 20.0
		damage *= 1 - min(armour, max(armour/5, armour-damage/2))/25
	}
	if damage, ok = mobEffectDamage(b.effects, damage, src); !ok {
		return 0, false
	}
	b.health.AddHealth(-damage)
	b.ai.Hurt(src)
	s.viewAction(HurtAction{})
	onMobHurt(s.tx, s)
	if s.Dead() {
		s.die(src)
		return damage, true
	}
	if !b.ai.started {
		return damage, true
	}
	m := b.ai.Mob(s.Ent, s)
	if b.health.Health() < b.health.MaxHealth()*.5 && m.Random().NextInt(4) == 0 {
		b.teleportSomewhere(m)
	} else if isProjectile && projectile.Projectile != nil && projectile.Projectile.H().Type() == ShulkerBulletType {
		b.hitByShulkerBullet(m)
	}
	return damage, true
}

// hitByShulkerBullet ports Shulker.hitByShulkerBullet: an open shulker that
// teleports away leaves a shulker of its colour behind, less often the more
// shulkers are near.
func (b *ShulkerBehaviour) hitByShulkerBullet(m *Mob) {
	oldPos, oldBox := m.data.Pos, m.BBox()
	if b.peek == 0 || !b.teleportSomewhere(m) {
		return
	}
	count := 0
	for e := range m.tx.EntitiesWithin(oldBox.Grow(8)) {
		if s, ok := e.(*Shulker); ok && !s.Dead() && e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(oldBox.Grow(8)) {
			count++
		}
	}
	if m.tx.RegionRandom().NextFloat() < float32(count-1)/5 {
		return
	}
	m.tx.AddEntity(NewShulker(world.EntitySpawnOpts{Position: oldPos, SpawnReason: "breeding"}, ShulkerBehaviourConfig{Color: b.color, colorSet: true}))
}

func (s *Shulker) Heal(health float64, _ world.HealingSource) float64 {
	if s.Dead() || health <= 0 {
		return 0
	}
	old := s.Health()
	s.behaviour().health.AddHealth(health)
	return s.Health() - old
}

// KnockBack does nothing: a shulker never moves.
func (s *Shulker) KnockBack(mgl64.Vec3, float64, float64) {}
func (s *Shulker) AddEffect(e effect.Effect)              { s.behaviour().effects.Add(e, s) }
func (s *Shulker) RemoveEffect(t effect.Type)             { s.behaviour().effects.Remove(t, s) }
func (s *Shulker) Effects() []effect.Effect               { return s.behaviour().effects.Effects() }
func (s *Shulker) Speed() float64                         { return 0 }
func (s *Shulker) SetSpeed(float64)                       {}
func (s *Shulker) updateState() {
	viewers := s.tx.Viewers(s.Position())
	for _, viewer := range viewers {
		viewer.ViewEntityState(s)
	}
	s.tx.ReleaseViewers(viewers)
}
func (s *Shulker) viewAction(action world.EntityAction) {
	viewers := s.tx.Viewers(s.Position())
	for _, viewer := range viewers {
		viewer.ViewEntityAction(s, action)
	}
	s.tx.ReleaseViewers(viewers)
}
func (s *Shulker) die(src world.DamageSource) {
	b := s.behaviour()
	if b.dead {
		return
	}
	b.dead = true
	s.viewAction(DeathAction{})
	dropAllDeathLoot(s.tx, s, src)
	_ = s.CloseIn(s.tx)
}

// findNewAttachment ports Shulker.findNewAttachment.
func (b *ShulkerBehaviour) findNewAttachment(e *Ent, tx *world.Tx) {
	if face, ok := b.attachableSurface(tx, e.H(), cube.PosFromVec3(e.Position())); ok {
		b.attachFace = face
		return
	}
	if b.ai.started {
		b.teleportSomewhere(b.ai.Mob(e, &Shulker{Ent: e}))
	}
}

// attachableSurface ports Shulker.findAttachableSurface.
func (b *ShulkerBehaviour) attachableSurface(tx *world.Tx, self *world.EntityHandle, pos cube.Pos) (cube.Face, bool) {
	for face := cube.FaceDown; face <= cube.FaceEast; face++ {
		if b.canStayAt(tx, self, pos, face) {
			return face, true
		}
	}
	return 0, false
}

// canStayAt ports Shulker.canStayAt: a shulker stays in air on the sturdy
// face of a block, with room to open its shell fully.
func (b *ShulkerBehaviour) canStayAt(tx *world.Tx, self *world.EntityHandle, pos cube.Pos, face cube.Face) bool {
	if face > cube.FaceEast {
		return false
	}
	if _, air := tx.Block(pos).(block.Air); !air {
		return false
	}
	support := pos.Side(face)
	if !tx.Block(support).Model().FaceSolid(support, face.Opposite(), tx) {
		return false
	}
	open := shulkerProgressBox(face.Opposite(), -1, 1).Translate(pos.Vec3Middle()).Grow(-1e-6)
	if !boxFree(tx, open) {
		return false
	}
	for e := range tx.EntitiesWithin(open.Grow(1)) {
		if e.H() != self {
			if _, ok := e.(*Shulker); ok && e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(open) {
				return false
			}
		}
	}
	return true
}

// teleportSomewhere ports Shulker.teleportSomewhere: a shulker tries five
// times to teleport to a spot within eight blocks it may stay at.
func (b *ShulkerBehaviour) teleportSomewhere(m *Mob) bool {
	if m.AI.NoAI || !m.Alive() {
		return false
	}
	old := m.BlockPos()
	r := m.Random()
	for range 5 {
		target := old.Add(cube.Pos{int(r.NextInt(17)) - 8, int(r.NextInt(17)) - 8, int(r.NextInt(17)) - 8})
		if target[1] <= m.tx.Range().Min() || !m.tx.World().WithinBorder(target) {
			continue
		}
		if _, air := m.tx.Block(target).(block.Air); !air || !boxFree(m.tx, cube.Box(0, 0, 0, 1, 1, 1).Translate(target.Vec3()).Grow(-1e-6)) {
			continue
		}
		face, ok := b.attachableSurface(m.tx, m.H(), target)
		if !ok || blockInTag(m.tx.Block(target.Side(face)), "shulker_does_not_teleport_to") {
			continue
		}
		if m.data.Riding != nil {
			DismountRider(m.Self, m.tx)
		}
		b.attachFace = face
		m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.shulker.teleport", Volume: 1, Pitch: 1})
		m.Self.(*Shulker).Teleport(target.Vec3Middle())
		m.tx.EmitGameEvent(world.GameEventTeleport, old.Vec3Centre(), world.GameEventContext{Source: m.Self})
		b.peek = 0
		m.SetTarget(nil)
		return true
	}
	return false
}

// FinalizeSpawn applies the shulker's zero-yaw spawn pose.
func (s *Shulker) FinalizeSpawn(_ string, _ *vanilla.Random) {
	s.data.Rot = cube.Rotation{}
}

// NewShulkerBullet creates a shulker bullet flying with the velocity of the
// spawn options at a target.
func NewShulkerBullet(opts world.EntitySpawnOpts, owner, target *world.EntityHandle) *world.EntityHandle {
	return opts.New(ShulkerBulletType, shulkerBulletConf{owner: owner, target: target})
}

// newShulkerBulletFrom ports the ShulkerBullet constructor: a bullet leaves
// the middle of the shulker for its target along any axis but that of the
// face the shulker is attached to.
func newShulkerBulletFrom(m *Mob, target world.Entity, avoidAxis cube.Pos) *world.EntityHandle {
	box := m.BBox()
	centre := box.Min().Add(box.Max()).Mul(.5)
	return world.EntitySpawnOpts{Position: centre}.New(ShulkerBulletType, shulkerBulletConf{owner: m.H(), target: target.H(), avoid: &avoidAxis})
}

type shulkerBulletConf struct {
	owner, target *world.EntityHandle
	avoid         *cube.Pos
}

func (c shulkerBulletConf) Apply(data *world.EntityData) {
	projectile := ProjectileBehaviourConfig{
		Owner: c.owner, Gravity: 0, Drag: 0, Damage: 4,
		damageCalculator: func(float64, float64, bool, int) float64 { return 4 },
		Hit: func(e *Ent, tx *world.Tx, hit trace.Result) {
			if result, ok := hit.(trace.EntityResult); ok {
				if living, ok := result.Entity().(Living); ok {
					living.AddEffect(effect.New(effect.Levitation, 1, 10*time.Second))
				}
				return
			}
			// ShulkerBullet.onHitBlock.
			tx.AddParticle(e.Position(), particle.HugeExplosion{})
			tx.PlaySound(e.Position(), sound.Custom{Name: "entity.shulker_bullet.hit", Volume: 1, Pitch: 1})
		},
	}.New()
	data.Data = &shulkerBulletBehaviour{owner: c.owner, target: c.target, avoid: c.avoid, projectile: projectile, steps: -1}
}

type shulkerBulletBehaviour struct {
	owner, target *world.EntityHandle
	avoid         *cube.Pos
	projectile    *ProjectileBehaviour
	// dir is ShulkerBullet.currentMoveDirection, nil while it has none.
	dir         *cube.Face
	steps       int
	targetDelta mgl64.Vec3
}

// selectNextMoveDirection ports ShulkerBullet.selectNextMoveDirection: a
// bullet turns along an axis towards its target every half second to two and
// a half seconds, avoiding the axis it flew along.
func (b *shulkerBulletBehaviour) selectNextMoveDirection(e *Ent, tx *world.Tx, avoid int, target world.Entity) {
	yOffset := .5
	var targetPos cube.Pos
	pos := e.Position()
	if target == nil {
		targetPos = cube.PosFromVec3(pos).Side(cube.FaceDown)
	} else {
		yOffset = target.H().Type().BBox(target).Height() * .5
		tp := target.Position()
		targetPos = cube.PosFromVec3(mgl64.Vec3{tp[0], tp[1] + yOffset, tp[2]})
	}
	goal := mgl64.Vec3{float64(targetPos[0]) + .5, float64(targetPos[1]) + yOffset, float64(targetPos[2]) + .5}
	var selection *cube.Face
	r := tx.RegionRandom()
	if targetPos.Vec3Centre().Sub(pos).LenSqr() >= 4 {
		current := cube.PosFromVec3(pos)
		empty := func(p cube.Pos) bool { _, air := tx.Block(p).(block.Air); return air }
		var options []cube.Face
		for axis, pair := range [3][2]cube.Face{{cube.FaceEast, cube.FaceWest}, {cube.FaceUp, cube.FaceDown}, {cube.FaceSouth, cube.FaceNorth}} {
			if axis == avoid {
				continue
			}
			switch {
			case current[axis] < targetPos[axis] && empty(current.Side(pair[0])):
				options = append(options, pair[0])
			case current[axis] > targetPos[axis] && empty(current.Side(pair[1])):
				options = append(options, pair[1])
			}
		}
		face := cube.Face(r.NextInt(6))
		if len(options) == 0 {
			for attempts := 5; !empty(current.Side(face)) && attempts > 0; attempts-- {
				face = cube.Face(r.NextInt(6))
			}
		} else {
			face = options[r.NextInt(int32(len(options)))]
		}
		selection = &face
		goal = pos.Add(cube.Pos{}.Side(face).Vec3())
	}
	b.dir = selection
	d := goal.Sub(pos)
	if l := d.Len(); l == 0 {
		b.targetDelta = mgl64.Vec3{}
	} else {
		b.targetDelta = d.Mul(.15 / l)
	}
	b.steps = 10 + int(r.NextInt(5))*10
}

// faceAxis returns the axis of a face, 0 for x, 1 for y and 2 for z.
func faceAxis(f cube.Face) int {
	switch f {
	case cube.FaceWest, cube.FaceEast:
		return 0
	case cube.FaceDown, cube.FaceUp:
		return 1
	}
	return 2
}

// Tick ports ShulkerBullet.tick: a bullet without a target falls, and one
// with a target accelerates towards it along its current axis.
func (b *shulkerBulletBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if tx.World().Difficulty() == world.DifficultyPeaceful {
		_ = e.CloseIn(tx)
		return nil
	}
	var target world.Entity
	if b.target != nil {
		if t, ok := b.target.Entity(tx); ok {
			target = t
		} else {
			b.target = nil
		}
	}
	if b.steps < 0 {
		// The constructor of ShulkerBullet: the bullet starts upwards.
		up := cube.FaceUp
		b.dir = &up
		avoid := -1
		if b.avoid != nil {
			avoid = faceAxis(posFace(*b.avoid))
		}
		if b.avoid != nil || target != nil {
			b.selectNextMoveDirection(e, tx, avoid, target)
		} else {
			b.steps = 0
		}
	}
	if target == nil || !entityAlive(target) || spectator(target) {
		e.data.Vel[1] -= .04
	} else {
		for i := range 3 {
			b.targetDelta[i] = max(-1, min(1, b.targetDelta[i]*1.025))
		}
		e.data.Vel = e.data.Vel.Add(b.targetDelta.Sub(e.data.Vel).Mul(.2))
	}
	mov := b.projectile.Tick(e, tx)
	if _, alive := e.H().Entity(tx); target == nil || !alive {
		return mov
	}
	if b.steps > 0 {
		if b.steps--; b.steps == 0 {
			avoid := -1
			if b.dir != nil {
				avoid = faceAxis(*b.dir)
			}
			b.selectNextMoveDirection(e, tx, avoid, target)
		}
	}
	if b.dir != nil {
		current := cube.PosFromVec3(e.Position())
		axis := faceAxis(*b.dir)
		next := current.Side(*b.dir)
		tp := cube.PosFromVec3(target.Position())
		if tx.Block(next).Model().FaceSolid(next, cube.FaceUp, tx) || current[axis] == tp[axis] {
			b.selectNextMoveDirection(e, tx, axis, target)
		}
	}
	return mov
}

// posFace returns the face a unit offset points towards.
func posFace(p cube.Pos) cube.Face {
	for f := cube.FaceDown; f <= cube.FaceEast; f++ {
		if (cube.Pos{}).Side(f) == p {
			return f
		}
	}
	return cube.FaceUp
}

// ShulkerBullet is the homing bullet of a shulker, which players may
// destroy by hitting it.
type ShulkerBullet struct{ *Ent }

// Destroy ports ShulkerBullet.hurtServer: a hit bullet breaks.
func (b *ShulkerBullet) Destroy(tx *world.Tx, _ world.DamageSource, _ world.Entity) bool {
	tx.PlaySound(b.Position(), sound.Custom{Name: "entity.shulker_bullet.hurt", Volume: 1, Pitch: 1})
	tx.EmitGameEvent(world.GameEventEntityDamage, b.Position(), world.GameEventContext{Source: b})
	_ = b.CloseIn(tx)
	return true
}

var ShulkerBulletType shulkerBulletType

type shulkerBulletType struct{}

func (shulkerBulletType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	if data.Data == nil {
		shulkerBulletConf{}.Apply(data)
	}
	return &ShulkerBullet{Ent: &Ent{tx: tx, handle: handle, data: data}}
}
func (shulkerBulletType) EncodeEntity() string { return "minecraft:shulker_bullet" }
func (shulkerBulletType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.15625, -0.15625, -0.15625, 0.15625, 0.15625, 0.15625)
}
func (shulkerBulletType) DecodeNBT(_ map[string]any, data *world.EntityData) {
	shulkerBulletConf{}.Apply(data)
}
func (shulkerBulletType) EncodeNBT(*world.EntityData) map[string]any { return nil }
