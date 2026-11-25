// Package mob provides declarative custom entities, MobAI goal builders,
// sensor/memory/activity Brains, equipment rendering, death drops, and custom
// NBT persistence.
package mob

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

// GoalOption configures a Goal constructed by NewGoal.
type GoalOption func(*goalConfig)

type goalConfig struct {
	flags         entity.GoalFlag
	canStart      func(*entity.Mob) bool
	start         func(*entity.Mob)
	canContinue   func(*entity.Mob) bool
	tick          func(*entity.Mob)
	stop          func(*entity.Mob)
	interruptable *bool
	everyTick     bool
}

// GoalFlags sets the mutex flags for a Goal.
func GoalFlags(f entity.GoalFlag) GoalOption { return func(c *goalConfig) { c.flags = f } }

// GoalCanStart sets the start condition callback.
func GoalCanStart(fn func(*entity.Mob) bool) GoalOption {
	return func(c *goalConfig) { c.canStart = fn }
}

// GoalStart sets the callback run when the goal begins.
func GoalStart(fn func(*entity.Mob)) GoalOption { return func(c *goalConfig) { c.start = fn } }

// GoalCanContinue sets the continuation predicate (defaults to CanStart).
func GoalCanContinue(fn func(*entity.Mob) bool) GoalOption {
	return func(c *goalConfig) { c.canContinue = fn }
}

// GoalTick sets the per-tick callback while the goal is active.
func GoalTick(fn func(*entity.Mob)) GoalOption { return func(c *goalConfig) { c.tick = fn } }

// GoalStop sets the callback run when the goal finishes or is interrupted.
func GoalStop(fn func(*entity.Mob)) GoalOption { return func(c *goalConfig) { c.stop = fn } }

// GoalInterruptable overrides whether higher-priority goals may interrupt this goal.
func GoalInterruptable(v bool) GoalOption { return func(c *goalConfig) { c.interruptable = &v } }

// GoalEveryTick makes the goal tick every simulation tick instead of alternating.
func GoalEveryTick() GoalOption { return func(c *goalConfig) { c.everyTick = true } }

// Goal implements entity.Goal using callbacks configured via NewGoal.
type Goal struct{ cfg goalConfig }

// NewGoal builds an entity.Goal from opts.
func NewGoal(opts ...GoalOption) *Goal {
	var c goalConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&c)
		}
	}
	return &Goal{cfg: c}
}

func (g *Goal) Flags() entity.GoalFlag { return g.cfg.flags }
func (g *Goal) CanUse(m *entity.Mob) bool {
	return g.cfg.canStart != nil && g.cfg.canStart(m)
}
func (g *Goal) CanContinueToUse(m *entity.Mob) bool {
	if g.cfg.canContinue != nil {
		return g.cfg.canContinue(m)
	}
	return g.CanUse(m)
}
func (g *Goal) IsInterruptable() bool {
	if g.cfg.interruptable != nil {
		return *g.cfg.interruptable
	}
	return true
}
func (g *Goal) Start(m *entity.Mob) {
	if g.cfg.start != nil {
		g.cfg.start(m)
	}
}
func (g *Goal) Stop(m *entity.Mob) {
	if g.cfg.stop != nil {
		g.cfg.stop(m)
	}
}
func (g *Goal) Tick(m *entity.Mob) {
	if g.cfg.tick != nil {
		g.cfg.tick(m)
	}
}
func (g *Goal) RequiresUpdateEveryTick() bool { return g.cfg.everyTick }

// GoalAdder is implemented by vanilla mobs, *entity.Ent, *entity.Mob,
// *entity.MobAI, and *mob.Entity.
type GoalAdder interface {
	AddGoal(priority int, g entity.Goal)
	AddTargetGoal(priority int, g entity.Goal)
}

// AddGoal registers g on m's goal selector at priority.
func AddGoal(m GoalAdder, priority int, g entity.Goal) {
	if m != nil && g != nil {
		m.AddGoal(priority, g)
	}
}

