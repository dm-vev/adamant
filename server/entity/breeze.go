package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// BreezeType is the world entity type for the trial-chamber breeze.
var BreezeType breezeType

type breezeType struct{}

func (breezeType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	if data.Data == nil {
		breezeConfig{}.Apply(data)
	}
	return &Breeze{Ent: &Ent{tx: tx, handle: handle, data: data}}
}

func (breezeType) EncodeEntity() string        { return "minecraft:breeze" }
func (breezeType) BBox(world.Entity) cube.BBox { return cube.Box(-.3, 0, -.3, .3, 1.77, .3) }
func (breezeType) DecodeNBT(n map[string]any, data *world.EntityData) {
	conf := breezeConfig{}
	for _, attr := range ReadNativeMobAttributes(n) {
		switch attr.Name {
		case "minecraft:health":
			conf.health, conf.healthSet = attr.Current, true
		case "minecraft:movement":
			if attr.Current > 0 {
				conf.speed = attr.Current
			}
		}
	}
	if !conf.healthSet {
		if _, ok := n["Health"]; ok {
			conf.health, conf.healthSet = float64(nbtconv.Float32(n, "Health")), true
		}
	}
	data.Data = conf.New()
}
func (breezeType) EncodeNBT(data *world.EntityData) map[string]any {
	b := data.Data.(*breezeState)
	attrs := EncodeNativeMobAttributes(ReadNativeMobAttributes(data.NativeNBT), []NativeMobAttribute{
		{Name: "minecraft:health", Base: 30, Current: b.health.Health(), DefaultMin: 0, DefaultMax: 30, Min: 0, Max: 30},
		{Name: "minecraft:movement", Base: .63, Current: b.speed, DefaultMin: 0, DefaultMax: .63, Min: 0, Max: .63},
		{Name: "minecraft:follow_range", Base: 24, Current: 24, DefaultMin: 0, DefaultMax: 2048, Min: 0, Max: 2048},
		{Name: "minecraft:attack_damage", Base: 3, Current: 3, DefaultMin: 0, DefaultMax: 2048, Min: 0, Max: 2048},
	})
	native := CloneNativeMobNBT(data.NativeNBT)
	if native == nil {
		native = make(map[string]any)
	}
	native["Attributes"] = attrs
	return native
}

type breezeConfig struct {
	health    float64
	healthSet bool
	speed     float64
}

func (c breezeConfig) Apply(data *world.EntityData) { data.Data = c.New() }
func (c breezeConfig) New() *breezeState {
	health := c.health
	if !c.healthSet || health <= 0 {
		health = 30
	}
	speed := c.speed
	if speed <= 0 {
		speed = float64(float32(.63))
	}
	s := &breezeState{health: NewHealthManager(health, 30), effects: NewEffectManager(), speed: speed}
	s.br = newBreezeBrain(s)
	return s
}

// breezePose is the pose of a breeze, from the Pose of Java.
type breezePose uint8

const (
	breezeStanding breezePose = iota
	breezeSliding
	breezeShooting
	breezeInhaling
	breezeLongJumping
)

type breezeState struct {
	health  *HealthManager
	effects *EffectManager
	ai      MobAI
	br      *Brain
	speed   float64
	dead    bool
	pose    breezePose
}

type Breeze struct{ *Ent }

// NewBreeze creates a Breeze with the source attributes and no equipment.
func NewBreeze(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(BreezeType, breezeConfig{})
}
func (*Breeze) SpeciesType() world.EntityType { return BreezeType }
func (b *Breeze) state() *breezeState         { return b.Behaviour().(*breezeState) }
func (b *Breeze) Tick(tx *world.Tx, current int64) {
	b.state().effects.Tick(b, tx)
	b.Ent.Tick(tx, current)
}

func (s *breezeState) mobAI() *MobAI { return &s.ai }
func (s *breezeState) brain() *Brain { return s.br }

// Tick runs the MobAI of the breeze with its brain.
func (s *breezeState) Tick(e *Ent, tx *world.Tx) *Movement {
	// Breeze.createAttributes, getMaxHeadYRot and getHeadRotSpeed.
	s.ai.MovementSpeed, s.ai.FollowRange, s.ai.EyeHeight = s.speed, 24, float64(float32(1.3452))
	s.ai.MaxHeadYaw, s.ai.HeadRotSpeed = 30, 25
	return s.ai.Tick(e, &Breeze{Ent: e}, func(m *Mob) {
		m.AI.SetPathfindingMalus(PathOnTopOfTrapdoor, -1)
		m.AI.SetPathfindingMalus(PathFire, -1)
	})
}

// customServerAiStep ports Breeze.customServerAiStep and BreezeAi.updateActivity.
func (s *breezeState) customServerAiStep(m *Mob) {
	s.br.tick(m)
	s.br.setActiveToFirstValid(activityFight, activityIdle)
}

// canAttackEntity ports Breeze.canAttack: a breeze only attacks players and
// iron golems.
func (s *breezeState) canAttackEntity(_ *Mob, t world.Entity) bool {
	return isPlayerEntity(t) || t.H().Type().EncodeEntity() == "minecraft:iron_golem"
}

