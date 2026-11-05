package world

import (
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/go-gl/mathgl/mgl64"
)

// AddParticle spawns a Particle at a given position in the World. Viewers that
// are viewing the chunk will be shown the particle.
func (tx *Tx) AddParticle(pos mgl64.Vec3, p Particle) {
	tx.World().addParticle(tx, pos, p)
}

// PlayEntityAnimation plays an animation on an entity in the World. The animation is played for all viewers
// of the entity.
func (tx *Tx) PlayEntityAnimation(e Entity, a EntityAnimation) {
	viewers := tx.World().viewersOf(tx, e.Position())
	// We deliberately iterate using the pooled slice returned by viewersOf and hand it back afterwards. Animation
	// fan-out happens frequently (movement, swings, etc.), so minimising transient allocations has a noticeable
	// impact on GC pauses during crowded gameplay sessions.
	var b Broadcast
	for _, viewer := range viewers {
		if sv, ok := viewer.(SharedViewer); ok {
			sv.ViewEntityAnimationShared(e, a, &b)
		} else {
			viewer.ViewEntityAnimation(e, a)
		}
	}
	tx.World().releaseViewers(viewers)
}

// PlaySound plays a sound at a specific position in the World. Viewers of that
// position will be able to hear the sound if they are close enough.
func (tx *Tx) PlaySound(pos mgl64.Vec3, s Sound) {
	tx.World().playSound(tx, pos, s)
}

// BroadcastSleepingIndicator broadcasts a sleeping indicator to all sleepers in the world.
func (tx *Tx) BroadcastSleepingIndicator() {
	if tx.region != nil {
		// Every sleeper of the world counts.
		tx.deferToOwner(func(tx *Tx) { tx.BroadcastSleepingIndicator() })
		return
	}
	sleepers := make([]Sleeper, 0)
	sleeping := 0

	for s := range tx.Sleepers() {
		sleepers = append(sleepers, s)
		if _, ok := s.Sleeping(); ok {
			sleeping++
		}
	}

	total := len(sleepers)
	if total == 0 {
		return
	}
	for _, s := range sleepers {
		s.SendSleepingIndicator(sleeping, total)
	}
}

// BroadcastSleepingReminder broadcasts a sleeping reminder message to all sleepers in the world, excluding the sleeper
// passed.
func (tx *Tx) BroadcastSleepingReminder(sleeper Sleeper) {
	if tx.region != nil {
		tx.deferToOwner(func(tx *Tx) { tx.BroadcastSleepingReminder(sleeper) })
		return
	}
	pending := make([]Sleeper, 0)

	for s := range tx.Sleepers() {
		if _, ok := s.Sleeping(); !ok {
			pending = append(pending, s)
		}
	}

	remaining := len(pending)
	if remaining == 0 {
		return
	}
	for _, s := range pending {
		s.Messaget(chat.MessageSleeping, sleeper.Name(), remaining)
	}
}
