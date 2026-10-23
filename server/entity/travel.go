package entity

import (
	"context"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/portal"
	"github.com/go-gl/mathgl/mgl64"
)

// PortalTravelComputer handles portal-triggered interdimensional travel for an entity.
type PortalTravelComputer struct {
	// Delay returns how long the entity must stay in a portal before it travels
	// to the target dimension. Nil or zero travels immediately, as Java does
	// for every non-player entity.
	Delay func(tx *world.Tx, target world.Dimension) time.Duration
	// Teleport installs the resolved position, absolute rotation and velocity. If nil, Traveller methods are used.
	Teleport func(e Traveller, pos mgl64.Vec3, rotation cube.Rotation, velocity mgl64.Vec3)
	// SpawnPoint returns the position the entity arrives at when returning from the End; if nil the world spawn is used.
	SpawnPoint func(tx *world.Tx) mgl64.Vec3
	// TeleportAllowed runs after resolving the destination but before changing traveller state.
	TeleportAllowed func(e Traveller, pos mgl64.Vec3) bool
	// DestinationWorld may select a portal destination from entity-owned respawn configuration.
	DestinationWorld func(tx *world.Tx, target world.Dimension) *world.World
	// SpawnRotation returns an absolute arrival rotation and whether a source-specific respawn point was used.
	SpawnRotation func(tx *world.Tx) (cube.Rotation, bool)
	// Player specifies whether the traveller is a player. Java players arrive at y=49, other entities at y=50.
	Player bool
	// CreatePortal specifies if the entity may create a portal at the destination when none is found.
	CreatePortal bool
	// Cooldown is how long the entity must wait after a travel attempt before it may travel again.
	Cooldown time.Duration

	mu             sync.Mutex
	start          time.Time
	cooldownUntil  time.Time
	inside         bool
	awaitingTravel bool
	travelling     bool
	timedOut       bool
	pending        *world.World
	deniedDim      world.Dimension
	deniedActive   bool
}

// NewPortalTravelComputer creates a PortalTravelComputer for instant portal travel.
func NewPortalTravelComputer() *PortalTravelComputer {
	return &PortalTravelComputer{Cooldown: time.Second * 15}
}

type portalTravelComputerProvider interface {
	PortalTravelComputer() *PortalTravelComputer
}

// Traveller represents a world.Entity that can travel between dimensions.
type Traveller interface {
	world.Entity
	Teleport(pos mgl64.Vec3)
}
type portalTravelOrigin struct {
	position    mgl64.Vec3
	rotation    cube.Rotation
	velocity    mgl64.Vec3
	hasVelocity bool
}

func capturePortalTravelOrigin(e Traveller) portalTravelOrigin {
	origin := portalTravelOrigin{position: e.Position(), rotation: e.Rotation()}
	if mover, ok := e.(interface{ Velocity() mgl64.Vec3 }); ok {
		origin.velocity, origin.hasVelocity = mover.Velocity(), true
	}
	return origin
}

type portalTravelHandler interface {
	HandlePortalTravel(source, destination world.Dimension)
}

// EnterPortal handles an entity touching a portal block.
func (t *PortalTravelComputer) EnterPortal(e Traveller, tx *world.Tx, target world.Dimension) {
	if !netherEntryAllowed(tx, target) {
		return
	}
	destination := t.portalDestination(tx, target)
	if destination == nil || destination == tx.World() {
		t.notifyDenied(e, tx, target)
		return
	}
	t.clearDenied()
	if t.enterPortalDestination(t.delay(tx, target), destination) {
		t.travelQueued(e, tx, destination)
	}
}

func (t *PortalTravelComputer) queuePortalTravel(tx *world.Tx, target world.Dimension) {
	if destination := t.enterPortal(tx, target); destination != nil {
		t.mu.Lock()
		t.pending = destination
		t.mu.Unlock()
	}
}

func (t *PortalTravelComputer) enterPortal(tx *world.Tx, target world.Dimension) *world.World {
	source := tx.World()
	if !netherEntryAllowed(tx, target) {
		return nil
	}
	destination := t.portalDestination(tx, target)
	if destination == nil || destination == source {
		return nil
	}
	if t.enterPortalDestination(t.delay(tx, target), destination) {
		return destination
	}
	return nil
}

