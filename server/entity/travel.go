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
	// Instantaneous returns true if the entity should skip the portal wait timer when moving between dimensions.
	Instantaneous func(source, target world.Dimension) bool
	// Teleport teleports the entity to the final portal position. If nil, Traveller.Teleport is used.
	Teleport func(e Traveller, pos mgl64.Vec3)
	// SpawnPoint returns the position the entity arrives at when returning from the End; if nil the world spawn is used.
	SpawnPoint func(tx *world.Tx) mgl64.Vec3
	// Player specifies if the entity is a player. Players arrive one block lower on the End platform than other entities.
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
	return &PortalTravelComputer{Instantaneous: func(world.Dimension, world.Dimension) bool { return true }, Cooldown: time.Second * 15}
}

const portalSearchRadius = 128

type portalTravelComputerProvider interface {
	PortalTravelComputer() *PortalTravelComputer
}

// Traveller represents a world.Entity that can travel between dimensions.
type Traveller interface {
	world.Entity
	Teleport(pos mgl64.Vec3)
}

type portalTravelHandler interface {
	HandlePortalTravel(source, destination world.Dimension)
}

// EnterPortal handles an entity touching a portal block.
func (t *PortalTravelComputer) EnterPortal(e Traveller, tx *world.Tx, target world.Dimension) {
	destination := tx.World().PortalDestination(target)
	if destination == nil || destination == tx.World() {
		t.notifyDenied(e, tx, target)
		return
	}
	t.clearDenied()
	if t.enterPortalDestination(tx.World().Dimension(), target, destination) {
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
	destination := source.PortalDestination(target)
	if destination == nil || destination == source {
		return nil
	}
	if t.enterPortalDestination(source.Dimension(), target, destination) {
		return destination
	}
	return nil
}

func (t *PortalTravelComputer) enterPortalDestination(source, target world.Dimension, destination *world.World) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.inside = true
	if t.timedOut || time.Now().Before(t.cooldownUntil) {
		return false
	}
	travelNow := t.instantaneous(source, target) || (t.awaitingTravel && time.Since(t.start) >= time.Second*4)
	if !travelNow && !t.awaitingTravel {
		t.start, t.awaitingTravel = time.Now(), true
	}
	return travelNow && destination != nil
}

func (t *PortalTravelComputer) instantaneous(source, target world.Dimension) bool {
	return t.Instantaneous != nil && t.Instantaneous(source, target)
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
	origin := e.Position()
	pos := translatePortalPosition(cube.PosFromVec3(origin), sourceDim, destinationDim)

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
	origin := e.Position()
	pos := translatePortalPosition(cube.PosFromVec3(origin), sourceDim, destinationDim)

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

func (t *PortalTravelComputer) transfer(handle *world.EntityHandle, source, destination *world.World, origin mgl64.Vec3, pos cube.Pos, sourceDim, destinationDim world.Dimension) {
	travelled, err := safeWorldCall(destination, func(tx *world.Tx) (bool, error) {
		spawn, ok := t.destinationSpawn(tx, sourceDim, pos)
		if !ok {
			return false, nil
		}
		e, ok := tx.AddEntity(handle).(Traveller)
		if !ok {
			return false, nil
		}
		t.finishTravel(e, spawn, sourceDim, destinationDim)
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
				e, ok := tx.AddEntity(handle).(Traveller)
				if !ok {
					return false, nil
				}
				e.Teleport(origin)
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
	if !t.CreatePortal {
		n, ok := portal.FindNetherPortal(tx, pos, portalSearchRadius)
		if !ok {
			return mgl64.Vec3{}, false
		}
		return n.Spawn().Vec3Middle(), true
	}
	if n, ok := portal.FindOrCreateNetherPortal(tx, pos, portalSearchRadius); ok {
		return n.Spawn().Vec3Middle(), true
	}
	return mgl64.Vec3{}, false
}

func (t *PortalTravelComputer) finishTravel(e Traveller, pos mgl64.Vec3, source, destination world.Dimension) {
	handlePortalTravel(e, source, destination)
	if t.Teleport != nil {
		t.Teleport(e, pos)
		return
	}
	e.Teleport(pos)
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
