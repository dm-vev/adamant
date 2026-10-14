package entity

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func (s *speciesState) DismountAll(e *Ent, tx *world.Tx) {
	(&speciesMob{Ent: e, kind: s.kind}).DismountAll(e, tx)
}

func (m *speciesMob) Mount(tx *world.Tx, passenger world.Entity) bool {
	if m.kind != spiderSpecies || passenger == nil || m.s().passenger != nil {
		return false
	}
	rider, ok := passenger.(Rider)
	if !ok || rider.Riding() != nil {
		return false
	}
	m.s().passenger = passenger.H()
	seatRider(rider, m)
	m.viewLink(tx, passenger.H(), true)
	m.updateJockey(tx)
	return true
}

func (m *speciesMob) Dismount(tx *world.Tx, passenger world.Entity) {
	if passenger == nil || m.s().passenger != passenger.H() {
		return
	}
	if rider, ok := passenger.(Rider); ok {
		rider.SetRiding(nil)
	}
	h := m.s().passenger
	m.s().passenger = nil
	m.viewLink(tx, h, false)
}

func (m *speciesMob) DismountAll(_ *Ent, tx *world.Tx) {
	h := m.s().passenger
	if h == nil {
		return
	}
	if passenger, ok := h.Entity(tx); ok {
		m.Dismount(tx, passenger)
		return
	}
	m.s().passenger = nil
}

// RiderSeat returns the seat of Bedrock's spider, whose jockey groups seat
// bogged lower and wither skeletons centred.
func (m *speciesMob) RiderSeat(rider world.Entity) world.RiderSeat {
	pos := mgl64.Vec3{0, 0.54, -0.1}
	switch rider.H().Type().EncodeEntity() {
	case "minecraft:bogged":
		pos[1] = 0.325
	case "minecraft:wither_skeleton":
		pos[2] = 0
	}
	return seat{pos: pos}.riderSeat(rider)
}
func (m *speciesMob) Links() []Link {
	if h := m.s().passenger; h != nil {
		return []Link{{Ridden: m.H(), Rider: h, Type: LinkRider}}
	}
	return nil
}

func (m *speciesMob) updateJockey(tx *world.Tx) {
	h := m.s().passenger
	if h == nil {
		return
	}
	passenger, ok := h.Entity(tx)
	if !ok {
		m.s().passenger = nil
		return
	}
	rider, ok := passenger.(Rider)
	if !ok || rider.Riding() != m.H() {
		m.s().passenger = nil
		return
	}
	if teleporter, ok := passenger.(interface{ Teleport(mgl64.Vec3) }); ok {
		teleporter.Teleport(m.Position().Add(mgl64.Vec3{0, .85, 0}))
	}
}

func (m *speciesMob) viewLink(tx *world.Tx, rider *world.EntityHandle, mounted bool) {
	viewers := tx.Viewers(m.Position())
	for _, viewer := range viewers {
		if linkViewer, ok := viewer.(interface{ ViewEntityLinkType(*world.EntityHandle, *world.EntityHandle, LinkType, bool) }); ok {
			linkViewer.ViewEntityLinkType(m.H(), rider, LinkRider, mounted)
		} else if linkViewer, ok := viewer.(interface{ ViewEntityLink(*world.EntityHandle, *world.EntityHandle, bool) }); ok {
			linkViewer.ViewEntityLink(m.H(), rider, mounted)
		}
	}
	tx.ReleaseViewers(viewers)
}
