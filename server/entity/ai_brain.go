package entity

import (
	"math"
	"math/bits"
	"slices"
	"sort"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports Java's Brain: the memories, sensors and activities of the
// behaviours that drive mobs such as goats, frogs, piglins and villagers.

// activity ports Java's Activity: a set of behaviours a brain runs together.
type activity uint8

const (
	activityCore activity = iota
	activityIdle
	activityWork
	activityPlay
	activityRest
	activityMeet
	activityPanic
	activityRaid
	activityPreRaid
	activityHide
	activityFight
	activityCelebrate
	activityAdmireItem
	activityAvoid
	activityRide
	activityPlayDead
	activityLongJump
	activityRam
	activityTongue
	activitySwim
	activityLaySpawn
	activitySniff
	activityInvestigate
	activityRoar
	activityEmerge
	activityDig
	activityCount
)

// memoryType ports Java's MemoryModuleType: a thing a brain remembers.
type memoryType uint8

const (
	memHome memoryType = iota
	memJobSite
	memPotentialJobSite
	memMeetingPoint
	memNearestLivingEntities
	memNearestVisibleLivingEntities
	memNearestVisiblePlayer
	memNearestVisibleAttackablePlayer
	memNearestPlayers
	memWalkTarget
	memLookTarget
	memAttackTarget
	memAttackCoolingDown
	memInteractionTarget
	memBreedTarget
	memRideTarget
	memPath
	memNearestBed
	memHurtBy
	memHurtByEntity
	memAvoidTarget
	memNearestHostile
	memNearestAttackable
	memHidingPlace
	memHeardBellTime
	memCantReachWalkTargetSince
	memGolemDetectedRecently
	memDangerDetectedRecently
	memLastSlept
	memLastWoken
	memLastWorkedAtPoi
	memNearestVisibleAdult
	memNearestVisibleWantedItem
	memNearestVisibleNemesis
	memPlayDeadTicks
	memTemptingPlayer
	memTemptationCooldownTicks
	memGazeCooldownTicks
	memLongJumpCooldownTicks
	memLongJumpMidJump
	memHasHuntingCooldown
	memRamCooldownTicks
	memRamTarget
	memIsInWater
	memIsPregnant
	memIsPanicking
	memTransportItemsCooldownTicks
	memChargeCooldownTicks
	memAttackTargetCooldown
	memAngryAt
	memUniversalAnger
	memAdmiringItem
	memTimeTryingToReachAdmireItem
	memDisableWalkToAdmireItem
	memAdmiringDisabled
	memHuntedRecently
	memCelebrateLocation
	memDancing
	memNearestVisibleHuntableHoglin
	memNearestVisibleBabyHoglin
	memNearestTargetablePlayerNotWearingGold
	memNearbyAdultPiglins
	memNearestVisibleAdultPiglins
	memNearestVisibleAdultHoglins
	memNearestVisibleAdultPiglin
	memNearestVisibleZombified
	memVisibleAdultPiglinCount
	memVisibleAdultHoglinCount
	memNearestPlayerHoldingWantedItem
	memAteRecently
	memNearestRepellent
	memPacified
	memRoarTarget
	memDisturbanceLocation
	memRecentProjectile
	memIsSniffing
	memIsEmerging
	memRoarSoundDelay
	memDigCooldown
	memRoarSoundCooldown
	memSniffCooldown
	memTouchCooldown
	memVibrationCooldown
	memSonicBoomCooldown
	memSonicBoomSoundCooldown
	memSonicBoomSoundDelay
	memLikedPlayer
	memLikedNoteblockPosition
	memLikedNoteblockCooldownTicks
	memItemPickupCooldownTicks
	memSnifferExploredPositions
	memSnifferSniffingTarget
	memSnifferDigging
	memSnifferHappy
	memBreezeJumpCooldown
	memBreezeShoot
	memBreezeShootCharging
	memBreezeShootRecovering
	memBreezeShootCooldown
	memBreezeJumpInhaling
	memBreezeJumpTarget
	memBreezeLeavingWater
	memSecondaryJobSite
	memNearestVisibleWantedItemHolder
	memVisibleVillagerBabies
	memNearestVisibleAttackableHoglin
	memLastHurtBy
	memUnreachableTongueTargets
	memDoorsToClose
	memSpearStatus
	memSpearEngageTime
	memSpearChargePosition
	memSpearFleeingTime
	memSpearFleeingPosition
	memVisitedBlockPositions
	memUnreachableTransportBlockPositions
	memNearestVisibleAttackablePlayers
	memMemoryTypeCount
)

const (
	_ = uint8(64 - activityCount)
	_ = uint8(128 - memMemoryTypeCount)
	_ = uint8(255 - memMemoryTypeCount)
)

// memoryStatus ports Java's MemoryStatus: the state a memory must be in for
// a behaviour or activity to run.
type memoryStatus uint8

const (
	memPresent memoryStatus = iota
	memAbsent
	memRegistered
)

// memoryCondition pairs a memory with the status it must be in.
type memoryCondition struct {
	mem    memoryType
	status memoryStatus
}

// neverExpire is the time to live of a memory that does not expire.
const neverExpire = math.MaxInt64

// memorySlot ports Java's MemorySlot: a remembered value, if any, and the
// ticks it is remembered for.
type memorySlot struct {
	value any
	ttl   int64
}

func (s *memorySlot) set(v any, ttl int64) { s.value, s.ttl = v, ttl }
func (s *memorySlot) clear()               { s.value, s.ttl = nil, neverExpire }

// behaviorStatus ports Behavior.Status.
type behaviorStatus uint8

const (
	behaviorStopped behaviorStatus = iota
	behaviorRunning
)

// behaviorControl ports Java's BehaviorControl: something a brain starts,
// ticks and stops.
type behaviorControl interface {
	status() behaviorStatus
	tryStart(m *Mob, time int64) bool
	tickOrStop(m *Mob, time int64)
	doStop(m *Mob, time int64)
	requiredMemories() []memoryType
}

// prioritizedBehavior pairs a behaviour with the priority it runs at.
type prioritizedBehavior struct {
	priority int
	b        behaviorControl
}

// orderedBehavior pairs a top-level behaviour with its position in the
// priority-sorted schedule so running behaviours stay in exact priority order.
type orderedBehavior struct {
	bh    behaviorControl
	order uint16
}

// activityBehaviors are the behaviours of an activity at a priority.
type activityBehaviors struct {
	activity  activity
	behaviors []orderedBehavior
}

// priorityBehaviors are the behaviours of every activity at a priority.
type priorityBehaviors struct {
	priority   int
	actMask    uint64
	activities []activityBehaviors
}

// activeBehaviorGroup points to a single active (priority, activity) slice so
// Brain.tick skips inactive activities in O(1).
type activeBehaviorGroup struct {
	pbIdx     uint16
	abIdx     uint16
	behaviors []orderedBehavior
}

// sensor ports Java's Sensor: something a brain runs every scanRate ticks
// to remember what is around.
type sensor interface {
	// scanRate is the ticks between scans, 20 if zero.
	scanRate() int
	requires() []memoryType
	doTick(m *Mob, b *brain)
}

type sensorSlot struct {
	s          sensor
	timeToTick int64
}

// Brain ports Java's Brain.
type brain struct {
	// memIdx maps each memoryType to its 1-based slot index in memories (0 = unregistered).
	memIdx [memMemoryTypeCount]uint8
	// memories holds registered memory slots densely by value.
	memories []memorySlot
	// expiringMask tracks registered memories currently holding an expiring value.
	expiringMask [2]uint64
	sensors      []sensorSlot
	// byPriority are the behaviours in ascending order of priority.
	byPriority      []priorityBehaviors
	activeCache     []activeBehaviorGroup
	activeCacheMask uint64
	orderDirty      bool
	runningBuf      []orderedBehavior
	tickBuf         []orderedBehavior

	registeredActs uint64
	requirements   [activityCount][]memoryCondition
	eraseOnStop    map[activity][]memoryType
	coreMask       uint64
	activeMask     uint64
	defaultAct     activity
	// lastScheduleUpdate is the tick the schedule last changed the
	// activity.
	lastScheduleUpdate int64
	started            bool
}

// newBrain returns a brain with the sensors passed, its core activity core
// and its default activity idle.
func newBrain(sensors ...sensor) *brain {
	b := &brain{
		memories:           make([]memorySlot, 0, 16),
		coreMask:           uint64(1) << activityCore,
		defaultAct:         activityIdle,
		lastScheduleUpdate: -9999,
	}
	if len(sensors) > 0 {
		b.sensors = make([]sensorSlot, len(sensors))
		for i, s := range sensors {
			b.sensors[i] = sensorSlot{s: s}
			for _, mem := range s.requires() {
				b.register(mem)
			}
		}
	}
	b.useDefaultActivity()
	return b
}

// start delays the first scan of every sensor by a random part of its
// scan rate, as Sensor.randomlyDelayStart.
func (b *brain) start(m *Mob) {
	if b.started {
		return
	}
	b.started = true
	for i := range b.sensors {
		b.sensors[i].timeToTick = int64(m.Random().NextInt(int32(sensorScanRate(b.sensors[i].s))))
	}
}

func sensorScanRate(s sensor) int {
	if r := s.scanRate(); r > 0 {
		return r
	}
	return 20
}

func (b *brain) setExpiringBit(mem memoryType) {
	b.expiringMask[mem>>6] |= uint64(1) << (mem & 63)
}

func (b *brain) clearExpiringBit(mem memoryType) {
	b.expiringMask[mem>>6] &^= uint64(1) << (mem & 63)
}

func (b *brain) register(mem memoryType) {
	if b.memIdx[mem] == 0 {
		b.memories = append(b.memories, memorySlot{ttl: neverExpire})
		b.memIdx[mem] = uint8(len(b.memories))
	}
}

// copyMemoryFrom copies a registered memory slot (value and remaining TTL) from old.
func (b *brain) copyMemoryFrom(old *brain, mem memoryType) {
	idx := old.memIdx[mem]
	if idx == 0 {
		return
	}
	src := old.memories[idx-1]
	b.register(mem)
	b.memories[b.memIdx[mem]-1] = src
	if src.value != nil && src.ttl != neverExpire {
		b.setExpiringBit(mem)
	} else {
		b.clearExpiringBit(mem)
	}
}

// has reports if the brain remembers a value of the memory.
func (b *brain) has(mem memoryType) bool { return b.check(mem, memPresent) }

// check ports Brain.checkMemory.
func (b *brain) check(mem memoryType, status memoryStatus) bool {
	idx := b.memIdx[mem]
	if idx == 0 {
		return false
	}
	switch status {
	case memPresent:
		return b.memories[idx-1].value != nil
	case memAbsent:
		return b.memories[idx-1].value == nil
	}
	return true
}

// checkAll reports if every condition holds.
func (b *brain) checkAll(conds []memoryCondition) bool {
	for _, c := range conds {
		if !b.check(c.mem, c.status) {
			return false
		}
	}
	return true
}

// get returns the value of a memory, if the brain remembers one.
func (b *brain) get(mem memoryType) (any, bool) {
	if idx := b.memIdx[mem]; idx != 0 {
		if v := b.memories[idx-1].value; v != nil {
			return v, true
		}
	}
	return nil, false
}

// clearMemories forgets every memory, as Brain.clearMemories.
func (b *brain) clearMemories() {
	for i := range b.memories {
		b.memories[i].clear()
	}
	b.expiringMask = [2]uint64{}
}

// erase forgets a memory.
func (b *brain) erase(mem memoryType) {
	if idx := b.memIdx[mem]; idx != 0 {
		b.memories[idx-1].clear()
		b.clearExpiringBit(mem)
	}
}

// set remembers a value of a memory, or forgets it if v is nil or an empty
// slice, as Brain.setMemory.
func (b *brain) set(mem memoryType, v any) { b.setWithExpiry(mem, v, neverExpire) }

// setWithExpiry remembers a value of a memory for ttl ticks.
func (b *brain) setWithExpiry(mem memoryType, v any, ttl int64) {
	idx := b.memIdx[mem]
	if idx == 0 {
		return
	}
	s := &b.memories[idx-1]
	if v == nil || emptyMemoryValue(v) {
		s.clear()
		b.clearExpiringBit(mem)
		return
	}
	s.set(v, ttl)
	if ttl != neverExpire {
		b.setExpiringBit(mem)
	} else {
		b.clearExpiringBit(mem)
	}
}

func emptyMemoryValue(v any) bool {
	switch v := v.(type) {
	case []*world.EntityHandle:
		return len(v) == 0
	case *world.EntityHandle:
		return v == nil
	}
	return false
}

// timeUntilExpiry returns the ticks the memory is remembered for.
func (b *brain) timeUntilExpiry(mem memoryType) int64 {
	if idx := b.memIdx[mem]; idx != 0 {
		return b.memories[idx-1].ttl
	}
	return 0
}

// entity returns the entity a memory holds, if it is in the world.
func (b *brain) entity(m *Mob, mem memoryType) world.Entity {
	v, ok := b.get(mem)
	if !ok {
		return nil
	}
	h, ok := v.(*world.EntityHandle)
	if !ok {
		return nil
	}
	e, ok := h.Entity(m.tx)
	if !ok {
		return nil
	}
	return e
}

// setEntity remembers an entity, or forgets the memory if e is nil.
func (b *brain) setEntity(mem memoryType, e world.Entity) {
	if e == nil {
		b.erase(mem)
		return
	}
	b.set(mem, e.H())
}

// int returns the integer a memory holds.
func (b *brain) int(mem memoryType) (int, bool) {
	v, ok := b.get(mem)
	if !ok {
		return 0, false
	}
	i, ok := v.(int)
	return i, ok
}

// isActive reports if an activity runs.
func (b *brain) isActive(a activity) bool { return b.activeMask&(uint64(1)<<a) != 0 }

// activeNonCore returns the activity running beside the core ones.
func (b *brain) activeNonCore() (activity, bool) {
	nonCore := b.activeMask &^ b.coreMask
	if nonCore == 0 {
		return 0, false
	}
	return activity(bits.TrailingZeros64(nonCore)), true
}

func (b *brain) useDefaultActivity() { b.setActive(b.defaultAct) }

// setActiveIfPossible ports Brain.setActiveActivityIfPossible.
func (b *brain) setActiveIfPossible(a activity) {
	if b.requirementsMet(a) {
		b.setActive(a)
	} else {
		b.useDefaultActivity()
	}
}

// setActiveToFirstValid ports Brain.setActiveActivityToFirstValid.
func (b *brain) setActiveToFirstValid(acts ...activity) {
	for _, a := range acts {
		if b.requirementsMet(a) {
			b.setActive(a)
			return
		}
	}
}

func (b *brain) setActive(a activity) {
	if b.isActive(a) {
		return
	}
	bit := uint64(1) << a
	if len(b.eraseOnStop) > 0 {
		for rem := b.activeMask &^ bit; rem != 0; rem &= rem - 1 {
			old := activity(bits.TrailingZeros64(rem))
			for _, mem := range b.eraseOnStop[old] {
				b.erase(mem)
			}
		}
	}
	b.activeMask = b.coreMask | bit
}

func (b *brain) requirementsMet(a activity) bool {
	return b.registeredActs&(uint64(1)<<a) != 0 && b.checkAll(b.requirements[a])
}

// addActivity adds the behaviours of an activity at priorities counting up
// from priority, as ActivityData.create does.
func (b *brain) addActivity(a activity, priority int, behaviors ...behaviorControl) {
	pairs := make([]prioritizedBehavior, len(behaviors))
	for i, bh := range behaviors {
		pairs[i] = prioritizedBehavior{priority + i, bh}
	}
	b.addActivityWith(a, pairs, nil, nil)
}

// addActivityWith ports Brain.addActivity: the activity runs its behaviours
// at their priorities while the conditions hold, and forgets the memories
// erase when it stops.
func (b *brain) addActivityWith(a activity, pairs []prioritizedBehavior, conds []memoryCondition, erase []memoryType) {
	actBit := uint64(1) << a
	b.registeredActs |= actBit
	b.requirements[a] = conds
	if len(erase) > 0 {
		if b.eraseOnStop == nil {
			b.eraseOnStop = make(map[activity][]memoryType, 2)
		}
		b.eraseOnStop[a] = erase
	}
	for _, p := range pairs {
		for _, mem := range p.b.requiredMemories() {
			b.register(mem)
		}
		i := sort.Search(len(b.byPriority), func(i int) bool { return b.byPriority[i].priority >= p.priority })
		if i == len(b.byPriority) || b.byPriority[i].priority != p.priority {
			b.byPriority = slices.Insert(b.byPriority, i, priorityBehaviors{priority: p.priority})
		}
		pb := &b.byPriority[i]
		pb.actMask |= actBit
		j := slices.IndexFunc(pb.activities, func(ab activityBehaviors) bool { return ab.activity == a })
		if j < 0 {
			pb.activities = append(pb.activities, activityBehaviors{activity: a})
			j = len(pb.activities) - 1
		}
		pb.activities[j].behaviors = append(pb.activities[j].behaviors, orderedBehavior{bh: p.b})
	}
	b.orderDirty = true
}

func (b *brain) syncOrder() {
	var order uint16
	hadRunning := len(b.runningBuf) > 0
	if hadRunning {
		clear(b.runningBuf)
		b.runningBuf = b.runningBuf[:0]
	}
	for pi := range b.byPriority {
		pb := &b.byPriority[pi]
		for ai := range pb.activities {
			ab := &pb.activities[ai]
			for bi := range ab.behaviors {
				ab.behaviors[bi].order = order
				if hadRunning && ab.behaviors[bi].bh.status() == behaviorRunning {
					b.runningBuf = append(b.runningBuf, ab.behaviors[bi])
				}
				order++
			}
		}
	}
	b.orderDirty = false
	b.activeCacheMask = 0
}

func (b *brain) rebuildActiveCache() {
	b.activeCache = b.activeCache[:0]
	mask := b.activeMask
	for pi := range b.byPriority {
		pb := &b.byPriority[pi]
		if pb.actMask&mask == 0 {
			continue
		}
		for ai := range pb.activities {
			ab := &pb.activities[ai]
			if mask&(uint64(1)<<ab.activity) != 0 {
				b.activeCache = append(b.activeCache, activeBehaviorGroup{
					pbIdx:     uint16(pi),
					abIdx:     uint16(ai),
					behaviors: ab.behaviors,
				})
			}
		}
	}
	b.activeCacheMask = mask
}

func (b *brain) insertRunning(ob orderedBehavior) {
	n := len(b.runningBuf)
	if n == 0 || b.runningBuf[n-1].order < ob.order {
		b.runningBuf = append(b.runningBuf, ob)
		return
	}
	i := 0
	for i < n && b.runningBuf[i].order < ob.order {
		i++
	}
	if i < n && b.runningBuf[i].order == ob.order {
		return
	}
	b.runningBuf = append(b.runningBuf, orderedBehavior{})
	copy(b.runningBuf[i+1:], b.runningBuf[i:])
	b.runningBuf[i] = ob
}

func (b *brain) tickExpiringMemories() {
	for word := range 2 {
		for rem := b.expiringMask[word]; rem != 0; rem &= rem - 1 {
			mem := memoryType(word*64 + bits.TrailingZeros64(rem))
			s := &b.memories[b.memIdx[mem]-1]
			if s.value == nil || s.ttl == neverExpire {
				b.clearExpiringBit(mem)
				continue
			}
			if s.ttl <= 0 {
				s.clear()
				b.clearExpiringBit(mem)
				continue
			}
			s.ttl--
		}
	}
}

func (b *brain) startBehaviors(m *Mob, time int64) {
	if b.activeCacheMask != b.activeMask {
		b.rebuildActiveCache()
	}
	startMask := b.activeMask
	for _, grp := range b.activeCache {
		for _, ob := range grp.behaviors {
			if ob.bh.status() == behaviorStopped && ob.bh.tryStart(m, time) && ob.bh.status() == behaviorRunning {
				b.insertRunning(ob)
			}
		}
		if b.activeMask != startMask {
			b.startBehaviorsFrom(m, time, int(grp.pbIdx), int(grp.abIdx)+1)
			return
		}
	}
}

func (b *brain) startBehaviorsFrom(m *Mob, time int64, startPB, startAB int) {
	for pi := startPB; pi < len(b.byPriority); pi++ {
		pb := &b.byPriority[pi]
		if pb.actMask&b.activeMask == 0 {
			startAB = 0
			continue
		}
		for ai := startAB; ai < len(pb.activities); ai++ {
			ab := &pb.activities[ai]
			if !b.isActive(ab.activity) {
				continue
			}
			for _, ob := range ab.behaviors {
				if ob.bh.status() == behaviorStopped && ob.bh.tryStart(m, time) && ob.bh.status() == behaviorRunning {
					b.insertRunning(ob)
				}
			}
		}
		startAB = 0
	}
}

// tick ports Brain.tick: memories run out, sensors scan, and behaviours of
// the active activities start and tick.
func (b *brain) tick(m *Mob) {
	b.start(m)
	if b.orderDirty {
		b.syncOrder()
	}
	if b.expiringMask[0]|b.expiringMask[1] != 0 {
		b.tickExpiringMemories()
	}
	for i := range b.sensors {
		s := &b.sensors[i]
		if s.timeToTick--; s.timeToTick <= 0 {
			s.timeToTick = int64(sensorScanRate(s.s))
			s.s.doTick(m, b)
		}
	}
	time := m.tx.CurrentTick()
	b.startBehaviors(m, time)
	if len(b.runningBuf) == 0 {
		return
	}
	b.tickBuf = append(b.tickBuf[:0], b.runningBuf...)
	for _, ob := range b.tickBuf {
		ob.bh.tickOrStop(m, time)
	}
	clear(b.tickBuf)
	b.tickBuf = b.tickBuf[:0]
	n := 0
	for _, ob := range b.runningBuf {
		if ob.bh.status() == behaviorRunning {
			b.runningBuf[n] = ob
			n++
		}
	}
	clear(b.runningBuf[n:])
	b.runningBuf = b.runningBuf[:n]
}

// stopAll ports Brain.stopAll.
func (b *brain) stopAll(m *Mob) {
	if len(b.runningBuf) == 0 {
		return
	}
	time := m.tx.CurrentTick()
	b.tickBuf = append(b.tickBuf[:0], b.runningBuf...)
	clear(b.runningBuf)
	b.runningBuf = b.runningBuf[:0]
	for _, ob := range b.tickBuf {
		if ob.bh.status() == behaviorRunning {
			ob.bh.doStop(m, time)
		}
	}
	clear(b.tickBuf)
	b.tickBuf = b.tickBuf[:0]
}

// mobBrain is implemented by the behaviours of mobs driven by a brain.
type mobBrain interface {
	brain() *brain
}

// Brain returns the brain of the mob, nil if it has none.
func (m *Mob) brain() *brain {
	if m.AI.caps == 0 {
		m.AI.initCaps(m)
	}
	if m.AI.capsBrain != nil {
		return m.AI.capsBrain.brain()
	}
	return nil
}

// behavior ports Java's Behavior: a behaviour that starts when its entry
// conditions hold, runs for a random duration between minDuration and
// maxDuration while canStillUse allows, and stops. Each hook may be nil.
type behavior struct {
	entry                    []memoryCondition
	minDuration, maxDuration int
	// noTimeout keeps the behaviour from timing out, as behaviours
	// overriding timedOut to return false do.
	noTimeout bool

	checkExtra  func(m *Mob) bool
	start       func(m *Mob, time int64)
	tick        func(m *Mob, time int64)
	stop        func(m *Mob, time int64)
	canStillUse func(m *Mob, time int64) bool

	st           behaviorStatus
	endTimestamp int64
}

func (b *behavior) status() behaviorStatus { return b.st }

func (b *behavior) requiredMemories() []memoryType {
	mems := make([]memoryType, len(b.entry))
	for i, c := range b.entry {
		mems[i] = c.mem
	}
	return mems
}

func (b *behavior) tryStart(m *Mob, time int64) bool {
	if !m.brain().checkAll(b.entry) || b.checkExtra != nil && !b.checkExtra(m) {
		return false
	}
	b.st = behaviorRunning
	minD, maxD := b.minDuration, b.maxDuration
	if minD == 0 && maxD == 0 {
		minD, maxD = 60, 60
	}
	b.endTimestamp = time + int64(minD) + int64(m.Random().NextInt(int32(maxD+1-minD)))
	if b.start != nil {
		b.start(m, time)
	}
	return true
}

func (b *behavior) tickOrStop(m *Mob, time int64) {
	if (b.noTimeout || time <= b.endTimestamp) && b.canStillUse != nil && b.canStillUse(m, time) {
		if b.tick != nil {
			b.tick(m, time)
		}
		return
	}
	b.doStop(m, time)
}

func (b *behavior) doStop(m *Mob, time int64) {
	b.st = behaviorStopped
	if b.stop != nil {
		b.stop(m, time)
	}
}

// oneShot ports Java's OneShot: a behaviour that runs for the tick its
// trigger reports it started.
type oneShot struct {
	mems    []memoryType
	trigger func(m *Mob, time int64) bool
	st      behaviorStatus
}

// newOneShot returns a OneShot that triggers when the conditions hold and
// trigger reports it did something.
func newOneShot(conds []memoryCondition, trigger func(m *Mob, b *brain, time int64) bool) *oneShot {
	mems := make([]memoryType, len(conds))
	for i, c := range conds {
		mems[i] = c.mem
	}
	return &oneShot{mems: mems, trigger: func(m *Mob, time int64) bool {
		b := m.brain()
		return b.checkAll(conds) && trigger(m, b, time)
	}}
}

func (o *oneShot) status() behaviorStatus         { return o.st }
func (o *oneShot) requiredMemories() []memoryType { return o.mems }
func (o *oneShot) tryStart(m *Mob, time int64) bool {
	if o.trigger(m, time) {
		o.st = behaviorRunning
		return true
	}
	return false
}
func (o *oneShot) tickOrStop(m *Mob, time int64) { o.doStop(m, time) }
func (o *oneShot) doStop(*Mob, int64)            { o.st = behaviorStopped }

// weightedBehavior pairs a behaviour with the weight a gate shuffles it by.
type weightedBehavior struct {
	b      behaviorControl
	weight int
}

// gateBehavior ports Java's GateBehavior: behaviours a gate starts in order
// or shuffled, one of them or all, while its entry conditions hold.
type gateBehavior struct {
	entry    []memoryCondition
	erase    []memoryType
	shuffled bool
	runOne   bool
	children []weightedBehavior
	rand     []float64
	st       behaviorStatus
}

// newRunOne ports RunOne: a gate that runs one of its behaviours, picked by
// weight.
func newRunOne(entry []memoryCondition, children ...weightedBehavior) *gateBehavior {
	return &gateBehavior{entry: entry, shuffled: true, runOne: true, children: children}
}

func (g *gateBehavior) status() behaviorStatus { return g.st }

func (g *gateBehavior) requiredMemories() []memoryType {
	var mems []memoryType
	for _, c := range g.entry {
		mems = append(mems, c.mem)
	}
	for _, c := range g.children {
		mems = append(mems, c.b.requiredMemories()...)
	}
	return mems
}

func (g *gateBehavior) tryStart(m *Mob, time int64) bool {
	if !m.brain().checkAll(g.entry) {
		return false
	}
	g.st = behaviorRunning
	if g.shuffled {
		// ShufflingList.shuffle: an entry draws -random^(1/weight), and the
		// lowest go first.
		r := m.Random()
		var stackKeys [16]float64
		var keys []float64
		if len(g.children) <= len(stackKeys) {
			keys = stackKeys[:len(g.children)]
		} else {
			if cap(g.rand) < len(g.children) {
				g.rand = make([]float64, len(g.children))
			}
			keys = g.rand[:len(g.children)]
		}
		for i, c := range g.children {
			keys[i] = -math.Pow(float64(r.NextFloat()), 1/float64(c.weight))
		}
		for i := 1; i < len(g.children); i++ {
			k, ch := keys[i], g.children[i]
			j := i
			for j > 0 && k < keys[j-1] {
				keys[j], g.children[j] = keys[j-1], g.children[j-1]
				j--
			}
			keys[j], g.children[j] = k, ch
		}
	}
	for _, c := range g.children {
		if c.b.status() == behaviorStopped && c.b.tryStart(m, time) && g.runOne {
			break
		}
	}
	return true
}

func (g *gateBehavior) tickOrStop(m *Mob, time int64) {
	running := false
	for _, c := range g.children {
		if c.b.status() == behaviorRunning {
			c.b.tickOrStop(m, time)
		}
		running = running || c.b.status() == behaviorRunning
	}
	if !running {
		g.doStop(m, time)
	}
}

func (g *gateBehavior) doStop(m *Mob, time int64) {
	g.st = behaviorStopped
	for _, c := range g.children {
		if c.b.status() == behaviorRunning {
			c.b.doStop(m, time)
		}
	}
	for _, mem := range g.erase {
		m.brain().erase(mem)
	}
}

// doNothing ports Java's DoNothing: a behaviour that runs for a while and
// does nothing.
type doNothing struct {
	minDuration, maxDuration int
	st                       behaviorStatus
	endTimestamp             int64
}

func newDoNothing(minDuration, maxDuration int) *doNothing {
	return &doNothing{minDuration: minDuration, maxDuration: maxDuration}
}

func (d *doNothing) status() behaviorStatus         { return d.st }
func (d *doNothing) requiredMemories() []memoryType { return nil }
func (d *doNothing) tryStart(m *Mob, time int64) bool {
	d.st = behaviorRunning
	d.endTimestamp = time + int64(d.minDuration) + int64(m.Random().NextInt(int32(d.maxDuration+1-d.minDuration)))
	return true
}
func (d *doNothing) tickOrStop(m *Mob, time int64) {
	if time > d.endTimestamp {
		d.doStop(m, time)
	}
}
func (d *doNothing) doStop(*Mob, int64) { d.st = behaviorStopped }

// positionTracker ports Java's PositionTracker: where a mob walks or looks.
type positionTracker interface {
	currentPosition(tx *world.Tx) mgl64.Vec3
	currentBlockPos(tx *world.Tx) cube.Pos
	visibleBy(m *Mob) bool
}

// blockPosTracker ports Java's BlockPosTracker.
type blockPosTracker struct {
	pos    cube.Pos
	centre mgl64.Vec3
}

func newBlockPosTracker(pos cube.Pos) blockPosTracker {
	return blockPosTracker{pos: pos, centre: pos.Vec3Centre()}
}

func newVecTracker(v mgl64.Vec3) blockPosTracker {
	return blockPosTracker{pos: cube.PosFromVec3(v), centre: v}
}

func (t blockPosTracker) currentPosition(*world.Tx) mgl64.Vec3 { return t.centre }
func (t blockPosTracker) currentBlockPos(*world.Tx) cube.Pos   { return t.pos }
func (t blockPosTracker) visibleBy(*Mob) bool                  { return true }

// entityTracker ports Java's EntityTracker: an entity, at its eyes if
// trackEye, and at the block of its eyes if targetEye.
type entityTracker struct {
	h                   *world.EntityHandle
	trackEye, targetEye bool
	// last is where the entity was last seen, for when it left the world.
	last mgl64.Vec3
}

func newEntityTracker(e world.Entity, trackEye bool) *entityTracker {
	return &entityTracker{h: e.H(), trackEye: trackEye, last: e.Position()}
}

func (t *entityTracker) entity(tx *world.Tx) world.Entity {
	e, ok := t.h.Entity(tx)
	if !ok {
		return nil
	}
	t.last = e.Position()
	return e
}

func (t *entityTracker) currentPosition(tx *world.Tx) mgl64.Vec3 {
	e := t.entity(tx)
	if e == nil {
		return t.last
	}
	if t.trackEye {
		return mgl64.Vec3{e.Position()[0], entityEyeY(e), e.Position()[2]}
	}
	return e.Position()
}

func (t *entityTracker) currentBlockPos(tx *world.Tx) cube.Pos {
	e := t.entity(tx)
	if e == nil {
		return cube.PosFromVec3(t.last)
	}
	if t.targetEye {
		return cube.PosFromVec3(mgl64.Vec3{e.Position()[0], entityEyeY(e), e.Position()[2]})
	}
	return cube.PosFromVec3(e.Position())
}

func (t *entityTracker) visibleBy(m *Mob) bool {
	e := t.entity(m.tx)
	if e == nil {
		return false
	}
	if _, living := e.(Living); !living {
		return true
	}
	return entityAlive(e) && brainCanSee(m, e)
}

// walkTarget ports Java's WalkTarget.
type walkTarget struct {
	target      positionTracker
	speed       float64
	closeEnough int
}

// nearestVisibleLiving ports Java's NearestVisibleLivingEntities: the living
// entities near a mob, nearest first, and which of them it can see.
type nearestVisibleLiving struct {
	entities []*world.EntityHandle
	seen     map[*world.EntityHandle]bool
	mob      *world.EntityHandle
}

// visible reports if the mob sees the entity, as Sensor.isEntityTargetable
// decides it once per scan.
func (n *nearestVisibleLiving) visible(m *Mob, e world.Entity) bool {
	if v, ok := n.seen[e.H()]; ok {
		return v
	}
	c := forNonCombat()
	c.rangeLimit = m.AI.FollowRange
	if v, ok := m.brain().get(memAttackTarget); ok && v == e.H() {
		c.ignoreInvis = true
	}
	v := c.test(m, e)
	if n.seen == nil {
		n.seen = make(map[*world.EntityHandle]bool, min(len(n.entities), 8))
	}
	n.seen[e.H()] = v
	return v
}

// findClosest returns the nearest entity the mob sees for which filter
// returns true.
func (n *nearestVisibleLiving) findClosest(m *Mob, filter func(world.Entity) bool) world.Entity {
	for _, h := range n.entities {
		e, ok := h.Entity(m.tx)
		if ok && filter(e) && n.visible(m, e) {
			return e
		}
	}
	return nil
}

// findAll returns every entity the mob sees for which filter returns true.
func (n *nearestVisibleLiving) findAll(m *Mob, filter func(world.Entity) bool) []world.Entity {
	var out []world.Entity
	for _, h := range n.entities {
		e, ok := h.Entity(m.tx)
		if ok && filter(e) && n.visible(m, e) {
			out = append(out, e)
		}
	}
	return out
}

// contains reports if the mob sees the entity among those near it.
func (n *nearestVisibleLiving) contains(m *Mob, e world.Entity) bool {
	return slices.Contains(n.entities, e.H()) && n.visible(m, e)
}

// visibleLiving returns what the brain of the mob remembers seeing.
func visibleLiving(m *Mob) *nearestVisibleLiving {
	v, ok := m.brain().get(memNearestVisibleLivingEntities)
	if !ok {
		return nil
	}
	return v.(*nearestVisibleLiving)
}

// brainCanSee ports BehaviorUtils.canSee: the mob remembers seeing the
// entity.
func brainCanSee(m *Mob, e world.Entity) bool {
	v := visibleLiving(m)
	return v != nil && v.contains(m, e)
}

// setWalkAndLookTarget ports BehaviorUtils.setWalkAndLookTargetMemories.
func setWalkAndLookTarget(m *Mob, target positionTracker, speed float64, closeEnough int) {
	b := m.brain()
	b.set(memLookTarget, target)
	b.set(memWalkTarget, walkTarget{target: target, speed: speed, closeEnough: closeEnough})
}

// brainMobOf returns the mob of an entity driven by a brain, if it is one.
func brainMobOf(e world.Entity) (*Mob, bool) {
	m, ok := mobOf(e)
	if !ok || m.brain() == nil {
		return nil, false
	}
	return m, true
}
