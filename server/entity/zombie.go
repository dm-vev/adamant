package entity

import (
	"encoding/binary"
	"math"
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	mobai "github.com/df-mc/dragonfly/server/entity/ai"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

const (
	zombieDefaultMaxHealth    = 20
	zombieDefaultSpeed        = 0.23
	zombieDefaultAttackDamage = 3
	zombieDefaultFollowRange  = 35
	zombieDefaultArmor        = 2

	zombieAttackIntervalTicks = 20
	zombieDoorBreakTicks      = 240

	zombieWidth                = 0.6
	zombieHeight               = 1.95
	zombieEyeHeight            = 1.74
	zombieBabyEyeHeightOffset  = 0.81
	zombieBabySpeedBoost       = 0.5
	zombieBreakDoorsChanceHard = 0.1

	zombieJumpVelocity   = 0.42
	zombieDefaultGravity = 0.08
	zombieDefaultDrag    = 0.02
	zombieWaterGravity   = 0.02
	zombieWaterDrag      = 0.2
	zombieLavaGravity    = 0.02
	zombieLavaDrag       = 0.5
)

// NewZombie creates a new zombie entity.
func NewZombie(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(ZombieType, ZombieConfig{})
}

// ZombieConfig configures a zombie entity.
type ZombieConfig struct {
	Health                    float64
	MaxHealth                 float64
	Speed                     float64
	AttackDamage              float64
	FollowRange               float64
	AttackRange               float64
	IsBaby                    *bool
	CanBreakDoors             *bool
	SpawnReinforcementsChance *float64
	Armor                     *float64
}

// Apply applies the zombie config to the entity data.
func (c ZombieConfig) Apply(data *world.EntityData) {
	maxHealth := c.MaxHealth
	if maxHealth <= 0 {
		maxHealth = zombieDefaultMaxHealth
	}
	health := c.Health
	if health <= 0 {
		health = maxHealth
	}
	speed := c.Speed
	if speed <= 0 {
		speed = zombieDefaultSpeed
	}
	attackDamage := c.AttackDamage
	if attackDamage <= 0 {
		attackDamage = zombieDefaultAttackDamage
	}
	followRange := c.FollowRange
	if followRange <= 0 {
		followRange = zombieDefaultFollowRange
	}
	attackRange := c.AttackRange
	if attackRange < 0 {
		attackRange = 0
	}
	armor := float64(zombieDefaultArmor)
	if c.Armor != nil {
		armor = max(*c.Armor, 0)
	}
	spawnChance := rand.Float64() * 0.1
	if c.SpawnReinforcementsChance != nil {
		spawnChance = min(max(*c.SpawnReinforcementsChance, 0), 1)
	}
	isBaby := false
	if c.IsBaby != nil {
		isBaby = *c.IsBaby
	}
	breakDoors := false
	breakDoorsSet := false
	if c.CanBreakDoors != nil {
		breakDoors = *c.CanBreakDoors
		breakDoorsSet = true
	}

	data.Data = &zombieData{
		health:                    NewHealthManager(health, maxHealth),
		speed:                     speed,
		speedMultiplier:           1,
		attackDamage:              attackDamage,
		followRange:               followRange,
		attackRange:               attackRange,
		armor:                     armor,
		spawnReinforcementsChance: spawnChance,
		isBaby:                    isBaby,
		breakDoors:                breakDoors,
		breakDoorsSet:             breakDoorsSet,
		effects:                   NewEffectManager(),
	}
}

type zombieData struct {
	health *HealthManager

	speed                     float64
	speedMultiplier           float64
	attackDamage              float64
	followRange               float64
	attackRange               float64
	armor                     float64
	spawnReinforcementsChance float64

	isBaby        bool
	breakDoors    bool
	breakDoorsSet bool
	armsRaised    bool
	breakingDoor  bool

	effects *EffectManager

	brain           *mobai.Brain
	brainState      *zombieBrain
	intent          mobai.Intent
	forcedTarget    *world.EntityHandle
	lastTarget      *world.EntityHandle
	raiseArmTicks   int
	breakDoorTicks  int
	breakDoorPos    cube.Pos
	breakDoorPosSet bool

	path          []cube.Pos
	pathIndex     int
	nextPathTick  int64
	lastPathTarget cube.Pos
	hasPathTarget bool
	lastSeenTick  int64
	lastSeenTarget *world.EntityHandle
	wantJump      bool
	knockbackTicks int
	lastMoveDir   mgl64.Vec3
	stuckTicks    int
	lastPos       mgl64.Vec3

	lastDamage  float64
	immuneUntil time.Time

	lastAttackTick int64
}

type zombieBrain struct {
	rng *rand.Rand

	nextWanderTick int64
	wanderDir      mgl64.Vec3
	idleUntilTick  int64
}

func newZombieBrain(seed uint64) *zombieBrain {
	// Spread the seed a little to avoid trivial consecutive values when entities are spawned in bursts.
	a := seed ^ 0x9e3779b97f4a7c15
	b := seed + 0xbf58476d1ce4e5b9
	return &zombieBrain{
		rng: rand.New(rand.NewPCG(a, b)),
	}
}

func (b *zombieBrain) Compute(s mobai.Snapshot) mobai.Intent {
	intent := mobai.Intent{ComputedAt: s.Tick}
	if s.HasNearestPlayer && s.NearestPlayer.Handle != nil {
		intent.Target = s.NearestPlayer.Handle
		intent.HasLookAt = true
		intent.LookAt = s.NearestPlayer.Pos

		diff := s.NearestPlayer.Pos.Sub(s.SelfPos)
		diff[1] = 0
		intent.MoveDir = diff
		intent.WantAttack = true
		return intent
	}

	if s.Tick < b.idleUntilTick {
		return intent
	}
	if s.Tick >= b.nextWanderTick || b.wanderDir.LenSqr() == 0 {
		// Pick a new random horizontal direction.
		angle := b.rng.Float64() * 2 * math.Pi
		b.wanderDir = mgl64.Vec3{math.Sin(angle), 0, math.Cos(angle)}
		// Wander for 2–4 seconds, then idle for 1–2 seconds.
		wanderTicks := int64(40 + b.rng.IntN(41))
		b.nextWanderTick = s.Tick + wanderTicks
		b.idleUntilTick = b.nextWanderTick + int64(20+b.rng.IntN(21))
	}
	intent.MoveDir = b.wanderDir
	intent.HasLookAt = true
	intent.LookAt = s.SelfPos.Add(b.wanderDir)
	return intent
}

// ZombieType is a world.EntityType implementation for zombies.
var ZombieType zombieType

type zombieType struct{}

func (t zombieType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	z := &Zombie{
		tx:     tx,
		handle: handle,
		data:   data,
	}
	zd, _ := data.Data.(*zombieData)
	if zd == nil {
		// Should not happen for entities spawned through ZombieConfig, but keep the type robust when loading
		// malformed NBT.
		conf := ZombieConfig{}
		conf.Apply(data)
		zd = data.Data.(*zombieData)
	}
	z.zd = zd

	if z.zd.effects == nil {
		z.zd.effects = NewEffectManager()
	}
	if z.zd.health == nil {
		z.zd.health = NewHealthManager(zombieDefaultMaxHealth, zombieDefaultMaxHealth)
	}
	if z.zd.speedMultiplier <= 0 {
		z.zd.speedMultiplier = 1
	}
	if !z.zd.breakDoorsSet && tx != nil && tx.World().Difficulty() == world.DifficultyHard {
		z.zd.breakDoors = rand.Float64() < zombieBreakDoorsChanceHard
	}

	if z.zd.brain == nil {
		id := handle.UUID()
		seed := binary.LittleEndian.Uint64(id[8:])
		z.zd.brainState = newZombieBrain(seed)
		z.zd.brain = mobai.NewBrain(nil, z.zd.brainState.Compute)
	}

	z.mc = &MovementComputer{Gravity: zombieDefaultGravity, Drag: zombieDefaultDrag, CollideEntities: true}
	return z
}

func (zombieType) EncodeEntity() string { return "minecraft:zombie" }

func (zombieType) BBox(e world.Entity) cube.BBox {
	width, height := zombieWidth, zombieHeight
	if z, ok := e.(*Zombie); ok && z.Baby() {
		width *= 0.5
		height *= 0.5
	}
	half := width / 2
	return cube.Box(-half, 0, -half, half, height, half)
}

func (zombieType) DecodeNBT(m map[string]any, data *world.EntityData) {
	maxHealth := float64(nbtconv.Float32(m, "MaxHealth"))
	if maxHealth <= 0 {
		maxHealth = zombieDefaultMaxHealth
	}
	health := float64(nbtconv.Float32(m, "Health"))
	if health <= 0 {
		health = maxHealth
	}
	speed := float64(nbtconv.Float32(m, "Speed"))
	if speed <= 0 {
		speed = zombieDefaultSpeed
	}
	attackDamage := float64(nbtconv.Float32(m, "AttackDamage"))
	if attackDamage <= 0 {
		attackDamage = zombieDefaultAttackDamage
	}
	followRange := float64(nbtconv.Float32(m, "FollowRange"))
	if followRange <= 0 {
		followRange = zombieDefaultFollowRange
	}
	attackRange := float64(nbtconv.Float32(m, "AttackRange"))
	if attackRange < 0 {
		attackRange = 0
	}
	armor := float64(nbtconv.Float32(m, "Armor"))
	if armor <= 0 {
		armor = zombieDefaultArmor
	}
	spawnChance := float64(nbtconv.Float32(m, "SpawnReinforcementsChance"))
	if _, ok := m["SpawnReinforcementsChance"]; !ok {
		spawnChance = rand.Float64() * 0.1
	} else {
		spawnChance = min(max(spawnChance, 0), 1)
	}
	isBaby := nbtconv.Bool(m, "IsBaby")
	breakDoors := nbtconv.Bool(m, "CanBreakDoors")
	_, breakDoorsSet := m["CanBreakDoors"]

	data.Data = &zombieData{
		health:                    NewHealthManager(health, maxHealth),
		speed:                     speed,
		speedMultiplier:           1,
		attackDamage:              attackDamage,
		followRange:               followRange,
		attackRange:               attackRange,
		armor:                     armor,
		spawnReinforcementsChance: spawnChance,
		isBaby:                    isBaby,
		breakDoors:                breakDoors,
		breakDoorsSet:             breakDoorsSet,
		effects:                   NewEffectManager(),
	}
}

func (zombieType) EncodeNBT(data *world.EntityData) map[string]any {
	z, ok := data.Data.(*zombieData)
	if !ok || z == nil || z.health == nil {
		return nil
	}
	return map[string]any{
		"Health":                    float32(z.health.Health()),
		"MaxHealth":                 float32(z.health.MaxHealth()),
		"Speed":                     float32(z.speed),
		"AttackDamage":              float32(z.attackDamage),
		"FollowRange":               float32(z.followRange),
		"AttackRange":               float32(z.attackRange),
		"Armor":                     float32(z.armor),
		"SpawnReinforcementsChance": float32(z.spawnReinforcementsChance),
		"IsBaby":                    boolByte(z.isBaby),
		"CanBreakDoors":             boolByte(z.breakDoors),
	}
}

// Zombie is a basic implementation of a hostile mob (zombie) with asynchronous AI decisions.
type Zombie struct {
	tx     *world.Tx
	handle *world.EntityHandle
	data   *world.EntityData

	zd *zombieData
	mc *MovementComputer
}

func (z *Zombie) H() *world.EntityHandle   { return z.handle }
func (z *Zombie) Position() mgl64.Vec3     { return z.data.Pos }
func (z *Zombie) Velocity() mgl64.Vec3     { return z.data.Vel }
func (z *Zombie) SetVelocity(v mgl64.Vec3) { z.data.Vel = v }
func (z *Zombie) Rotation() cube.Rotation  { return z.data.Rot }
func (z *Zombie) EyeHeight() float64 {
	if z.Baby() {
		return zombieEyeHeight - zombieBabyEyeHeightOffset
	}
	return zombieEyeHeight
}
func (z *Zombie) NameTag() string                            { return z.data.Name }
func (z *Zombie) Speed() float64                             { return z.movementSpeed() }
func (z *Zombie) SetSpeed(v float64)                         { z.zd.speed = max(v, 0) }
func (z *Zombie) Effects() []effect.Effect                   { return z.zd.effects.Effects() }
func (z *Zombie) Effect(t effect.Type) (effect.Effect, bool) { return z.zd.effects.Effect(t) }
func (z *Zombie) Baby() bool                                 { return z.zd.isBaby }
func (z *Zombie) CanBreakDoors() bool                        { return z.zd.breakDoors }
func (z *Zombie) ArmsRaised() bool                           { return z.zd.armsRaised }
func (z *Zombie) BreakingObstruction() bool                  { return z.zd.breakingDoor }

func (z *Zombie) SetBaby(b bool) {
	if z.zd.isBaby == b {
		return
	}
	z.zd.isBaby = b
	z.updateState()
}

func (z *Zombie) SetBreakDoors(b bool) {
	z.zd.breakDoorsSet = true
	if z.zd.breakDoors == b {
		return
	}
	z.zd.breakDoors = b
	z.updateState()
}

func (z *Zombie) updateState() {
	if z.tx == nil {
		return
	}
	viewers := z.tx.Viewers(z.Position())
	for _, v := range viewers {
		v.ViewEntityState(z)
	}
	z.tx.ReleaseViewers(viewers)
}

func (z *Zombie) movementSpeed() float64 {
	speed := z.zd.speed
	if z.zd.isBaby {
		speed *= 1 + zombieBabySpeedBoost
	}
	return speed
}

func (z *Zombie) setArmsRaised(raised bool) {
	if z.zd.armsRaised == raised {
		return
	}
	z.zd.armsRaised = raised
	z.updateState()
}

func (z *Zombie) setBreakingDoor(b bool) {
	if z.zd.breakingDoor == b {
		return
	}
	z.zd.breakingDoor = b
	z.updateState()
}

func (z *Zombie) resetAttackState() {
	z.zd.raiseArmTicks = 0
	z.zd.lastTarget = nil
	z.zd.path = nil
	z.zd.pathIndex = 0
	z.zd.hasPathTarget = false
	z.zd.wantJump = false
	z.setArmsRaised(false)
}

func (z *Zombie) SetNameTag(s string) {
	z.data.Name = s
	if z.tx == nil {
		return
	}
	viewers := z.tx.Viewers(z.Position())
	for _, v := range viewers {
		v.ViewEntityState(z)
	}
	z.tx.ReleaseViewers(viewers)
}

func (z *Zombie) OnFireDuration() time.Duration { return z.data.FireDuration }

func (z *Zombie) SetOnFire(duration time.Duration) {
	duration = max(duration, 0)
	stateChanged := (z.data.FireDuration > 0) != (duration > 0)
	z.data.FireDuration = duration
	if !stateChanged || z.tx == nil {
		return
	}
	viewers := z.tx.Viewers(z.Position())
	for _, v := range viewers {
		v.ViewEntityState(z)
	}
	z.tx.ReleaseViewers(viewers)
}

func (z *Zombie) Extinguish() { z.SetOnFire(0) }

func (z *Zombie) Health() float64    { return z.zd.health.Health() }
func (z *Zombie) MaxHealth() float64 { return z.zd.health.MaxHealth() }
func (z *Zombie) SetMaxHealth(v float64) {
	z.zd.health.SetMaxHealth(v)
	if z.Health() > z.MaxHealth() {
		z.zd.health.AddHealth(z.MaxHealth() - z.Health())
	}
}

func (z *Zombie) Dead() bool { return z.Health() <= mgl64.Epsilon }

func (z *Zombie) addHealth(delta float64) {
	before := z.Health()
	z.zd.health.AddHealth(delta)
	if z.tx == nil || before == z.Health() {
		return
	}
	viewers := z.tx.Viewers(z.Position())
	for _, v := range viewers {
		v.ViewEntityState(z)
	}
	z.tx.ReleaseViewers(viewers)
}

// FinalDamageFrom resolves the final damage taken after status effects like resistance.
func (z *Zombie) FinalDamageFrom(dmg float64, src world.DamageSource) float64 {
	dmg = max(dmg, 0)
	dmg = z.applyArmour(dmg, src)
	if res, ok := z.Effect(effect.Resistance); ok {
		dmg *= effect.Resistance.Multiplier(src, res.Level())
	}
	return dmg
}

func (z *Zombie) applyArmour(dmg float64, src world.DamageSource) float64 {
	if z.zd.armor <= 0 || !src.ReducedByArmour() {
		return dmg
	}
	reduction := min(20.0, max(z.zd.armor/5, z.zd.armor-dmg/2))
	if scaler, ok := src.(world.ArmourEffectivenessReducer); ok {
		reduction *= max(0, min(scaler.ArmourEffectivenessMultiplier(), 1))
	}
	return dmg * (1 - reduction/25)
}

func (z *Zombie) Hurt(dmg float64, src world.DamageSource) (float64, bool) {
	if z.Dead() || dmg < 0 {
		return 0, false
	}
	if fr, ok := z.Effect(effect.FireResistance); ok && src.Fire() && fr.Level() > 0 {
		return 0, false
	}

	total := z.FinalDamageFrom(dmg, src)
	damageLeft := total

	immune := time.Now().Before(z.zd.immuneUntil)
	if immune {
		if damageLeft = damageLeft - z.zd.lastDamage; damageLeft <= 0 {
			return 0, false
		}
	}
	z.zd.lastDamage = total
	z.zd.immuneUntil = time.Now().Add(time.Second / 2)

	z.addHealth(-damageLeft)
	if aggressor := z.aggressorFrom(src); aggressor != nil {
		if h := aggressor.H(); h != nil {
			z.zd.forcedTarget = h
		}
		if z.tx != nil {
			z.trySpawnReinforcement(z.tx, aggressor)
		}
	}

	if z.tx != nil {
		pos := z.Position()
		viewers := z.tx.Viewers(pos)
		for _, v := range viewers {
			v.ViewEntityAction(z, HurtAction{})
		}
		z.tx.ReleaseViewers(viewers)
		if src.Fire() {
			z.tx.PlaySound(pos, sound.Burning{})
		}
	}

	if z.Dead() {
		z.die(src)
	}
	return total, true
}

func (z *Zombie) Heal(health float64, _ world.HealingSource) {
	if z.Dead() || health <= 0 {
		return
	}
	z.addHealth(health)
}

func (z *Zombie) KnockBack(src mgl64.Vec3, force, height float64) {
	if z.Dead() {
		return
	}
	velocity := z.Position().Sub(src)
	velocity[1] = 0
	if velocity.Len() != 0 {
		velocity = velocity.Normalize().Mul(force)
	}
	velocity[1] = height
	z.SetVelocity(velocity)
	z.zd.knockbackTicks = 8
}

func (z *Zombie) AddEffect(e effect.Effect) {
	z.zd.effects.Add(e, z)
	if z.tx == nil {
		return
	}
	viewers := z.tx.Viewers(z.Position())
	for _, v := range viewers {
		v.ViewEntityState(z)
	}
	z.tx.ReleaseViewers(viewers)
}

func (z *Zombie) RemoveEffect(e effect.Type) {
	z.zd.effects.Remove(e, z)
	if z.tx == nil {
		return
	}
	viewers := z.tx.Viewers(z.Position())
	for _, v := range viewers {
		v.ViewEntityState(z)
	}
	z.tx.ReleaseViewers(viewers)
}

func (z *Zombie) Tick(tx *world.Tx, current int64) {
	z.tx = tx
	pos := z.Position()
	prevRot := z.data.Rot
	if pos[1] < float64(tx.Range()[0]) && current%10 == 0 {
		_ = z.CloseIn(tx)
		return
	}
	if tx.World().Difficulty() == world.DifficultyPeaceful {
		_ = z.CloseIn(tx)
		return
	}

	if z.data.FireDuration > 0 {
		z.data.FireDuration = max(z.data.FireDuration-time.Second/20, 0)
	}
	if z.data.FireDuration > 0 {
		if tx.RainingAt(cube.PosFromVec3(pos)) {
			z.Extinguish()
		} else if z.data.FireDuration%time.Second == 0 {
			z.Hurt(1, block.FireDamageSource{})
		}
	}
	z.handleDaylightBurn(tx)

	z.zd.effects.Tick(z, tx)

	// Update intent from async AI, then schedule the next computation.
	if intent, ok := z.zd.brain.Poll(); ok {
		z.zd.intent = intent
	}
	z.zd.brain.Request(z.snapshot(tx, current))

	z.applyEnvironment(tx)
	z.applyIntent(tx, current)
	z.handleDoorBreaking(tx)

	m := z.mc.TickMovement(z, z.data.Pos, z.data.Vel, z.data.Rot, prevRot, tx)
	z.data.Pos, z.data.Vel, z.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	z.applyJump(tx, m)
	z.applyEntityInsiders(tx)
	if !z.mc.CollideEntities {
		z.resolveEntityOverlap(tx)
	}
	if z.zd.intent.Target != nil && z.zd.lastMoveDir.LenSqr() > 0.0001 {
		delta := z.data.Pos.Sub(pos)
		delta[1] = 0
		if delta.LenSqr() < 0.0004 {
			z.zd.stuckTicks++
		} else {
			z.zd.stuckTicks = 0
		}
		if z.zd.stuckTicks > 10 {
			z.zd.nextPathTick = current
			z.zd.wantJump = true
			z.zd.stuckTicks = 0
		}
	} else {
		z.zd.stuckTicks = 0
	}
	z.zd.lastPos = z.data.Pos
	m.Send()

	z.data.Age += time.Second / 20
}

func (z *Zombie) snapshot(tx *world.Tx, current int64) mobai.Snapshot {
	pos := z.Position()
	vel := z.Velocity()

	var (
		nearestHandle *world.EntityHandle
		nearestPos    mgl64.Vec3
		nearestDistSq = math.MaxFloat64
	)

	maxDistSq := z.zd.followRange * z.zd.followRange
	shouldSearch := current%10 == 0
	if z.zd.lastTarget != nil {
		if ent, ok := z.zd.lastTarget.Entity(tx); ok {
			if living, ok := ent.(Living); ok && !living.Dead() {
				d := ent.Position().Sub(pos)
				distSq := d.LenSqr()
				if distSq <= maxDistSq {
					hasLOS := z.hasLineOfSight(tx, ent)
					if hasLOS || current-z.zd.lastSeenTick <= 60 {
						if hasLOS {
							z.zd.lastSeenTick = current
							z.zd.lastSeenTarget = z.zd.lastTarget
						}
						nearestDistSq = distSq
						nearestHandle = z.zd.lastTarget
						nearestPos = entityEyePos(ent)
					} else {
						z.zd.lastTarget = nil
					}
				} else {
					z.zd.lastTarget = nil
				}
			} else {
				z.zd.lastTarget = nil
			}
		} else {
			z.zd.lastTarget = nil
		}
	}
	if z.zd.forcedTarget != nil {
		if ent, ok := z.zd.forcedTarget.Entity(tx); ok {
			if living, ok := ent.(Living); ok && !living.Dead() {
				d := ent.Position().Sub(pos)
				distSq := d.LenSqr()
				if distSq <= maxDistSq {
					hasLOS := z.hasLineOfSight(tx, ent)
					if hasLOS || current-z.zd.lastSeenTick <= 60 {
						if hasLOS {
							z.zd.lastSeenTick = current
							z.zd.lastSeenTarget = z.zd.forcedTarget
						}
						if distSq < nearestDistSq {
							nearestDistSq = distSq
							nearestHandle = z.zd.forcedTarget
							nearestPos = entityEyePos(ent)
						}
					} else {
						z.zd.forcedTarget = nil
					}
				} else {
					z.zd.forcedTarget = nil
				}
			} else {
				z.zd.forcedTarget = nil
			}
		} else {
			z.zd.forcedTarget = nil
		}
	}
	if nearestHandle == nil && shouldSearch {
		for p := range tx.Players() {
			if gm, ok := p.(interface{ GameMode() world.GameMode }); ok && !gm.GameMode().AllowsTakingDamage() {
				continue
			}
			d := p.Position().Sub(pos)
			distSq := d.LenSqr()
			if distSq > maxDistSq || distSq >= nearestDistSq {
				continue
			}
			hasLOS := z.hasLineOfSight(tx, p)
			if !hasLOS {
				if z.zd.lastSeenTarget == nil || z.zd.lastSeenTarget != p.H() || current-z.zd.lastSeenTick > 60 {
					continue
				}
			}
			if hasLOS {
				z.zd.lastSeenTick = current
				z.zd.lastSeenTarget = p.H()
			}
			nearestDistSq = distSq
			nearestHandle = p.H()
			nearestPos = entityEyePos(p)
		}
	}

	s := mobai.Snapshot{
		Tick:    current,
		SelfPos: pos,
		SelfVel: vel,
	}
	if nearestHandle != nil {
		s.HasNearestPlayer = true
		s.NearestPlayer = mobai.EntitySnapshot{Handle: nearestHandle, Pos: nearestPos}
	}
	return s
}

func (z *Zombie) applyIntent(tx *world.Tx, current int64) {
	intent := z.zd.intent

	if intent.HasLookAt {
		diff := intent.LookAt.Sub(z.Position())
		if diff.LenSqr() > 0.0001 {
			yaw := mgl64.RadToDeg(-math.Atan2(diff[0], diff[2]))
			pitch := mgl64.RadToDeg(-math.Atan2(diff[1], math.Hypot(diff[0], diff[2])))
			z.data.Rot = cube.Rotation{yaw, pitch}
		}
	}

	if z.zd.knockbackTicks > 0 {
		z.zd.knockbackTicks--
		if intent.Target == nil {
			z.resetAttackState()
		}
		return
	}

	if intent.Target == nil {
		z.resetAttackState()
		moveDir := intent.MoveDir
		moveDir[1] = 0
		if moveDir.LenSqr() > 0 {
			moveDir = moveDir.Normalize()
		}
		z.zd.lastMoveDir = moveDir
		vel := z.Velocity()
		speed := z.movementSpeed() * z.zd.speedMultiplier
		vel[0] = moveDir[0] * speed
		vel[2] = moveDir[2] * speed
		z.SetVelocity(vel)
		return
	}
	target, ok := intent.Target.Entity(tx)
	if !ok {
		z.resetAttackState()
		return
	}
	living, ok := target.(Living)
	if !ok || living.Dead() {
		z.resetAttackState()
		return
	}
	moveDir := z.pathMoveDir(tx, current, target)
	if moveDir.LenSqr() > 0 {
		moveDir = moveDir.Normalize()
	}
	z.zd.lastMoveDir = moveDir
	vel := z.Velocity()
	speed := z.movementSpeed() * z.zd.speedMultiplier
	vel[0] = moveDir[0] * speed
	vel[2] = moveDir[2] * speed
	z.SetVelocity(vel)

	if z.zd.lastTarget != intent.Target {
		z.zd.lastTarget = intent.Target
		z.zd.raiseArmTicks = 0
		z.zd.hasPathTarget = false
		z.zd.nextPathTick = current
	}
	z.zd.raiseArmTicks++
	attackTick := zombieAttackIntervalTicks - int(current-z.zd.lastAttackTick)
	if attackTick < 0 {
		attackTick = 0
	}
	z.setArmsRaised(z.zd.raiseArmTicks >= 5 && attackTick < 10)

	diff := target.Position().Sub(z.Position())
	diff[1] = 0
	if diff.LenSqr() > z.attackReachSq(target) {
		return
	}
	if attackTick > 0 {
		return
	}
	z.zd.lastAttackTick = current

	viewers := tx.Viewers(z.Position())
	for _, v := range viewers {
		v.ViewEntityAction(z, SwingArmAction{})
	}
	tx.ReleaseViewers(viewers)

	living.Hurt(z.zd.attackDamage, world.DamageSource(AttackDamageSource{Attacker: z}))
	if z.OnFireDuration() > 0 {
		if flammable, ok := target.(Flammable); ok && rand.Float64() < 0.3 {
			flammable.SetOnFire(time.Second * 2)
		}
	}
}

func (z *Zombie) attackReachSq(target world.Entity) float64 {
	if z.zd.attackRange > 0 {
		return z.zd.attackRange * z.zd.attackRange
	}
	attackerWidth := z.H().Type().BBox(z).Width()
	targetWidth := target.H().Type().BBox(target).Width()
	reach := attackerWidth * 2
	return reach*reach + targetWidth
}

func (z *Zombie) hasLineOfSight(tx *world.Tx, target world.Entity) bool {
	from := entityEyePos(z)
	to := entityEyePos(target)
	dir := to.Sub(from)
	dist := dir.Len()
	if dist <= 0.001 {
		return true
	}
	step := dir.Normalize().Mul(0.3)
	steps := int(dist / 0.3)
	pos := from
	for i := 0; i < steps; i++ {
		pos = pos.Add(step)
		blockPos := cube.PosFromVec3(pos)
		if _, ok := tx.Liquid(blockPos); ok {
			continue
		}
		if blocksSight(tx, blockPos) {
			return false
		}
	}
	return true
}

func entityEyePos(e world.Entity) mgl64.Vec3 {
	pos := e.Position()
	if eh, ok := e.(interface{ EyeHeight() float64 }); ok {
		pos = pos.Add(mgl64.Vec3{0, eh.EyeHeight(), 0})
	} else {
		pos[1] += 1
	}
	return pos
}

func blocksSight(tx *world.Tx, pos cube.Pos) bool {
	b := tx.Block(pos)
	m := b.Model()
	for face := cube.Face(0); face < 6; face++ {
		if m.FaceSolid(pos, face, tx) {
			return true
		}
	}
	return false
}

func (z *Zombie) pathMoveDir(tx *world.Tx, current int64, target world.Entity) mgl64.Vec3 {
	targetPos := cube.PosFromVec3(target.Position())
	if z.shouldRepath(current, targetPos) {
		allowWater := false
		if _, ok := z.liquidInBox(tx); ok {
			allowWater = true
		}
		if _, ok := tx.Liquid(targetPos); ok {
			allowWater = true
		}
		allowDoors := z.zd.breakDoors
		start := cube.PosFromVec3(z.Position())
		z.zd.path = findPath(tx, start, targetPos, 512, allowWater, allowDoors)
		z.zd.pathIndex = 0
		z.zd.lastPathTarget = targetPos
		z.zd.hasPathTarget = true
		delay := int64(4 + rand.IntN(7))
		distSq := z.Position().Sub(target.Position()).LenSqr()
		if distSq > 1024 {
			delay += 10
		} else if distSq > 256 {
			delay += 5
		}
		if len(z.zd.path) == 0 {
			delay += 15
		}
		z.zd.nextPathTick = current + delay
	}

	if len(z.zd.path) == 0 || z.zd.pathIndex >= len(z.zd.path) {
		z.zd.wantJump = false
		return target.Position().Sub(z.Position())
	}
	next := z.zd.path[z.zd.pathIndex]
	selfPos := cube.PosFromVec3(z.Position())
	z.zd.wantJump = next.Y() > selfPos.Y()
	nextPos := next.Vec3Middle()
	if z.Position().Sub(nextPos).LenSqr() < 0.4*0.4 {
		z.zd.pathIndex++
		if z.zd.pathIndex >= len(z.zd.path) {
			return target.Position().Sub(z.Position())
		}
		next = z.zd.path[z.zd.pathIndex]
		nextPos = next.Vec3Middle()
	}
	return nextPos.Sub(z.Position())
}

func (z *Zombie) shouldRepath(current int64, target cube.Pos) bool {
	if !z.zd.hasPathTarget {
		return true
	}
	if current >= z.zd.nextPathTick {
		return true
	}
	if z.zd.pathIndex >= len(z.zd.path) {
		return true
	}
	if rand.Float64() < 0.05 {
		return true
	}
	dx := target.X() - z.zd.lastPathTarget.X()
	dy := target.Y() - z.zd.lastPathTarget.Y()
	dz := target.Z() - z.zd.lastPathTarget.Z()
	if dx*dx+dy*dy+dz*dz > 4 {
		return true
	}
	return false
}

func (z *Zombie) aggressorFrom(src world.DamageSource) world.Entity {
	switch s := src.(type) {
	case AttackDamageSource:
		return s.Attacker
	case MaceSmashDamageSource:
		return s.Attacker
	case ProjectileDamageSource:
		if s.Owner != nil {
			return s.Owner
		}
		return s.Projectile
	default:
		return nil
	}
}

func (z *Zombie) handleDaylightBurn(tx *world.Tx) {
	if z.zd.isBaby || !z.shouldBurnInDay() {
		return
	}
	if !z.isDaytime(tx.World().Time()) {
		return
	}
	eyePos := z.Position().Add(mgl64.Vec3{0, z.EyeHeight(), 0})
	eyeBlock := cube.PosFromVec3(eyePos)
	if tx.SkyLight(eyeBlock) < 12 || tx.RainingAt(eyeBlock) {
		return
	}
	brightness := float64(tx.SkyLight(eyeBlock)) / 15
	if brightness > 0.5 && rand.Float64()*30 < (brightness-0.4)*2 {
		z.SetOnFire(8 * time.Second)
	}
}

func (z *Zombie) shouldBurnInDay() bool {
	return true
}

func (z *Zombie) isDaytime(t int) bool {
	t %= world.TimeFull
	return t < world.TimeSunset || t >= world.TimeSunrise
}

func (z *Zombie) applyEnvironment(tx *world.Tx) {
	z.mc.Gravity = zombieDefaultGravity
	z.mc.Drag = zombieDefaultDrag
	z.zd.speedMultiplier = 1

	liquid, ok := z.liquidInBox(tx)
	if !ok {
		return
	}
	switch liquid.(type) {
	case block.Water:
		z.mc.Gravity = zombieWaterGravity
		z.mc.Drag = zombieWaterDrag
		z.zd.speedMultiplier = 0.7
		z.applyBuoyancy(0.04)
	case block.Lava:
		z.mc.Gravity = zombieLavaGravity
		z.mc.Drag = zombieLavaDrag
		z.zd.speedMultiplier = 0.25
		z.applyBuoyancy(0.02)
	default:
		z.mc.Gravity = zombieWaterGravity
		z.mc.Drag = zombieWaterDrag
		z.zd.speedMultiplier = 0.6
		z.applyBuoyancy(0.03)
	}
}

func (z *Zombie) applyBuoyancy(minRise float64) {
	vel := z.Velocity()
	if vel[1] < minRise {
		vel[1] = minRise
		z.SetVelocity(vel)
	}
}

func (z *Zombie) applyJump(tx *world.Tx, m *Movement) {
	if !z.mc.OnGround() {
		return
	}
	moveDir := z.zd.lastMoveDir
	moveDir[1] = 0
	if moveDir.LenSqr() < 0.01 {
		moveDir = z.data.Rot.Vec3()
		moveDir[1] = 0
		if moveDir.LenSqr() < 0.01 {
			return
		}
	}
	if !z.zd.wantJump && !z.canJumpAhead(tx, moveDir) {
		return
	}
	if _, ok := z.liquidInBox(tx); ok {
		return
	}
	if !z.hasHeadroom(tx) {
		return
	}
	vel := z.Velocity()
	if vel[1] >= zombieJumpVelocity {
		return
	}
	vel[1] = zombieJumpVelocity
	z.SetVelocity(vel)
	m.dvel = vel.Sub(m.vel)
	m.vel = vel
	z.zd.wantJump = false
}

func (z *Zombie) applyEntityInsiders(tx *world.Tx) {
	checkEntityInsiders(tx, z)
}

func (z *Zombie) resolveEntityOverlap(tx *world.Tx) {
	box := z.H().Type().BBox(z).Translate(z.Position())
	expanded := box.Grow(0.1)
	for e := range tx.EntitiesWithin(expanded) {
		if e == z {
			continue
		}
		l, ok := e.(Living)
		if !ok || l.Dead() {
			continue
		}
		other := e.H().Type().BBox(e).Translate(e.Position())
		if !box.IntersectsWith(other) {
			continue
		}
		dir := z.Position().Sub(e.Position())
		dir[1] = 0
		if dir.LenSqr() == 0 {
			dir = mgl64.Vec3{0.15, 0, 0}
		} else {
			dir = dir.Normalize().Mul(0.15)
		}
		z.data.Pos = z.data.Pos.Add(dir)
		vel := z.Velocity()
		vel[0] += dir[0]
		vel[2] += dir[2]
		z.SetVelocity(vel)
		box = z.H().Type().BBox(z).Translate(z.Position())
	}
}

func (z *Zombie) hasHeadroom(tx *world.Tx) bool {
	box := z.H().Type().BBox(z).Translate(z.Position()).Grow(-0.0001)
	upper := box.Translate(mgl64.Vec3{0, 1, 0})
	blocks := blockBBoxsAround(tx, upper)
	blocked := cube.AnyIntersections(blocks, upper)
	blockBBoxPool.Put(blocks[:0])
	return !blocked
}

func (z *Zombie) blockedAhead(tx *world.Tx, moveDir mgl64.Vec3) bool {
	face := faceFromMoveDir(moveDir)
	pos := cube.PosFromVec3(z.Position())
	front := pos.Side(face)
	blocked := len(tx.Block(front).Model().BBox(front, tx)) > 0
	if !blocked {
		return false
	}
	above := front.Side(cube.FaceUp)
	return len(tx.Block(above).Model().BBox(above, tx)) == 0
}

func (z *Zombie) canJumpAhead(tx *world.Tx, moveDir mgl64.Vec3) bool {
	face := faceFromMoveDir(moveDir)
	pos := cube.PosFromVec3(z.Position())
	front := pos.Side(face)
	if isPassable(tx, front) {
		return false
	}
	above := front.Side(cube.FaceUp)
	if !isPassable(tx, above) {
		return false
	}
	above2 := above.Side(cube.FaceUp)
	if !isPassable(tx, above2) {
		return false
	}
	return true
}

func (z *Zombie) liquidInBox(tx *world.Tx) (world.Liquid, bool) {
	box := z.H().Type().BBox(z).Translate(z.Position()).Grow(-0.0001)
	low, high := cube.PosFromVec3(box.Min()), cube.PosFromVec3(box.Max())
	for y := low[1]; y <= high[1]; y++ {
		for x := low[0]; x <= high[0]; x++ {
			for zed := low[2]; zed <= high[2]; zed++ {
				if l, ok := tx.Liquid(cube.Pos{x, y, zed}); ok {
					return l, true
				}
			}
		}
	}
	return nil, false
}

func (z *Zombie) handleDoorBreaking(tx *world.Tx) {
	if !z.zd.breakDoors || tx.World().Difficulty() != world.DifficultyHard {
		z.resetDoorBreaking()
		return
	}
	face, ok := z.breakDoorFace()
	if !ok {
		z.resetDoorBreaking()
		return
	}
	doorPos, door, ok := z.doorAtFace(tx, face)
	if !ok || door.Open {
		z.resetDoorBreaking()
		return
	}
	if z.Position().Sub(doorPos.Vec3Centre()).LenSqr() > 4 {
		z.resetDoorBreaking()
		return
	}
	if z.zd.breakDoorPosSet && doorPos != z.zd.breakDoorPos {
		z.zd.breakDoorTicks = 0
	}
	z.zd.breakDoorPos = doorPos
	z.zd.breakDoorPosSet = true
	z.zd.breakDoorTicks++
	z.setBreakingDoor(true)

	if z.zd.breakDoorTicks%20 == 0 {
		tx.PlaySound(doorPos.Vec3Centre(), sound.DoorCrash{})
	}
	if z.zd.breakDoorTicks >= zombieDoorBreakTicks {
		tx.SetBlock(doorPos, nil, nil)
		tx.SetBlock(doorPos.Side(cube.FaceUp), nil, nil)
		z.zd.breakDoorTicks = 0
		z.zd.breakDoorPosSet = false
		z.setBreakingDoor(false)
	}
}

func (z *Zombie) resetDoorBreaking() {
	if z.zd.breakDoorTicks == 0 && !z.zd.breakDoorPosSet {
		z.setBreakingDoor(false)
		return
	}
	z.zd.breakDoorTicks = 0
	z.zd.breakDoorPosSet = false
	z.setBreakingDoor(false)
}

func (z *Zombie) breakDoorFace() (cube.Face, bool) {
	moveDir := z.zd.lastMoveDir
	if moveDir.LenSqr() > 0.001 {
		return faceFromMoveDir(moveDir), true
	}
	return z.data.Rot.Direction().Face(), true
}

func (z *Zombie) doorAtFace(tx *world.Tx, face cube.Face) (cube.Pos, block.WoodDoor, bool) {
	pos := cube.PosFromVec3(z.Position()).Side(face)
	if doorPos, door, ok := doorAt(tx, pos); ok {
		return doorPos, door, true
	}
	return doorAt(tx, pos.Side(cube.FaceUp))
}

func doorAt(tx *world.Tx, pos cube.Pos) (cube.Pos, block.WoodDoor, bool) {
	door, ok := tx.Block(pos).(block.WoodDoor)
	if !ok {
		return cube.Pos{}, block.WoodDoor{}, false
	}
	if door.Top {
		pos = pos.Side(cube.FaceDown)
		door, ok = tx.Block(pos).(block.WoodDoor)
		if !ok {
			return cube.Pos{}, block.WoodDoor{}, false
		}
	}
	return pos, door, true
}

func faceFromMoveDir(dir mgl64.Vec3) cube.Face {
	if math.Abs(dir[0]) > math.Abs(dir[2]) {
		if dir[0] > 0 {
			return cube.FaceEast
		}
		return cube.FaceWest
	}
	if dir[2] > 0 {
		return cube.FaceSouth
	}
	return cube.FaceNorth
}

func (z *Zombie) trySpawnReinforcement(tx *world.Tx, aggressor world.Entity) {
	if aggressor == nil || z.zd.spawnReinforcementsChance <= 0 {
		return
	}
	if tx.World().Difficulty() != world.DifficultyHard {
		return
	}
	if rand.Float64() >= z.zd.spawnReinforcementsChance {
		return
	}
	base := cube.PosFromVec3(z.Position())
	for i := 0; i < 50; i++ {
		pos := cube.Pos{
			base.X() + randomReinforcementOffset(),
			base.Y() + randomReinforcementOffset(),
			base.Z() + randomReinforcementOffset(),
		}
		if !z.canSpawnReinforcementAt(tx, pos) {
			continue
		}
		handle := NewZombie(world.EntitySpawnOpts{Position: pos.Vec3()})
		ent := tx.AddEntity(handle)
		if nz, ok := ent.(*Zombie); ok {
			nz.zd.spawnReinforcementsChance = max(0, nz.zd.spawnReinforcementsChance-0.05)
			if h := aggressor.H(); h != nil {
				nz.zd.forcedTarget = h
			}
		}
		z.zd.spawnReinforcementsChance = max(0, z.zd.spawnReinforcementsChance-0.05)
		return
	}
}

func randomReinforcementOffset() int {
	amount := rand.IntN(34) + 7
	dir := rand.IntN(3) - 1
	return amount * dir
}

func (z *Zombie) canSpawnReinforcementAt(tx *world.Tx, pos cube.Pos) bool {
	if pos.OutOfBounds(tx.Range()) {
		return false
	}
	below := pos.Side(cube.FaceDown)
	if !tx.Block(below).Model().FaceSolid(below, cube.FaceUp, tx) {
		return false
	}
	if tx.Light(pos) >= 10 {
		return false
	}
	if z.playerWithinRange(tx, pos.Vec3(), 7) {
		return false
	}
	box := cube.Box(-zombieWidth/2, 0, -zombieWidth/2, zombieWidth/2, zombieHeight, zombieWidth/2).Translate(pos.Vec3())
	for e := range tx.EntitiesWithin(box) {
		if e != z {
			return false
		}
	}
	blocks := blockBBoxsAround(tx, box)
	blocked := cube.AnyIntersections(blocks, box)
	blockBBoxPool.Put(blocks[:0])
	if blocked {
		return false
	}
	if z.boxHasLiquid(tx, box) {
		return false
	}
	return true
}

func (z *Zombie) boxHasLiquid(tx *world.Tx, box cube.BBox) bool {
	min, max := box.Min(), box.Max()
	minX, minY, minZ := int(math.Floor(min[0])), int(math.Floor(min[1])), int(math.Floor(min[2]))
	maxX, maxY, maxZ := int(math.Ceil(max[0])), int(math.Ceil(max[1])), int(math.Ceil(max[2]))
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			for zed := minZ; zed <= maxZ; zed++ {
				if _, ok := tx.Liquid(cube.Pos{x, y, zed}); ok {
					return true
				}
			}
		}
	}
	return false
}

func (z *Zombie) playerWithinRange(tx *world.Tx, pos mgl64.Vec3, r float64) bool {
	r2 := r * r
	for p := range tx.Players() {
		if p.Position().Sub(pos).LenSqr() < r2 {
			return true
		}
	}
	return false
}

func (z *Zombie) die(src world.DamageSource) {
	if z.tx == nil {
		return
	}
	viewers := z.tx.Viewers(z.Position())
	for _, v := range viewers {
		v.ViewEntityAction(z, DeathAction{})
	}
	z.tx.ReleaseViewers(viewers)
	_ = z.CloseIn(z.tx)
}

func (z *Zombie) Close() error {
	if z.handle == nil {
		return nil
	}
	z.handle.ExecWorld(func(tx *world.Tx, _ world.Entity) {
		_ = z.CloseIn(tx)
	})
	return nil
}

func (z *Zombie) CloseIn(tx *world.Tx) error {
	if z.handle == nil {
		return nil
	}
	tx.RemoveEntity(z)
	_ = z.handle.Close()
	return nil
}