// AddTargetGoal registers g on m's target goal selector at priority.
func AddTargetGoal(m GoalAdder, priority int, g entity.Goal) {
	if m != nil && g != nil {
		m.AddTargetGoal(priority, g)
	}
}

// Sensor periodically scans the world and updates memories in a Brain.
type Sensor struct {
	Name          string
	IntervalTicks int64
	Sense         func(e *Entity, brain *Brain, tx *world.Tx)
}

// Behaviour defines one activity-scoped behaviour driven by a Brain.
type Behaviour struct {
	Name        string
	CanStart    func(e *Entity, brain *Brain, tx *world.Tx) bool
	Start       func(e *Entity, brain *Brain, tx *world.Tx)
	CanContinue func(e *Entity, brain *Brain, tx *world.Tx) bool
	Tick        func(e *Entity, brain *Brain, tx *world.Tx)
	Stop        func(e *Entity, brain *Brain, tx *world.Tx)
}

// BrainOption configures a BrainTemplate created by NewBrain.
type BrainOption func(*BrainTemplate)

// BrainTemplate is a reusable sensor/memory/activity blueprint attached to a
// custom entity Type via WithBrain.
type BrainTemplate struct {
	defaultActivity string
	core            []Behaviour
	activities      map[string][]Behaviour
	sensors         []Sensor
	selector        func(e *Entity, brain *Brain, tx *world.Tx) string
}

// NewBrain creates a BrainTemplate starting in defaultActivity (defaults to "idle").
func NewBrain(defaultActivity string, opts ...BrainOption) *BrainTemplate {
	if defaultActivity == "" {
		defaultActivity = "idle"
	}
	bt := &BrainTemplate{
		defaultActivity: defaultActivity,
		activities:      make(map[string][]Behaviour),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(bt)
		}
	}
	return bt
}

// WithSensor registers a periodic sensor on the BrainTemplate.
func WithSensor(name string, intervalTicks int64, fn func(e *Entity, b *Brain, tx *world.Tx)) BrainOption {
	return func(bt *BrainTemplate) {
		bt.sensors = append(bt.sensors, Sensor{Name: name, IntervalTicks: intervalTicks, Sense: fn})
	}
}

// CoreBehaviours registers behaviours that run regardless of active activity.
func CoreBehaviours(behaviours ...Behaviour) BrainOption {
	return func(bt *BrainTemplate) { bt.core = append(bt.core, behaviours...) }
}

// Activity registers behaviours for a named activity state.
func Activity(name string, behaviours ...Behaviour) BrainOption {
	return func(bt *BrainTemplate) {
		bt.activities[name] = append(bt.activities[name], behaviours...)
	}
}

// ActivitySelector selects the next active activity name each tick.
func ActivitySelector(fn func(e *Entity, b *Brain, tx *world.Tx) string) BrainOption {
	return func(bt *BrainTemplate) { bt.selector = fn }
}

type memoryEntry struct {
	val    any
	hasTTL bool
	ttl    int64
}

type runningBehaviour struct {
	beh    Behaviour
	active bool
}

// Brain manages memories (with optional TTL expiry), periodic sensors, and
// activity-switched behaviours for one live Entity instance.
type Brain struct {
	tmpl       *BrainTemplate
	activity   string
	memories   map[string]memoryEntry
	sensorNext []int64
	core       []runningBehaviour
	actState   map[string][]runningBehaviour
	tickCount  int64
}

func newBrainInstance(tmpl *BrainTemplate) *Brain {
	b := &Brain{
		tmpl:       tmpl,
		activity:   tmpl.defaultActivity,
		memories:   make(map[string]memoryEntry),
		sensorNext: make([]int64, len(tmpl.sensors)),
		core:       make([]runningBehaviour, len(tmpl.core)),
		actState:   make(map[string][]runningBehaviour, len(tmpl.activities)),
	}
	for i, beh := range tmpl.core {
		b.core[i] = runningBehaviour{beh: beh}
	}
	for name, list := range tmpl.activities {
		rs := make([]runningBehaviour, len(list))
		for i, beh := range list {
			rs[i] = runningBehaviour{beh: beh}
		}
		b.actState[name] = rs
	}
	return b
}