func (t *PortalTravelComputer) enterPortalDestination(delay time.Duration, destination *world.World) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.inside = true
	if t.timedOut || time.Now().Before(t.cooldownUntil) {
		return false
	}
	travelNow := delay <= 0 || (t.awaitingTravel && time.Since(t.start) >= delay)
	if !travelNow && !t.awaitingTravel {
		t.start, t.awaitingTravel = time.Now(), true
	}
	return travelNow && destination != nil
}

func (t *PortalTravelComputer) delay(tx *world.Tx, target world.Dimension) time.Duration {
	if t.Delay == nil {
		return 0
	}
	return t.Delay(tx, target)
}

// netherEntryAllowed ports MinecraftServer.isLevelEnabled for portal travel:
// with allow_entering_nether_using_portals off, portals do nothing on the
// way into the Nether while the way back still works.
func netherEntryAllowed(tx *world.Tx, target world.Dimension) bool {
	return target != world.Nether || tx.World().GameRuleBool("allow_entering_nether_using_portals")
}

func (t *PortalTravelComputer) hasPendingPortalTravel() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.pending != nil
}

func (t *PortalTravelComputer) finishPendingPortalTravel(e Traveller, tx *world.Tx) bool {
	t.mu.Lock()
	destination := t.pending
	t.pending = nil
	t.mu.Unlock()
	if destination == nil {
		return false
	}
	t.travel(e, tx, destination)
	return true
}

// StopPortalContact resets the portal timer if the entity was not inside a portal this tick.
func (t *PortalTravelComputer) StopPortalContact() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.inside {
		t.inside = false
		return
	}
	if t.travelling || t.pending != nil {
		return
	}
	t.timedOut, t.awaitingTravel, t.deniedActive = false, false, false
}

func (t *PortalTravelComputer) travel(e Traveller, tx *world.Tx, destination *world.World) {
	source := tx.World()
	if destination == nil || destination == source {
		return
	}
	sourceDim, destinationDim := source.Dimension(), destination.Dimension()
	origin := capturePortalTravelOrigin(e)
	pos := translatePortalPosition(cube.PosFromVec3(origin.position), sourceDim, destinationDim)

	t.beginTravel()
	handle := tx.RemoveEntity(e)
	if handle == nil {
		t.resetFailedTravel()
		return
	}
	go t.transfer(handle, source, destination, origin, pos, sourceDim, destinationDim)
}

func (t *PortalTravelComputer) travelQueued(e Traveller, tx *world.Tx, destination *world.World) {
	source := tx.World()
	if destination == nil || destination == source {
		return
	}
	sourceDim, destinationDim := source.Dimension(), destination.Dimension()
	origin := capturePortalTravelOrigin(e)
	pos := translatePortalPosition(cube.PosFromVec3(origin.position), sourceDim, destinationDim)

	t.beginTravel()
	h := e.H()
	tx.Defer(func(tx *world.Tx) {
		e, ok := h.Entity(tx)
		if !ok {
			t.resetFailedTravel()
			return
		}
		handle := tx.RemoveEntity(e)
		if handle == nil {
			t.resetFailedTravel()
			return
		}
		go t.transfer(handle, source, destination, origin, pos, sourceDim, destinationDim)
	})
}

func (t *PortalTravelComputer) beginTravel() {
	t.mu.Lock()
	t.travelling, t.timedOut, t.awaitingTravel, t.deniedActive = true, true, false, false
	t.mu.Unlock()
}

func (t *PortalTravelComputer) resetFailedTravel() {
	t.mu.Lock()
	t.travelling, t.timedOut = false, false
	t.mu.Unlock()
}

// portalPreloadTimeout bounds how long a traveller waits for the chunks of
// its destination portal to load before the destination reads them itself.
const portalPreloadTimeout = 30 * time.Second

// portalFrameReach is how far a portal found in the POI index may extend from
// the portal block found: the widest portal and its frame.
const portalFrameReach = maximumNetherPortalSize + 1

// maximumNetherPortalSize is the largest width or height of a Nether portal.
const maximumNetherPortalSize = 21

// portalCreationReach is how far a new portal and its frame may extend from
// the position it is created for: CreateNetherPortal searches 16 blocks
// around it and builds a frame up to four blocks beyond.
const portalCreationReach = 16 + 4

