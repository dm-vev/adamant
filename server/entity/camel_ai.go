package entity

import (
	"github.com/df-mc/dragonfly/server/world"
)

// poseTime ports Camel.getPoseTime: the game ticks since the camel last sat
// down or stood up.
func (b *MountBehaviour) poseTime(tx *world.Tx) int64 {
	last := b.lastPoseTick
	if last < 0 {
		last = -last
	}
	now := tx.CurrentTick()
	if last > now {
		// A pose change after the current game tick was saved against the
		// time of day: it counts as long finished.
		b.lastPoseTick = 0
		return now
	}
	return now - last
}

// refuseToMove ports Camel.refuseToMove: a camel does not move while it sits
// or sits down or stands up.
func (b *MountBehaviour) refuseToMove(tx *world.Tx) bool {
	return b.camel() && (b.sitting || b.inPoseTransition(tx))
}

// inPoseTransition ports Camel.isInPoseTransition: sitting down takes 40
// ticks and standing up 52.
func (b *MountBehaviour) inPoseTransition(tx *world.Tx) bool {
	transition := int64(52)
	if b.sitting {
		transition = 40
	}
	return b.poseTime(tx) < transition
}

// sitDown ports Camel.sitDown.
func (b *MountBehaviour) sitDown(m *Mount) {
	if b.sitting {
		return
	}
	b.sitting, b.lastPoseTick = true, -m.tx.CurrentTick()
	m.tx.EmitGameEvent(world.GameEventEntityAction, m.Position(), world.GameEventContext{Source: m})
	m.updateState()
}

// standUp ports Camel.standUp: the camel takes 52 ticks to stand up.
func (b *MountBehaviour) standUp(m *Mount) {
	if !b.sitting {
		return
	}
	b.sitting, b.lastPoseTick = false, m.tx.CurrentTick()
	m.tx.EmitGameEvent(world.GameEventEntityAction, m.Position(), world.GameEventContext{Source: m})
	m.updateState()
}

// standUpInstantly ports Camel.standUpInstantly.
func (b *MountBehaviour) standUpInstantly(m *Mount) {
	wasSitting := b.sitting
	b.sitting, b.lastPoseTick = false, max(0, m.tx.CurrentTick()-52-1)
	m.tx.EmitGameEvent(world.GameEventEntityAction, m.Position(), world.GameEventContext{Source: m})
	if wasSitting {
		m.updateState()
	}
}
