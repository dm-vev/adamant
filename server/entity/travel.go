package entity

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/portal"
	"github.com/go-gl/mathgl/mgl64"
)

// TravelComputer handles the interdimensional travelling of an entity.
type TravelComputer struct {
	// Instantaneous is a function that returns true if the entity given can travel instantly.
	Instantaneous func() bool

	mu             sync.RWMutex
	start          time.Time
	awaitingTravel bool
	travelling     bool
	timedOut       bool
	deniedDim      world.Dimension
	deniedActive   bool
}

// Traveller represents a world.Entity that can travel between dimensions.
type Traveller interface {
	world.Entity
	// Teleport teleports the entity to the position given.
	Teleport(pos mgl64.Vec3)
}

// portalBlock represents a block that can be used as a portal to travel between dimensions.
type portalBlock interface {
	world.Block
	// Portal returns the dimension that the portal leads to.
	Portal() world.Dimension
}

// TickTravelling checks if the player is colliding with a nether portal block. If so, it teleports the player
// to the other dimension after four seconds or instantly if instantaneous is true.
func (t *TravelComputer) TickTravelling(travel Traveller, tx *world.Tx) {
	box := travel.H().Type().BBox(travel).Translate(travel.Position()).Grow(0.25)

	min, max := box.Min(), box.Max()
	minX, minY, minZ := int(math.Floor(min[0])), int(math.Floor(min[1])), int(math.Floor(min[2]))
	maxX, maxY, maxZ := int(math.Ceil(max[0])), int(math.Ceil(max[1])), int(math.Ceil(max[2]))
	found, target := false, world.Dimension(nil)
search:
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			for z := minZ; z <= maxZ; z++ {
				pos := cube.Pos{x, y, z}
				p, ok := tx.Block(pos).(portalBlock)
				if !ok {
					continue
				}
				for _, blockBox := range p.Model().BBox(pos, tx) {
					if blockBox.Translate(pos.Vec3()).IntersectsWith(box) {
						found, target = true, p.Portal()
						break search
					}
				}
			}
		}
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if !found {
		if t.travelling {
			return
		}
		t.timedOut, t.awaitingTravel = false, false
		t.deniedActive = false
		return
	}

	switch target {
	case world.Nether:
		if t.timedOut {
			return
		}
		destination := tx.World().PortalDestination(world.Nether)
		if destination == nil || destination == tx.World() {
			t.notifyDenied(travel, tx, world.Nether)
			t.awaitingTravel = false
			return
		}
		t.deniedActive = false
		instantaneous := t.Instantaneous != nil && t.Instantaneous()
		if instantaneous || (t.awaitingTravel && time.Since(t.start) >= time.Second*4) {
			t.mu.Unlock()
			t.travel(travel, tx, destination)
			t.mu.Lock()
		} else if !t.awaitingTravel {
			t.start, t.awaitingTravel = time.Now(), true
		}
	}
}

// travel defers removal until the current owner callback completes, then moves e to destination.
func (t *TravelComputer) travel(e Traveller, tx *world.Tx, destination *world.World) {
	source := tx.World()
	if destination == nil || destination == source {
		return
	}
	origin := e.Position()
	pos := cube.PosFromVec3(origin)
	if source.Dimension() == world.Overworld {
		pos = cube.Pos{floorDiv(pos.X(), 8), pos.Y(), floorDiv(pos.Z(), 8)}
	} else if source.Dimension() == world.Nether {
		pos = cube.Pos{pos.X() * 8, pos.Y(), pos.Z() * 8}
	}

	t.mu.Lock()
	t.travelling, t.timedOut, t.awaitingTravel = true, true, false
	t.deniedActive = false
	t.mu.Unlock()

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
		go t.transfer(handle, source, destination, origin, pos)
	})
}

// transfer adds a removed entity to destination, restoring it to source if destination is unavailable.
func (t *TravelComputer) transfer(handle *world.EntityHandle, source, destination *world.World, origin mgl64.Vec3, pos cube.Pos) {
	travelled, err := world.Call(context.Background(), destination, func(tx *world.Tx) (bool, error) {
		spawn := pos.Vec3Middle()
		if netherPortal, ok := portal.FindOrCreateNetherPortal(tx, pos, 128); ok {
			spawn = netherPortal.Spawn().Vec3Middle()
		}
		if e, ok := tx.AddEntity(handle).(Traveller); ok {
			e.Teleport(spawn)
		}
		return true, nil
	})
	if err != nil {
		travelled = false
	}
	if !travelled {
		_, err = world.Call(context.Background(), source, func(tx *world.Tx) (struct{}, error) {
			if e, ok := tx.AddEntity(handle).(Traveller); ok {
				e.Teleport(origin)
			}
			return struct{}{}, nil
		})
		if err != nil {
			_ = handle.Close()
		}
	}

	t.mu.Lock()
	t.travelling = false
	if !travelled {
		t.timedOut = false
	}
	t.mu.Unlock()
}

func (t *TravelComputer) resetFailedTravel() {
	t.mu.Lock()
	t.travelling, t.timedOut = false, false
	t.mu.Unlock()
}

// floorDiv returns floor(n/d) for positive divisors, matching Minecraft coordinate scaling.
func floorDiv(n, d int) int {
	if d <= 0 {
		panic("floorDiv requires a positive divisor")
	}
	q := n / d
	r := n % d
	if r != 0 && n < 0 {
		q--
	}
	return q
}

func (t *TravelComputer) notifyDenied(travel Traveller, tx *world.Tx, dim world.Dimension) {
	if msg := tx.World().PortalDisabledMessage(dim); msg != "" {
		if m, ok := travel.(interface{ Message(...any) }); ok {
			if !t.deniedActive || t.deniedDim != dim {
				t.deniedActive, t.deniedDim = true, dim
				m.Message(msg)
			}
		}
	}
}