// Activity returns the currently active activity name.
func (b *Brain) Activity() string {
	if b == nil {
		return ""
	}
	return b.activity
}

// SetActivity transitions to name, stopping running behaviours of the previous activity.
func (b *Brain) SetActivity(name string, e *Entity, tx *world.Tx) {
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

// SetMemory stores a persistent memory value under key.
func (b *Brain) SetMemory(key string, val any) {
	if b != nil && key != "" {
		b.memories[key] = memoryEntry{val: val}
	}
}

// SetMemoryWithTTL stores a memory value that expires after ttlTicks ticks.
func (b *Brain) SetMemoryWithTTL(key string, val any, ttlTicks int64) {
	if b == nil || key == "" {
		return
	}
	if ttlTicks <= 0 {
		delete(b.memories, key)
		return
	}
	b.memories[key] = memoryEntry{val: val, hasTTL: true, ttl: ttlTicks}
}

// Memory returns the value stored under key, if present.
func (b *Brain) Memory(key string) (any, bool) {
	if b == nil {
		return nil, false
	}
	entry, ok := b.memories[key]
	return entry.val, ok
}

// EraseMemory removes key from memory.
func (b *Brain) EraseMemory(key string) {
	if b != nil {
		delete(b.memories, key)
	}
}

func (b *Brain) tick(e *Entity, tx *world.Tx) {
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
	for i, s := range b.tmpl.sensors {
		if s.Sense == nil {
			continue
		}
		if b.tickCount >= b.sensorNext[i] {
			b.sensorNext[i] = b.tickCount + max(s.IntervalTicks, 1)
			s.Sense(e, b, tx)
		}
	}
	if b.tmpl.selector != nil {
		if next := b.tmpl.selector(e, b, tx); next != "" && next != b.activity {
			b.SetActivity(next, e, tx)
		}
	}
	b.tickList(b.core, e, tx)
	if list := b.actState[b.activity]; len(list) > 0 {
		b.tickList(list, e, tx)
	}
}

func (b *Brain) tickList(list []runningBehaviour, e *Entity, tx *world.Tx) {
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

// Equipment holds held items and armour rendered for an Entity.
type Equipment struct {
	MainHand   item.Stack
	OffHand    item.Stack
	Helmet     item.Stack
	Chestplate item.Stack
	Leggings   item.Stack
	Boots      item.Stack
}

type goalEntry struct {
	priority int
	goal     entity.Goal
}

// Option configures a custom entity Type defined by Define.
type Option func(*typeConfig)

type typeConfig struct {
	name        string
	networkType string
	bbox        cube.BBox
	bboxFunc    func(world.Entity) cube.BBox
	spawnOpts   world.EntitySpawnOpts
	maxHealth   float64
	speed       float64
	canDespawn  bool
	enableAI    bool
	goals       []goalEntry
	targetGoals []goalEntry
	brain       *BrainTemplate
	equipment   Equipment
	drops       []item.Stack
	dropsFunc   func(*Entity, world.DamageSource) []item.Stack
	xpDrops     int
	encodeNBT   func(any) map[string]any
	decodeNBT   func(map[string]any) any
	onSpawn     func(*Entity, *world.Tx)
	onTick      func(*Entity, *world.Tx)
	onInteract  func(*Entity, *world.Tx, item.User, *item.UseContext) bool
	onAttack    func(*Entity, world.Entity, *world.Tx)
	onHurt      func(*Entity, *float64, world.DamageSource, *world.Tx) bool
	onDeath     func(*Entity, world.DamageSource, *world.Tx)
	onMeta      func(world.Entity, *world.ActorMetaPatch)
}

// NetworkType overrides the Bedrock actor identifier sent to clients.
func NetworkType(id string) Option { return func(c *typeConfig) { c.networkType = id } }

// BBox sets a static bounding box.
func BBox(box cube.BBox) Option { return func(c *typeConfig) { c.bbox = box } }

// BBoxFunc sets a dynamic bounding box callback.
func BBoxFunc(fn func(world.Entity) cube.BBox) Option { return func(c *typeConfig) { c.bboxFunc = fn } }

// DefaultSpawnOpts sets default EntitySpawnOpts merged into Spawn calls.
func DefaultSpawnOpts(opts world.EntitySpawnOpts) Option {
	return func(c *typeConfig) { c.spawnOpts = opts }
}

// MaxHealth enables Living entity support (health, damage, healing, effects).
func MaxHealth(hp float64) Option { return func(c *typeConfig) { c.maxHealth = hp } }

// Speed sets the initial movement speed (defaults to 0.25).
func Speed(v float64) Option { return func(c *typeConfig) { c.speed = v } }

// CanDespawn allows natural distance despawning.
func CanDespawn() Option { return func(c *typeConfig) { c.canDespawn = true } }

// EnableAI enables MobAI ticking even before goals are added dynamically.
func EnableAI() Option { return func(c *typeConfig) { c.enableAI = true } }

// WithGoal registers an initial MobAI goal at priority.
func WithGoal(priority int, g entity.Goal) Option {
	return func(c *typeConfig) { c.goals = append(c.goals, goalEntry{priority, g}) }
}

// WithTargetGoal registers an initial MobAI target goal at priority.
func WithTargetGoal(priority int, g entity.Goal) Option {
	return func(c *typeConfig) { c.targetGoals = append(c.targetGoals, goalEntry{priority, g}) }
}

// WithBrain attaches a BrainTemplate to every instance of this Type.
func WithBrain(bt *BrainTemplate) Option { return func(c *typeConfig) { c.brain = bt } }

// WithEquipment sets initial held items and armour.
func WithEquipment(eq Equipment) Option { return func(c *typeConfig) { c.equipment = eq } }

// Drops sets static item drops spawned on death.
func Drops(stacks ...item.Stack) Option {
	return func(c *typeConfig) { c.drops = slices.Clone(stacks) }
}

// DropsFunc dynamically computes death drops.
func DropsFunc(fn func(e *Entity, src world.DamageSource) []item.Stack) Option {
	return func(c *typeConfig) { c.dropsFunc = fn }
}

// XP sets experience points spawned as orbs on death.
func XP(amount int) Option { return func(c *typeConfig) { c.xpDrops = amount } }

// Codec attaches custom NBT encode/decode callbacks for Entity.CustomState.
func Codec(encode func(state any) map[string]any, decode func(data map[string]any) any) Option {
	return func(c *typeConfig) { c.encodeNBT = encode; c.decodeNBT = decode }
}

// OnSpawn runs once when an entity instance first opens in a world.
func OnSpawn(fn func(e *Entity, tx *world.Tx)) Option { return func(c *typeConfig) { c.onSpawn = fn } }

// OnTick runs every simulation tick for the entity.
func OnTick(fn func(e *Entity, tx *world.Tx)) Option { return func(c *typeConfig) { c.onTick = fn } }

// OnInteract runs when a player interacts with the entity.
func OnInteract(fn func(e *Entity, tx *world.Tx, u item.User, ctx *item.UseContext) bool) Option {
	return func(c *typeConfig) { c.onInteract = fn }
}

// OnAttack runs when the entity is attacked.
func OnAttack(fn func(e *Entity, attacker world.Entity, tx *world.Tx)) Option {
	return func(c *typeConfig) { c.onAttack = fn }
}

// OnHurt runs before damage is applied; returning false cancels the damage.
func OnHurt(fn func(e *Entity, damage *float64, src world.DamageSource, tx *world.Tx) bool) Option {
	return func(c *typeConfig) { c.onHurt = fn }
}

// OnDeath runs when a living custom entity dies.
func OnDeath(fn func(e *Entity, src world.DamageSource, tx *world.Tx)) Option {
	return func(c *typeConfig) { c.onDeath = fn }
}

// OnMeta customizes network metadata sent to clients.
func OnMeta(fn func(e world.Entity, patch *world.ActorMetaPatch)) Option {
	return func(c *typeConfig) { c.onMeta = fn }
}

// Type implements world.EntityType for custom entities defined by Define.
type Type struct{ cfg typeConfig }

// Define creates a reusable custom entity *Type named id configured by opts.
func Define(id string, opts ...Option) *Type {
	if id == "" {
		panic("sdk/mob: entity id must not be empty")
	}
	c := typeConfig{name: id, speed: 0.25}
	for _, opt := range opts {
		if opt != nil {
			opt(&c)
		}
	}
	if c.speed <= 0 {
		c.speed = 0.25
	}
	return &Type{cfg: c}
}

func (t *Type) EncodeEntity() string { return t.cfg.name }

// NetworkEncodeEntity returns the Bedrock actor identifier sent to clients.
func (t *Type) NetworkEncodeEntity() string {
	if t.cfg.networkType != "" {
		return t.cfg.networkType
	}
	return t.cfg.name
}

func (t *Type) BBox(e world.Entity) cube.BBox {
	if t.cfg.bboxFunc != nil {
		return t.cfg.bboxFunc(e)
	}
	return t.cfg.bbox
}

func (t *Type) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	if data.Data == nil {
		t.Config().Apply(data)
	}
	beh := data.Data.(*behaviour)
	beh.tx = tx
	ce := &Entity{Ent: entity.Open(tx, handle, data), typ: t, beh: beh}
	var out world.Entity = ce
	if beh.health != nil {
		out = &Living{Entity: ce}
	}
	if !beh.spawned {
		beh.spawned = true
		if t.cfg.onSpawn != nil && tx != nil && !tx.Closed() {
			t.cfg.onSpawn(ce, tx)
		}
	}
	return out
}

func (t *Type) DecodeNBT(m map[string]any, data *world.EntityData) {
	t.Config().Apply(data)
	beh := data.Data.(*behaviour)
	if t.cfg.decodeNBT != nil && len(m) > 0 {
		beh.customState = t.cfg.decodeNBT(m)
	}
}

func (t *Type) EncodeNBT(data *world.EntityData) map[string]any {
	if t.cfg.encodeNBT == nil || data == nil || data.Data == nil {
		return nil
	}
	beh, ok := data.Data.(*behaviour)
	if !ok {
		return nil
	}
	return t.cfg.encodeNBT(beh.customState)
}

// Config returns the world.EntityConfig initializing instances of t.
func (t *Type) Config() world.EntityConfig { return entityConfig{typ: t} }

// Spawn creates a worldless *world.EntityHandle for t.
func (t *Type) Spawn(opts ...world.EntitySpawnOpts) *world.EntityHandle {
	merged := t.cfg.spawnOpts
	if len(opts) > 0 {
		merged = mergeSpawnOpts(merged, opts[0])
	}
	return merged.New(t, t.Config())
}

// SpawnIn spawns an instance of t directly into tx and returns its *Entity.
func (t *Type) SpawnIn(tx *world.Tx, opts ...world.EntitySpawnOpts) *Entity {
	if tx == nil || tx.Closed() {
		return nil
	}
	ent := tx.AddEntity(t.Spawn(opts...))
	switch v := ent.(type) {
	case *Entity:
		return v
	case *Living:
		return v.Entity
	default:
		return nil
	}
}

// Register returns a new world.EntityRegistry extending base with types.
func Register(base world.EntityRegistry, types ...world.EntityType) world.EntityRegistry {
	existing := base.Types()
	all := make([]world.EntityType, 0, len(existing)+len(types))
	all = append(all, existing...)
	all = append(all, types...)
	return base.Config().New(all)
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

type entityConfig struct{ typ *Type }

func (c entityConfig) Apply(data *world.EntityData) {
	t := c.typ
	b := &behaviour{
		typ:       t,
		aiActive:  t.cfg.enableAI || len(t.cfg.goals) > 0 || len(t.cfg.targetGoals) > 0,
		speed:     t.cfg.speed,
		ai:        entity.MobAI{MovementSpeed: t.cfg.speed},
		equipment: t.cfg.equipment,
	}
	if t.cfg.maxHealth > 0 {
		b.health = entity.NewHealthManager(t.cfg.maxHealth, t.cfg.maxHealth)
		b.effects = entity.NewEffectManager()
	}
	if t.cfg.brain != nil {
		b.brain = newBrainInstance(t.cfg.brain)
	}
	data.Data = b
}

type behaviour struct {
	typ            *Type
	tx             *world.Tx
	ai             entity.MobAI
	aiActive       bool
	goalsInstalled bool
	brain          *Brain
	equipment      Equipment
	customState    any
	spawned        bool
	speed          float64
	health         *entity.HealthManager
	effects        *entity.EffectManager
	dead           bool
	portalTravel   *entity.PortalTravelComputer
}

func (b *behaviour) PortalTravelComputer() *entity.PortalTravelComputer {
	if b.portalTravel == nil {
		b.portalTravel = entity.NewPortalTravelComputer()
	}
	return b.portalTravel
}

func (b *behaviour) installGoals(m *entity.Mob) {
	if b.goalsInstalled {
		return
	}
	b.goalsInstalled = true
	for _, g := range b.typ.cfg.goals {
		if g.goal != nil {
			m.AddGoal(g.priority, g.goal)
		}
	}
	for _, g := range b.typ.cfg.targetGoals {
		if g.goal != nil {
			m.AddTargetGoal(g.priority, g.goal)
		}
	}
}

func (b *behaviour) Tick(e *entity.Ent, tx *world.Tx) *entity.Movement {
	b.tx = tx
	var self world.Entity = e
	var ce *Entity
	if opened, ok := e.H().Entity(tx); ok {
		self = opened
		switch v := opened.(type) {
		case *Entity:
			ce = v
		case *Living:
			ce = v.Entity
		}
	}
	var m *entity.Movement
	if b.aiActive {
		m = b.ai.Tick(e, self, b.installGoals)
	}
	if ce != nil && b.brain != nil {
		b.brain.tick(ce, tx)
	}
	if ce != nil && b.typ.cfg.onTick != nil {
		b.typ.cfg.onTick(ce, tx)
	}
	return m
}

// Entity is a live custom entity instance opened inside a world transaction.
type Entity struct {
	*entity.Ent
	typ *Type
	beh *behaviour
}

// Type returns the custom entity *Type.
func (e *Entity) Type() *Type { return e.typ }

// PersistenceRequired reports whether the entity resists distance despawning.
func (e *Entity) PersistenceRequired() bool { return !e.typ.cfg.canDespawn }

// Brain returns the entity's *Brain, or nil if none was configured.
func (e *Entity) Brain() *Brain { return e.beh.brain }

// Equipment returns the entity's held items and armour.
func (e *Entity) Equipment() Equipment { return e.beh.equipment }

// SetEquipment updates the entity's held items and armour.
func (e *Entity) SetEquipment(eq Equipment) { e.beh.equipment = eq }

// HeldItems exposes MainHand and OffHand for Bedrock rendering.
func (e *Entity) HeldItems() (item.Stack, item.Stack) {
	return e.beh.equipment.MainHand, e.beh.equipment.OffHand
}

// EquipmentArmour exposes Helmet, Chestplate, Leggings, and Boots for Bedrock rendering.
func (e *Entity) EquipmentArmour() (item.Stack, item.Stack, item.Stack, item.Stack) {
	eq := e.beh.equipment
	return eq.Helmet, eq.Chestplate, eq.Leggings, eq.Boots
}

// CustomState returns custom state persisted via Codec.
func (e *Entity) CustomState() any { return e.beh.customState }

// SetCustomState sets custom state persisted via Codec.
func (e *Entity) SetCustomState(v any) { e.beh.customState = v }

// MobAI activates and returns the entity's *entity.MobAI.
func (e *Entity) MobAI() *entity.MobAI {
	e.beh.aiActive = true
	return &e.beh.ai
}

// AddGoal activates AI and adds g to the goal selector.
func (e *Entity) AddGoal(priority int, g entity.Goal) {
	if g != nil {
		e.MobAI().AddGoal(priority, g)
	}
}

// AddTargetGoal activates AI and adds g to the target selector.
func (e *Entity) AddTargetGoal(priority int, g entity.Goal) {
	if g != nil {
		e.MobAI().AddTargetGoal(priority, g)
	}
}

// Tick advances the entity by one tick.
func (e *Entity) Tick(tx *world.Tx, current int64) {
	e.beh.tx = tx
	e.Ent.Tick(tx, current)
}

// Interact invokes OnInteract when a user interacts with e.
func (e *Entity) Interact(tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	return e.typ.cfg.onInteract != nil && e.typ.cfg.onInteract(e, tx, u, ctx)
}

// Attack invokes OnAttack when e is attacked.
func (e *Entity) Attack(attacker world.Entity, tx *world.Tx) {
	if e.typ.cfg.onAttack != nil {
		e.typ.cfg.onAttack(e, attacker, tx)
	}
}

// ContributeMetadata applies OnMeta overrides to patch.
func (e *Entity) ContributeMetadata(patch *world.ActorMetaPatch) {
	if e.typ.cfg.onMeta != nil {
		e.typ.cfg.onMeta(e, patch)
	}
}

// Living extends Entity with entity.Living behaviour when MaxHealth > 0.
type Living struct{ *Entity }

// Tick advances active status effects and delegates to Entity.Tick.
func (l *Living) Tick(tx *world.Tx, current int64) {
	l.beh.tx = tx
	l.beh.effects.Tick(l, tx)
	l.Entity.Tick(tx, current)
}

func (l *Living) Health() float64        { return l.beh.health.Health() }
func (l *Living) MaxHealth() float64     { return l.beh.health.MaxHealth() }
func (l *Living) SetMaxHealth(v float64) { l.beh.health.SetMaxHealth(v) }
func (l *Living) Dead() bool             { return l.beh.dead || l.Health() <= 0 }
func (l *Living) Speed() float64         { return l.beh.speed }
func (l *Living) SetSpeed(v float64) {
	l.beh.speed = v
	l.beh.ai.MovementSpeed = v
}
func (l *Living) AddEffect(ef effect.Effect) { l.beh.effects.Add(ef, l) }
func (l *Living) RemoveEffect(t effect.Type) { l.beh.effects.Remove(t, l) }
func (l *Living) Effects() []effect.Effect   { return l.beh.effects.Effects() }

// Hurt deals damage to l, firing OnHurt, damage cooldown, death drops, XP, and OnDeath.
func (l *Living) Hurt(damage float64, src world.DamageSource) (float64, bool) {
	if l.Dead() || damage <= 0 {
		return 0, false
	}
	if l.typ.cfg.onHurt != nil {
		if !l.typ.cfg.onHurt(l.Entity, &damage, src, l.beh.tx) || damage <= 0 {
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

func (l *Living) handleDeath(src world.DamageSource) {
	tx := l.beh.tx
	if tx != nil && !tx.Closed() {
		pos := l.Position()
		var drops []item.Stack
		if l.typ.cfg.dropsFunc != nil {
			drops = l.typ.cfg.dropsFunc(l.Entity, src)
		} else {
			drops = l.typ.cfg.drops
		}
		for _, stack := range drops {
			if !stack.Empty() {
				tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: pos}, stack))
			}
		}
		if l.typ.cfg.xpDrops > 0 {
			for _, orb := range entity.NewExperienceOrbs(tx, pos, l.typ.cfg.xpDrops) {
				tx.AddEntity(orb)
			}
		}
		if l.typ.cfg.onDeath != nil {
			l.typ.cfg.onDeath(l.Entity, src, tx)
		}
		world.BroadcastEntityAction(tx, l, entity.DeathAction{})
		_ = l.CloseIn(tx)
		return
	}
	if l.typ.cfg.onDeath != nil {
		l.typ.cfg.onDeath(l.Entity, src, nil)
	}
	_ = l.Close()
}

// Heal restores health up to MaxHealth and returns the amount healed.
func (l *Living) Heal(health float64, _ world.HealingSource) float64 {
	if l.Dead() || health <= 0 {
		return 0
	}
	before := l.Health()
	l.beh.health.AddHealth(health)
	return l.Health() - before
}

// KnockBack pushes l away from src.
func (l *Living) KnockBack(src mgl64.Vec3, force, height float64) {
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
