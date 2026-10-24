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

func (b *MountBehaviour) brain() *Brain { return b.br }

// customServerAiStep ports Camel.customServerAiStep and CamelAi.updateActivity.
func (b *MountBehaviour) customServerAiStep(m *Mob) {
	if b.br == nil {
		return
	}
	b.br.tick(m)
	b.br.setActiveToFirstValid(activityIdle)
}

// newCamelBrain ports CamelAi and the brain provider of Camel.
func newCamelBrain(b *MountBehaviour) *Brain {
	br := newBrain(nearestLivingEntitySensor{}, hurtBySensor{}, temptingSensor{items: animalTemptation}, adultSensor{})
	moves := func(m *Mob) bool { return !b.refuseToMove(m.tx) }
	br.addActivity(activityCore, 0,
		newSwim(.8),
		newCamelPanic(b),
		newLookAtTargetSink(45, 90),
		newMoveToTargetSink(150, 250, nil),
		newCountDownCooldownTicks(memTemptationCooldownTicks),
		newCountDownCooldownTicks(memGazeCooldownTicks),
	)
	br.addActivityWith(activityIdle, []prioritizedBehavior{
		{0, newSetEntityLookTargetSometimes(isPlayerType, 6, uniformInt{30, 60})},
		{1, newAnimalMakeLove(mountTypeFor(b.kind), 1, 2)},
		{2, newRunOne(nil,
			weightedBehavior{newFollowTemptation(constSpeed(2.5), func(m *Mob) float64 {
				if b.ageTicks < 0 {
					return 2.5
				}
				return 3.5
			}, false), 1},
			weightedBehavior{newTriggerIf(moves, newBabyFollowAdult(uniformInt{5, 16}, constSpeed(2.5), memNearestVisibleAdult, false)), 1},
		)},
		{3, newRandomLookAroundBehavior(uniformInt{150, 250}, 30, 0, 0)},
		{4, newRunOne([]memoryCondition{{memWalkTarget, memAbsent}},
			weightedBehavior{newTriggerIf(moves, newRandomStroll(2, true)), 1},
			weightedBehavior{newTriggerIf(moves, newSetWalkTargetFromLookTarget(nil, constSpeed(2), 3)), 1},
			weightedBehavior{newCamelRandomSitting(b, 20), 1},
			weightedBehavior{newDoNothing(30, 60), 1},
		)},
	}, nil, nil)
	return br
}

// newCamelPanic ports CamelAi.CamelPanic: an AnimalPanic at four times the
// speed that stands the camel up at once, unless a mob rides it.
func newCamelPanic(b *MountBehaviour) *behavior {
	p := newAnimalPanic(4, nil, nil, func(m *Mob) { b.standUpInstantly(&Mount{Ent: m.Ent}) })
	check := p.checkExtra
	p.checkExtra = func(m *Mob) bool { return check(m) && !b.mobControlled() }
	return p
}

// newCamelRandomSitting ports CamelAi.RandomSitting: a camel on the ground
// that kept its pose for a while sits down or stands up, unless it is in
// water, on a lead or ridden.
func newCamelRandomSitting(b *MountBehaviour, minimalPoseSeconds int) *behavior {
	return &behavior{
		checkExtra: func(m *Mob) bool {
			return !m.InWater() && b.poseTime(m.tx) >= int64(minimalPoseSeconds*20) && m.Ent.HolderLeashID() == 0 && m.OnGround() &&
				!b.controlled() && !b.mobControlled()
		},
		start: func(m *Mob, _ int64) {
			mount := &Mount{Ent: m.Ent}
			if b.sitting {
				b.standUp(mount)
			} else if !m.Brain().has(memIsPanicking) {
				b.sitDown(mount)
			}
		},
	}
}

// camelLookControl ports Camel.CamelLookControl: a camel someone steers
// does not look about.
type camelLookControl struct{ mount *MountBehaviour }

func (c camelLookControl) tick(m *Mob) {
	if c.mount.playerController(m.tx) == nil && !c.mount.mobControlled() {
		m.AI.look.tick(m)
	}
}
