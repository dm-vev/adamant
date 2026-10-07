package entity

import (
	"hash/fnv"
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
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
	state := data.Data.(*breezeState)
	if handle != nil {
		uuid := handle.UUID()
		hash := fnv.New64a()
		_, _ = hash.Write(uuid[:])
		state.random = java26.NewLegacyRandom(int64(hash.Sum64()))
	}
	return &Breeze{Ent: &Ent{tx: tx, handle: handle, data: data}}
}

func (breezeType) EncodeEntity() string { return "minecraft:breeze" }
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
	if java := ReadNativeMobJavaState(n); java != nil {
		conf.shootCooldown = int(nbtconv.Int32(java, "ShootCooldown"))
		conf.chargeTicks = int(nbtconv.Int32(java, "ChargeTicks"))
		conf.jumpCooldown = int(nbtconv.Int32(java, "JumpCooldown"))
		conf.jumpCharge = int(nbtconv.Int32(java, "JumpCharge"))
		conf.jumping = nbtconv.Bool(java, "Jumping")
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
	if native == nil { native = make(map[string]any) }
	native["Attributes"] = attrs
	return MergeNativeMobJavaState(native, map[string]any{
		"ShootCooldown": int32(b.shootCooldown), "ChargeTicks": int32(b.chargeTicks),
		"JumpCooldown": int32(b.jumpCooldown), "JumpCharge": int32(b.jumpCharge), "Jumping": b.jumping,
	})
}

type breezeConfig struct {
	health float64
	healthSet bool
	speed float64
	shootCooldown, chargeTicks, jumpCooldown, jumpCharge int
	jumping bool
}
func (c breezeConfig) Apply(data *world.EntityData) { data.Data = c.New() }
func (c breezeConfig) New() *breezeState {
	health := c.health
	if !c.healthSet || health <= 0 { health = 30 }
	speed := c.speed
	if speed <= 0 { speed = .63 }
	return &breezeState{
		health: NewHealthManager(health, 30), effects: NewEffectManager(),
		mc: &MovementComputer{Gravity: .08, Drag: .02, DragBeforeGravity: true},
		random: java26.NewLegacyRandom(0), speed: speed,
		shootCooldown: max(0, c.shootCooldown), chargeTicks: max(0, c.chargeTicks),
		jumpCooldown: max(0, c.jumpCooldown), jumpCharge: max(0, c.jumpCharge), jumping: c.jumping,
	}
}

type breezeState struct {
	health *HealthManager
	effects *EffectManager
	mc *MovementComputer
	random java26.Random
	speed float64
	shootCooldown, chargeTicks, jumpCooldown, jumpCharge, soundTick int
	wander int
	path []cube.Pos
	pathIndex int
	jumping, dead bool
}

type Breeze struct{ *Ent }

// NewBreeze creates a Breeze with the source attributes and no equipment.
func NewBreeze(opts world.EntitySpawnOpts) *world.EntityHandle { return opts.New(BreezeType, breezeConfig{}) }
func (*Breeze) SpeciesType() world.EntityType { return BreezeType }
func (b *Breeze) state() *breezeState { return b.Behaviour().(*breezeState) }
func (b *Breeze) Tick(tx *world.Tx, current int64) { b.state().effects.Tick(b, tx); b.Ent.Tick(tx, current) }

