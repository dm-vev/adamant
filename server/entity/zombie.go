package entity

import (
	"encoding/binary"
	"math"
	"math/rand/v2"
	"time"

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
	zombieDefaultSpeed        = 0.1
	zombieDefaultAttackDamage = 3
	zombieDefaultFollowRange  = 16
	zombieDefaultAttackRange  = 1.5

	zombieAttackIntervalTicks = 20
)

// NewZombie creates a new zombie entity.
func NewZombie(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(ZombieType, ZombieConfig{})
}

// ZombieConfig configures a zombie entity.
type ZombieConfig struct {
	Health       float64
	MaxHealth    float64
	Speed        float64
	AttackDamage float64
	FollowRange  float64
	AttackRange  float64
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
	if attackRange <= 0 {
		attackRange = zombieDefaultAttackRange
	}

	data.Data = &zombieData{
		health:       NewHealthManager(health, maxHealth),
		speed:        speed,
		attackDamage: attackDamage,
		followRange:  followRange,
		attackRange:  attackRange,
		effects:      NewEffectManager(),
	}
}

type zombieData struct {
	health *HealthManager

	speed        float64
	attackDamage float64
	followRange  float64
	attackRange  float64

	effects *EffectManager

	brain      *mobai.Brain
	brainState *zombieBrain
	intent     mobai.Intent

	lastDamage  float64
	immuneUntil time.Time

	lastAttackTick int64
}

type zombieBrain struct {
	rng *rand.Rand

	nextWanderTick int64
	wanderDir      mgl64.Vec3

	attackRangeSq float64
}

func newZombieBrain(seed uint64, attackRange float64) *zombieBrain {
	// Spread the seed a little to avoid trivial consecutive values when entities are spawned in bursts.
	a := seed ^ 0x9e3779b97f4a7c15
	b := seed + 0xbf58476d1ce4e5b9
	return &zombieBrain{
		rng:           rand.New(rand.NewPCG(a, b)),
		attackRangeSq: attackRange * attackRange,
	}
}