// fluidJumpThreshold ports Breeze.getFluidJumpThreshold: a breeze swims up
// from water as deep as its eyes are high.
func (s *breezeState) fluidJumpThreshold(m *Mob) float64 { return m.AI.EyeHeight }

// setPose sets the pose of the breeze, which viewers see.
func (s *breezeState) setPose(m *Mob, pose breezePose) {
	if s.pose != pose {
		s.pose = pose
		m.updateState()
	}
}

// Shooting reports if the breeze shoots at its target, which Bedrock shows
// as facing its target to attack from range.
func (b *Breeze) Shooting() bool { return b.state().pose == breezeShooting }

// JumpGoalJumping reports if the breeze long jumps.
func (b *Breeze) JumpGoalJumping() bool { return b.state().pose == breezeLongJumping }

// ShootTargetHandle returns the target of the breeze while it shoots.
func (b *Breeze) ShootTargetHandle() *world.EntityHandle {
	s := b.state()
	if s.pose != breezeShooting {
		return nil
	}
	v, _ := s.br.get(memAttackTarget)
	h, _ := v.(*world.EntityHandle)
	return h
}

func (b *Breeze) Health() float64        { return b.state().health.Health() }
func (b *Breeze) MaxHealth() float64     { return b.state().health.MaxHealth() }
func (b *Breeze) SetMaxHealth(v float64) { b.state().health.SetMaxHealth(v) }
func (b *Breeze) Dead() bool             { return b.state().dead || b.Health() <= 0 }

// Hurt ports Breeze.isInvulnerableTo: a breeze takes no damage from
// breezes.
func (b *Breeze) Hurt(damage float64, src world.DamageSource) (float64, bool) {
	if b.Dead() || damage <= 0 {
		return 0, false
	}
	if killer := mobKiller(src); killer != nil && killer.H().Type() == BreezeType {
		return 0, false
	}
	s := b.state()
	var ok bool
	if damage, ok = s.ai.DamageCooldown(b.tx, damage); !ok {
		return 0, false
	}
	if damage, ok = mobEffectDamage(s.effects, damage, src); !ok {
		return 0, false
	}
	s.health.AddHealth(-damage)
	s.ai.Hurt(src)
	b.viewAction(HurtAction{})
	onMobHurt(b.tx, b)
	if b.Dead() {
		s.dead = true
		b.viewAction(DeathAction{})
		dropAllDeathLoot(b.tx, b, src)
		_ = b.CloseIn(b.tx)
	}
	return damage, true
}

func (b *Breeze) Heal(amount float64, _ world.HealingSource) float64 {
	if amount <= 0 || b.Dead() {
		return 0
	}
	old := b.Health()
	b.state().health.AddHealth(amount)
	return b.Health() - old
}
func (b *Breeze) KnockBack(source mgl64.Vec3, force, height float64) {
	knockBack(b, source, force, height)
}
func (b *Breeze) AddEffect(e effect.Effect)  { b.state().effects.Add(e, b) }
func (b *Breeze) RemoveEffect(t effect.Type) { b.state().effects.Remove(t, b) }
func (b *Breeze) Effects() []effect.Effect   { return b.state().effects.Effects() }
func (b *Breeze) Speed() float64             { return b.state().speed }
func (b *Breeze) SetSpeed(speed float64)     { b.state().speed = speed }
func (b *Breeze) viewAction(action world.EntityAction) {
	viewers := b.tx.Viewers(b.Position())
	for _, viewer := range viewers {
		viewer.ViewEntityAction(b, action)
	}
	b.tx.ReleaseViewers(viewers)
}

// DeflectProjectile reverses incoming projectiles except both wind-charge types, as in Breeze.deflection().
func (b *Breeze) DeflectProjectile(projectile *Ent, movement *Movement, velocity mgl64.Vec3) bool {
	if projectile.H().Type().EncodeEntity() == "minecraft:breeze_wind_charge" || projectile.H().Type().EncodeEntity() == "minecraft:wind_charge_projectile" {
		return false
	}
	behaviour, ok := projectile.Behaviour().(*ProjectileBehaviour)
	if !ok {
		return false
	}
	reflected := velocity.Mul(-1)
	projectile.SetVelocity(reflected)
	behaviour.conf.Owner = b.H()
	movement.vel = reflected
	movement.dvel = reflected.Sub(velocity)
	movement.rot = cube.Rotation{
		mgl64.RadToDeg(math.Atan2(reflected[0], reflected[2])),
		mgl64.RadToDeg(math.Atan2(reflected[1], math.Hypot(reflected[0], reflected[2]))),
	}
	movement.rotChanged = true
	projectile.data.Rot = movement.rot
	b.tx.PlaySound(b.Position(), sound.Custom{Name: "entity.breeze.deflect", Volume: 1, Pitch: 1})
	return true
}

var _ Living = (*Breeze)(nil)
var _ world.EntityType = breezeType{}