func (s *breezeState) Tick(e *Ent, tx *world.Tx) *Movement {
	if s.soundTick == 0 {
		s.soundTick = 1 + int(s.random.NextInt(80))
		tx.PlaySound(e.Position(), sound.Custom{Name: "entity.breeze.whirl", Volume: .8 + .2*float64(s.random.NextFloat()), Pitch: .7 + .4*float64(s.random.NextFloat())})
	} else {
		s.soundTick--
	}

	if s.shootCooldown > 0 { s.shootCooldown-- }
	if s.jumpCooldown > 0 { s.jumpCooldown-- }
	if s.jumping {
		return s.jumpTick(e, tx)
	}

	var target world.Entity
	best := math.MaxFloat64
	area := cube.Box(-24, -8, -24, 24, 8, 24).Translate(e.Position())
	for candidate := range tx.EntitiesWithin(area) {
		if candidate == e { continue }
		id := candidate.H().Type().EncodeEntity()
		if id != "minecraft:player" && id != "minecraft:iron_golem" { continue }
		living, ok := candidate.(Living)
		if !ok || living.Dead() { continue }
		distance := candidate.Position().Sub(e.Position()).Len()
		if distance < best && distance < 24 { target, best = candidate, distance }
	}
	if target == nil {
		s.chargeTicks, s.jumpCharge = 0, 0
		s.wanderTick(e, tx)
		return nil
	}
	if best <= 16 && s.shootCooldown == 0 {
		if s.chargeTicks == 0 { tx.PlaySound(e.Position(), sound.Custom{Name: "entity.breeze.inhale", Volume: 1, Pitch: 1}) }
		s.chargeTicks++
		if s.chargeTicks >= 15 {
			s.shoot(e, tx, target)
			s.chargeTicks, s.shootCooldown = 0, 14 // Four recovery ticks plus the ten-tick attack cooldown.
		}
		return s.move(e, tx, target.Position(), .6)
	}
	s.chargeTicks = 0
	if best > 4 && best < 24 && s.jumpCooldown == 0 && s.mc.OnGround() {
		if s.jumpCharge == 0 { tx.PlaySound(e.Position(), sound.Custom{Name: "entity.breeze.inhale", Volume: 1, Pitch: 1}) }
		s.jumpCharge++

		if s.jumpCharge >= 10 {
			direction := target.Position().Sub(e.Position())
			direction[1] = 0
			if direction.Len() > 0 {
				direction = direction.Normalize()
				e.SetVelocity(mgl64.Vec3{direction[0] * .9, .5, direction[2] * .9})
				s.jumping, s.jumpCharge = true, 0
				tx.PlaySound(e.Position(), sound.Custom{Name: "entity.breeze.jump", Volume: 1, Pitch: 1})
				return s.jumpTick(e, tx)
			}
		}
	} else { s.jumpCharge = 0 }
	if best > 2.5 { return s.move(e, tx, target.Position(), .6) }
	return s.stop(e, tx)
}

func (s *breezeState) jumpTick(e *Ent, tx *world.Tx) *Movement {
	movement := s.mc.TickMovement(e, e.Position(), e.Velocity(), e.Rotation(), e.Rotation(), tx)
	e.data.Pos, e.data.Vel, e.data.Rot = movement.Position(), movement.Velocity(), movement.Rotation()
	if s.mc.OnGround() {
		s.jumping = false
		s.jumpCooldown = 10
		s.shootCooldown = max(s.shootCooldown, 100)
		tx.PlaySound(e.Position(), sound.Custom{Name: "entity.breeze.land", Volume: 1, Pitch: 1})
	}
	return movement
}

func (s *breezeState) shoot(e *Ent, tx *world.Tx, target world.Entity) {
	origin := e.Position().Add(mgl64.Vec3{0, 1.185, 0})
	direction := target.Position().Add(mgl64.Vec3{0, .3, 0}).Sub(origin)
	if direction.Len() == 0 { return }
	difficultyID, _ := world.DifficultyID(tx.LocalDifficulty(cube.PosFromVec3(e.Position())).Base)
	uncertainty := 5 - difficultyID*4
	spread := float64(uncertainty) * .0075
	direction[0] += s.random.NextGaussian() * spread
	direction[1] += s.random.NextGaussian() * spread
	direction[2] += s.random.NextGaussian() * spread
	velocity := direction.Normalize().Mul(.7)
	tx.AddEntity(NewBreezeWindCharge(world.EntitySpawnOpts{Position: origin, Velocity: velocity}, e))
	tx.PlaySound(e.Position(), sound.Custom{Name: "entity.breeze.shoot", Volume: 1.5, Pitch: 1})
}

