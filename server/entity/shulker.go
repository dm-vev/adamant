package entity

import (
	"math"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
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
	if value, ok := source["Color"]; ok {
		conf.Color = nativeNBTByte(value)
		conf.colorSet = true
	} else if colour, ok := NativeMobColor(m); ok {
		conf.Color, conf.colorSet = colour, true
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
	native := map[string]any{"Attributes": attributes}
	if b.color < 16 {
		native = SetNativeMobColor(native, byte(b.color))
	}
	javaState := MergeNativeMobNBT(ReadNativeMobJavaState(data.NativeNBT), map[string]any{
		"AttachFace": b.attachFace.String(), "Peek": int8(b.peek), "Color": int8(b.color),
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
}

func (s *Shulker) behaviour() *ShulkerBehaviour { return s.Behaviour().(*ShulkerBehaviour) }
func (s *Shulker) Tick(tx *world.Tx, current int64) {
	s.behaviour().effects.Tick(s, tx)
	s.Ent.Tick(tx, current)
}
func (b *ShulkerBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if !b.canStayAt(tx, cube.PosFromVec3(e.Position()), b.attachFace) {
		b.findNewAttachment(e, tx)
	}
	if b.attackCooldown > 0 {
		b.attackCooldown--
	}
	b.updateAttack(e, tx)
	e.data.Vel = mgl64.Vec3{}
	return nil
}
func (b *ShulkerBehaviour) updateAttack(e *Ent, tx *world.Tx) {
	var nearest Living
	nearestDistance := 16.0 * 16.0
	for target := range tx.EntitiesWithin(shulkerType{}.BBox(nil).Translate(e.Position()).Grow(16)) {
		living, ok := target.(Living)
		if !ok || living.Dead() || target.H() == e.H() {
			continue
		}
		if target.H().Type().EncodeEntity() != "minecraft:player" {
			continue
		}
		delta := target.Position().Sub(e.Position())
		distance := delta.LenSqr()
		if distance < nearestDistance {
			nearest, nearestDistance = living, distance
		}
	}
	if nearest == nil {
		if b.peek > 0 {
			b.peek -= min(b.peek, uint8(5))
		}
		return
	}
	b.peek = min(b.peek+5, 100)
	if b.attackCooldown > 0 || nearestDistance >= 400 {
		return
	}
	pos := e.Position().Add(mgl64.Vec3{0, .5, 0})
	delta := nearest.Position().Add(mgl64.Vec3{0, .5, 0}).Sub(pos)
	velocity := mgl64.Vec3{}
	if delta.Len() != 0 {
		velocity = delta.Normalize().Mul(.15)
	}
	tx.AddEntity(NewShulkerBullet(world.EntitySpawnOpts{Position: pos, Velocity: velocity}, e.H(), nearest.H()))
	b.attackCooldown = 20 + rand.IntN(91)
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
	if s.Dead() {
		s.die()
	} else if b.health.Health() < b.health.MaxHealth()*0.5 && rand.IntN(4) == 0 {
		b.teleportSomewhere(s.Ent, s.tx)
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
func (s *Shulker) viewAction(action world.EntityAction) {
	viewers := s.tx.Viewers(s.Position())
	for _, viewer := range viewers {
		viewer.ViewEntityAction(s, action)
	}
	s.tx.ReleaseViewers(viewers)
}
func (s *Shulker) die() {
	b := s.behaviour()
	if b.dead {
		return
	}
	b.dead = true
	s.viewAction(DeathAction{})
	if rand.IntN(2) == 0 {
		s.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: s.Position()}, item.NewStack(item.ShulkerShell{}, 1)))
	}
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
				e.data.Pos = mgl64.Vec3{float64(target[0]) + .5, float64(target[1]), float64(target[2]) + .5}
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
					living.AddEffect(effect.New(effect.Levitation, 0, 10*time.Second))
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
func (s *Shulker) FinalizeSpawn(_ string, _ *java26.Random) {
	s.data.Rot = cube.Rotation{}
}