func (b *zombieBrain) Compute(s mobai.Snapshot) mobai.Intent {
	intent := mobai.Intent{ComputedAt: s.Tick}
	if s.HasNearestPlayer && s.NearestPlayer.Handle != nil {
		intent.Target = s.NearestPlayer.Handle
		intent.HasLookAt = true
		intent.LookAt = s.NearestPlayer.Pos

		diff := s.NearestPlayer.Pos.Sub(s.SelfPos)
		distSq := diff.LenSqr()

		diff[1] = 0
		intent.MoveDir = diff
		intent.WantAttack = distSq > 0 && distSq <= b.attackRangeSq
		return intent
	}

	if s.Tick >= b.nextWanderTick || b.wanderDir.LenSqr() == 0 {
		// Pick a new random horizontal direction.
		angle := b.rng.Float64() * 2 * math.Pi
		b.wanderDir = mgl64.Vec3{math.Sin(angle), 0, math.Cos(angle)}
		// Wander for 2–4 seconds.
		b.nextWanderTick = s.Tick + int64(40+b.rng.IntN(41))
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

	if z.zd.brain == nil {
		id := handle.UUID()
		seed := binary.LittleEndian.Uint64(id[8:])
		z.zd.brainState = newZombieBrain(seed, z.zd.attackRange)
		z.zd.brain = mobai.NewBrain(nil, z.zd.brainState.Compute)
	}

	z.mc = &MovementComputer{Gravity: 0.08, Drag: 0.02}
	return z
}

func (zombieType) EncodeEntity() string { return "minecraft:zombie" }

func (zombieType) BBox(world.Entity) cube.BBox {
	// Default adult zombie size: 0.6 × 1.8.
	return cube.Box(-0.3, 0, -0.3, 0.3, 1.8, 0.3)
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
	if attackRange <= 0 {
		attackRange = zombieDefaultAttackRange
	}

	data.Data = &zombieData{
		health:       NewHealthManager(health, maxHealth),
		speed:        speed,
		attackDamage: attackDamage,
		followRange:  followRange,
		attackRange:  attackRange,
		effects:      NewEffectManager(),
	}
}

func (zombieType) EncodeNBT(data *world.EntityData) map[string]any {
	z, ok := data.Data.(*zombieData)
	if !ok || z == nil || z.health == nil {
		return nil
	}
	return map[string]any{
		"Health":       float32(z.health.Health()),
		"MaxHealth":    float32(z.health.MaxHealth()),
		"Speed":        float32(z.speed),
		"AttackDamage": float32(z.attackDamage),
		"FollowRange":  float32(z.followRange),
		"AttackRange":  float32(z.attackRange),
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

func (z *Zombie) H() *world.EntityHandle                     { return z.handle }
func (z *Zombie) Position() mgl64.Vec3                       { return z.data.Pos }
func (z *Zombie) Velocity() mgl64.Vec3                       { return z.data.Vel }
func (z *Zombie) SetVelocity(v mgl64.Vec3)                   { z.data.Vel = v }
func (z *Zombie) Rotation() cube.Rotation                    { return z.data.Rot }
func (z *Zombie) EyeHeight() float64                         { return 1.74 }
func (z *Zombie) NameTag() string                            { return z.data.Name }
func (z *Zombie) Speed() float64                             { return z.zd.speed }
func (z *Zombie) SetSpeed(v float64)                         { z.zd.speed = max(v, 0) }
func (z *Zombie) Effects() []effect.Effect                   { return z.zd.effects.Effects() }
func (z *Zombie) Effect(t effect.Type) (effect.Effect, bool) { return z.zd.effects.Effect(t) }

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
	if res, ok := z.Effect(effect.Resistance); ok {
		dmg *= effect.Resistance.Multiplier(src, res.Level())
	}
	return dmg
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
	if pos[1] < float64(tx.Range()[0]) && current%10 == 0 {
		_ = z.CloseIn(tx)
		return
	}

	if z.data.FireDuration > 0 {
		z.data.FireDuration = max(z.data.FireDuration-time.Second/20, 0)
	}

	z.zd.effects.Tick(z, tx)

	// Update intent from async AI, then schedule the next computation.
	if intent, ok := z.zd.brain.Poll(); ok {
		z.zd.intent = intent
	}
	z.zd.brain.Request(z.snapshot(tx, current))

	z.applyIntent(tx, current)

	m := z.mc.TickMovement(z, z.data.Pos, z.data.Vel, z.data.Rot, tx)
	z.data.Pos, z.data.Vel, z.data.Rot = m.Position(), m.Velocity(), m.Rotation()
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
	for p := range tx.Players() {
		d := p.Position().Sub(pos)
		distSq := d.LenSqr()
		if distSq > maxDistSq || distSq >= nearestDistSq {
			continue
		}
		nearestDistSq = distSq
		nearestHandle = p.H()
		nearestPos = p.Position()
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
		yaw := mgl64.RadToDeg(math.Atan2(diff[0], diff[2]))
		pitch := mgl64.RadToDeg(math.Atan2(diff[1], math.Hypot(diff[0], diff[2])))
		z.data.Rot = cube.Rotation{yaw, pitch}
	}

	moveDir := intent.MoveDir
	moveDir[1] = 0
	if moveDir.LenSqr() > 0 {
		moveDir = moveDir.Normalize()
	}

	vel := z.Velocity()
	vel[0] = moveDir[0] * z.zd.speed
	vel[2] = moveDir[2] * z.zd.speed
	z.SetVelocity(vel)

	if intent.Target == nil {
		return
	}
	target, ok := intent.Target.Entity(tx)
	if !ok {
		return
	}
	living, ok := target.(Living)
	if !ok || living.Dead() {
		return
	}
	diff := target.Position().Sub(z.Position())
	diff[1] = 0
	if diff.LenSqr() > z.zd.attackRange*z.zd.attackRange {
		return
	}
	if !intent.WantAttack {
		return
	}
	if current-z.zd.lastAttackTick < zombieAttackIntervalTicks {
		return
	}
	z.zd.lastAttackTick = current

	viewers := tx.Viewers(z.Position())
	for _, v := range viewers {
		v.ViewEntityAction(z, SwingArmAction{})
	}
	tx.ReleaseViewers(viewers)

	living.Hurt(z.zd.attackDamage, world.DamageSource(AttackDamageSource{Attacker: z}))
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
