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

// BrainSensor periodically scans the world and updates memories in an EntityBrain.
type BrainSensor struct {
	Name          string
	IntervalTicks int64
	Sense         func(e *CustomEntity, brain *EntityBrain, tx *world.Tx)
}

// BrainBehaviour defines one activity-scoped behaviour driven by an EntityBrain.
type BrainBehaviour struct {
	Name        string
	CanStart    func(e *CustomEntity, brain *EntityBrain, tx *world.Tx) bool
	Start       func(e *CustomEntity, brain *EntityBrain, tx *world.Tx)
	CanContinue func(e *CustomEntity, brain *EntityBrain, tx *world.Tx) bool
	Tick        func(e *CustomEntity, brain *EntityBrain, tx *world.Tx)
	Stop        func(e *CustomEntity, brain *EntityBrain, tx *world.Tx)
}

// BrainSpec declaratively configures a sensor/memory/activity Brain on a
// CustomEntity (modelled after Java Edition's Brain architecture).
type BrainSpec struct {
	DefaultActivity  string
	CoreBehaviours   []BrainBehaviour
	Activities       map[string][]BrainBehaviour
	Sensors          []BrainSensor
	ActivitySelector func(e *CustomEntity, brain *EntityBrain, tx *world.Tx) string
}

type brainMemoryEntry struct {
	val    any
	hasTTL bool
	ttl    int64
}

type runningBehaviour struct {
	beh    BrainBehaviour
	active bool
}

// EntityBrain manages typed memories (with optional TTL expiry), periodic
// sensors, and activity-switched behaviours for a CustomEntity.
type EntityBrain struct {
	spec       BrainSpec
	activity   string
	memories   map[string]brainMemoryEntry
	sensorNext []int64
	core       []runningBehaviour
	actState   map[string][]runningBehaviour
	tickCount  int64
}

func newEntityBrain(spec BrainSpec) *EntityBrain {
	defAct := spec.DefaultActivity
	if defAct == "" {
		defAct = "idle"
	}
	b := &EntityBrain{
		spec:       spec,
		activity:   defAct,
		memories:   make(map[string]brainMemoryEntry),
		sensorNext: make([]int64, len(spec.Sensors)),
		core:       make([]runningBehaviour, len(spec.CoreBehaviours)),
		actState:   make(map[string][]runningBehaviour, len(spec.Activities)),
	}
	for i, beh := range spec.CoreBehaviours {
		b.core[i] = runningBehaviour{beh: beh}
	}
	for name, list := range spec.Activities {
		rs := make([]runningBehaviour, len(list))
		for i, beh := range list {
			rs[i] = runningBehaviour{beh: beh}
		}
		b.actState[name] = rs
	}
	return b
}

// Activity returns the brain's active activity name.
func (b *EntityBrain) Activity() string {
	if b == nil {
		return ""
	}
	return b.activity
}

// SetActivity switches the brain's current activity, stopping running behaviours
// of the previous activity.
func (b *EntityBrain) SetActivity(name string, e *CustomEntity, tx *world.Tx) {
	if b == nil || name == "" || name == b.activity {
		return
	}
	if prev := b.actState[b.activity]; len(prev) > 0 {
		for i := range prev {
			if prev[i].active {
				prev[i].active = false
				if prev[i].beh.Stop != nil {
					prev[i].beh.Stop(e, b, tx)
				}
			}
		}
	}
	b.activity = name
}

// SetMemory stores a persistent memory value under key until erased.
func (b *EntityBrain) SetMemory(key string, val any) {
	if b == nil || key == "" {
		return
	}
	b.memories[key] = brainMemoryEntry{val: val}
}

// SetMemoryWithTTL stores a memory value that expires automatically after ttlTicks ticks.
func (b *EntityBrain) SetMemoryWithTTL(key string, val any, ttlTicks int64) {
	if b == nil || key == "" {
		return
	}
	if ttlTicks <= 0 {
		delete(b.memories, key)
		return
	}
	b.memories[key] = brainMemoryEntry{val: val, hasTTL: true, ttl: ttlTicks}
}

// Memory returns the value stored under key, if present.
func (b *EntityBrain) Memory(key string) (any, bool) {
	if b == nil {
		return nil, false
	}
	entry, ok := b.memories[key]
	if !ok {
		return nil, false
	}
	return entry.val, true
}

// EraseMemory removes key from memory.
func (b *EntityBrain) EraseMemory(key string) {
	if b != nil {
		delete(b.memories, key)
	}
}

