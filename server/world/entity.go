package world

import (
	"encoding/binary"
	"io"
	"maps"
	"math"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// EntityType is the type of Entity. It specifies the name, encoded Entity
// ID and bounding box of an Entity.
type EntityType interface {
	// Open returns an Entity implementation in a transaction.
	Open(tx *Tx, handle *EntityHandle, data *EntityData) Entity

	// EncodeEntity converts the Entity to its encoded representation: It
	// returns the type of the Minecraft Entity, for example
	// 'minecraft:falling_block'.
	EncodeEntity() string
	// BBox returns the bounding box of an Entity with this EntityType.
	BBox(e Entity) cube.BBox
	// DecodeNBT reads the fields from the NBT data map passed and converts it
	// to an Entity of the same EntityType.
	DecodeNBT(m map[string]any, data *EntityData)
	// EncodeNBT encodes the Entity of the same EntityType passed to a map of
	// properties that can be encoded to NBT.
	EncodeNBT(data *EntityData) map[string]any
}

// EntityConfig is used to configure the initial settings of an Entity upon
// creation using NewEntity.
type EntityConfig interface {
	Apply(data *EntityData)
}

// EntityHandle is a persistent identifier of an entity. It holds data of the
// entity that can be transformed into an Entity implementation in the context
// of a transaction.
type EntityHandle struct {
	id uuid.UUID
	t  EntityType

	cond         *sync.Cond
	worldless    *atomic.Bool
	weakTxActive bool
	w            *World
	// state caches the world-owned entity state for fast tick-path access.
	// It is set/cleared by the world goroutine when entities enter/leave worlds.
	state *entityState
	// worldReady becomes true only after AddEntity finishes registering and
	// opening the entity in w. Scheduled callbacks wait for this handoff.
	worldReady bool
	// worldVersion increments on every change to w, letting weak transactions
	// detect that the entity moved while they were queued.
	worldVersion atomic.Uint64
	// closed closes once the handle is closed. worldChanged, created lazily
	// for delayed schedulers, is closed and dropped on every change to w.
	closed       chan struct{}
	worldChanged chan struct{}
	closeOnce    sync.Once

	data           EntityData
	spawnReason    string
	spawnSeed      int64
	spawnGroup     *EntitySpawnGroup
	javaRandom     *vanilla.Random
	pendingVehicle uuid.UUID

	// TODO Handler? Handle world change here?
}

// EntitySpawnOpts holds spawning related options for entities created.
type EntitySpawnOpts struct {
	// Position is the position that an Entity should be spawned at.
	Position mgl64.Vec3
	// Rotation is the rotation that an Entity should be spawned with.
	Rotation cube.Rotation
	// Velocity specifies the initial velocity of the Entity.
	Velocity mgl64.Vec3
	// ID specifies the UUID of an entity. This field should usually be left
	// empty, as a valid UUID is generated when not set. Non-player entities
	// only have the last 8 bytes of the UUID set.
	ID uuid.UUID
	// NameTag is the name tag that the entity is spawned with.
	NameTag string
	// SpawnReason records the Java spawn origin and requests initialisation
	// on first insertion unless SpawnFinalized is set.
	SpawnReason string
	// SpawnFinalized marks Source state already prepared by a generation worker.
	// Origin metadata is retained, but owner activation must not repeat RNG draws.
	SpawnFinalized bool
	// EntityRandom carries the independent Entity.random continuation from
	// preparation. New copies it because entity state has a different owner.
	EntityRandom *vanilla.Random
	// Riding attaches a newly prepared passenger to its worldless vehicle.
	// The vehicle must be installed before the passenger.
	Riding *EntityHandle
	// SpawnSeed supplies the independent spawn random stream. It is not the
	// structure decoration stream used for processors and loot.
	SpawnSeed int64
	// SpawnGroup carries species-specific cohort data between sequential spawns
	// on the same world thread. It is not retained by a published entity.
	SpawnGroup *EntitySpawnGroup
	// SpawnData retains the selected Java weighted spawn entry beside its
	// embedded entity compound during structure-driven entity construction.
	SpawnData map[string]any
}

// EntitySpawnGroup is the mutable group context shared by a spawn batch.
// Species own the concrete Data value, as with Java SpawnGroupData.
type EntitySpawnGroup struct {
	Data any
}

// New creates an EntityHandle using an EntityType and EntityConfig passed. The
// EntityHandle may be added to a world by calling Tx.AddEntity().
// The spawn conditions depend on the options set in opts.
func (opts EntitySpawnOpts) New(t EntityType, conf EntityConfig) *EntityHandle {
	if opts.ID == uuid.Nil {
		// Generate a new UUID with only the upper 8 bytes filled. This UUID
		// needs to be translatable to an int64.
		opts.ID = uuid.New()
		clear(opts.ID[:8])
	}
	handle := &EntityHandle{
		id:          opts.ID,
		t:           t,
		cond:        sync.NewCond(&sync.Mutex{}),
		worldless:   &atomic.Bool{},
		closed:      make(chan struct{}),
		data:        EntityData{AlwaysShowNameTag: true, SpawnReason: opts.SpawnReason},
		spawnReason: opts.SpawnReason,
		spawnSeed:   opts.SpawnSeed,
		spawnGroup:  opts.SpawnGroup,
	}
	handle.worldless.Store(true)
	handle.data.Pos, handle.data.Rot, handle.data.Vel = opts.Position, opts.Rotation, opts.Velocity
	handle.data.Name = opts.NameTag
	conf.Apply(&handle.data)
	handle.data.Riding = opts.Riding
	if opts.SpawnFinalized {
		handle.spawnReason = ""
	}
	if opts.EntityRandom != nil {
		random := *opts.EntityRandom
		handle.javaRandom = &random
	}
	return handle
}

// finalizeSpawn consumes pending initialisation before viewers see the entity.
// Clearing first prevents transfers or re-entrant insertion from repeating it.
func (e *EntityHandle) finalizeSpawn(entity Entity) {
	if e.spawnReason == "" {
		e.spawnGroup = nil
		return
	}
	reason, seed := e.spawnReason, e.spawnSeed
	e.spawnReason, e.spawnSeed = "", 0
	defer func() { e.spawnGroup = nil }()
	if finalizer, ok := entity.(interface {
		FinalizeSpawn(string, *vanilla.Random)
	}); ok {
		random := vanilla.NewXoroshiroRandom(seed)
		finalizer.FinalizeSpawn(reason, &random)
	}
}

// SpawnGroup returns the current cohort context. It is only valid during the
// entity's owner-thread FinalizeSpawn callback.
func (e *EntityHandle) SpawnGroup() *EntitySpawnGroup {
	return e.spawnGroup
}

// SpawnReason reports the reason this entity was created, including after it is
// restored from the world's provider.
func (e *EntityHandle) SpawnReason() string {
	return e.data.SpawnReason
}

// NewEntity creates an EntityHandle using an EntityType and EntityConfig
// passed. The EntityHandle may be added to a world by calling Tx.AddEntity().
// NewEntity uses the zero value for EntitySpawnOpts.
func NewEntity(t EntityType, conf EntityConfig) *EntityHandle {
	var opts EntitySpawnOpts
	return opts.New(t, conf)
}

// entityFromData reads an entity from the decoded NBT data passed and returns
// an EntityHandle.
func entityFromData(t EntityType, id int64, data map[string]any) *EntityHandle {
	handle := &EntityHandle{
		t:         t,
		cond:      sync.NewCond(&sync.Mutex{}),
		worldless: &atomic.Bool{},
		closed:    make(chan struct{}),
		data:      EntityData{AlwaysShowNameTag: true},
	}
	binary.LittleEndian.PutUint64(handle.id[8:], uint64(id))
	handle.decodeNBT(data)
	t.DecodeNBT(data, &handle.data)
	return handle
}

// Type returns the EntityType of the EntityHandle.
func (e *EntityHandle) Type() EntityType {
	return e.t
}

// Entity attempts to convert an EntityHandle to an Entity using the Tx passed.
// A non-nil Entity is returned only if the entity's world matches the world of
// the Tx. If they do not match, false is returned.
func (e *EntityHandle) Entity(tx *Tx) (Entity, bool) {
	if e == nil || e.w != tx.World() {
		return nil, false
	}
	return e.t.Open(tx, e, &e.data), true
}

// World returns the world currently owning the entity, or nil if the entity is worldless or closed.
func (e *EntityHandle) World() *World {
	if e == nil {
		return nil
	}
	e.cond.L.Lock()
	defer e.cond.L.Unlock()
	if e.w == closeWorld {
		return nil
	}
	return e.w
}

// SetPosition sets the position used the next time the worldless handle is added to a World.
// It panics if the handle is currently in a World.
func (e *EntityHandle) SetPosition(pos mgl64.Vec3) {
	e.cond.L.Lock()
	defer e.cond.L.Unlock()
	if e.w != nil {
		panic("cannot set entity handle position while it is in a world")
	}
	e.data.Pos = pos
}

// WorldlessEntity opens a view for preparing a newly created handle before it
// is added to a world. The caller must own the handle exclusively and must not
// retain the view after AddEntity. World membership is not changed.
func (e *EntityHandle) WorldlessEntity(tx *Tx) Entity {
	e.cond.L.Lock()
	defer e.cond.L.Unlock()
	if e.w != nil {
		panic("cannot prepare an entity handle already in a world")
	}
	return e.t.Open(tx, e, &e.data)
}

// mustEntity calls Entity but panics if the worlds do not match.
func (e *EntityHandle) mustEntity(tx *Tx) Entity {
	if ent, ok := e.Entity(tx); ok {
		return ent
	}
	panic("can't load entity with Tx of different world")
}

// UUID returns the identifier of the EntityHandle.
func (e *EntityHandle) UUID() uuid.UUID {
	return e.id
}

// UniqueID returns the signed Bedrock actor identity used by the world provider.
// It is distinct from a session's entity runtime ID.
func (e *EntityHandle) UniqueID() int64 {
	return int64(binary.LittleEndian.Uint64(e.id[8:]))
}

// Closed reports whether the EntityHandle has been closed.
func (e *EntityHandle) Closed() bool {
	if e == nil {
		return true
	}
	select {
	case <-e.closed:
		return true
	default:
		return false
	}
}

// Close closes the EntityHandle. Any subsequently scheduled work will fail
// with ErrEntityClosed without the transaction function being called. Close
// always returns nil.
func (e *EntityHandle) Close() error {
	e.closeOnce.Do(func() {
		e.setAndUnlockWorld(closeWorld)
		close(e.closed)
	})
	return nil
}

func cancelled(c <-chan struct{}) bool {
	if c == nil {
		return false
	}
	select {
	case <-c:
		return true
	default:
		return false
	}
}

// execWorld uses a sync.Cond to synchronise access to the handler's world. We
// are dealing with a rather complicated synchronisation pattern here. The goal
// for execWorld is to block until e.w becomes accessible. Meanwhile, World.exec
// may also affect e.w, which execWorld needs to deal with.
func (e *EntityHandle) execWorld(f func(tx *Tx, e Entity), weak bool, cancel <-chan struct{}, allowedCloseWorld *World) bool {
	e.cond.L.Lock()
	for e.w == nil || (e.w != closeWorld && (!e.worldReady || (!weak && e.weakTxActive))) {
		if cancelled(cancel) {
			if weak {
				e.clearWeakTxActiveLocked()
			}
			e.cond.L.Unlock()
			return false
		}
		// Wait suspends the current goroutine and unlocks e.cond.L, until
		// e.cond.Broadcast() is called. After this, one of the goroutines
		// waiting will acquire a lock of e.cond.L again. This means that only
		// one goroutine will run the code after this simultaneously.
		e.cond.Wait()
	}
	if cancelled(cancel) {
		if weak {
			e.clearWeakTxActiveLocked()
		}
		e.cond.L.Unlock()
		return false
	}
	// If a goroutine manages to exit the for loop, it will have acquired a lock
	// on e.cond.L. This also means that e.w can be assumed to not be nil here.
	// Because of the lock on e.cond.L, no other transaction will be able to
	// change e.w until we finish. e.worldless is set to true in
	// e.unsetAndLockWorld(), where the entity's world is removed.
	e.worldless.Store(false)
	if e.w == closeWorld {
		// EntityHandle was closed. No need to continue.
		if weak {
			e.clearWeakTxActiveLocked()
		}
		e.cond.L.Unlock()
		return false
	}
	if e.w.closed.Load() && !e.w.closeAcceptingEntityTasks.Load() && e.w != allowedCloseWorld {
		if weak {
			e.clearWeakTxActiveLocked()
		}
		e.cond.L.Unlock()
		return false
	}
	// We now arrive at the more complicated part. When we call e.w.exec(), our
	// transaction must await earlier transactions in the world. If one of those
	// earlier transactions tries to change e.w (through e.unsetAndLockWorld()
	// or e.setAndUnlockWorld()), it must lock e.cond.L. This would lead to a
	// deadlock, because we already have e.cond.L locked here.
	// We work around this with so-called "weak transactions". This is a
	// transaction that may be invalidated before it is executed. In this case,
	// this invalidation happens by setting e.worldless to true. If the
	// transaction turns out to be invalidated (ret == false), we simply try
	// again, this time with e.execWorld(f, true) to make this goroutine bypass
	// any goroutines still awaiting e.cond.
	var ran atomic.Bool
	ret := e.weakExec(func(tx *Tx) {
		e.cond.L.Lock()
		if e.w != tx.World() {
			e.cond.L.Unlock()
			return
		}
		var ent Entity
		if e.state == nil {
			ent = e.t.Open(tx, e, &e.data)
		} else {
			ent = e.state.entity(tx, e)
		}
		e.cond.L.Unlock()

		ran.Store(true)
		f(tx, ent)
	}, allowedCloseWorld)
	if !ret && e.w != nil && e.w != closeWorld && e.w.closed.Load() && !e.w.closeAcceptingEntityTasks.Load() && e.w != allowedCloseWorld {
		e.clearWeakTxActiveLocked()
		e.cond.L.Unlock()
		return false
	}
	e.cond.L.Unlock()

	if ran.Load() {
		return true
	}
	if !ret {
		// Our weak transaction was suspended. We try again, this time with
		// e.execWorld(f, true) to make this goroutine bypass any goroutines
		// still awaiting e.cond.
		return e.execWorld(f, true, cancel, allowedCloseWorld)
	}
	return true
}

// weakExec performs a "weak transaction". It adds a transaction to the world
// that is invalidated when e.worldless is set to true. In this case, weakExec
// returns false. If the weak transaction is successfully executed, it returns
// true, and any calls to execWorld waiting on e.cond are awakened. The goal of
// weakExec is to suspend the current goroutine and unlock e.cond.L while
// waiting for previous transactions to finish.
func (e *EntityHandle) weakExec(f execFunc, allowedCloseWorld *World) bool {
	e.weakTxActive = true
	w, version := e.w, e.worldVersion.Load()

	// We create a weak transaction and start a for loop to listen for the
	// length of the channel. This might look weird, but the crucial part here
	// is the call to e.cond.Wait(), which unlocks e.cond.L. This is required
	// to prevent a deadlock if an earlier transaction tries to change e.w.
	c := w.weakExec(func() bool {
		return e.worldVersion.Load() == version && !e.worldless.Load()
	}, e.cond, f, w == allowedCloseWorld)
	for len(c) == 0 && e.w != closeWorld {
		// Calling e.cond.Wait() here will free the lock on e.cond.L until our
		// transaction finishes. e.w.weakExec() ensures that e.cond.Broadcast()
		// is called once the transaction finished/is suspended, so we can
		// continue after that.
		e.cond.Wait()
	}
	if e.w != closeWorld && !<-c {
		// Weak transaction was suspended. Return false and try again.
		return false
	}
	// After setting e.weakTxActive back to false, we must Broadcast to make
	// sure any goroutines waiting in e.execWorld as a result of the
	// e.weakTxActive condition can continue.
	closed := e.w == closeWorld
	e.weakTxActive = false
	e.cond.Broadcast()
	return !closed
}

func (e *EntityHandle) clearWeakTxActiveLocked() {
	if e.weakTxActive {
		e.weakTxActive = false
		e.cond.Broadcast()
	}
}

var closeWorld = &World{}

// unsetAndLockWorld sets e.w to nil, causing any subsequent calls to execWorld
// to block until e.w is set to a non-nil value.
func (e *EntityHandle) unsetAndLockWorld() {
	e.cond.L.Lock()
	defer e.cond.L.Unlock()

	e.worldless.Store(true)
	e.w = nil
	e.worldReady = false
	e.worldVersion.Add(1)
	e.notifyWorldChangedLocked()
}

// setAndUnlockWorld starts binding e to w. Scheduled callbacks remain blocked
// until markWorldReady is called after AddEntity finishes.
func (e *EntityHandle) setAndUnlockWorld(w *World) {
	e.cond.L.Lock()
	defer e.cond.L.Unlock()

	if e.w != nil {
		panic("cannot add entity to new world before removing from old world")
	}
	e.w = w
	e.worldReady = false
	e.worldVersion.Add(1)
	e.notifyWorldChangedLocked()
}

// markWorldReady wakes scheduled callbacks after AddEntity has fully opened
// and registered the entity in w.
func (e *EntityHandle) markWorldReady(w *World) {
	e.cond.L.Lock()
	defer e.cond.L.Unlock()
	if e.w == w {
		e.worldReady = true
		e.cond.Broadcast()
	}
}

func (e *EntityHandle) notifyWorldChangedLocked() {
	if e.worldChanged != nil {
		close(e.worldChanged)
		e.worldChanged = nil
	}
	e.cond.Broadcast()
}

// decodeNBT decodes the position, velocity, rotation, age, on-fire duration and
// name tag of an entity.
func (e *EntityHandle) decodeNBT(m map[string]any) {
	e.data.NativeNBT = CloneNativeEntityNBT(m)
	e.data.Pos = readVec3(m, "Pos")
	e.data.Vel = readVec3(m, "Motion")
	e.data.Rot = readRotation(m)
	e.data.Age = time.Duration(readInt16(m, "Age")) * (time.Second / 20)
	e.data.FireDuration = time.Duration(readInt16(m, "Fire")) * time.Second / 20
	e.data.Name, _ = m["NameTag"].(string)
	if name, ok := m["CustomName"].(string); ok && e.data.Name == "" {
		e.data.Name = name
		if visible, ok := m["CustomNameVisible"].(uint8); ok {
			e.data.AlwaysShowNameTag = visible != 0
		}
	}
	if javaData, ok := m["adamant:java"].(map[string]any); ok {
		e.data.SpawnReason, _ = javaData["SpawnReason"].(string)
		if words, ok := javaData["UUID"].([]int32); ok && len(words) == 4 {
			var id uuid.UUID
			for i, word := range words {
				binary.BigEndian.PutUint32(id[i*4:], uint32(word))
			}
			if binary.LittleEndian.Uint64(id[8:]) == binary.LittleEndian.Uint64(e.id[8:]) {
				e.id = id
			}
		}
		if words, ok := javaData["VehicleUUID"].([]int32); ok && len(words) == 4 {
			for i, word := range words {
				binary.BigEndian.PutUint32(e.pendingVehicle[i*4:], uint32(word))
			}
		}
	}
}

// encodeNBT encodes the position, velocity, rotation, age, on-fire duration and
// name tag of an entity.
func (e *EntityHandle) encodeNBT() map[string]any {
	data := MergeNativeEntityNBT(e.data.NativeNBT, map[string]any{
		"Pos":    []float32{float32(e.data.Pos[0]), float32(e.data.Pos[1]), float32(e.data.Pos[2])},
		"Motion": []float32{float32(e.data.Vel[0]), float32(e.data.Vel[1]), float32(e.data.Vel[2])},
		// BDS saves the rotation as a list; Yaw and Pitch are kept for
		// earlier Adamant versions.
		"Rotation": []float32{float32(e.data.Rot[0]), float32(e.data.Rot[1])},
		"Yaw":      float32(e.data.Rot[0]),
		"Pitch":    float32(e.data.Rot[1]),
		"Fire":     int16(e.data.FireDuration.Seconds() * 20),
		"Age":      int16(min(e.data.Age/(time.Second/20), math.MaxInt16)),
		"NameTag":  e.data.Name,
	})
	if e.data.Name != "" {
		// The name as BDS saves it.
		data["CustomName"] = e.data.Name
		data["CustomNameVisible"] = uint8(0)
		if e.data.AlwaysShowNameTag {
			data["CustomNameVisible"] = uint8(1)
		}
	}
	if e.data.SpawnReason != "" || binary.BigEndian.Uint64(e.id[:8]) != 0 || e.data.Riding != nil || e.pendingVehicle != uuid.Nil || data["adamant:java"] != nil {
		javaData, _ := data["adamant:java"].(map[string]any)
		javaData = CloneNativeEntityNBT(javaData)
		if javaData == nil {
			javaData = make(map[string]any)
		}
		if e.data.SpawnReason != "" {
			javaData["SpawnReason"] = e.data.SpawnReason
		}
		if binary.BigEndian.Uint64(e.id[:8]) != 0 {
			words := make([]int32, 4)
			for i := range words {
				words[i] = int32(binary.BigEndian.Uint32(e.id[i*4:]))
			}
			javaData["UUID"] = words
		}
		delete(javaData, "VehicleUUID")
		vehicle := e.pendingVehicle
		if e.data.Riding != nil {
			vehicle = e.data.Riding.id
		}
		if vehicle != uuid.Nil {
			words := make([]int32, 4)
			for i := range words {
				words[i] = int32(binary.BigEndian.Uint32(vehicle[i*4:]))
			}
			javaData["VehicleUUID"] = words
		}
		data["adamant:java"] = javaData
	}
	return data
}

// EntityData holds data shared by every entity. It is kept in an EntityHandle.
type EntityData struct {
	Pos, Vel          mgl64.Vec3
	Rot               cube.Rotation
	Name              string
	SpawnReason       string
	AlwaysShowNameTag bool
	FireDuration      time.Duration
	Age               time.Duration
	Riding            *EntityHandle
	// Seat is the seat of the vehicle the entity rides, or last rode: like
	// Bedrock, the entity keeps it after dismounting.
	Seat RiderSeat
	// FrozenTicks is Java's Entity.ticksFrozen: how far the entity froze in
	// powder snow, up to 140 when it is fully frozen.
	FrozenTicks int
	// InPowderSnow is set while the entity's box is inside powder snow during
	// the current tick.
	InPowderSnow bool
	// WasInPowderSnow is InPowderSnow of the previous tick.
	WasInPowderSnow bool
	// OnGround is set while the entity's last movement ended on a surface.
	OnGround bool
	// FallDistance is how far the entity fell since it last stood on the
	// ground.
	FallDistance float64

	// NativeNBT retains the original actor payload so entity serializers can
	// update known fields without discarding fields unknown to this server.
	NativeNBT map[string]any

	Data any
}

// RiderSeat is the seat a rider takes on its vehicle, as a seat of Bedrock's
// minecraft:rideable component places it. Clients render riders from it.
type RiderSeat struct {
	// Position is the rider's offset from the vehicle.
	Position mgl64.Vec3
	// LockRotation reports whether the rider can turn only
	// LockRotationDegrees away from the vehicle. Seats without a lock
	// report 181 degrees.
	LockRotation        bool
	LockRotationDegrees float64
	// RotateRider reports whether the rider faces RotateRiderDegrees away
	// from the vehicle.
	RotateRider        bool
	RotateRiderDegrees float64
}

// Entity represents an Entity in the world, typically an object that may be moved around and can be
// interacted with by other entities.
// Viewers of a world may view an Entity when near it.
type Entity interface {
	io.Closer
	// H returns the EntityHandle that points to the entity.
	H() *EntityHandle
	// Position returns the current position of the Entity in the world.
	Position() mgl64.Vec3
	// Rotation returns the yaw (horizontal rotation) and pitch (vertical
	// rotation) of the entity in degrees.
	Rotation() cube.Rotation
}

// TickerEntity represents an Entity that has a Tick method which should be called every time the Entity is
// ticked every 20th of a second.
type TickerEntity interface {
	Entity
	// Tick ticks the Entity with the current World and tick passed.
	Tick(tx *Tx, current int64)
}

// EntityAction represents an action that may be performed by an Entity. Typically, these actions are sent to
// viewers in a world so that they can see these actions.
type EntityAction interface {
	EntityAction()
}

// DamageSource represents the source of the damage dealt to an Entity. This
// source may be passed to the Hurt() method of an Entity in order to deal
// damage to an Entity with a specific source.
type DamageSource interface {
	// ReducedByArmour checks if the source of damage may be reduced if the
	// receiver of the damage is wearing armour.
	ReducedByArmour() bool
	// ReducedByResistance specifies if the Source is affected by the resistance
	// effect. If false, damage dealt to an Entity with this source will not be
	// lowered if the Entity has the resistance effect.
	ReducedByResistance() bool
	// Fire specifies if the Source is fire related and should be ignored when
	// an Entity has the fire resistance effect.
	Fire() bool
	// IgnoreTotem specifies whether the totem will be ignored if the damage is lethal.
	IgnoreTotem() bool
}

// ArmourEffectivenessReducer exposes a multiplier that scales how much armour reduces
// incoming damage for a specific world.DamageSource.
type ArmourEffectivenessReducer interface {
	DamageSource
	// ArmourEffectivenessMultiplier returns a multiplier in the range [0, 1]
	// that scales the armour damage reduction. Values close to 0 drastically
	// reduce armour effectiveness, while 1 keeps vanilla behaviour.
	ArmourEffectivenessMultiplier() float64
}

// HealingSource represents a source of healing for an Entity. This source may
// be passed to the Heal() method of a living Entity.
type HealingSource interface {
	HealingSource()
}

// EntityRegistry is a mapping that EntityTypes may be registered to. It is used
// for loading entities from disk in a World's Provider.
type EntityRegistry struct {
	conf EntityRegistryConfig
	ent  map[string]EntityType
}

// EntityRegistryConfig holds functions used by the block and item packages to
// create entities as a result of their behaviour. ALL functions of
// EntityRegistryConfig must be filled out for the behaviour of these blocks and
// items not to fail.
type EntityRegistryConfig struct {
	// SpawnMob creates a concrete registered living mob for a Java entity ID.
	// It returns nil for an ID without an implementation.
	SpawnMob func(id string, opts EntitySpawnOpts) *EntityHandle
	// SpawnMobFromNBT creates a concrete mob and applies its species codec
	// before the handle is published in a world.
	SpawnMobFromNBT func(id string, opts EntitySpawnOpts, data map[string]any) *EntityHandle
	// OminousItemSpawner creates a persistent item-spawner entity for trial spawners.
	OminousItemSpawner func(opts EntitySpawnOpts, stack any, delay int64) *EntityHandle
	// NaturalSpawnTick runs source-defined natural ecology once per world tick.
	NaturalSpawnTick func(*Tx)
	// EntityCollisions pushes overlapping entities apart once per world tick.
	EntityCollisions func(*Tx)
	// EncodeSharedNBT and DecodeSharedNBT persist state many entity types
	// share beside each type's own codec, such as the status effects of mobs.
	EncodeSharedNBT func(data *EntityData) map[string]any
	DecodeSharedNBT func(data *EntityData, m map[string]any)
	Item             func(opts EntitySpawnOpts, it any) *EntityHandle
	// Painting creates a painting occupying pos and facing away from its wall,
	// choosing a canvas that fits. It returns nil if none fits.
	Painting func(tx *Tx, pos cube.Pos, facing cube.Direction) *EntityHandle
	// EyeOfEnder throws an eye of ender signal towards a located stronghold.
	EyeOfEnder func(opts EntitySpawnOpts, target mgl64.Vec3) *EntityHandle
	// TieLeashedEntities attaches entities held by a user to a fence knot.
	TieLeashedEntities func(pos cube.Pos, tx *Tx, user Entity) bool
	// ExperienceOrbs creates the source denominations for a block's XP reward.
	ExperienceOrbs func(pos mgl64.Vec3, amount int) []*EntityHandle
	NewFromBucket  func(opts EntitySpawnOpts, entityID string, data map[string]any) *EntityHandle
	EnderDragon    func(opts EntitySpawnOpts) *EntityHandle
	DragonFight    func(seed int64, data DragonFightData) DragonFightController
	// GolemPattern attempts source-defined constructed-mob patterns after a
	// carved pumpkin or jack-o'-lantern replaces a different block type.
	GolemPattern func(tx *Tx, pos cube.Pos) bool
	// WitherPattern attempts the source skull-and-soul-sand construction pattern.
	WitherPattern      func(tx *Tx, pos cube.Pos) bool
	FallingBlock       func(opts EntitySpawnOpts, bl Block) *EntityHandle
	TNT                func(opts EntitySpawnOpts, fuse time.Duration, owner Entity) *EntityHandle
	Boat               func(opts EntitySpawnOpts, variant int) *EntityHandle
	ChestBoat          func(opts EntitySpawnOpts, variant int) *EntityHandle
	Minecart           func(opts EntitySpawnOpts) *EntityHandle
	MinecartChest      func(opts EntitySpawnOpts) *EntityHandle
	MinecartHopper     func(opts EntitySpawnOpts) *EntityHandle
	MinecartTNT        func(opts EntitySpawnOpts) *EntityHandle
	BottleOfEnchanting func(opts EntitySpawnOpts, owner Entity) *EntityHandle
	Arrow              func(opts EntitySpawnOpts, conf ArrowSpawnConfig) *EntityHandle
	Trident            func(opts EntitySpawnOpts, owner Entity, stack any, loyalty, impaling int, channeling bool) *EntityHandle
	Egg                func(opts EntitySpawnOpts, owner Entity) *EntityHandle
	EggVariant         func(opts EntitySpawnOpts, owner Entity, variant int) *EntityHandle
	EnderPearl         func(opts EntitySpawnOpts, owner Entity) *EntityHandle
	Firework           func(opts EntitySpawnOpts, firework Item, owner Entity, sidewaysVelocityMultiplier, upwardsAcceleration float64, attached bool) *EntityHandle
	LingeringPotion    func(opts EntitySpawnOpts, t any, owner Entity) *EntityHandle
	Snowball           func(opts EntitySpawnOpts, owner Entity) *EntityHandle
	SmallFireball      func(opts EntitySpawnOpts, owner Entity) *EntityHandle
	WindCharge         func(opts EntitySpawnOpts, owner Entity) *EntityHandle
	SplashPotion       func(opts EntitySpawnOpts, t any, owner Entity) *EntityHandle
	Lightning          func(opts EntitySpawnOpts) *EntityHandle
	// VisualLightning creates a bolt that neither damages nor lights fires,
	// as the bolt of a skeleton trap.
	VisualLightning func(opts EntitySpawnOpts) *EntityHandle
	EndCrystal      func(opts EntitySpawnOpts, showBase bool) *EntityHandle
	Shulker         func(opts EntitySpawnOpts) *EntityHandle
	ShulkerBullet   func(opts EntitySpawnOpts, owner, target *EntityHandle) *EntityHandle
	Cushion         func(opts EntitySpawnOpts, color int) *EntityHandle
	ArmourStand     func(opts EntitySpawnOpts) *EntityHandle
	FishingHook     func(opts EntitySpawnOpts, owner Entity, rod any) *EntityHandle
	// RaidSpawner creates a registered raider with its raid wave metadata before
	// the entity is published to world viewers.
	RaidSpawner func(opts EntitySpawnOpts, entityID string, raidID int64, wave int, leader bool) *EntityHandle
	// MinecartCommandBlock creates a command block minecart.
	MinecartCommandBlock func(opts EntitySpawnOpts) *EntityHandle
}

// ArrowSpawnConfig holds the options used to spawn an arrow entity.
type ArrowSpawnConfig struct {
	// Damage specifies the base damage dealt by the arrow.
	Damage float64
	// PowerLevel specifies the level of the Power enchantment applied to the arrow.
	PowerLevel int
	// Owner is the entity that fired the arrow.
	Owner Entity
	// Critical specifies if the arrow should deal critical damage.
	Critical bool
	// DisablePickup specifies if picking up the arrow should be disabled.
	DisablePickup bool
	// ObtainArrowOnPickup specifies if the arrow should be returned as an item when picked up.
	ObtainArrowOnPickup bool
	// PunchLevel specifies the level of punch knockback applied to the arrow.
	PunchLevel int
	// PiercingLevel is the crossbow Piercing enchantment level. The arrow passes
	// through PiercingLevel entities and damages PiercingLevel+1 in total. A
	// value of 0 means no piercing.
	PiercingLevel int
	// Tip specifies the potion tip carried by the arrow.
	Tip any
}

// New creates an EntityRegistry using conf and the EntityTypes passed.
func (conf EntityRegistryConfig) New(ent []EntityType) EntityRegistry {
	m := make(map[string]EntityType, len(ent))
	for _, e := range ent {
		name := e.EncodeEntity()
		if _, ok := m[name]; ok {
			panic("cannot register the same entity (" + name + ") twice")
		}
		m[name] = e
	}
	return EntityRegistry{conf: conf, ent: m}
}

// Config returns the EntityRegistryConfig that was used to create the
// EntityRegistry.
func (reg EntityRegistry) Config() EntityRegistryConfig {
	return reg.conf
}

// Lookup looks up an EntityType by its name, or by the Bedrock identifier of
// a type whose name differs from it, as worlds saved by BDS hold. If found,
// the EntityType is returned and the bool is true. The bool is false otherwise.
func (reg EntityRegistry) Lookup(name string) (EntityType, bool) {
	t, ok := reg.ent[name]
	if !ok {
		if registered, alias := registeredEntityNames[name]; alias {
			t, ok = reg.ent[registered]
		}
	}
	return t, ok
}

// bedrockEntityIdentifiers maps the names entity types are registered and
// compared by to the Bedrock identifiers BDS saves and clients spawn, for the
// types whose names differ: clients show nothing for minecraft:evoker or
// minecraft:tropical_fish and the legacy, pre-1.14 mob for minecraft:villager.
var bedrockEntityIdentifiers = map[string]string{
	"minecraft:villager":        "minecraft:villager_v2",
	"minecraft:zombie_villager": "minecraft:zombie_villager_v2",
	"minecraft:evoker":          "minecraft:evocation_illager",
	"minecraft:tropical_fish":   "minecraft:tropicalfish",
}

// registeredEntityNames is the inverse of bedrockEntityIdentifiers.
var registeredEntityNames = func() map[string]string {
	m := make(map[string]string, len(bedrockEntityIdentifiers))
	for name, id := range bedrockEntityIdentifiers {
		m[id] = name
	}
	return m
}()

// BedrockEntityIdentifier returns the Bedrock identifier of the entity type
// registered as name, which saves and clients use.
func BedrockEntityIdentifier(name string) string {
	if id, ok := bedrockEntityIdentifiers[name]; ok {
		return id
	}
	return name
}

// Types returns all EntityTypes passed upon construction of the EntityRegistry.
func (reg EntityRegistry) Types() []EntityType {
	return slices.Collect(maps.Values(reg.ent))
}

func readVec3(x map[string]any, k string) mgl64.Vec3 {
	if i, ok := x[k].([]any); ok {
		if len(i) != 3 {
			return mgl64.Vec3{}
		}
		var v mgl64.Vec3
		for index, f := range i {
			f32, _ := f.(float32)
			v[index] = float64(f32)
		}
		return v
	} else if i, ok := x[k].([]float32); ok {
		if len(i) != 3 {
			return mgl64.Vec3{}
		}
		return mgl64.Vec3{float64(i[0]), float64(i[1]), float64(i[2])}
	}
	return mgl64.Vec3{}
}

func readFloat32(m map[string]any, k string) float32 {
	v, _ := m[k].(float32)
	return v
}

// readRotation reads the rotation BDS saves as a list of yaw and pitch, or
// the Yaw and Pitch earlier Adamant versions saved.
func readRotation(m map[string]any) cube.Rotation {
	switch r := m["Rotation"].(type) {
	case []float32:
		if len(r) == 2 {
			return cube.Rotation{float64(r[0]), float64(r[1])}
		}
	case []any:
		if len(r) == 2 {
			yaw, _ := r[0].(float32)
			pitch, _ := r[1].(float32)
			return cube.Rotation{float64(yaw), float64(pitch)}
		}
	}
	return cube.Rotation{float64(readFloat32(m, "Yaw")), float64(readFloat32(m, "Pitch"))}
}

func readInt16(m map[string]any, k string) int16 {
	v, _ := m[k].(int16)
	return v
}
