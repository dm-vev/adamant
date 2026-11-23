package api

import (
	"math"
	"slices"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// GoalSpec declaratively defines an AI goal without implementing every method
// of entity.Goal manually.
type GoalSpec struct {
	Flags         entity.GoalFlag
	CanStart      func(m *entity.Mob) bool
	Start         func(m *entity.Mob)
	CanContinue   func(m *entity.Mob) bool
	Tick          func(m *entity.Mob)
	Stop          func(m *entity.Mob)
	Interruptable *bool
	EveryTick     bool
}

// CustomGoal implements entity.Goal and all optional Goal hooks using GoalSpec.
type CustomGoal struct {
	spec GoalSpec
}

// NewGoal creates a *CustomGoal from spec.
func NewGoal(spec GoalSpec) *CustomGoal {
	return &CustomGoal{spec: spec}
}

func (g *CustomGoal) Flags() entity.GoalFlag { return g.spec.Flags }
func (g *CustomGoal) CanUse(m *entity.Mob) bool {
	return g.spec.CanStart != nil && g.spec.CanStart(m)
}
func (g *CustomGoal) CanContinueToUse(m *entity.Mob) bool {
	if g.spec.CanContinue != nil {
		return g.spec.CanContinue(m)
	}
	return g.CanUse(m)
}
func (g *CustomGoal) IsInterruptable() bool {
	if g.spec.Interruptable != nil {
		return *g.spec.Interruptable
	}
	return true
}
func (g *CustomGoal) Start(m *entity.Mob) {
	if g.spec.Start != nil {
		g.spec.Start(m)
	}
}
func (g *CustomGoal) Stop(m *entity.Mob) {
	if g.spec.Stop != nil {
		g.spec.Stop(m)
	}
}
func (g *CustomGoal) Tick(m *entity.Mob) {
	if g.spec.Tick != nil {
		g.spec.Tick(m)
	}
}
func (g *CustomGoal) RequiresUpdateEveryTick() bool { return g.spec.EveryTick }

// GoalEntry pairs an entity.Goal with its selector priority.
type GoalEntry struct {
	Priority int
	Goal     entity.Goal
}

// GoalAdder is satisfied by *entity.Ent, *entity.Mob, *entity.MobAI, vanilla
// mobs, and *CustomEntity.
type GoalAdder interface {
	AddGoal(priority int, g entity.Goal)
	AddTargetGoal(priority int, g entity.Goal)
}

// AddGoal registers g in mob's goal selector at priority.
func AddGoal(mob GoalAdder, priority int, g entity.Goal) {
	if mob == nil || g == nil {
		return
	}
	mob.AddGoal(priority, g)
}

// AddTargetGoal registers g in mob's target goal selector at priority.
func AddTargetGoal(mob GoalAdder, priority int, g entity.Goal) {
	if mob == nil || g == nil {
		return
	}
	mob.AddTargetGoal(priority, g)
}

// CustomEntitySpec declaratively configures a custom entity type.
type CustomEntitySpec struct {
	// Name is the entity type identifier (e.g. "myplugin:guard").
	Name string
	// NetworkType overrides the Bedrock actor identifier sent to clients
	// (defaults to Name when empty).
	NetworkType string
	// BBox sets the entity's bounding box when BBoxFunc is nil.
	BBox cube.BBox
	// BBoxFunc dynamically supplies the entity's bounding box when set.
	BBoxFunc func(e world.Entity) cube.BBox
	// SpawnOpts supplies default EntitySpawnOpts merged into Spawn calls.
	SpawnOpts world.EntitySpawnOpts
	// MaxHealth enables Living entity behaviour (Health, Hurt, Heal, Effects)
	// when > 0.
	MaxHealth float64
	// Speed sets the initial MobAI movement speed (defaults to 0.25 when <= 0).
	Speed float64
	// CanDespawn allows natural distance despawning when false by default.
	CanDespawn bool
	// EnableAI enables MobAI ticking even before goals are added dynamically.
	EnableAI bool
	// Goals lists initial AI goals registered on spawn.
	Goals []GoalEntry
	// TargetGoals lists initial AI target goals registered on spawn.
	TargetGoals []GoalEntry

	OnTick     func(e *CustomEntity, tx *world.Tx)
	OnInteract func(e *CustomEntity, tx *world.Tx, user item.User, ctx *item.UseContext) bool
	OnAttack   func(e *CustomEntity, attacker world.Entity, tx *world.Tx)
	OnMeta     func(e world.Entity, patch *world.ActorMetaPatch)
}

// CustomEntityType implements world.EntityType for declarative custom entities.
type CustomEntityType struct {
	spec CustomEntitySpec
}

// NewCustomEntityType creates a reusable *CustomEntityType from spec.
func NewCustomEntityType(spec CustomEntitySpec) *CustomEntityType {
	if spec.Name == "" {
		panic("api: CustomEntitySpec.Name must not be empty")
	}
	if spec.Speed <= 0 {
		spec.Speed = 0.25
	}
	spec.Goals = slices.Clone(spec.Goals)
	spec.TargetGoals = slices.Clone(spec.TargetGoals)
	return &CustomEntityType{spec: spec}
}

func (t *CustomEntityType) EncodeEntity() string { return t.spec.Name }

// NetworkEncodeEntity returns the Bedrock actor identifier for this type.
func (t *CustomEntityType) NetworkEncodeEntity() string {
	if t.spec.NetworkType != "" {
		return t.spec.NetworkType
	}
	return t.spec.Name
}

func (t *CustomEntityType) BBox(e world.Entity) cube.BBox {
	if t.spec.BBoxFunc != nil {
		return t.spec.BBoxFunc(e)
	}
	return t.spec.BBox
}

func (t *CustomEntityType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	if data.Data == nil {
		t.Config().Apply(data)
	}
	beh := data.Data.(*customEntityBehaviour)
	beh.tx = tx
	ce := &CustomEntity{
		Ent: entity.Open(tx, handle, data),
		typ: t,
		beh: beh,
	}
	if beh.health != nil {
		return &LivingCustomEntity{CustomEntity: ce}
	}
	return ce
}

func (t *CustomEntityType) DecodeNBT(_ map[string]any, data *world.EntityData) {
	t.Config().Apply(data)
}

func (t *CustomEntityType) EncodeNBT(*world.EntityData) map[string]any { return nil }

// Config returns the world.EntityConfig initializing instances of t.
func (t *CustomEntityType) Config() world.EntityConfig {
	return customEntityConfig{typ: t}
}

// Spawn creates a worldless *world.EntityHandle for this custom entity type.
func (t *CustomEntityType) Spawn(opts ...world.EntitySpawnOpts) *world.EntityHandle {
	merged := t.spec.SpawnOpts
	if len(opts) > 0 {
		merged = mergeSpawnOpts(merged, opts[0])
	}
	return merged.New(t, t.Config())
}

func mergeSpawnOpts(base, over world.EntitySpawnOpts) world.EntitySpawnOpts {
	if over.Position != (mgl64.Vec3{}) {
		base.Position = over.Position
	}
	if over.Rotation != (cube.Rotation{}) {
		base.Rotation = over.Rotation
	}
	if over.Velocity != (mgl64.Vec3{}) {
		base.Velocity = over.Velocity
	}
	if over.ID != ([16]byte{}) {
		base.ID = over.ID
	}
	if over.NameTag != "" {
		base.NameTag = over.NameTag
	}
	if over.SpawnReason != "" {
		base.SpawnReason = over.SpawnReason
	}
	if over.Riding != nil {
		base.Riding = over.Riding
	}
	return base
}

type customEntityConfig struct {
	typ *CustomEntityType
}

func (c customEntityConfig) Apply(data *world.EntityData) {
	data.Data = c.typ.newBehaviour()
}

func (t *CustomEntityType) newBehaviour() *customEntityBehaviour {
	b := &customEntityBehaviour{
		typ:      t,
		aiActive: t.spec.EnableAI || len(t.spec.Goals) > 0 || len(t.spec.TargetGoals) > 0,
		speed:    t.spec.Speed,
		ai:       entity.MobAI{MovementSpeed: t.spec.Speed},
	}
	if t.spec.MaxHealth > 0 {
		b.health = entity.NewHealthManager(t.spec.MaxHealth, t.spec.MaxHealth)
		b.effects = entity.NewEffectManager()
	}
	return b
}

// NewCustomEntity creates a *world.EntityHandle from spec and optional opts.
func NewCustomEntity(spec CustomEntitySpec, opts ...world.EntitySpawnOpts) *world.EntityHandle {
	return NewCustomEntityType(spec).Spawn(opts...)
}

// SpawnCustomEntity spawns a custom entity directly into tx and returns the
// opened *CustomEntity (or nil if tx is nil/closed).
func SpawnCustomEntity(tx *world.Tx, spec CustomEntitySpec, opts ...world.EntitySpawnOpts) *CustomEntity {
	if tx == nil || tx.Closed() {
		return nil
	}
	ent := tx.AddEntity(NewCustomEntity(spec, opts...))
	switch v := ent.(type) {
	case *CustomEntity:
		return v
	case *LivingCustomEntity:
		return v.CustomEntity
	default:
		return nil
	}
}

// RegisterEntityTypes creates a new world.EntityRegistry cloning base's types
// and config with additional custom types registered.
func RegisterEntityTypes(base world.EntityRegistry, types ...world.EntityType) world.EntityRegistry {
	existing := base.Types()
	all := make([]world.EntityType, 0, len(existing)+len(types))
	all = append(all, existing...)
	all = append(all, types...)
	return base.Config().New(all)
}

type customEntityBehaviour struct {
	typ            *CustomEntityType
	tx             *world.Tx
	ai             entity.MobAI
	aiActive       bool
	goalsInstalled bool
	speed          float64
	health         *entity.HealthManager
	effects        *entity.EffectManager
	dead           bool
	portalTravel   *entity.PortalTravelComputer
}

func (b *customEntityBehaviour) PortalTravelComputer() *entity.PortalTravelComputer {
	if b.portalTravel == nil {
		b.portalTravel = entity.NewPortalTravelComputer()
	}
	return b.portalTravel
}

func (b *customEntityBehaviour) installGoals(m *entity.Mob) {
	if b.goalsInstalled {
		return
	}
	b.goalsInstalled = true
	for _, g := range b.typ.spec.Goals {
		if g.Goal != nil {
			m.AddGoal(g.Priority, g.Goal)
		}
	}
	for _, g := range b.typ.spec.TargetGoals {
		if g.Goal != nil {
			m.AddTargetGoal(g.Priority, g.Goal)
		}
	}
}

func (b *customEntityBehaviour) Tick(e *entity.Ent, tx *world.Tx) *entity.Movement {
	b.tx = tx
	var self world.Entity = e
	var ce *CustomEntity
	if opened, ok := e.H().Entity(tx); ok {
		self = opened
		switch v := opened.(type) {
		case *CustomEntity:
			ce = v
		case *LivingCustomEntity:
			ce = v.CustomEntity
		}
	}
	var m *entity.Movement
	if b.aiActive {
		m = b.ai.Tick(e, self, b.installGoals)
	}
	if ce != nil && b.typ.spec.OnTick != nil {
		b.typ.spec.OnTick(ce, tx)
	}
	return m
}

// CustomEntity is a live custom entity instance opened inside a transaction.
type CustomEntity struct {
	*entity.Ent
	typ *CustomEntityType
	beh *customEntityBehaviour
}

// Type returns the *CustomEntityType of this entity.
func (c *CustomEntity) Type() *CustomEntityType { return c.typ }

// PersistenceRequired reports whether the entity resists distance despawning.
func (c *CustomEntity) PersistenceRequired() bool { return !c.typ.spec.CanDespawn }

// MobAI activates and returns the entity's *entity.MobAI.
func (c *CustomEntity) MobAI() *entity.MobAI {
	c.beh.aiActive = true
	return &c.beh.ai
}

// AddGoal activates AI and adds g at priority to the entity's goal selector.
func (c *CustomEntity) AddGoal(priority int, g entity.Goal) {
	if g == nil {
		return
	}
	c.MobAI().AddGoal(priority, g)
}

// AddTargetGoal activates AI and adds g at priority to the target selector.
func (c *CustomEntity) AddTargetGoal(priority int, g entity.Goal) {
	if g == nil {
		return
	}
	c.MobAI().AddTargetGoal(priority, g)
}

// Tick advances the entity by one tick.
func (c *CustomEntity) Tick(tx *world.Tx, current int64) {
	c.beh.tx = tx
	c.Ent.Tick(tx, current)
}

// Interact invokes OnInteract when a player or user interacts with the entity.
func (c *CustomEntity) Interact(tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	if c.typ.spec.OnInteract != nil {
		return c.typ.spec.OnInteract(c, tx, user, ctx)
	}
	return false
}

// Attack invokes OnAttack when the entity is attacked.
func (c *CustomEntity) Attack(attacker world.Entity, tx *world.Tx) {
	if c.typ.spec.OnAttack != nil {
		c.typ.spec.OnAttack(c, attacker, tx)
	}
}

// ContributeMetadata applies OnMeta overrides to patch.
func (c *CustomEntity) ContributeMetadata(patch *world.ActorMetaPatch) {
	if c.typ.spec.OnMeta != nil {
		c.typ.spec.OnMeta(c, patch)
	}
}

// LivingCustomEntity extends CustomEntity with entity.Living support when
// CustomEntitySpec.MaxHealth > 0.
type LivingCustomEntity struct {
	*CustomEntity
}

// Tick ticks active status effects and delegates to CustomEntity.Tick.
func (l *LivingCustomEntity) Tick(tx *world.Tx, current int64) {
	l.beh.tx = tx
	l.beh.effects.Tick(l, tx)
	l.CustomEntity.Tick(tx, current)
}

func (l *LivingCustomEntity) Health() float64        { return l.beh.health.Health() }
func (l *LivingCustomEntity) MaxHealth() float64     { return l.beh.health.MaxHealth() }
func (l *LivingCustomEntity) SetMaxHealth(v float64) { l.beh.health.SetMaxHealth(v) }
func (l *LivingCustomEntity) Dead() bool             { return l.beh.dead || l.Health() <= 0 }
func (l *LivingCustomEntity) Speed() float64         { return l.beh.speed }
func (l *LivingCustomEntity) SetSpeed(v float64) {
	l.beh.speed = v
	l.beh.ai.MovementSpeed = v
}
func (l *LivingCustomEntity) AddEffect(e effect.Effect)  { l.beh.effects.Add(e, l) }
func (l *LivingCustomEntity) RemoveEffect(t effect.Type) { l.beh.effects.Remove(t, l) }
func (l *LivingCustomEntity) Effects() []effect.Effect   { return l.beh.effects.Effects() }

// Hurt deals damage to the custom living entity, applying damage cooldown and
// closing the entity when health reaches zero.
func (l *LivingCustomEntity) Hurt(damage float64, src world.DamageSource) (float64, bool) {
	if l.Dead() || damage <= 0 {
		return 0, false
	}
	if l.beh.tx != nil && !l.beh.tx.Closed() {
		var ok bool
		if damage, ok = l.beh.ai.DamageCooldown(l.beh.tx, damage); !ok {
			return 0, false
		}
	}
	l.beh.health.AddHealth(-damage)
	l.beh.ai.Hurt(src)
	if l.beh.tx != nil && !l.beh.tx.Closed() {
		world.BroadcastEntityAction(l.beh.tx, l, entity.HurtAction{})
	}
	if l.Dead() {
		l.beh.dead = true
		if l.beh.tx != nil && !l.beh.tx.Closed() {
			world.BroadcastEntityAction(l.beh.tx, l, entity.DeathAction{})
			_ = l.CloseIn(l.beh.tx)
		} else {
			_ = l.Close()
		}
	}
	return damage, true
}

// Heal restores health up to MaxHealth and returns the amount healed.
func (l *LivingCustomEntity) Heal(health float64, _ world.HealingSource) float64 {
	if l.Dead() || health <= 0 {
		return 0
	}
	before := l.Health()
	l.beh.health.AddHealth(health)
	return l.Health() - before
}

// KnockBack pushes the entity away from src with the given horizontal force and lift.
func (l *LivingCustomEntity) KnockBack(src mgl64.Vec3, force, height float64) {
	if l.Dead() || (force <= 0 && height <= 0) {
		return
	}
	pos := l.Position()
	dx, dz := pos[0]-src[0], pos[2]-src[2]
	dist := math.Sqrt(dx*dx + dz*dz)
	vel := l.Velocity().Mul(0.5)
	if dist > 1e-5 {
		vel[0] += (dx / dist) * force
		vel[2] += (dz / dist) * force
	}
	if l.OnGround() {
		vel[1] = min(vel[1]+height, height)
	}
	l.SetVelocity(vel)
}