func (b *EntityBrain) tick(e *CustomEntity, tx *world.Tx) {
	b.tickCount++
	for k, entry := range b.memories {
		if entry.hasTTL {
			entry.ttl--
			if entry.ttl <= 0 {
				delete(b.memories, k)
			} else {
				b.memories[k] = entry
			}
		}
	}
	for i, s := range b.spec.Sensors {
		if s.Sense == nil {
			continue
		}
		if b.tickCount >= b.sensorNext[i] {
			interval := max(s.IntervalTicks, 1)
			b.sensorNext[i] = b.tickCount + interval
			s.Sense(e, b, tx)
		}
	}
	if b.spec.ActivitySelector != nil {
		if next := b.spec.ActivitySelector(e, b, tx); next != "" && next != b.activity {
			b.SetActivity(next, e, tx)
		}
	}
	b.tickList(b.core, e, tx)
	if list := b.actState[b.activity]; len(list) > 0 {
		b.tickList(list, e, tx)
	}
}

func (b *EntityBrain) tickList(list []runningBehaviour, e *CustomEntity, tx *world.Tx) {
	for i := range list {
		r := &list[i]
		if !r.active {
			if r.beh.CanStart == nil || r.beh.CanStart(e, b, tx) {
				r.active = true
				if r.beh.Start != nil {
					r.beh.Start(e, b, tx)
				}
			}
		}
		if r.active {
			cont := true
			if r.beh.CanContinue != nil {
				cont = r.beh.CanContinue(e, b, tx)
			} else if r.beh.CanStart != nil {
				cont = r.beh.CanStart(e, b, tx)
			}
			if !cont {
				r.active = false
				if r.beh.Stop != nil {
					r.beh.Stop(e, b, tx)
				}
				continue
			}
			if r.beh.Tick != nil {
				r.beh.Tick(e, b, tx)
			}
		}
	}
}

// CustomEquipment configures initial and live held items and armour rendered on
// a CustomEntity for Bedrock clients.
type CustomEquipment struct {
	MainHand   item.Stack
	OffHand    item.Stack
	Helmet     item.Stack
	Chestplate item.Stack
	Leggings   item.Stack
	Boots      item.Stack
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
	// CanDespawn allows natural distance despawning when true (false by default).
	CanDespawn bool
	// EnableAI enables MobAI ticking even before goals are added dynamically.
	EnableAI bool
	// Goals lists initial AI goals registered on spawn.
	Goals []GoalEntry
	// TargetGoals lists initial AI target goals registered on spawn.
	TargetGoals []GoalEntry
	// Brain optionally attaches a sensor/memory/activity Brain to every instance.
	Brain *BrainSpec
	// Equipment sets initial held items and armour shown to clients.
	Equipment CustomEquipment
	// Drops defines static item drops spawned on death (used when DropsFunc is nil).
	Drops []item.Stack
	// DropsFunc dynamically computes item drops when the entity dies.
	DropsFunc func(e *CustomEntity, src world.DamageSource) []item.Stack
	// XPDrops spawns this many experience points as XP orbs on death when > 0.
	XPDrops int
	// EncodeNBT optionally serializes custom entity state to NBT on chunk save.
	EncodeNBT func(state any) map[string]any
	// DecodeNBT optionally restores custom entity state from NBT on chunk load.
	DecodeNBT func(data map[string]any) any

	OnSpawn    func(e *CustomEntity, tx *world.Tx)
	OnTick     func(e *CustomEntity, tx *world.Tx)
	OnInteract func(e *CustomEntity, tx *world.Tx, user item.User, ctx *item.UseContext) bool
	OnAttack   func(e *CustomEntity, attacker world.Entity, tx *world.Tx)
	// OnHurt runs before damage is applied to a living custom entity. Return
	// false to cancel the damage; mutating *damage adjusts the final amount.
	OnHurt func(e *CustomEntity, damage *float64, src world.DamageSource, tx *world.Tx) bool
	// OnDeath runs when a living custom entity's health reaches zero.
	OnDeath func(e *CustomEntity, src world.DamageSource, tx *world.Tx)
	OnMeta  func(e world.Entity, patch *world.ActorMetaPatch)
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
	spec.Drops = slices.Clone(spec.Drops)
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
	var out world.Entity = ce
	if beh.health != nil {
		out = &LivingCustomEntity{CustomEntity: ce}
	}
	if !beh.spawned {
		beh.spawned = true
		if t.spec.OnSpawn != nil && tx != nil && !tx.Closed() {
			t.spec.OnSpawn(ce, tx)
		}
	}
	return out
}

