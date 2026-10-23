package entity

import (
	"math"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
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
func (shulkerType) BBox(entity world.Entity) cube.BBox {
	shulker, ok := entity.(*Shulker)
	if !ok {
		return cube.Box(-0.5, 0, -0.5, 0.5, 1, 0.5)
	}
	amount := float64(shulker.Peek()) / 100
	extension := 0.5 - math.Sin((0.5+amount)*math.Pi)*0.5
	minX, minY, minZ, maxX, maxY, maxZ := -0.5, 0.0, -0.5, 0.5, 1.0, 0.5
	switch shulker.AttachFace() {
	case cube.FaceDown:
		maxY += extension
	case cube.FaceUp:
		minY -= extension
	case cube.FaceNorth:
		maxZ += extension
	case cube.FaceSouth:
		minZ -= extension
	case cube.FaceWest:
		maxX += extension
	case cube.FaceEast:
		minX -= extension
	}
	return cube.Box(minX, minY, minZ, maxX, maxY, maxZ)
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
		conf.Color, conf.colorSet = nativeNBTByte(value), true
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
		"AttachFace": b.attachFace.String(), "Peek": b.peek, "Color": b.color,
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
	return &ShulkerBehaviour{health: NewHealthManager(conf.Health, conf.MaxHealth), effects: NewEffectManager(), attachFace: conf.AttachFace, peek: conf.Peek, color: conf.Color, attackCooldown: 20}
}

// Shulker is an immobile hostile living entity.
type Shulker struct{ *Ent }
type ShulkerBehaviour struct {
	health         *HealthManager
	effects        *EffectManager
	attachFace     cube.Face
	peek, color    uint8
	attackCooldown int
	dead           bool
	target         *world.EntityHandle
	unseenTicks    int
}

func (s *Shulker) behaviour() *ShulkerBehaviour { return s.Behaviour().(*ShulkerBehaviour) }
func (s *Shulker) Tick(tx *world.Tx, current int64) {
	b := s.behaviour()
	face, peek, pos := b.attachFace, b.peek, s.Position()
	b.effects.Tick(s, tx)
	s.Ent.Tick(tx, current)
	if b.attachFace != face || b.peek != peek || s.Position() != pos {
		s.updateState()
	}
}
func (b *ShulkerBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if !b.canStayAt(tx, cube.PosFromVec3(e.Position()), b.attachFace) {
		b.findNewAttachment(e, tx)
	}
	b.updateAttack(e, tx)
	e.data.Vel = mgl64.Vec3{}
	return nil
}
func (b *ShulkerBehaviour) updateAttack(e *Ent, tx *world.Tx) {
	target := b.attackTarget(e, tx)
	if target == nil {
		b.peek = 0
		return
	}
	// Peek is the target opening sent to the client, not an animation frame.
	b.peek = 100
	b.attackCooldown--
	if b.attackCooldown > 0 {
		return
	}
	pos := e.Position().Add(mgl64.Vec3{0, .5, 0})
	delta := target.Position().Add(mgl64.Vec3{0, .5, 0}).Sub(pos)
	velocity := mgl64.Vec3{}
	if delta.Len() != 0 {
		velocity = delta.Normalize().Mul(.15)
	}
	tx.AddEntity(NewShulkerBullet(world.EntitySpawnOpts{Position: pos, Velocity: velocity}, e.H(), target.H()))
	b.attackCooldown = 20 + rand.IntN(91)
}

func (b *ShulkerBehaviour) attackTarget(e *Ent, tx *world.Tx) world.Entity {
	if tx.World().Difficulty() == world.DifficultyPeaceful {
		b.target, b.unseenTicks = nil, 0
		return nil
	}
	if b.target != nil {
		target, loaded := b.target.Entity(tx)
		if loaded && combatTargetable(target) && target.Position().Sub(e.Position()).LenSqr() <= 16*16 {
			if shulkerVisible(e, tx, target) {
				b.unseenTicks = 0
			} else {
				b.unseenTicks++
			}
			if b.unseenTicks <= 60 {
				return target
			}
		}
		b.target, b.unseenTicks = nil, 0
	}

	// ShulkerNearestAttackGoal searches only four blocks along the attachment
	// axis. A new target must be visible; an existing one has 60 ticks of memory.
	grow := mgl64.Vec3{16, 4, 16}
	switch b.attachFace {
	case cube.FaceWest, cube.FaceEast:
		grow = mgl64.Vec3{4, 16, 16}
	case cube.FaceNorth, cube.FaceSouth:
		grow = mgl64.Vec3{16, 16, 4}
	}
	var nearest world.Entity
	nearestDistance := 16.0 * 16.0
	for target := range tx.EntitiesWithin(shulkerType{}.BBox(nil).Translate(e.Position()).GrowVec3(grow)) {
		if target.H().Type().EncodeEntity() != "minecraft:player" || !combatTargetable(target) {
			continue
		}
		distance := target.Position().Sub(e.Position()).LenSqr()
		if distance < nearestDistance && shulkerVisible(e, tx, target) {
			nearest, nearestDistance = target, distance
		}
	}
	if nearest != nil {
		b.target, b.attackCooldown = nearest.H(), 20
	}
	return nearest
}

func shulkerVisible(e *Ent, tx *world.Tx, target world.Entity) bool {
	return dragonLineOfSight(tx, e.Position().Add(mgl64.Vec3{0, .5, 0}), EyePosition(target))
}

func (s *Shulker) Health() float64 { return s.behaviour().health.Health() }

// AttachFace returns the face to which the shulker is attached.
func (s *Shulker) AttachFace() cube.Face { return s.behaviour().attachFace }

// Peek returns the current shell opening amount.
func (s *Shulker) Peek() uint8 { return s.behaviour().peek }

// Color returns the shulker colour, or 16 when it has no colour.
func (s *Shulker) Color() uint8           { return s.behaviour().color }
func (s *Shulker) MaxHealth() float64     { return s.behaviour().health.MaxHealth() }
func (s *Shulker) SetMaxHealth(v float64) { s.behaviour().health.SetMaxHealth(v) }
func (s *Shulker) Dead() bool             { return s.behaviour().dead || s.Health() <= 0 }
func (s *Shulker) Hurt(damage float64, src world.DamageSource) (float64, bool) {
	if s.Dead() || damage <= 0 || (src.Fire() && s.hasEffect(effect.FireResistance)) {
		return 0, false
	}
	b := s.behaviour()
	if b.peek == 0 {
		if projectile, ok := src.(ProjectileDamageSource); ok && projectile.Projectile != nil && strings.Contains(projectile.Projectile.H().Type().EncodeEntity(), "arrow") {
			return 0, false
		}
		armor := 20.0
		damage *= 1 - min(armor, max(armor/5, armor-damage/2))/25
	}
	if resistance, ok := b.effects.Effect(effect.Resistance); ok {
		damage *= effect.Resistance.Multiplier(src, resistance.Level())
	}
	b.health.AddHealth(-damage)
	s.viewAction(HurtAction{})
	onMobHurt(s.tx, s)
	if s.Dead() {
		s.die(src)
	} else if b.health.Health() < b.health.MaxHealth()*0.5 && rand.IntN(4) == 0 {
		if b.teleportSomewhere(s.Ent, s.tx) {
			s.updateState()
		}
	}
	return damage, true
}
func (s *Shulker) Heal(health float64, _ world.HealingSource) float64 {
	if s.Dead() || health <= 0 {
		return 0
	}
	old := s.Health()
	s.behaviour().health.AddHealth(health)
	return s.Health() - old
}
func (s *Shulker) KnockBack(src mgl64.Vec3, force, height float64) { _ = src; _ = force; _ = height }
func (s *Shulker) AddEffect(e effect.Effect)                       { s.behaviour().effects.Add(e, s) }
func (s *Shulker) RemoveEffect(t effect.Type)                      { s.behaviour().effects.Remove(t, s) }
func (s *Shulker) Effects() []effect.Effect                        { return s.behaviour().effects.Effects() }
func (s *Shulker) Speed() float64                                  { return 0 }
func (s *Shulker) SetSpeed(float64)                                {}
func (s *Shulker) hasEffect(t effect.Type) bool                    { _, ok := s.behaviour().effects.Effect(t); return ok }
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
func (b *ShulkerBehaviour) findNewAttachment(e *Ent, tx *world.Tx) {
	pos := cube.PosFromVec3(e.Position())
	for face := cube.FaceDown; face <= cube.FaceEast; face++ {
		if b.canStayAt(tx, pos, face) {
			b.attachFace = face
			return
		}
	}
	b.teleportSomewhere(e, tx)
}
func (b *ShulkerBehaviour) canStayAt(tx *world.Tx, pos cube.Pos, face cube.Face) bool {
	if face > cube.FaceEast || len(tx.Block(pos).Model().BBox(pos, tx)) != 0 {
		return false
	}
	support := pos.Side(face)
	return tx.Block(support).Model().FaceSolid(support, face.Opposite(), tx)
}
func (b *ShulkerBehaviour) teleportSomewhere(e *Ent, tx *world.Tx) bool {
	old := cube.PosFromVec3(e.Position())
	for range 5 {
		target := old.Add(cube.Pos{rand.IntN(17) - 8, rand.IntN(17) - 8, rand.IntN(17) - 8})
		if target[1] <= 1 || len(tx.Block(target).Model().BBox(target, tx)) != 0 {
			continue
		}
		for face := cube.FaceDown; face <= cube.FaceEast; face++ {
			if b.canStayAt(tx, target, face) {
				e.Teleport(mgl64.Vec3{float64(target[0]) + .5, float64(target[1]), float64(target[2]) + .5})
				b.attachFace, b.peek = face, 0
				return true
			}
		}
	}
	return false
}

// NewShulkerBullet creates a homing shulker bullet worldless handle.
func NewShulkerBullet(opts world.EntitySpawnOpts, owner, target *world.EntityHandle) *world.EntityHandle {
	return opts.New(ShulkerBulletType, shulkerBulletConf{owner: owner, target: target})
}

type shulkerBulletConf struct{ owner, target *world.EntityHandle }

func (c shulkerBulletConf) Apply(data *world.EntityData) {
	projectile := ProjectileBehaviourConfig{
		Owner: c.owner, Gravity: 0, Drag: 0.01, Damage: 4,
		damageCalculator: func(float64, float64, bool, int) float64 { return 4 },
		Hit: func(_ *Ent, tx *world.Tx, hit trace.Result) {
			if result, ok := hit.(trace.EntityResult); ok {
				if living, ok := result.Entity().(Living); ok {
					living.AddEffect(effect.New(effect.Levitation, 1, 10*time.Second))
				}
			}
		},
	}.New()
	data.Data = &shulkerBulletBehaviour{owner: c.owner, target: c.target, projectile: projectile}
}

type shulkerBulletBehaviour struct {
	owner, target *world.EntityHandle
	projectile    *ProjectileBehaviour
	age           int
}

func (b *shulkerBulletBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	b.age++
	if b.age > 200 {
		_ = e.CloseIn(tx)
		return nil
	}
	if b.target != nil {
		if target, ok := b.target.Entity(tx); ok {
			delta := target.Position().Add(mgl64.Vec3{0, targetTypeEyeHeight(target), 0}).Sub(e.Position())
			if delta.Len() != 0 {
				desired := delta.Normalize().Mul(0.15)
				e.data.Vel = e.data.Vel.Mul(0.8).Add(desired.Mul(0.2))
				if e.data.Vel.Len() != 0 {
					e.data.Vel = e.data.Vel.Normalize().Mul(0.15)
				}
			}
		}
	}
	return b.projectile.Tick(e, tx)
}
func targetTypeEyeHeight(e world.Entity) float64 {
	if l, ok := e.(interface{ EyeHeight() float64 }); ok {
		return l.EyeHeight() * 0.5
	}
	return 0.5
}

var ShulkerBulletType shulkerBulletType

type shulkerBulletType struct{}

func (shulkerBulletType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	if data.Data == nil {
		shulkerBulletConf{}.Apply(data)
	}
	return &Ent{tx: tx, handle: handle, data: data}
}
func (shulkerBulletType) EncodeEntity() string { return "minecraft:shulker_bullet" }
func (shulkerBulletType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.15625, -0.15625, -0.15625, 0.15625, 0.15625, 0.15625)
}
func (shulkerBulletType) DecodeNBT(_ map[string]any, data *world.EntityData) {
	shulkerBulletConf{}.Apply(data)
}
func (shulkerBulletType) EncodeNBT(*world.EntityData) map[string]any { return nil }

// FinalizeSpawn applies the shulker's zero-yaw spawn pose.
func (s *Shulker) FinalizeSpawn(_ string, _ *vanilla.Random) {
	s.data.Rot = cube.Rotation{}
}