func (t *PortalTravelComputer) transfer(handle *world.EntityHandle, source, destination *world.World, origin portalTravelOrigin, pos cube.Pos, sourceDim, destinationDim world.Dimension) {
	// The chunks the destination reads are loaded, or generated, before it
	// places the traveller, so that it does not wait for them on its tick.
	if ready, err := safeWorldCall(destination, func(tx *world.Tx) (<-chan struct{}, error) {
		return t.loadDestinationArea(tx, sourceDim, pos), nil
	}); err == nil && ready != nil {
		select {
		case <-ready:
		case <-time.After(portalPreloadTimeout):
		}
	}
	travelled, err := safeWorldCall(destination, func(tx *world.Tx) (bool, error) {
		spawn, ok := t.destinationSpawn(tx, sourceDim, pos)
		if !ok {
			return false, nil
		}
		handle.SetPosition(spawn)
		e, ok := tx.AddEntity(handle).(Traveller)
		if !ok {
			return false, nil
		}
		var rotation cube.Rotation
		applyRotation := false
		if sourceDim == world.End && t.SpawnRotation != nil {
			rotation, applyRotation = t.SpawnRotation(tx)
		}
		if !t.finishTravel(e, spawn, sourceDim, destinationDim, rotation, applyRotation) {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		travelled = false
	}
	if !travelled {
		detachPortalEntity(handle, destination)
		restored := false
		var restoreErr error
		if handle.World() == nil {
			restored, restoreErr = safeWorldCall(source, func(tx *world.Tx) (bool, error) {
				handle.SetPosition(origin.position)
				e, ok := tx.AddEntity(handle).(Traveller)
				if !ok {
					return false, nil
				}
				applyPortalRotation(e, origin.rotation)
				if origin.hasVelocity {
					if mover, ok := e.(interface{ SetVelocity(mgl64.Vec3) }); ok {
						mover.SetVelocity(origin.velocity)
					}
				}
				return true, nil
			})
		}
		if restoreErr != nil || !restored {
			detachPortalEntity(handle, source)
			detachPortalEntity(handle, destination)
			if handle.World() == nil {
				_ = handle.Close()
			}
		}
	}

	t.mu.Lock()
	t.travelling = false
	t.cooldownUntil = time.Now().Add(t.Cooldown)
	if !travelled {
		t.timedOut = false
	}
	t.mu.Unlock()
}

func safeWorldCall[T any](w *world.World, f func(*world.Tx) (T, error)) (result T, err error) {
	defer func() {
		if recover() != nil {
			var zero T
			result, err = zero, world.ErrTaskPanicked
		}
	}()
	return world.Call(context.Background(), w, f)
}

func detachPortalEntity(handle *world.EntityHandle, w *world.World) {
	_, _ = safeWorldCall(w, func(tx *world.Tx) (struct{}, error) {
		if e, ok := handle.Entity(tx); ok {
			tx.RemoveEntity(e)
		}
		return struct{}{}, nil
	})
}

// destinationSpawn returns the position the entity should be placed at in the destination world. False is returned
// if no linked nether portal was found and none could be created.
func (t *PortalTravelComputer) destinationSpawn(tx *world.Tx, sourceDim world.Dimension, pos cube.Pos) (mgl64.Vec3, bool) {
	if tx.World().Dimension() == world.End {
		portal.GenerateEndSpawnPlatform(tx)
		return portal.EndSpawnPosition(t.Player), true
	}
	if sourceDim == world.End {
		if t.SpawnPoint != nil {
			return t.SpawnPoint(tx), true
		}
		return tx.World().Spawn().Vec3Middle(), true
	}
	radius := portalSearchRadius(tx.World().Dimension())
	if !t.CreatePortal {
		n, ok := portal.FindNetherPortal(tx, pos, radius)
		if !ok {
			return mgl64.Vec3{}, false
		}
		return n.Spawn().Vec3Middle(), true
	}
	if n, ok := portal.FindOrCreateNetherPortal(tx, pos, radius); ok {
		return n.Spawn().Vec3Middle(), true
	}
	return mgl64.Vec3{}, false
}

// loadDestinationArea starts loading the chunks destinationSpawn reads in the
// destination of tx: those around the nearest linked portal, or those a new
// portal may be built in. It returns nil if there are none to load.
func (t *PortalTravelComputer) loadDestinationArea(tx *world.Tx, sourceDim world.Dimension, pos cube.Pos) <-chan struct{} {
	if tx.World().Dimension() == world.End || sourceDim == world.End {
		return nil
	}
	centre, reach := pos, portalCreationReach
	if found, ok := portal.NearestNetherPortal(tx, pos, portalSearchRadius(tx.World().Dimension())); ok {
		centre, reach = found, portalFrameReach
	} else if !t.CreatePortal {
		return nil
	}
	return tx.LoadArea(centre.Sub(cube.Pos{reach, 0, reach}), centre.Add(cube.Pos{reach, 0, reach}))
}

// portalSearchRadius is how far from the translated position a linked portal
// is searched for in a destination dimension.
func portalSearchRadius(dim world.Dimension) int {
	if dim == world.Nether {
		return 16
	}
	return 128
}

func (t *PortalTravelComputer) portalDestination(tx *world.Tx, target world.Dimension) *world.World {
	if t.DestinationWorld != nil {
		return t.DestinationWorld(tx, target)
	}
	return tx.World().PortalDestination(target)
}

func applyPortalRotation(e Traveller, rotation cube.Rotation) {
	if setter, ok := e.(interface{ setRotation(cube.Rotation) }); ok {
		setter.setRotation(rotation)
	} else if mover, ok := e.(interface {
		Move(mgl64.Vec3, float64, float64)
	}); ok {
		current := e.Rotation()
		mover.Move(mgl64.Vec3{}, rotation.Yaw()-current.Yaw(), rotation.Pitch()-current.Pitch())
	}
}

func (t *PortalTravelComputer) finishTravel(e Traveller, pos mgl64.Vec3, source, destination world.Dimension, rotation cube.Rotation, applyRotation bool) bool {
	if t.TeleportAllowed != nil && !t.TeleportAllowed(e, pos) {
		return false
	}
	endTransition := source == world.End || destination == world.End
	var velocity mgl64.Vec3
	if mover, ok := e.(interface{ Velocity() mgl64.Vec3 }); ok {
		velocity = mover.Velocity()
	}
	// ServerPlayer's End exit uses a respawn transition with an absolute zero
	// delta. Other End transitions retain the incoming DELTA relatives.
	if source == world.End && t.Player {
		velocity = mgl64.Vec3{}
	}
	if !applyRotation {
		rotation = e.Rotation()
		if source == world.End && t.Player {
			rotation = cube.Rotation{}
			applyRotation = true
		}
	}
	if destination == world.End && source != world.End {
		rotation = cube.Rotation{90, e.Rotation().Pitch()}
		applyRotation = true
	}
	if t.Teleport != nil {
		t.Teleport(e, pos, rotation, velocity)
	} else {
		if applyRotation {
			applyPortalRotation(e, rotation)
		}
		e.Teleport(pos)
		if endTransition {
			if setter, ok := e.(interface{ SetVelocity(mgl64.Vec3) }); ok {
				setter.SetVelocity(velocity)
			}
		}
	}
	handlePortalTravel(e, source, destination)
	return true
}

func handlePortalTravel(e Traveller, source, destination world.Dimension) {
	if ent, ok := e.(*Ent); ok {
		if h, ok := ent.Behaviour().(portalTravelHandler); ok {
			h.HandlePortalTravel(source, destination)
		}
		return
	}
	if h, ok := e.(portalTravelHandler); ok {
		h.HandlePortalTravel(source, destination)
	}
}

func translatePortalPosition(pos cube.Pos, source, target world.Dimension) cube.Pos {
	switch source {
	case world.Overworld:
		pos[0], pos[2] = pos[0]>>3, pos[2]>>3
	case world.Nether:
		pos[0], pos[2] = pos[0]*8, pos[2]*8
	}
	r := target.Range()
	pos[1] = min(max(pos[1], r.Min()), r.Max())
	return pos
}

func (t *PortalTravelComputer) notifyDenied(e Traveller, tx *world.Tx, dim world.Dimension) {
	msg := tx.World().PortalDisabledMessage(dim)
	t.mu.Lock()
	t.inside = true
	if msg == "" {
		t.mu.Unlock()
		return
	}
	if t.deniedActive && t.deniedDim == dim {
		t.mu.Unlock()
		return
	}
	t.deniedActive, t.deniedDim = true, dim
	t.mu.Unlock()
	if m, ok := e.(interface{ Message(...any) }); ok {
		m.Message(msg)
	}
}

func (t *PortalTravelComputer) clearDenied() {
	t.mu.Lock()
	t.deniedActive = false
	t.mu.Unlock()
}