func (s *breezeState) move(e *Ent, tx *world.Tx, target mgl64.Vec3, speed float64) *Movement {
	direction := target.Sub(e.Position())
	direction[1] = 0
	if direction.Len() > 0 { direction = direction.Normalize() }
	velocity := e.Velocity()
	velocity[0], velocity[2] = direction[0]*speed, direction[2]*speed
	rotation := e.Rotation()
	if direction.Len() > 0 { rotation[0] = math.Atan2(-direction[0], direction[2]) * 180 / math.Pi }
	movement := s.mc.TickMovement(e, e.Position(), velocity, e.Rotation(), rotation, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = movement.Position(), movement.Velocity(), movement.Rotation()
	return movement
}
func (s *breezeState) stop(e *Ent, tx *world.Tx) *Movement {
	velocity := e.Velocity(); velocity[0], velocity[2] = 0, 0
	movement := s.mc.TickMovement(e, e.Position(), velocity, e.Rotation(), e.Rotation(), tx)
	e.data.Pos, e.data.Vel, e.data.Rot = movement.Position(), movement.Velocity(), movement.Rotation()
	return movement
}
func (s *breezeState) wanderTick(e *Ent, tx *world.Tx) {
	if s.wander > 0 { s.wander-- }
	if s.pathIndex >= len(s.path) && s.wander == 0 {
		s.wander = 50
		start := cube.PosFromVec3(e.Position())
		goal := start.Add(cube.Pos{int(s.random.NextInt(11)) - 5, 0, int(s.random.NextInt(11)) - 5})
		s.path = findPath(tx, start, goal, 100, false, false)
		s.pathIndex = 0
	}
	if s.pathIndex < len(s.path) {
		goal := s.path[s.pathIndex].Vec3().Add(mgl64.Vec3{.5, 0, .5})
		if goal.Sub(e.Position()).Len() < .3 { s.pathIndex++; return }
		s.move(e, tx, goal, .6)
	} else { s.stop(e, tx) }
}

func (b *Breeze) Health() float64 { return b.state().health.Health() }
func (b *Breeze) MaxHealth() float64 { return b.state().health.MaxHealth() }
func (b *Breeze) SetMaxHealth(v float64) { b.state().health.SetMaxHealth(v) }
func (b *Breeze) Dead() bool { return b.state().dead || b.Health() <= 0 }
func (b *Breeze) Hurt(damage float64, src world.DamageSource) (float64, bool) {
	if b.Dead() || damage <= 0 { return 0, false }
	switch source := src.(type) {
	case AttackDamageSource:
		if source.Attacker != nil && source.Attacker.H().Type().EncodeEntity() == "minecraft:breeze" { return 0, false }
	case ProjectileDamageSource:
		if source.Owner != nil && source.Owner.H().Type().EncodeEntity() == "minecraft:breeze" { return 0, false }
	}
	s := b.state()
	if resistance, ok := s.effects.Effect(effect.Resistance); ok { damage *= effect.Resistance.Multiplier(src, resistance.Level()) }
	s.health.AddHealth(-damage)
	b.viewAction(HurtAction{})
	if b.Dead() {
		s.dead = true
		b.viewAction(DeathAction{})
		killer := breezeDamageAttacker(src)
		if killer != nil && killer.H().Type().EncodeEntity() == "minecraft:player" {
			n := 1 + int(s.random.NextInt(2))
			b.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: b.Position()}, item.NewStack(item.BreezeRod{}, n)))
			for _, orb := range NewExperienceOrbs(b.Position(), 10) { b.tx.AddEntity(orb) }
		}
		b.tx.PlaySound(b.Position(), sound.Custom{Name: "entity.breeze.death", Volume: 1, Pitch: 1})
		_ = b.CloseIn(b.tx)
	} else {
		b.tx.PlaySound(b.Position(), sound.Custom{Name: "entity.breeze.hurt", Volume: 1, Pitch: 1})
	}
	return damage, true
}

func breezeDamageAttacker(src world.DamageSource) world.Entity {
	switch source := src.(type) {
	case AttackDamageSource:
		return source.Attacker
	case SpearDamageSource:
		return source.Attacker
	case MaceSmashDamageSource:
		return source.Attacker
	case ProjectileDamageSource:
		return source.Owner
	}
	return nil
}

func (b *Breeze) Heal(amount float64, _ world.HealingSource) float64 {
	if amount <= 0 || b.Dead() { return 0 }
	old := b.Health(); b.state().health.AddHealth(amount); return b.Health() - old
}
func (b *Breeze) KnockBack(source mgl64.Vec3, force, height float64) {
	direction := b.Position().Sub(source); direction[1] = 0
	if direction.Len() > 0 { direction = direction.Normalize().Mul(force) }
	direction[1] = height; b.SetVelocity(direction)
}
func (b *Breeze) AddEffect(e effect.Effect) { b.state().effects.Add(e, b) }
func (b *Breeze) RemoveEffect(t effect.Type) { b.state().effects.Remove(t, b) }
func (b *Breeze) Effects() []effect.Effect { return b.state().effects.Effects() }
func (b *Breeze) Speed() float64 { return b.state().speed }
func (b *Breeze) SetSpeed(speed float64) { b.state().speed = speed }
func (b *Breeze) viewAction(action world.EntityAction) { viewers := b.tx.Viewers(b.Position()); for _, viewer := range viewers { viewer.ViewEntityAction(b, action) }; b.tx.ReleaseViewers(viewers) }

// DeflectProjectile reverses incoming projectiles except both wind-charge types, as in Breeze.deflection().
func (b *Breeze) DeflectProjectile(projectile *Ent, movement *Movement, velocity mgl64.Vec3) bool {
	if projectile.H().Type().EncodeEntity() == "minecraft:breeze_wind_charge" || projectile.H().Type().EncodeEntity() == "minecraft:wind_charge_projectile" { return false }
	behaviour, ok := projectile.Behaviour().(*ProjectileBehaviour)
	if !ok { return false }
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
