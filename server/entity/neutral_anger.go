package entity

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

// neutralAnger is the anger of Java's NeutralMob: how long the mob stays
// angry and at whom, or at every player under universal_anger.
type neutralAnger struct {
	angerTicks  int
	angerTarget uuid.UUID
}

// angry ports NeutralMob.isAngry.
func (n *neutralAnger) angry() bool { return n.angerTicks > 0 }

// startAngerTimer ports startPersistentAngerTimer with the
// PERSISTENT_ANGER_TIME of 20 to 39 seconds every neutral mob uses.
func (n *neutralAnger) startAngerTimer(m *Mob) { n.angerTicks = 400 + int(m.Random().NextInt(381)) }

// stopBeingAngry ports NeutralMob.stopBeingAngry.
func (n *neutralAnger) stopBeingAngry(m *Mob) {
	m.AI.SetLastHurtBy(nil)
	n.angerTarget = uuid.Nil
	m.SetTarget(nil)
	n.angerTicks = 0
}

// forgetCurrentTargetAndRefreshUniversalAnger ports the NeutralMob method of
// that name.
func (n *neutralAnger) forgetCurrentTargetAndRefreshUniversalAnger(m *Mob) {
	n.stopBeingAngry(m)
	n.startAngerTimer(m)
}

// angryAt ports NeutralMob.isAngryAt.
func (n *neutralAnger) angryAt(m *Mob, e world.Entity) bool {
	if !m.CanAttack(e) {
		return false
	}
	if validPlayerTarget(m.tx, e) && m.tx.World().GameRuleBool("universal_anger") && n.angry() && n.angerTarget == uuid.Nil {
		return true
	}
	return n.angerTarget != uuid.Nil && n.angerTarget == e.H().UUID()
}

// updatePersistentAnger ports NeutralMob.updatePersistentAnger: the target
// of the mob becomes the one it is angry at, which it may stay while it has
// the target.
func (n *neutralAnger) updatePersistentAnger(m *Mob, stayAngryIfTargetPresent bool) {
	if n.angerTicks > 0 {
		n.angerTicks--
	}
	angerTarget := n.angerTarget
	var previous world.Entity
	if m.AI.target != nil {
		previous, _ = m.AI.target.Entity(m.tx)
	}
	if previous != nil && !entityAlive(previous) && angerTarget != uuid.Nil && angerTarget == previous.H().UUID() && !isPlayerEntity(previous) {
		n.stopBeingAngry(m)
		return
	}
	target := m.Target()
	if target != nil {
		newTarget := angerTarget != target.H().UUID()
		if newTarget {
			n.angerTarget = target.H().UUID()
		}
		if newTarget || stayAngryIfTargetPresent {
			n.startAngerTimer(m)
		}
	}
	if angerTarget != uuid.Nil && !n.angry() && (target == nil || !validPlayerTarget(m.tx, target) || !stayAngryIfTargetPresent) {
		n.stopBeingAngry(m)
	}
	if angerTarget == uuid.Nil {
		return
	}
	if p, ok := m.tx.EntityByUUID(angerTarget); ok && isPlayerEntity(p) && (creativeOrSpectator(p) || m.tx.World().Difficulty() == world.DifficultyPeaceful) {
		n.stopBeingAngry(m)
	}
}

// forgiveAnger ports NeutralMob.playerDied: the mob stops being angry at a
// player that died.
func (n *neutralAnger) forgiveAnger(ai *MobAI, id string) {
	if n.angerTarget == uuid.Nil || n.angerTarget.String() != id {
		return
	}
	n.angerTarget, n.angerTicks = uuid.Nil, 0
	ai.lastHurtBy = nil
	if ai.target != nil && ai.target.UUID().String() == id {
		ai.target = nil
	}
}