func (t *CustomEntityType) DecodeNBT(m map[string]any, data *world.EntityData) {
	t.Config().Apply(data)
	beh := data.Data.(*customEntityBehaviour)
	if t.spec.DecodeNBT != nil && len(m) > 0 {
		beh.customState = t.spec.DecodeNBT(m)
	}
}

func (t *CustomEntityType) EncodeNBT(data *world.EntityData) map[string]any {
	if t.spec.EncodeNBT == nil || data == nil || data.Data == nil {
		return nil
	}
	beh, ok := data.Data.(*customEntityBehaviour)
	if !ok {
		return nil
	}
	return t.spec.EncodeNBT(beh.customState)
}

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
		typ:       t,
		aiActive:  t.spec.EnableAI || len(t.spec.Goals) > 0 || len(t.spec.TargetGoals) > 0,
		speed:     t.spec.Speed,
		ai:        entity.MobAI{MovementSpeed: t.spec.Speed},
		equipment: t.spec.Equipment,
	}
	if t.spec.MaxHealth > 0 {
		b.health = entity.NewHealthManager(t.spec.MaxHealth, t.spec.MaxHealth)
		b.effects = entity.NewEffectManager()
	}
	if t.spec.Brain != nil {
		b.brain = newEntityBrain(*t.spec.Brain)
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
	brain          *EntityBrain
	equipment      CustomEquipment
	customState    any
	spawned        bool
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
	if ce != nil && b.brain != nil {
		b.brain.tick(ce, tx)
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

// Brain returns the entity's *EntityBrain, or nil if no BrainSpec was configured.
func (c *CustomEntity) Brain() *EntityBrain { return c.beh.brain }

// Equipment returns the entity's current CustomEquipment.
func (c *CustomEntity) Equipment() CustomEquipment { return c.beh.equipment }

// SetEquipment updates the entity's held items and armour.
func (c *CustomEntity) SetEquipment(eq CustomEquipment) { c.beh.equipment = eq }

// HeldItems exposes MainHand and OffHand for Bedrock entity equipment rendering.
func (c *CustomEntity) HeldItems() (item.Stack, item.Stack) {
	return c.beh.equipment.MainHand, c.beh.equipment.OffHand
}

// EquipmentArmour exposes Helmet, Chestplate, Leggings, and Boots for Bedrock
// entity armour rendering.
func (c *CustomEntity) EquipmentArmour() (item.Stack, item.Stack, item.Stack, item.Stack) {
	eq := c.beh.equipment
	return eq.Helmet, eq.Chestplate, eq.Leggings, eq.Boots
}

// CustomState returns arbitrary custom state serialized via EncodeNBT/DecodeNBT.
func (c *CustomEntity) CustomState() any { return c.beh.customState }

// SetCustomState sets arbitrary custom state serialized via EncodeNBT/DecodeNBT.
func (c *CustomEntity) SetCustomState(v any) { c.beh.customState = v }

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

// Hurt deals damage to the custom living entity, applying OnHurt hooks, damage
// cooldown, death drops, XP orbs, OnDeath callbacks, and closing on death.
func (l *LivingCustomEntity) Hurt(damage float64, src world.DamageSource) (float64, bool) {
	if l.Dead() || damage <= 0 {
		return 0, false
	}
	if l.typ.spec.OnHurt != nil {
		if !l.typ.spec.OnHurt(l.CustomEntity, &damage, src, l.beh.tx) || damage <= 0 {
			return 0, false
		}
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
		l.handleDeath(src)
	}
	return damage, true
}

func (l *LivingCustomEntity) handleDeath(src world.DamageSource) {
	tx := l.beh.tx
	if tx != nil && !tx.Closed() {
		pos := l.Position()
		var drops []item.Stack
		if l.typ.spec.DropsFunc != nil {
			drops = l.typ.spec.DropsFunc(l.CustomEntity, src)
		} else {
			drops = l.typ.spec.Drops
		}
		for _, stack := range drops {
			if !stack.Empty() {
				tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: pos}, stack))
			}
		}
		if l.typ.spec.XPDrops > 0 {
			for _, orb := range entity.NewExperienceOrbs(tx, pos, l.typ.spec.XPDrops) {
				tx.AddEntity(orb)
			}
		}
		if l.typ.spec.OnDeath != nil {
			l.typ.spec.OnDeath(l.CustomEntity, src, tx)
		}
		world.BroadcastEntityAction(tx, l, entity.DeathAction{})
		_ = l.CloseIn(tx)
		return
	}
	if l.typ.spec.OnDeath != nil {
		l.typ.spec.OnDeath(l.CustomEntity, src, nil)
	}
	_ = l.Close()
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
