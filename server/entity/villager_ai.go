package entity

import (
	"math"
	"slices"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports VillagerGoalPackages, the brain provider of Villager and
// the behaviours and sensors only villagers use.

// villagerSpeed is Villager.SPEED_MODIFIER.
const villagerSpeed = .5

// newVillagerBrain ports the brain provider of Villager: babies play where
// adults work.
func newVillagerBrain(b *VillagerBehaviour) *Brain {
	br := newBrain(nearestLivingEntitySensor{}, playerSensor{}, nearestItemSensor{wants: func(m *Mob, s item.Stack) bool { return b.wantsToPickUp(m, s) }},
		&villagerNearestBedSensor{}, hurtBySensor{}, villagerHostilesSensor{}, villagerBabiesSensor{}, villagerSecondaryPoiSensor{b}, villagerGolemSensor{})
	for _, mem := range []memoryType{memHome, memJobSite, memPotentialJobSite, memMeetingPoint, memLastSlept, memLastWoken, memLastWorkedAtPoi,
		memHeardBellTime, memHidingPlace, memDoorsToClose, memBreedTarget, memInteractionTarget} {
		br.register(mem)
	}
	speed := float64(float32(villagerSpeed))
	if b.age < 0 {
		br.addActivityWith(activityPlay, villagerPlayPackage(speed), nil, nil)
	} else {
		br.addActivityWith(activityWork, villagerWorkPackage(b, speed), []memoryCondition{{memJobSite, memPresent}}, nil)
	}
	br.addActivityWith(activityCore, villagerCorePackage(b, speed), nil, nil)
	br.addActivityWith(activityMeet, villagerMeetPackage(speed), []memoryCondition{{memMeetingPoint, memPresent}}, nil)
	br.addActivityWith(activityRest, villagerRestPackage(b, speed), nil, nil)
	br.addActivityWith(activityIdle, villagerIdlePackage(speed), nil, nil)
	br.addActivityWith(activityPanic, villagerPanicPackage(b, speed), nil, nil)
	br.addActivityWith(activityPreRaid, villagerPreRaidPackage(speed), nil, nil)
	br.addActivityWith(activityRaid, villagerRaidPackage(b, speed), nil, nil)
	br.addActivityWith(activityHide, villagerHidePackage(speed), nil, nil)
	br.core = []activity{activityCore}
	return br
}

// villagerScheduled ports the villager activity timelines of the overworld:
// babies play twice a day where adults work and meet, and all rest at night.
func villagerScheduled(m *Mob, baby bool) activity {
	if m.tx.World().Dimension() != world.Overworld {
		return activityIdle
	}
	t := int(m.tx.World().Time() % 24000)
	if t < 0 {
		t += 24000
	}
	type keyframe struct {
		at  int
		act activity
	}
	frames := []keyframe{{10, activityIdle}, {2000, activityWork}, {9000, activityMeet}, {11000, activityIdle}, {12000, activityRest}}
	if baby {
		frames = []keyframe{{10, activityIdle}, {3000, activityPlay}, {6000, activityIdle}, {10000, activityPlay}, {12000, activityRest}}
	}
	act := frames[len(frames)-1].act
	for _, f := range frames {
		if t >= f.at {
			act = f.act
		}
	}
	return act
}

// updateActivityFromSchedule ports Brain.updateActivityFromSchedule.
func (b *VillagerBehaviour) updateActivityFromSchedule(m *Mob) {
	br := b.br
	time := m.tx.CurrentTick()
	if time-br.lastScheduleUpdate <= 20 {
		return
	}
	br.lastScheduleUpdate = time
	if act := villagerScheduled(m, b.age < 0); !br.isActive(act) {
		br.setActiveIfPossible(act)
	}
}

// newUpdateActivityFromSchedule ports UpdateActivityFromSchedule.
func newUpdateActivityFromSchedule(b *VillagerBehaviour) *oneShot {
	return newOneShot(nil, func(m *Mob, _ *Brain, _ int64) bool {
		b.updateActivityFromSchedule(m)
		return true
	})
}

// villagerMinimalLook ports VillagerGoalPackages.getMinimalLookBehavior.
func villagerMinimalLook() prioritizedBehavior {
	return prioritizedBehavior{5, newRunOne(nil,
		weightedBehavior{newSetEntityLookTarget(isVillagerMob, 8), 2},
		weightedBehavior{newSetEntityLookTarget(isPlayerType, 8), 2},
		weightedBehavior{newDoNothing(30, 60), 8},
	)}
}

// villagerFullLook ports VillagerGoalPackages.getFullLookBehavior.
func villagerFullLook() prioritizedBehavior {
	category := func(c string) func(world.Entity) bool {
		return func(e world.Entity) bool { return entityMobCategory(e) == c }
	}
	return prioritizedBehavior{5, newRunOne(nil,
		weightedBehavior{newSetEntityLookTarget(func(e world.Entity) bool { return e.H().Type().EncodeEntity() == "minecraft:cat" }, 8), 8},
		weightedBehavior{newSetEntityLookTarget(isVillagerMob, 8), 2},
		weightedBehavior{newSetEntityLookTarget(isPlayerType, 8), 2},
		weightedBehavior{newSetEntityLookTarget(category("creature"), 8), 1},
		weightedBehavior{newSetEntityLookTarget(category("water_creature"), 8), 1},
		weightedBehavior{newSetEntityLookTarget(category("axolotls"), 8), 1},
		weightedBehavior{newSetEntityLookTarget(category("underground_water_creature"), 8), 1},
		weightedBehavior{newSetEntityLookTarget(category("water_ambient"), 8), 1},
		weightedBehavior{newSetEntityLookTarget(category("monster"), 8), 1},
		weightedBehavior{newDoNothing(30, 60), 2},
	)}
}

// entityMobCategory returns the MobCategory of the type of an entity.
func entityMobCategory(e world.Entity) string {
	switch id := e.H().Type().EncodeEntity(); id {
	case "minecraft:player", "minecraft:villager", "minecraft:villager_v2", "minecraft:iron_golem", "minecraft:snow_golem", "minecraft:armor_stand",
		"minecraft:copper_golem", "minecraft:mannequin":
		return "misc"
	case "minecraft:allay", "minecraft:happy_ghast", "minecraft:wandering_trader", "minecraft:ocelot", "minecraft:mule", "minecraft:skeleton_horse",
		"minecraft:trader_llama", "minecraft:zombie_horse", "minecraft:camel_husk", "minecraft:nautilus":
		return "creature"
	default:
		c, _ := naturalCategory(id)
		switch c {
		case spawnCreature:
			return "creature"
		case spawnAmbient:
			return "ambient"
		case spawnWaterCreature:
			return "water_creature"
		case spawnWaterAmbient:
			return "water_ambient"
		case spawnUndergroundWaterCreature:
			return "underground_water_creature"
		case spawnAxolotls:
			return "axolotls"
		case spawnMonster:
			return "monster"
		}
		return "misc"
	}
}

func isVillagerMob(e world.Entity) bool { _, ok := e.(*Villager); return ok }

// villagerCorePackage ports VillagerGoalPackages.getCorePackage.
func villagerCorePackage(b *VillagerBehaviour, speed float64) []prioritizedBehavior {
	return []prioritizedBehavior{
		{0, newSwim(.8)},
		{0, newInteractWithDoor()},
		{0, newLookAtTargetSink(45, 90)},
		{0, newVillagerPanicTrigger(b)},
		{0, newWakeUp(b)},
		{0, newReactToBell(b)},
		{0, newSetRaidStatus(b)},
		{0, newValidateNearbyPoi(b, func(p world.POI) bool { return villagerHeldJobSite(b.profession, p) }, memJobSite)},
		{0, newValidateNearbyPoi(b, func(p world.POI) bool { return villagerAcquirableJobSite(b.profession, p) }, memPotentialJobSite)},
		{1, newMoveToTargetSink(150, 250, nil)},
		{2, newPoiCompetitorScan(b)},
		{3, newLookAndFollowTradingPlayerSink(b, speed)},
		{5, newGoToWantedItem(nil, speed, false, 4, func() bool { return true })},
		{6, newAcquirePoi(b, func(p world.POI) bool { return villagerAcquirableJobSite(b.profession, p) }, memJobSite, memPotentialJobSite, true, nil)},
		{7, newGoToPotentialJobSite(b, speed)},
		{8, newYieldJobSite(b, speed)},
		{10, newAcquirePoi(b, func(p world.POI) bool { return p.Kind == world.Home }, memHome, memHome, false, villagerBedFree)},
		{10, newAcquirePoi(b, func(p world.POI) bool { return p.Kind == world.Meeting }, memMeetingPoint, memMeetingPoint, true, nil)},
		{10, newAssignProfessionFromJobSite(b)},
		{10, newResetProfession(b)},
	}
}

// villagerBedFree ports VillagerGoalPackages.validateBedPoi: a bed nobody
// sleeps in.
func villagerBedFree(tx *world.Tx, pos cube.Pos) bool {
	bed, ok := tx.Block(pos).(block.Bed)
	return ok && !bed.Occupied
}

// villagerWorkPackage ports VillagerGoalPackages.getWorkPackage.
func villagerWorkPackage(b *VillagerBehaviour, speed float64) []prioritizedBehavior {
	farmer := b.profession == "minecraft:farmer"
	harvest, bonemeal := 5, 7
	if farmer {
		harvest, bonemeal = 2, 4
	}
	return []prioritizedBehavior{
		villagerMinimalLook(),
		{5, newRunOne(nil,
			weightedBehavior{newWorkAtPoi(b, farmer), 7},
			weightedBehavior{newStrollAroundPoi(memJobSite, float64(float32(.4)), 4), 2},
			weightedBehavior{newStrollToPoi(memJobSite, float64(float32(.4)), 1, 10), 5},
			weightedBehavior{newStrollToPoiList(memSecondaryJobSite, speed, 1, 6, memJobSite), 5},
			weightedBehavior{newHarvestFarmland(b), harvest},
			weightedBehavior{newUseBonemeal(b), bonemeal},
		)},
		{10, newShowTradesToPlayer(b, 400, 1600)},
		{10, newSetLookAndInteract(isPlayerType, 4)},
		{2, newSetWalkTargetFromBlockMemory(b, memJobSite, speed, 9, 100, 1200)},
		{3, newGiveGiftToHero(b, 100)},
		{99, newUpdateActivityFromSchedule(b)},
	}
}

// villagerPlayPackage ports VillagerGoalPackages.getPlayPackage.
func villagerPlayPackage(speed float64) []prioritizedBehavior {
	return []prioritizedBehavior{
		{0, newMoveToTargetSink(80, 120, nil)},
		villagerFullLook(),
		{5, newPlayTagWithOtherKids()},
		{5, newRunOne([]memoryCondition{{memVisibleVillagerBabies, memAbsent}},
			weightedBehavior{newInteractWith(isVillagerMob, 8, speed, 2), 2},
			weightedBehavior{newInteractWith(func(e world.Entity) bool { return e.H().Type().EncodeEntity() == "minecraft:cat" }, 8, speed, 2), 1},
			weightedBehavior{newVillageBoundRandomStroll(speed, 10, 7), 1},
			weightedBehavior{newSetWalkTargetFromLookTarget(nil, constSpeed(speed), 2), 1},
			weightedBehavior{newJumpOnBed(speed), 2},
			weightedBehavior{newDoNothing(20, 40), 2},
		)},
		{99, newOneShot(nil, func(m *Mob, _ *Brain, _ int64) bool {
			m.Behaviour().(*VillagerBehaviour).updateActivityFromSchedule(m)
			return true
		})},
	}
}

// villagerRestPackage ports VillagerGoalPackages.getRestPackage.
func villagerRestPackage(b *VillagerBehaviour, speed float64) []prioritizedBehavior {
	return []prioritizedBehavior{
		{2, newSetWalkTargetFromBlockMemory(b, memHome, speed, 1, 150, 1200)},
		{3, newValidateNearbyPoi(b, func(p world.POI) bool { return p.Kind == world.Home }, memHome)},
		{3, newSleepInBed(b)},
		{5, newRunOne([]memoryCondition{{memHome, memAbsent}},
			weightedBehavior{newSetClosestHomeAsWalkTarget(speed), 1},
			weightedBehavior{newInsideBrownianWalk(speed), 4},
			weightedBehavior{newGoToClosestVillage(speed, 4), 2},
			weightedBehavior{newDoNothing(20, 40), 2},
		)},
		villagerMinimalLook(),
		{99, newUpdateActivityFromSchedule(b)},
	}
}

// villagerMeetPackage ports VillagerGoalPackages.getMeetPackage.
func villagerMeetPackage(speed float64) []prioritizedBehavior {
	return []prioritizedBehavior{
		{2, newRunOne(nil,
			weightedBehavior{newStrollAroundPoi(memMeetingPoint, float64(float32(.4)), 40), 2},
			weightedBehavior{newSocializeAtBell(), 2},
		)},
		{10, newShowTradesToPlayerAny(400, 1600)},
		{10, newSetLookAndInteract(isPlayerType, 4)},
		{2, newSetWalkTargetFromBlockMemoryAny(memMeetingPoint, speed, 6, 100, 200)},
		{3, newGiveGiftToHeroAny(100)},
		{3, newValidateNearbyPoiAny(func(p world.POI) bool { return p.Kind == world.Meeting }, memMeetingPoint)},
		{3, &gateBehavior{erase: []memoryType{memInteractionTarget}, runOne: true, children: []weightedBehavior{{newTradeWithVillager(), 1}}}},
		villagerFullLook(),
		{99, newUpdateActivityFromScheduleAny()},
	}
}

// villagerIdlePackage ports VillagerGoalPackages.getIdlePackage.
func villagerIdlePackage(speed float64) []prioritizedBehavior {
	canBreed := func(e world.Entity) bool { v, ok := e.(*Villager); return ok && v.CanBreed() }
	return []prioritizedBehavior{
		{2, newRunOne(nil,
			weightedBehavior{newInteractWith(isVillagerMob, 8, speed, 2), 2},
			weightedBehavior{newInteractWithMem(isVillagerMob, 8, func(m *Mob) bool { return canBreed(m.Self) }, canBreed, memBreedTarget, speed, 2), 1},
			weightedBehavior{newInteractWith(func(e world.Entity) bool { return e.H().Type().EncodeEntity() == "minecraft:cat" }, 8, speed, 2), 1},
			weightedBehavior{newVillageBoundRandomStroll(speed, 10, 7), 1},
			weightedBehavior{newSetWalkTargetFromLookTarget(nil, constSpeed(speed), 2), 1},
			weightedBehavior{newJumpOnBed(speed), 1},
			weightedBehavior{newDoNothing(30, 60), 1},
		)},
		{3, newGiveGiftToHeroAny(100)},
		{3, newSetLookAndInteract(isPlayerType, 4)},
		{3, newShowTradesToPlayerAny(400, 1600)},
		{3, &gateBehavior{erase: []memoryType{memInteractionTarget}, runOne: true, children: []weightedBehavior{{newTradeWithVillager(), 1}}}},
		{3, &gateBehavior{erase: []memoryType{memBreedTarget}, runOne: true, children: []weightedBehavior{{newVillagerMakeLove(), 1}}}},
		villagerFullLook(),
		{99, newUpdateActivityFromScheduleAny()},
	}
}

// villagerPanicPackage ports VillagerGoalPackages.getPanicPackage.
func villagerPanicPackage(b *VillagerBehaviour, speed float64) []prioritizedBehavior {
	runaway := float64(float32(speed) * 1.5)
	away := func(m *Mob, v any) mgl64.Vec3 {
		if e, ok := v.(*world.EntityHandle).Entity(m.tx); ok {
			return e.Position()
		}
		return m.data.Pos
	}
	return []prioritizedBehavior{
		{0, newVillagerCalmDown(b)},
		{1, newSetWalkTargetAwayFrom(memNearestHostile, runaway, 6, false, away)},
		{1, newSetWalkTargetAwayFrom(memHurtByEntity, runaway, 6, false, away)},
		{3, newVillageBoundRandomStroll(runaway, 2, 2)},
		villagerMinimalLook(),
	}
}

// villagerPreRaidPackage ports VillagerGoalPackages.getPreRaidPackage.
func villagerPreRaidPackage(speed float64) []prioritizedBehavior {
	fast := float64(float32(speed) * 1.5)
	return []prioritizedBehavior{
		{0, newRingBell()},
		{0, newRunOne(nil,
			weightedBehavior{newSetWalkTargetFromBlockMemoryAny(memMeetingPoint, fast, 2, 150, 200), 6},
			weightedBehavior{newVillageBoundRandomStroll(fast, 10, 7), 2},
		)},
		villagerMinimalLook(),
		{99, newResetRaidStatus()},
	}
}

// villagerRaidPackage ports VillagerGoalPackages.getRaidPackage.
func villagerRaidPackage(b *VillagerBehaviour, speed float64) []prioritizedBehavior {
	return []prioritizedBehavior{
		{0, newTriggerIf(func(m *Mob) bool {
			r := m.tx.RaidAt(m.BlockPos())
			return r != nil && r.Victory()
		}, newOneShotGate(
			weightedBehavior{newMoveToSkySeeingSpot(speed), 5},
			weightedBehavior{newVillageBoundRandomStroll(float64(float32(speed)*1.1), 10, 7), 2},
		))},
		{0, newCelebrateVillagersSurvivedRaid(b, 600, 600)},
		{2, newTriggerIf(func(m *Mob) bool {
			r := m.tx.RaidAt(m.BlockPos())
			return r != nil && r.Active() && !r.Victory() && !r.Loss()
		}, newLocateHidingPlace(24, float64(float32(speed)*1.4), 1))},
		villagerMinimalLook(),
		{99, newResetRaidStatus()},
	}
}

// villagerHidePackage ports VillagerGoalPackages.getHidePackage.
func villagerHidePackage(speed float64) []prioritizedBehavior {
	return []prioritizedBehavior{
		{0, newSetHiddenState(15, 3)},
		{1, newLocateHidingPlace(32, float64(float32(speed)*1.25), 2)},
		villagerMinimalLook(),
	}
}

// newOneShotGate ports TriggerGate.triggerOneShuffled: triggers tried in a
// weighted random order until one triggers.
func newOneShotGate(children ...weightedBehavior) *oneShot {
	gate := newRunOne(nil, children...)
	return newOneShot(nil, func(m *Mob, _ *Brain, time int64) bool {
		gate.tryStart(m, time)
		gate.doStop(m, time)
		return true
	})
}

// villagerOf returns the behaviour of the villager driving a mob.
func villagerOf(m *Mob) *VillagerBehaviour { return m.Behaviour().(*VillagerBehaviour) }

func newUpdateActivityFromScheduleAny() *oneShot {
	return newOneShot(nil, func(m *Mob, _ *Brain, _ int64) bool {
		villagerOf(m).updateActivityFromSchedule(m)
		return true
	})
}

// newVillagerPanicTrigger ports VillagerPanicTrigger: a hurt villager or one
// near a hostile mob panics, and every five seconds panicking villagers may
// summon an iron golem.
func newVillagerPanicTrigger(b *VillagerBehaviour) *behavior {
	scared := func(m *Mob) bool { return m.Brain().has(memHurtBy) || m.Brain().has(memNearestHostile) }
	return &behavior{
		canStillUse: func(m *Mob, _ int64) bool { return scared(m) },
		start: func(m *Mob, _ int64) {
			if !scared(m) {
				return
			}
			br := m.Brain()
			if !br.isActive(activityPanic) {
				for _, mem := range []memoryType{memPath, memWalkTarget, memLookTarget, memBreedTarget, memInteractionTarget} {
					br.erase(mem)
				}
			}
			br.setActiveIfPossible(activityPanic)
		},
		tick: func(m *Mob, time int64) {
			if time%100 == 0 {
				(&Villager{Ent: m.Ent}).spawnGolemIfNeeded(m.tx, time, 3)
			}
		},
	}
}

// newVillagerCalmDown ports VillagerCalmDown: a villager neither hurt nor
// near danger calms down and returns to its schedule.
func newVillagerCalmDown(b *VillagerBehaviour) *oneShot {
	return newOneShot([]memoryCondition{{memHurtBy, memRegistered}, {memHurtByEntity, memRegistered}, {memNearestHostile, memRegistered}}, func(m *Mob, br *Brain, _ int64) bool {
		attacker := br.entity(m, memHurtByEntity)
		scared := br.has(memHurtBy) || br.has(memNearestHostile) || attacker != nil && attacker.Position().Sub(m.data.Pos).LenSqr() <= 36
		if !scared {
			br.erase(memHurtBy)
			br.erase(memHurtByEntity)
			b.updateActivityFromSchedule(m)
		}
		return true
	})
}

// newWakeUp ports WakeUp: a villager sleeping outside its rest wakes up.
func newWakeUp(b *VillagerBehaviour) *oneShot {
	return newOneShot(nil, func(m *Mob, br *Brain, _ int64) bool {
		if br.isActive(activityRest) || b.sleeping == nil {
			return false
		}
		b.stopSleeping(m)
		return true
	})
}

// newReactToBell ports ReactToBell: a villager that heard a bell without a
// raid near hides.
func newReactToBell(b *VillagerBehaviour) *oneShot {
	return newOneShot([]memoryCondition{{memHeardBellTime, memPresent}}, func(m *Mob, br *Brain, _ int64) bool {
		if m.tx.RaidAt(m.BlockPos()) == nil {
			br.setActiveIfPossible(activityHide)
		}
		return true
	})
}

// newSetRaidStatus ports SetRaidStatus: a villager in a raid prepares for it
// and then joins it.
func newSetRaidStatus(b *VillagerBehaviour) *oneShot {
	return newOneShot(nil, func(m *Mob, br *Brain, _ int64) bool {
		if m.Random().NextInt(20) != 0 {
			return false
		}
		if r := m.tx.RaidAt(m.BlockPos()); r != nil {
			act := activityPreRaid
			if r.FirstWaveSpawned() && !r.BetweenWaves() {
				act = activityRaid
			}
			br.defaultAct = act
			br.setActiveIfPossible(act)
		}
		return true
	})
}

// newResetRaidStatus ports ResetRaidStatus.
func newResetRaidStatus() *oneShot {
	return newOneShot(nil, func(m *Mob, br *Brain, _ int64) bool {
		if m.Random().NextInt(20) != 0 {
			return false
		}
		if r := m.tx.RaidAt(m.BlockPos()); r == nil || r.Stopped() || r.Loss() {
			br.defaultAct = activityIdle
			villagerOf(m).updateActivityFromSchedule(m)
		}
		return true
	})
}

// villagerPoi returns the POI a memory holds in the world of the mob.
func villagerPoi(m *Mob, mem memoryType) (cube.Pos, bool) {
	v, ok := m.Brain().get(mem)
	if !ok {
		return cube.Pos{}, false
	}
	g := v.(globalPos)
	return g.pos, g.dim == m.tx.World().Dimension()
}

func newValidateNearbyPoiAny(match func(world.POI) bool, mem memoryType) *oneShot {
	return newValidateNearbyPoi(nil, match, mem)
}

// newValidateNearbyPoi ports ValidateNearbyPoi: a villager within sixteen
// blocks of a POI it remembers forgets it once it is gone, or a bed somebody
// else sleeps in.
func newValidateNearbyPoi(_ *VillagerBehaviour, match func(world.POI) bool, mem memoryType) *oneShot {
	return newOneShot([]memoryCondition{{mem, memPresent}}, func(m *Mob, br *Brain, _ int64) bool {
		pos, here := villagerPoi(m, mem)
		if !here || pos.Vec3Centre().Sub(m.data.Pos).LenSqr() >= 256 {
			return false
		}
		poi, ok := m.tx.POI(pos)
		if !ok || !match(poi) {
			br.erase(mem)
			return true
		}
		b := villagerOf(m)
		if bed, isBed := m.tx.Block(pos).(block.Bed); isBed && bed.Occupied && b.sleeping == nil {
			br.erase(mem)
			if !villagerSleepsIn(m.tx, pos) {
				m.tx.ReleasePOI(pos, m.H().UUID())
			}
		}
		return true
	})
}

// villagerSleepsIn reports if a villager sleeps in the bed at pos.
func villagerSleepsIn(tx *world.Tx, pos cube.Pos) bool {
	for e := range tx.EntitiesWithin(cube.Box(0, 0, 0, 1, 1, 1).Translate(pos.Vec3()).Grow(1)) {
		if v, ok := e.(*Villager); ok {
			if s := v.behaviour().sleeping; s != nil && *s == pos {
				return true
			}
		}
	}
	return false
}

// newPoiCompetitorScan ports PoiCompetitorScan: of the villagers near that
// claim the same job site, the one with the most experience keeps it.
func newPoiCompetitorScan(b *VillagerBehaviour) *oneShot {
	return newOneShot([]memoryCondition{{memJobSite, memPresent}, {memNearestLivingEntities, memPresent}}, func(m *Mob, br *Brain, _ int64) bool {
		v, _ := br.get(memJobSite)
		site := v.(globalPos)
		poi, ok := m.tx.POI(site.pos)
		if !ok {
			return true
		}
		winner := m.Self.(*Villager)
		for _, e := range nearestLiving(m) {
			other, ok := e.(*Villager)
			if !ok || e.H() == m.H() || other.Dead() {
				continue
			}
			ov, has := other.behaviour().br.get(memJobSite)
			if !has || ov.(globalPos) != site || !villagerHeldJobSite(other.behaviour().profession, poi) {
				continue
			}
			loser := winner
			if winner.behaviour().xp > other.behaviour().xp {
				loser = other
			} else {
				winner = other
			}
			loser.behaviour().br.erase(memJobSite)
		}
		return true
	})
}

// newLookAndFollowTradingPlayerSink ports LookAndFollowTradingPlayerSink: a
// villager trading with a player walks up to and looks at them.
func newLookAndFollowTradingPlayerSink(b *VillagerBehaviour, speed float64) *behavior {
	trader := func(m *Mob) world.Entity {
		if b.trading.user == nil {
			return nil
		}
		p, ok := b.trading.user.Entity(m.tx)
		if !ok {
			return nil
		}
		return p
	}
	valid := func(m *Mob) bool {
		p := trader(m)
		return m.Alive() && p != nil && !m.InWater() && !m.HurtRecently() && m.DistanceToSqr(p) <= 16
	}
	follow := func(m *Mob) {
		p := trader(m)
		br := m.Brain()
		br.set(memWalkTarget, walkTarget{target: newEntityTracker(p, false), speed: speed, closeEnough: 2})
		br.set(memLookTarget, newEntityTracker(p, true))
	}
	return &behavior{
		entry:       []memoryCondition{{memWalkTarget, memRegistered}, {memLookTarget, memRegistered}},
		noTimeout:   true,
		checkExtra:  valid,
		canStillUse: func(m *Mob, _ int64) bool { return valid(m) },
		start:       func(m *Mob, _ int64) { follow(m) },
		tick:        func(m *Mob, _ int64) { follow(m) },
		stop: func(m *Mob, _ int64) {
			m.Brain().erase(memWalkTarget)
			m.Brain().erase(memLookTarget)
		},
	}
}

// villagerJobSiteProfession returns the profession whose job site a POI is.
func villagerJobSiteProfession(poi world.POI) string {
	if poi.Kind != world.Workstation {
		return ""
	}
	if strings.HasSuffix(poi.Type, "cauldron") {
		return "minecraft:leatherworker"
	}
	return villagerProfessionForWorkstation(poi.Type)
}

// villagerHeldJobSite ports VillagerProfession.heldJobSite.
func villagerHeldJobSite(profession string, poi world.POI) bool {
	p := villagerJobSiteProfession(poi)
	return p != "" && p == profession
}

// villagerAcquirableJobSite ports VillagerProfession.acquirableJobSite: an
// unemployed villager takes any job site, others only their own.
func villagerAcquirableJobSite(profession string, poi world.POI) bool {
	switch profession {
	case "minecraft:nitwit":
		return false
	case "minecraft:none":
		return villagerJobSiteProfession(poi) != ""
	}
	return villagerHeldJobSite(profession, poi)
}

// villagerPoiRange ports PoiType.validRange: a mob reaches a bell from six
// blocks, other POIs from one.
func villagerPoiRange(poi world.POI) int {
	if poi.Kind == world.Meeting {
		return 6
	}
	return 1
}

// poiRetry ports AcquirePoi.JitteredLinearRetry.
type poiRetry struct {
	previous, next int64
	delay          int
}

func (r *poiRetry) mark(m *Mob, time int64) {
	r.previous = time
	r.delay = min(r.delay+int(m.Random().NextInt(40))+40, 400)
	r.next = time + int64(r.delay)
}

// newAcquirePoi ports AcquirePoi: every second or so a villager without the
// POI tries to path to one of the five nearest free ones within 48 blocks,
// and takes the one it reaches. Those it cannot reach it retries later.
func newAcquirePoi(b *VillagerBehaviour, match func(world.POI) bool, validate, acquire memoryType, onlyAdult bool, valid func(*world.Tx, cube.Pos) bool) *oneShot {
	var nextStart int64
	cache := map[cube.Pos]*poiRetry{}
	conds := []memoryCondition{{acquire, memAbsent}}
	if validate != acquire {
		conds = append(conds, memoryCondition{validate, memAbsent})
	}
	return newOneShot(conds, func(m *Mob, br *Brain, time int64) bool {
		if onlyAdult && b.age < 0 {
			return false
		}
		if nextStart == 0 {
			nextStart = time + int64(m.Random().NextInt(20))
			return false
		}
		if time < nextStart {
			return false
		}
		nextStart = time + 20 + int64(m.Random().NextInt(20))
		for pos, r := range cache {
			if time-r.previous >= 400 {
				delete(cache, pos)
			}
		}
		type candidate struct {
			pos  cube.Pos
			poi  world.POI
			dist int
		}
		var all []candidate
		origin := m.BlockPos()
		for pos, poi := range m.tx.POIsInRange(origin, 48) {
			if match(poi) && poi.Vacant() {
				d := pos.Sub(origin)
				all = append(all, candidate{pos, poi, d[0]*d[0] + d[1]*d[1] + d[2]*d[2]})
			}
		}
		slices.SortStableFunc(all, func(a, c candidate) int { return a.dist - c.dist })
		var targets []cube.Pos
		reach := 1
		for _, c := range all {
			if len(targets) == 5 {
				break
			}
			if r, ok := cache[c.pos]; ok {
				if time < r.next {
					continue
				}
				r.mark(m, time)
			}
			if valid != nil && !valid(m.tx, c.pos) {
				continue
			}
			targets = append(targets, c.pos)
			reach = max(reach, villagerPoiRange(c.poi))
		}
		if len(targets) == 0 {
			return true
		}
		path := m.AI.nav.CreatePathToAny(m, targets, reach)
		if path != nil && path.CanReach() {
			target := path.Target()
			if _, ok := m.tx.POI(target); ok && m.tx.ClaimPOI(target, m.H().UUID()) {
				br.set(acquire, globalPos{dim: m.tx.World().Dimension(), pos: target})
				clear(cache)
			}
			return true
		}
		for _, pos := range targets {
			if _, ok := cache[pos]; !ok {
				r := &poiRetry{}
				r.mark(m, time)
				cache[pos] = r
			}
		}
		return true
	})
}

// newGoToPotentialJobSite ports GoToPotentialJobSite: a villager walks to the
// job site it found for up to a minute, then gives it up.
func newGoToPotentialJobSite(b *VillagerBehaviour, speed float64) *behavior {
	return &behavior{
		entry:       []memoryCondition{{memPotentialJobSite, memPresent}},
		minDuration: 1200, maxDuration: 1200,
		checkExtra: func(m *Mob) bool {
			act, ok := m.Brain().activeNonCore()
			return !ok || act == activityIdle || act == activityWork || act == activityPlay
		},
		canStillUse: func(m *Mob, _ int64) bool { return m.Brain().has(memPotentialJobSite) },
		tick: func(m *Mob, _ int64) {
			if pos, ok := villagerPoi(m, memPotentialJobSite); ok {
				setWalkAndLookTarget(m, newBlockPosTracker(pos), speed, 1)
			}
		},
		stop: func(m *Mob, _ int64) {
			if pos, ok := villagerPoi(m, memPotentialJobSite); ok {
				m.tx.ReleasePOI(pos, m.H().UUID())
			}
			m.Brain().erase(memPotentialJobSite)
		},
	}
}

// newYieldJobSite ports YieldJobSite: an unemployed villager hands the job
// site it found to a nearby villager of its profession that wants it.
func newYieldJobSite(b *VillagerBehaviour, speed float64) *oneShot {
	return newOneShot([]memoryCondition{{memPotentialJobSite, memPresent}, {memJobSite, memAbsent}, {memNearestLivingEntities, memPresent},
		{memWalkTarget, memRegistered}, {memLookTarget, memRegistered}}, func(m *Mob, br *Brain, _ int64) bool {
		if b.age < 0 || b.profession != "minecraft:none" {
			return false
		}
		pos, _ := villagerPoi(m, memPotentialJobSite)
		poi, ok := m.tx.POI(pos)
		if !ok {
			return true
		}
		for _, e := range nearestLiving(m) {
			other, ok := e.(*Villager)
			if !ok || e.H() == m.H() || other.Dead() || !villagerWantsJobSite(other, poi, pos) {
				continue
			}
			br.erase(memWalkTarget)
			br.erase(memLookTarget)
			br.erase(memPotentialJobSite)
			ob := other.behaviour()
			if !ob.br.has(memJobSite) {
				om := ob.ai.Mob(other.Ent, other)
				setWalkAndLookTarget(om, newBlockPosTracker(pos), speed, 1)
				ob.br.set(memPotentialJobSite, globalPos{dim: m.tx.World().Dimension(), pos: pos})
				// The ticket moves to the villager it is handed to.
				m.tx.ReleasePOI(pos, m.H().UUID())
				m.tx.ClaimPOI(pos, other.H().UUID())
			}
			break
		}
		return true
	})
}

// villagerWantsJobSite ports YieldJobSite.nearbyWantsJobsite.
func villagerWantsJobSite(v *Villager, poi world.POI, pos cube.Pos) bool {
	b := v.behaviour()
	if b.br.has(memPotentialJobSite) || !villagerHeldJobSite(b.profession, poi) {
		return false
	}
	if site, ok := b.br.get(memJobSite); ok {
		return site.(globalPos).pos == pos
	}
	m := b.ai.Mob(v.Ent, v)
	path := m.AI.nav.CreatePath(m, pos, villagerPoiRange(poi))
	return path != nil && path.CanReach()
}

// newAssignProfessionFromJobSite ports AssignProfessionFromJobSite: a
// villager at its potential job site takes it and, unemployed, the
// profession of it.
func newAssignProfessionFromJobSite(b *VillagerBehaviour) *oneShot {
	return newOneShot([]memoryCondition{{memPotentialJobSite, memPresent}, {memJobSite, memRegistered}}, func(m *Mob, br *Brain, _ int64) bool {
		v, _ := br.get(memPotentialJobSite)
		site := v.(globalPos)
		if site.pos.Vec3Centre().Sub(m.data.Pos).LenSqr() >= 4 {
			return false
		}
		br.erase(memPotentialJobSite)
		br.set(memJobSite, site)
		if b.profession != "minecraft:none" {
			return true
		}
		if poi, ok := m.tx.POI(site.pos); ok {
			if p := villagerJobSiteProfession(poi); p != "" {
				m.Self.(*Villager).SetProfession(p)
				b.refreshBrain(m)
			}
		}
		return true
	})
}

// newResetProfession ports ResetProfession: a villager that lost its job
// site before ever trading loses its profession.
func newResetProfession(b *VillagerBehaviour) *oneShot {
	return newOneShot([]memoryCondition{{memJobSite, memAbsent}}, func(m *Mob, _ *Brain, _ int64) bool {
		if b.profession == "minecraft:none" || b.profession == "minecraft:nitwit" || b.xp != 0 || b.level > 1 {
			return false
		}
		m.Self.(*Villager).SetProfession("minecraft:none")
		b.refreshBrain(m)
		return true
	})
}

// newWorkAtPoi ports WorkAtPoi and WorkAtComposter: every fifteen seconds or
// so a villager at its job site works at it and restocks its trades. A
// farmer makes bread and composts its spare seeds.
func newWorkAtPoi(b *VillagerBehaviour, composter bool) *behavior {
	var lastCheck int64
	atSite := func(m *Mob) bool {
		pos, ok := villagerPoi(m, memJobSite)
		return ok && pos.Vec3Centre().Sub(m.data.Pos).LenSqr() < 1.73*1.73
	}
	return &behavior{
		entry: []memoryCondition{{memJobSite, memPresent}, {memLookTarget, memRegistered}},
		checkExtra: func(m *Mob) bool {
			time := m.tx.CurrentTick()
			if time-lastCheck < 300 || m.Random().NextInt(2) != 0 {
				return false
			}
			lastCheck = time
			return atSite(m)
		},
		canStillUse: func(m *Mob, _ int64) bool { return m.Brain().has(memJobSite) && atSite(m) },
		start: func(m *Mob, time int64) {
			br := m.Brain()
			br.set(memLastWorkedAtPoi, time)
			pos, _ := villagerPoi(m, memJobSite)
			br.set(memLookTarget, newBlockPosTracker(pos))
			if composter {
				if c, ok := m.tx.Block(pos).(block.Composter); ok {
					b.makeBread(m)
					b.compostItems(m, pos, c)
				}
			}
			v := m.Self.(*Villager)
			if v.shouldRestock(m.tx) {
				v.restock()
			}
		},
	}
}

// makeBread ports WorkAtComposter.makeBread: a farmer bakes up to three loaves
// out of its wheat while it carries no more than 36.
func (b *VillagerBehaviour) makeBread(m *Mob) {
	if villagerItemCount(b.inventory.Slots(), "minecraft:bread") > 36 {
		return
	}
	loaves := min(3, villagerItemCount(b.inventory.Slots(), "minecraft:wheat")/3)
	if loaves == 0 {
		return
	}
	removeInventoryItems(b.inventory, "minecraft:wheat", loaves*3)
	if _, err := b.inventory.AddItem(item.NewStack(item.Bread{}, loaves)); err != nil {
		m.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: m.data.Pos.Add(mgl64.Vec3{0, .5})}, item.NewStack(item.Bread{}, loaves)))
	}
}

// compostItems ports WorkAtComposter.compostItems: a farmer empties a full
// composter and fills it with the seeds it has beyond ten of a kind, twenty
// at most.
func (b *VillagerBehaviour) compostItems(m *Mob, pos cube.Pos, c block.Composter) {
	if c.Level == 8 {
		c.Level = 0
		m.tx.SetBlock(pos, c, nil)
		m.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: pos.Vec3Centre().Add(mgl64.Vec3{0, .5})}, item.NewStack(item.BoneMeal{}, 1)))
		m.tx.PlaySound(pos.Vec3(), sound.ComposterEmpty{})
	}
	compostable := []string{"minecraft:wheat_seeds", "minecraft:beetroot_seeds"}
	seen := make([]int, len(compostable))
	left := 20
	slots := b.inventory.Slots()
	for i := len(slots) - 1; i >= 0 && left > 0; i-- {
		s := slots[i]
		if s.Empty() {
			continue
		}
		name, _ := s.Item().EncodeItem()
		k := slices.Index(compostable, name)
		if k < 0 {
			continue
		}
		seen[k] += s.Count()
		use := min(seen[k]-10, left, s.Count())
		if use <= 0 {
			continue
		}
		left -= use
		for range use {
			c, _ = m.tx.Block(pos).(block.Composter)
			if c.Level >= 7 {
				return
			}
			used := c.Activate(pos, cube.FaceUp, m.tx, nil, &item.UseContext{})
			s = s.Grow(-1)
			_ = b.inventory.SetItem(i, s)
			if !used {
				return
			}
			if c2, ok := m.tx.Block(pos).(block.Composter); ok && c2.Level == 7 {
				return
			}
		}
	}
}

func villagerItemCount(slots []item.Stack, name string) int {
	n := 0
	for _, s := range slots {
		if !s.Empty() {
			if sn, _ := s.Item().EncodeItem(); sn == name {
				n += s.Count()
			}
		}
	}
	return n
}

func removeInventoryItems(inv interface {
	Slots() []item.Stack
	SetItem(int, item.Stack) error
}, name string, count int) {
	for i, s := range inv.Slots() {
		if count <= 0 {
			return
		}
		if s.Empty() {
			continue
		}
		if sn, _ := s.Item().EncodeItem(); sn != name {
			continue
		}
		n := min(count, s.Count())
		_ = inv.SetItem(i, s.Grow(-n))
		count -= n
	}
}

// newStrollToPoiList ports StrollToPoiList: a villager near its job site
// strolls to one of the secondary POIs around it now and then.
func newStrollToPoiList(list memoryType, speed float64, closeEnough, maxDist int, near memoryType) *oneShot {
	var nextOK int64
	return newOneShot([]memoryCondition{{memWalkTarget, memRegistered}, {list, memPresent}, {near, memPresent}}, func(m *Mob, br *Brain, time int64) bool {
		v, _ := br.get(list)
		sites := v.([]globalPos)
		if len(sites) == 0 {
			return false
		}
		target := sites[m.Random().NextInt(int32(len(sites)))]
		stay, _ := br.get(near)
		if target.dim != m.tx.World().Dimension() || stay.(globalPos).pos.Vec3Centre().Sub(m.data.Pos).LenSqr() >= float64(maxDist*maxDist) {
			return false
		}
		if time > nextOK {
			br.set(memWalkTarget, walkTarget{target: newBlockPosTracker(target.pos), speed: speed, closeEnough: closeEnough})
			nextOK = time + 100
		}
		return true
	})
}

func newSetWalkTargetFromBlockMemoryAny(mem memoryType, speed float64, closeEnough, tooFar, tooLong int) *oneShot {
	return newSetWalkTargetFromBlockMemory(nil, mem, speed, closeEnough, tooFar, tooLong)
}

// newSetWalkTargetFromBlockMemory ports SetWalkTargetFromBlockMemory: a
// villager walks to the POI it remembers, towards it while it is far, and
// gives it up when it has not reached it for too long.
func newSetWalkTargetFromBlockMemory(_ *VillagerBehaviour, mem memoryType, speed float64, closeEnough, tooFar, tooLong int) *oneShot {
	return newOneShot([]memoryCondition{{memCantReachWalkTargetSince, memRegistered}, {memWalkTarget, memAbsent}, {mem, memPresent}}, func(m *Mob, br *Brain, time int64) bool {
		v, _ := br.get(mem)
		target := v.(globalPos)
		giveUp := func() {
			villagerOf(m).releasePoi(m, mem)
			br.erase(mem)
			br.set(memCantReachWalkTargetSince, time)
		}
		since, cant := br.get(memCantReachWalkTargetSince)
		if target.dim != m.tx.World().Dimension() || cant && time-since.(int64) > int64(tooLong) {
			giveUp()
			return true
		}
		switch d := manhattan(target.pos, m.BlockPos()); {
		case d > tooFar:
			var towards mgl64.Vec3
			found := false
			for tries := 0; !found || manhattan(cube.PosFromVec3(towards), m.BlockPos()) > tooFar; {
				towards, found = DefaultRandomPosTowards(m, 15, 7, target.pos.Vec3Middle(), math.Pi/2)
				if tries++; tries == 1000 {
					giveUp()
					return true
				}
			}
			br.set(memWalkTarget, walkTarget{target: newVecTracker(towards), speed: speed, closeEnough: closeEnough})
		case d > closeEnough:
			br.set(memWalkTarget, walkTarget{target: newBlockPosTracker(target.pos), speed: speed, closeEnough: closeEnough})
		}
		return true
	})
}

// newHarvestFarmland ports HarvestFarmland: a farmer harvests the grown crops
// around it and plants seeds on the bare farmland, for up to ten seconds.
func newHarvestFarmland(b *VillagerBehaviour) *behavior {
	var (
		target    *cube.Pos
		nextOK    int64
		worked    int
		farmlands []cube.Pos
	)
	pick := func(m *Mob) {
		target = nil
		if len(farmlands) > 0 {
			p := farmlands[m.Random().NextInt(int32(len(farmlands)))]
			target = &p
		}
	}
	valid := func(tx *world.Tx, pos cube.Pos) bool {
		if c, ok := tx.Block(pos).(block.Crop); ok && c.GrowthStage() >= 7 {
			return true
		}
		_, air := tx.Block(pos).(block.Air)
		_, farmland := tx.Block(pos.Side(cube.FaceDown)).(block.Farmland)
		return air && farmland
	}
	setTargets := func(m *Mob) {
		br := m.Brain()
		br.set(memWalkTarget, walkTarget{target: newBlockPosTracker(*target), speed: villagerSpeed, closeEnough: 1})
		br.set(memLookTarget, newBlockPosTracker(*target))
	}
	return &behavior{
		entry: []memoryCondition{{memLookTarget, memAbsent}, {memWalkTarget, memAbsent}, {memSecondaryJobSite, memPresent}},
		checkExtra: func(m *Mob) bool {
			if mobGriefingDisabled(m.tx) || b.profession != "minecraft:farmer" {
				return false
			}
			farmlands = farmlands[:0]
			for x := -1; x <= 1; x++ {
				for y := -1; y <= 1; y++ {
					for z := -1; z <= 1; z++ {
						p := cube.PosFromVec3(m.data.Pos.Add(mgl64.Vec3{float64(x), float64(y), float64(z)}))
						if valid(m.tx, p) {
							farmlands = append(farmlands, p)
						}
					}
				}
			}
			pick(m)
			return target != nil
		},
		canStillUse: func(*Mob, int64) bool { return worked < 200 },
		start: func(m *Mob, time int64) {
			if time > nextOK && target != nil {
				setTargets(m)
			}
		},
		stop: func(m *Mob, time int64) {
			m.Brain().erase(memLookTarget)
			m.Brain().erase(memWalkTarget)
			worked = 0
			nextOK = time + 40
		},
		tick: func(m *Mob, time int64) {
			if target != nil && target.Vec3Centre().Sub(m.data.Pos).LenSqr() >= 1 {
				return
			}
			if target != nil && time > nextOK {
				pos := *target
				bl := m.tx.Block(pos)
				if c, ok := bl.(block.Crop); ok && c.GrowthStage() >= 7 {
					villagerBreakBlock(m, pos)
				}
				_, air := bl.(block.Air)
				_, farmland := m.tx.Block(pos.Side(cube.FaceDown)).(block.Farmland)
				if air && farmland {
					b.plantSeeds(m, pos)
				}
				if c, ok := bl.(block.Crop); ok && c.GrowthStage() < 7 {
					farmlands = slices.DeleteFunc(farmlands, func(p cube.Pos) bool { return p == pos })
					pick(m)
					if target != nil {
						nextOK = time + 20
						setTargets(m)
					}
				}
			}
			worked++
		},
	}
}

// villagerBreakBlock ports Level.destroyBlock with drops, for a villager.
func villagerBreakBlock(m *Mob, pos cube.Pos) {
	bl := m.tx.Block(pos)
	if breakable, ok := bl.(block.Breakable); ok {
		for _, drop := range breakable.BreakInfo().Drops(item.ToolNone{}, nil) {
			m.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: pos.Vec3Centre()}, drop))
		}
	}
	m.tx.SetBlock(pos, nil, nil)
	m.tx.EmitGameEvent(world.GameEventBlockDestroy, pos.Vec3Centre(), world.GameEventContext{Source: m.Self, AffectedState: bl})
}

// plantSeeds plants the first plantable seed a farmer carries on bare
// farmland, as HarvestFarmland.tick does.
func (b *VillagerBehaviour) plantSeeds(m *Mob, pos cube.Pos) {
	for i, s := range b.inventory.Slots() {
		if s.Empty() || !inItemTag(s, "villager_plantable_seeds") {
			continue
		}
		crop, ok := s.Item().(world.Block)
		if !ok {
			continue
		}
		m.tx.SetBlock(pos, crop, nil)
		m.tx.EmitGameEvent(world.GameEventBlockPlace, pos.Vec3Centre(), world.GameEventContext{Source: m.Self, AffectedState: crop})
		m.tx.PlaySound(pos.Vec3(), sound.ItemUseOn{Block: crop})
		_ = b.inventory.SetItem(i, s.Grow(-1))
		return
	}
}

// newUseBonemeal ports UseBonemeal: a farmer with bone meal fertilises the
// growing crops around it for four seconds.
func newUseBonemeal(b *VillagerBehaviour) *behavior {
	var (
		next, last int64
		worked     int
		crop       *cube.Pos
	)
	pickNext := func(m *Mob) {
		crop = nil
		count := 0
		for x := -1; x <= 1; x++ {
			for y := -1; y <= 1; y++ {
				for z := -1; z <= 1; z++ {
					p := m.BlockPos().Add(cube.Pos{x, y, z})
					if c, ok := m.tx.Block(p).(block.Crop); ok && c.GrowthStage() < 7 {
						count++
						if m.Random().NextInt(int32(count)) == 0 {
							pos := p
							crop = &pos
						}
					}
				}
			}
		}
	}
	setTarget := func(m *Mob) {
		if crop == nil {
			return
		}
		br := m.Brain()
		br.set(memLookTarget, newBlockPosTracker(*crop))
		br.set(memWalkTarget, walkTarget{target: newBlockPosTracker(*crop), speed: villagerSpeed, closeEnough: 1})
	}
	return &behavior{
		entry: []memoryCondition{{memLookTarget, memAbsent}, {memWalkTarget, memAbsent}},
		checkExtra: func(m *Mob) bool {
			tick := int64(b.ai.tickCount)
			if tick%10 != 0 || last != 0 && last+160 > tick {
				return false
			}
			if villagerItemCount(b.inventory.Slots(), "minecraft:bone_meal") <= 0 {
				return false
			}
			pickNext(m)
			return crop != nil
		},
		canStillUse: func(*Mob, int64) bool { return worked < 80 && crop != nil },
		start: func(m *Mob, time int64) {
			setTarget(m)
			b.setHeld(m, item.NewStack(item.BoneMeal{}, 1))
			next, worked = time, 0
		},
		stop: func(m *Mob, _ int64) {
			b.setHeld(m, item.Stack{})
			last = int64(b.ai.tickCount)
		},
		tick: func(m *Mob, time int64) {
			if time < next || crop.Vec3Centre().Sub(m.data.Pos).LenSqr() >= 1 {
				return
			}
			if villagerItemCount(b.inventory.Slots(), "minecraft:bone_meal") > 0 {
				if bm, ok := m.tx.Block(*crop).(item.BoneMealAffected); ok && bm.BoneMeal(*crop, m.tx) != item.BoneMealResultNone {
					removeInventoryItems(b.inventory, "minecraft:bone_meal", 1)
					pickNext(m)
					setTarget(m)
					next = time + 40
				}
			}
			worked++
		},
	}
}

func newShowTradesToPlayerAny(minDuration, maxDuration int) *behavior {
	return newShowTradesToPlayer(nil, minDuration, maxDuration)
}

// newShowTradesToPlayer ports ShowTradesToPlayer: a villager a player near
// looks at shows the trades for what the player holds, cycling through them.
func newShowTradesToPlayer(_ *VillagerBehaviour, minDuration, maxDuration int) *behavior {
	var (
		playerItem   *item.Stack
		display      []item.Stack
		cycle, index int
		lookTime     int
	)
	valid := func(m *Mob) bool {
		target := m.Brain().entity(m, memInteractionTarget)
		return target != nil && isPlayerEntity(target) && m.Alive() && entityAlive(target) && villagerOf(m).age >= 0 && m.DistanceToSqr(target) <= 17
	}
	lookAt := func(m *Mob) world.Entity {
		target := m.Brain().entity(m, memInteractionTarget)
		m.Brain().set(memLookTarget, newEntityTracker(target, true))
		return target
	}
	return &behavior{
		entry:       []memoryCondition{{memInteractionTarget, memPresent}},
		minDuration: minDuration, maxDuration: maxDuration,
		checkExtra: valid,
		canStillUse: func(m *Mob, _ int64) bool {
			return valid(m) && lookTime > 0 && m.Brain().has(memInteractionTarget)
		},
		start: func(m *Mob, _ int64) {
			lookAt(m)
			cycle, index, lookTime = 0, 0, 40
		},
		tick: func(m *Mob, _ int64) {
			b := villagerOf(m)
			target := lookAt(m)
			held := item.Stack{}
			if h, ok := target.(interface {
				HeldItems() (item.Stack, item.Stack)
			}); ok {
				held, _ = h.HeldItems()
			}
			if playerItem == nil || !villagerSameItem(*playerItem, held) {
				playerItem = &held
				display = display[:0]
				if !held.Empty() {
					for _, o := range b.offers {
						if o.Uses < o.MaxUses && (villagerSameItem(held, o.Input) || villagerSameItem(held, o.SecondaryInput)) {
							display = append(display, o.Output)
						}
					}
					if len(display) > 0 {
						lookTime = 900
						b.setHeld(m, display[0])
					}
				}
			}
			if len(display) > 0 {
				if len(display) >= 2 {
					if cycle++; cycle >= 40 {
						cycle = 0
						if index++; index > len(display)-1 {
							index = 0
						}
						b.setHeld(m, display[index])
					}
				}
			} else {
				b.setHeld(m, item.Stack{})
				lookTime = min(lookTime, 40)
			}
			lookTime--
		},
		stop: func(m *Mob, _ int64) {
			m.Brain().erase(memInteractionTarget)
			villagerOf(m).setHeld(m, item.Stack{})
			playerItem = nil
		},
	}
}

// villagerSameItem ports ItemStack.isSameItem.
func villagerSameItem(a, b item.Stack) bool {
	if a.Empty() || b.Empty() {
		return a.Empty() && b.Empty()
	}
	an, _ := a.Item().EncodeItem()
	bn, _ := b.Item().EncodeItem()
	return an == bn
}

func newGiveGiftToHeroAny(timeout int) *behavior { return newGiveGiftToHero(nil, timeout) }

// villagerGifts are the loot tables of the gifts of GiveGiftToHero.
var villagerGifts = map[string]string{
	"minecraft:armorer": "armorer_gift", "minecraft:butcher": "butcher_gift", "minecraft:cartographer": "cartographer_gift",
	"minecraft:cleric": "cleric_gift", "minecraft:farmer": "farmer_gift", "minecraft:fisherman": "fisherman_gift",
	"minecraft:fletcher": "fletcher_gift", "minecraft:leatherworker": "leatherworker_gift", "minecraft:librarian": "librarian_gift",
	"minecraft:mason": "mason_gift", "minecraft:shepherd": "shepherd_gift", "minecraft:toolsmith": "toolsmith_gift",
	"minecraft:weaponsmith": "weaponsmith_gift",
}

// newGiveGiftToHero ports GiveGiftToHero: now and then a villager walks up to
// a Hero of the Village it sees and throws them a gift of its profession.
func newGiveGiftToHero(_ *VillagerBehaviour, timeout int) *behavior {
	untilNext := 600
	given := false
	var start int64
	hero := func(m *Mob) world.Entity {
		p := m.Brain().entity(m, memNearestVisiblePlayer)
		if p == nil {
			return nil
		}
		if l, ok := p.(interface {
			Effect(t effect.Type) (effect.Effect, bool)
		}); ok {
			if _, has := l.Effect(effect.HeroOfTheVillage); has {
				return p
			}
		}
		return nil
	}
	return &behavior{
		entry: []memoryCondition{{memWalkTarget, memRegistered}, {memLookTarget, memRegistered}, {memInteractionTarget, memRegistered},
			{memNearestVisiblePlayer, memPresent}},
		minDuration: timeout, maxDuration: timeout,
		checkExtra: func(m *Mob) bool {
			if hero(m) == nil {
				return false
			}
			if untilNext > 0 {
				untilNext--
				return false
			}
			return true
		},
		canStillUse: func(m *Mob, _ int64) bool { return hero(m) != nil && !given },
		start: func(m *Mob, time int64) {
			given, start = false, time
			p := hero(m)
			m.Brain().setEntity(memInteractionTarget, p)
			m.Brain().set(memLookTarget, newEntityTracker(p, true))
		},
		tick: func(m *Mob, time int64) {
			p := hero(m)
			m.Brain().set(memLookTarget, newEntityTracker(p, true))
			if p.Position().Sub(m.data.Pos).LenSqr() < 25 {
				if time-start > 20 {
					villagerOf(m).throwGift(m, p)
					given = true
				}
				return
			}
			setWalkAndLookTarget(m, newEntityTracker(p, false), villagerSpeed, 5)
		},
		stop: func(m *Mob, _ int64) {
			untilNext = 600 + int(m.tx.RegionRandom().NextInt(6001))
			br := m.Brain()
			br.erase(memInteractionTarget)
			br.erase(memWalkTarget)
			br.erase(memLookTarget)
		},
	}
}

// newPlayTagWithOtherKids ports PlayTagWithOtherKids: baby villagers chase
// each other, and run off into the village when chased.
func newPlayTagWithOtherKids() *oneShot {
	return newOneShot([]memoryCondition{{memVisibleVillagerBabies, memPresent}, {memWalkTarget, memAbsent}, {memLookTarget, memRegistered},
		{memInteractionTarget, memRegistered}}, func(m *Mob, br *Brain, _ int64) bool {
		if m.tx.RegionRandom().NextInt(10) != 0 {
			return false
		}
		v, _ := br.get(memVisibleVillagerBabies)
		var friends []world.Entity
		for _, h := range v.([]*world.EntityHandle) {
			if e, ok := h.Entity(m.tx); ok {
				friends = append(friends, e)
			}
		}
		chasing := func(friend world.Entity) world.Entity {
			if fv, ok := friend.(*Villager); ok {
				return fv.behaviour().br.entity(fv.behaviour().ai.Mob(fv.Ent, fv), memInteractionTarget)
			}
			return nil
		}
		chase := func(kid world.Entity) {
			br.setEntity(memInteractionTarget, kid)
			br.set(memLookTarget, newEntityTracker(kid, true))
			br.set(memWalkTarget, walkTarget{target: newEntityTracker(kid, false), speed: float64(float32(.6)), closeEnough: 1})
		}
		for _, f := range friends {
			if c := chasing(f); c != nil && c.H() == m.H() {
				for range 10 {
					if pos, ok := LandRandomPos(m, 20, 8); ok && m.tx.IsVillage(cube.PosFromVec3(pos)) {
						br.set(memWalkTarget, walkTarget{target: newVecTracker(pos), speed: float64(float32(.6))})
						break
					}
				}
				return true
			}
		}
		chasers := map[*world.EntityHandle]int{}
		var order []world.Entity
		for _, f := range friends {
			if c := chasing(f); c != nil {
				if chasers[c.H()] == 0 {
					order = append(order, c)
				}
				chasers[c.H()]++
			}
		}
		slices.SortStableFunc(order, func(a, b world.Entity) int { return chasers[a.H()] - chasers[b.H()] })
		for _, kid := range order {
			if n := chasers[kid.H()]; n > 0 && n <= 5 {
				chase(kid)
				return true
			}
		}
		if len(friends) > 0 {
			chase(friends[0])
		}
		return true
	})
}

// newVillageBoundRandomStroll ports VillageBoundRandomStroll: a villager
// strolls about the village, or towards it from outside.
func newVillageBoundRandomStroll(speed float64, horizontal, vertical int) *oneShot {
	return newOneShot([]memoryCondition{{memWalkTarget, memAbsent}}, func(m *Mob, br *Brain, _ int64) bool {
		bp := m.BlockPos()
		var pos mgl64.Vec3
		var ok bool
		if m.tx.IsVillage(bp) {
			pos, ok = LandRandomPos(m, horizontal, vertical)
		} else if section, closer := villageSectionTowards(m.tx, bp, 2); closer {
			pos, ok = DefaultRandomPosTowards(m, horizontal, vertical, section, math.Pi/2)
		} else {
			pos, ok = LandRandomPos(m, horizontal, vertical)
		}
		if ok {
			br.set(memWalkTarget, walkTarget{target: newVecTracker(pos), speed: speed})
		} else {
			br.erase(memWalkTarget)
		}
		return true
	})
}

// villageSectionTowards ports BehaviorUtils.findSectionClosestToVillage: the
// bottom centre of the section within radius sections closest to a village,
// if one is closer than the section of pos.
func villageSectionTowards(tx *world.Tx, pos cube.Pos, radius int) (mgl64.Vec3, bool) {
	centre := cube.Pos{pos[0] >> 4, pos[1] >> 4, pos[2] >> 4}
	sectionPos := func(s cube.Pos) cube.Pos { return cube.Pos{s[0]<<4 + 8, s[1]<<4 + 8, s[2]<<4 + 8} }
	best, bestDist := centre, tx.VillageDistance(sectionPos(centre))
	start := bestDist
	for x := -radius; x <= radius; x++ {
		for y := -radius; y <= radius; y++ {
			for z := -radius; z <= radius; z++ {
				s := centre.Add(cube.Pos{x, y, z})
				if d := tx.VillageDistance(sectionPos(s)); d < start && d < bestDist {
					best, bestDist = s, d
				}
			}
		}
	}
	if best == centre {
		return mgl64.Vec3{}, false
	}
	c := sectionPos(best)
	return mgl64.Vec3{float64(c[0]) + .5, float64(c[1]), float64(c[2]) + .5}, true
}

// newJumpOnBed ports JumpOnBed: a baby villager jumps on a bed three to six
// times.
func newJumpOnBed(speed float64) *behavior {
	var (
		bed                          *cube.Pos
		timeToReach, jumps, cooldown int
	)
	jumpable := func(tx *world.Tx, pos cube.Pos) bool { _, ok := tx.Block(pos).(block.Bed); return ok }
	onOrOver := func(m *Mob) bool {
		bp := m.BlockPos()
		return jumpable(m.tx, bp) || jumpable(m.tx, bp.Side(cube.FaceDown))
	}
	nearest := func(m *Mob) (cube.Pos, bool) {
		v, ok := m.Brain().get(memNearestBed)
		if !ok {
			return cube.Pos{}, false
		}
		return v.(cube.Pos), true
	}
	return &behavior{
		entry:     []memoryCondition{{memNearestBed, memPresent}, {memWalkTarget, memAbsent}},
		noTimeout: true,
		checkExtra: func(m *Mob) bool {
			if villagerOf(m).age >= 0 {
				return false
			}
			_, ok := nearest(m)
			return onOrOver(m) || ok
		},
		start: func(m *Mob, _ int64) {
			if p, ok := nearest(m); ok {
				bed = &p
				timeToReach, jumps, cooldown = 100, 3+int(m.tx.RegionRandom().NextInt(4)), 0
				m.Brain().set(memWalkTarget, walkTarget{target: newBlockPosTracker(p), speed: speed})
			}
		},
		stop: func(*Mob, int64) { bed, timeToReach, jumps, cooldown = nil, 0, 0, 0 },
		canStillUse: func(m *Mob, _ int64) bool {
			if villagerOf(m).age >= 0 || bed == nil || !jumpable(m.tx, *bed) {
				return false
			}
			over := onOrOver(m)
			return !(!over && timeToReach <= 0) && !(over && jumps <= 0)
		},
		tick: func(m *Mob, _ int64) {
			switch {
			case !onOrOver(m):
				timeToReach--
			case cooldown > 0:
				cooldown--
			case jumpable(m.tx, m.BlockPos()):
				m.AI.jump.Jump()
				jumps--
				cooldown = 5
			}
		},
	}
}

// newSleepInBed ports SleepInBed: at rest, a villager at its free bed sleeps
// in it.
func newSleepInBed(b *VillagerBehaviour) *behavior {
	var nextOK int64
	return &behavior{
		entry: []memoryCondition{{memHome, memPresent}, {memLastWoken, memRegistered}, {memLastSlept, memRegistered}, {memWalkTarget, memRegistered},
			{memCantReachWalkTargetSince, memRegistered}},
		noTimeout: true,
		checkExtra: func(m *Mob) bool {
			if m.data.Riding != nil {
				return false
			}
			pos, here := villagerPoi(m, memHome)
			if !here {
				return false
			}
			if v, ok := m.Brain().get(memLastWoken); ok {
				if since := m.tx.CurrentTick() - v.(int64); since > 0 && since < 100 {
					return false
				}
			}
			bed, ok := m.tx.Block(pos).(block.Bed)
			return pos.Vec3Centre().Sub(m.data.Pos).LenSqr() < 4 && ok && !bed.Occupied
		},
		canStillUse: func(m *Mob, _ int64) bool {
			pos, ok := villagerPoi(m, memHome)
			return ok && m.Brain().isActive(activityRest) && m.data.Pos[1] > float64(pos[1])+.4 && pos.Vec3Centre().Sub(m.data.Pos).LenSqr() < 1.14*1.14
		},
		start: func(m *Mob, time int64) {
			if time <= nextOK {
				return
			}
			br := m.Brain()
			pos, _ := villagerPoi(m, memHome)
			if b.startSleeping(m, pos) {
				br.set(memLastSlept, time)
			}
			br.erase(memWalkTarget)
			br.erase(memCantReachWalkTargetSince)
		},
		stop: func(m *Mob, time int64) {
			if b.sleeping != nil {
				b.stopSleeping(m)
				nextOK = time + 40
			}
		},
	}
}

// newSetClosestHomeAsWalkTarget ports SetClosestHomeAsWalkTarget: a villager
// without a home walks to a bed it can reach.
func newSetClosestHomeAsWalkTarget(speed float64) *oneShot {
	var lastUpdate int64
	cache := map[cube.Pos]int64{}
	return newOneShot([]memoryCondition{{memWalkTarget, memAbsent}, {memHome, memAbsent}}, func(m *Mob, br *Brain, _ int64) bool {
		time := m.tx.CurrentTick()
		if time-lastUpdate < 20 {
			return false
		}
		closest, _, found := m.tx.NearestPOI(m.BlockPos(), world.Home, 48, nil)
		if !found || distSqrPos(closest, m.BlockPos()) <= 4 {
			return false
		}
		lastUpdate = time + int64(m.tx.RegionRandom().NextInt(20))
		tried := 0
		var targets []cube.Pos
		for pos, poi := range m.tx.POIsInRange(m.BlockPos(), 48) {
			if poi.Kind != world.Home {
				continue
			}
			if _, cached := cache[pos]; cached {
				continue
			}
			if tried++; tried >= 5 {
				break
			}
			cache[pos] = lastUpdate + 40
			targets = append(targets, pos)
		}
		if path := m.AI.nav.CreatePathToAny(m, targets, 1); path != nil && path.CanReach() {
			if _, ok := m.tx.POI(path.Target()); ok {
				br.set(memWalkTarget, walkTarget{target: newBlockPosTracker(path.Target()), speed: speed, closeEnough: 1})
			}
		} else if tried < 5 {
			for pos, until := range cache {
				if until < lastUpdate {
					delete(cache, pos)
				}
			}
		}
		return true
	})
}

// newInsideBrownianWalk ports InsideBrownianWalk: a villager indoors without
// a bed shuffles about a block at a time, staying indoors.
func newInsideBrownianWalk(speed float64) *oneShot {
	return newOneShot([]memoryCondition{{memWalkTarget, memAbsent}}, func(m *Mob, br *Brain, _ int64) bool {
		if canSeeSky(m.tx, m.BlockPos()) {
			return false
		}
		bp := m.BlockPos()
		var poses []cube.Pos
		for x := -1; x <= 1; x++ {
			for y := -1; y <= 1; y++ {
				for z := -1; z <= 1; z++ {
					poses = append(poses, bp.Add(cube.Pos{x, y, z}))
				}
			}
		}
		r := m.Random()
		for i := len(poses) - 1; i > 0; i-- {
			j := int(r.NextInt(int32(i + 1)))
			poses[i], poses[j] = poses[j], poses[i]
		}
		for _, p := range poses {
			if !canSeeSky(m.tx, p) && standable(m.tx, p) {
				br.set(memWalkTarget, walkTarget{target: newBlockPosTracker(p), speed: speed})
				break
			}
		}
		return true
	})
}

// standable reports if a mob can stand on the block at pos.
func standable(tx *world.Tx, pos cube.Pos) bool {
	return tx.Block(pos).Model().FaceSolid(pos, cube.FaceUp, tx)
}

// newGoToClosestVillage ports GoToClosestVillage: a villager outside a
// village heads for the nearest one.
func newGoToClosestVillage(speed float64, closeEnough int) *oneShot {
	return newOneShot([]memoryCondition{{memWalkTarget, memAbsent}}, func(m *Mob, br *Brain, _ int64) bool {
		if m.tx.IsVillage(m.BlockPos()) {
			return false
		}
		here := m.tx.VillageDistance(m.BlockPos())
		var target mgl64.Vec3
		found := false
		for range 5 {
			pos, ok := LandRandomPos(m, 15, 7)
			if !ok {
				continue
			}
			d := m.tx.VillageDistance(cube.PosFromVec3(pos))
			if d < here {
				target, found = pos, true
				break
			}
			if d == here {
				target, found = pos, true
			}
		}
		if found {
			br.set(memWalkTarget, walkTarget{target: newVecTracker(target), speed: speed, closeEnough: closeEnough})
		}
		return true
	})
}

// newSocializeAtBell ports SocializeAtBell: a villager at the bell walks up
// to another one now and then.
func newSocializeAtBell() *oneShot {
	return newOneShot([]memoryCondition{{memWalkTarget, memRegistered}, {memLookTarget, memRegistered}, {memMeetingPoint, memPresent},
		{memNearestVisibleLivingEntities, memPresent}, {memInteractionTarget, memAbsent}}, func(m *Mob, br *Brain, _ int64) bool {
		pos, here := villagerPoi(m, memMeetingPoint)
		v := visibleLiving(m)
		if m.tx.RegionRandom().NextInt(100) != 0 || !here || pos.Vec3Centre().Sub(m.data.Pos).LenSqr() >= 16 || v.findClosest(m, isVillagerMob) == nil {
			return false
		}
		if e := v.findClosest(m, func(e world.Entity) bool { return isVillagerMob(e) && m.DistanceToSqr(e) <= 32 }); e != nil {
			br.setEntity(memInteractionTarget, e)
			br.set(memLookTarget, newEntityTracker(e, true))
			br.set(memWalkTarget, walkTarget{target: newEntityTracker(e, false), speed: float64(float32(.3)), closeEnough: 1})
		}
		return true
	})
}

// villagerTargetValid ports BehaviorUtils.targetIsValid for villagers.
func villagerTargetValid(m *Mob, mem memoryType) *Villager {
	e := m.Brain().entity(m, mem)
	v, ok := e.(*Villager)
	if !ok || !entityAlive(e) || !visibleLiving(m).contains(m, e) {
		return nil
	}
	return v
}

func villagerMob(v *Villager) *Mob { return v.behaviour().ai.Mob(v.Ent, v) }

// newTradeWithVillager ports TradeWithVillager: villagers that meet gossip,
// and share their spare food, wheat and the items the other's profession
// wants.
func newTradeWithVillager() *behavior {
	var trades []string
	valid := func(m *Mob) bool { return villagerTargetValid(m, memInteractionTarget) != nil }
	return &behavior{
		entry:       []memoryCondition{{memInteractionTarget, memPresent}, {memNearestVisibleLivingEntities, memPresent}},
		checkExtra:  valid,
		canStillUse: func(m *Mob, _ int64) bool { return valid(m) },
		start: func(m *Mob, _ int64) {
			target := villagerTargetValid(m, memInteractionTarget)
			lockGazeAndWalkToEachOther(m, target, villagerSpeed, 2)
			trades = trades[:0]
			mine := villagerRequestedItems(villagerOf(m).profession)
			for _, it := range villagerRequestedItems(target.behaviour().profession) {
				if !slices.Contains(mine, it) {
					trades = append(trades, it)
				}
			}
		},
		tick: func(m *Mob, time int64) {
			target := villagerTargetValid(m, memInteractionTarget)
			if target == nil || m.DistanceToSqr(target) > 5 {
				return
			}
			lockGazeAndWalkToEachOther(m, target, villagerSpeed, 2)
			b := villagerOf(m)
			b.gossip(m, target, time)
			farmer := b.profession == "minecraft:farmer"
			if b.foodPoints() >= 24 && (farmer || target.WantsMoreFood()) {
				b.throwHalfStack(m, func(s item.Stack) bool { return villagerFoodPoints(item.NewStack(s.Item(), 1)) > 0 }, target)
			}
			if farmer && villagerItemCount(b.inventory.Slots(), "minecraft:wheat") > 32 {
				b.throwHalfStack(m, func(s item.Stack) bool { n, _ := s.Item().EncodeItem(); return n == "minecraft:wheat" }, target)
			}
			if len(trades) > 0 {
				b.throwHalfStack(m, func(s item.Stack) bool { n, _ := s.Item().EncodeItem(); return slices.Contains(trades, n) }, target)
			}
		},
		stop: func(m *Mob, _ int64) { m.Brain().erase(memInteractionTarget) },
	}
}

// villagerRequestedItems ports VillagerProfession.requestedItems.
func villagerRequestedItems(profession string) []string {
	if profession == "minecraft:farmer" {
		return []string{"minecraft:wheat", "minecraft:wheat_seeds", "minecraft:beetroot_seeds", "minecraft:bone_meal"}
	}
	return nil
}

// throwHalfStack ports TradeWithVillager.throwHalfStack.
func (b *VillagerBehaviour) throwHalfStack(m *Mob, match func(item.Stack) bool, target world.Entity) {
	for i, s := range b.inventory.Slots() {
		if s.Empty() || !match(s) {
			continue
		}
		var n int
		switch {
		case s.Count() > s.MaxCount()/2:
			n = s.Count() / 2
		case s.Count() > 24:
			n = s.Count() - 24
		default:
			continue
		}
		_ = b.inventory.SetItem(i, s.Grow(-n))
		throwItemTowards(m, s.Grow(n-s.Count()), target.Position(), mgl64.Vec3{float64(float32(.3)), float64(float32(.3)), float64(float32(.3))}, float64(float32(.3)))
		return
	}
}

// newVillagerMakeLove ports VillagerMakeLove: two villagers able to breed
// court for fourteen to sixteen seconds and have a baby if there is a free
// bed for it.
func newVillagerMakeLove() *behavior {
	var birth int64
	possible := func(m *Mob) bool {
		target := villagerTargetValid(m, memBreedTarget)
		return target != nil && m.Self.(*Villager).CanBreed() && target.CanBreed()
	}
	return &behavior{
		entry:       []memoryCondition{{memBreedTarget, memPresent}, {memNearestVisibleLivingEntities, memPresent}},
		minDuration: 350, maxDuration: 350,
		checkExtra:  possible,
		canStillUse: func(m *Mob, time int64) bool { return time <= birth && possible(m) },
		start: func(m *Mob, time int64) {
			target := villagerTargetValid(m, memBreedTarget)
			lockGazeAndWalkToEachOther(m, target, villagerSpeed, 2)
			target.viewAction(LoveHeartsAction{})
			m.Self.(*Villager).viewAction(LoveHeartsAction{})
			birth = time + 275 + int64(m.Random().NextInt(50))
		},
		tick: func(m *Mob, time int64) {
			target := villagerTargetValid(m, memBreedTarget)
			if target == nil || m.DistanceToSqr(target) > 5 {
				return
			}
			self := m.Self.(*Villager)
			lockGazeAndWalkToEachOther(m, target, villagerSpeed, 2)
			if time >= birth {
				self.EatAndDigestFood()
				target.EatAndDigestFood()
				villagerTryToGiveBirth(m, self, target)
			} else if m.Random().NextInt(35) == 0 {
				target.viewAction(LoveHeartsAction{})
				self.viewAction(LoveHeartsAction{})
			}
		},
		stop: func(m *Mob, _ int64) { m.Brain().erase(memBreedTarget) },
	}
}

// villagerTryToGiveBirth ports VillagerMakeLove.tryToGiveBirth.
func villagerTryToGiveBirth(m *Mob, self, partner *Villager) {
	bed, ok := villagerTakeVacantBed(m)
	if !ok {
		return
	}
	child := self.BreedWith(m.tx, partner, m.Random())
	if child == nil {
		m.tx.ReleasePOI(bed, m.H().UUID())
		return
	}
	added := m.tx.AddEntity(child)
	if kid, ok := added.(*Villager); ok {
		kid.viewAction(LoveHeartsAction{})
		// The bed moves to the baby.
		m.tx.ReleasePOI(bed, m.H().UUID())
		m.tx.ClaimPOI(bed, kid.H().UUID())
		kid.behaviour().br.set(memHome, globalPos{dim: m.tx.World().Dimension(), pos: bed})
	}
}

// villagerTakeVacantBed ports VillagerMakeLove.takeVacantBed: the nearest
// free bed within 48 blocks the villager can reach.
func villagerTakeVacantBed(m *Mob) (cube.Pos, bool) {
	type candidate struct {
		pos  cube.Pos
		dist int
	}
	var all []candidate
	origin := m.BlockPos()
	for pos, poi := range m.tx.POIsInRange(origin, 48) {
		if poi.Kind == world.Home && poi.Vacant() {
			d := pos.Sub(origin)
			all = append(all, candidate{pos, d[0]*d[0] + d[1]*d[1] + d[2]*d[2]})
		}
	}
	slices.SortStableFunc(all, func(a, b candidate) int { return a.dist - b.dist })
	for _, c := range all {
		path := m.AI.nav.CreatePath(m, c.pos, 1)
		if path != nil && path.CanReach() && m.tx.ClaimPOI(c.pos, m.H().UUID()) {
			return c.pos, true
		}
	}
	return cube.Pos{}, false
}

// newRingBell ports RingBell: a villager by the bell during a raid rings it
// now and then.
func newRingBell() *oneShot {
	return newOneShot([]memoryCondition{{memMeetingPoint, memPresent}}, func(m *Mob, br *Brain, _ int64) bool {
		if m.tx.RegionRandom().NextFloat() <= .95 {
			return false
		}
		pos, _ := villagerPoi(m, memMeetingPoint)
		if distSqrPos(pos, m.BlockPos()) < 9 {
			if bell, ok := m.tx.Block(pos).(block.Bell); ok {
				bell.Ring(pos, m.tx, m.data.Rot.Direction().Face())
			}
		}
		return true
	})
}

// newMoveToSkySeeingSpot ports MoveToSkySeeingSpot.
func newMoveToSkySeeingSpot(speed float64) *oneShot {
	return newOneShot([]memoryCondition{{memWalkTarget, memAbsent}}, func(m *Mob, br *Brain, _ int64) bool {
		if canSeeSky(m.tx, m.BlockPos()) {
			return false
		}
		r := m.Random()
		for range 10 {
			p := m.BlockPos().Add(cube.Pos{int(r.NextInt(20)) - 10, int(r.NextInt(6)) - 3, int(r.NextInt(20)) - 10})
			if villagerNoBlocksAbove(m, p) {
				br.set(memWalkTarget, walkTarget{target: newVecTracker(mgl64.Vec3{float64(p[0]) + .5, float64(p[1]), float64(p[2]) + .5}), speed: speed})
				break
			}
		}
		return true
	})
}

// villagerNoBlocksAbove ports MoveToSkySeeingSpot.hasNoBlocksAbove.
func villagerNoBlocksAbove(m *Mob, pos cube.Pos) bool {
	return canSeeSky(m.tx, pos) && float64(m.tx.JavaMotionBlockingHeight(pos[0], pos[2], false)) <= m.data.Pos[1]
}

// newCelebrateVillagersSurvivedRaid ports CelebrateVillagersSurvivedRaid:
// villagers under the open sky celebrate a won raid with fireworks.
func newCelebrateVillagersSurvivedRaid(b *VillagerBehaviour, minDuration, maxDuration int) *behavior {
	var raid *world.Raid
	return &behavior{
		minDuration: minDuration, maxDuration: maxDuration,
		checkExtra: func(m *Mob) bool {
			raid = m.tx.RaidAt(m.BlockPos())
			return raid != nil && raid.Victory() && villagerNoBlocksAbove(m, m.BlockPos())
		},
		canStillUse: func(*Mob, int64) bool { return raid != nil && !raid.Stopped() },
		stop: func(m *Mob, _ int64) {
			raid = nil
			b.updateActivityFromSchedule(m)
		},
		tick: func(m *Mob, _ int64) {
			r := m.Random()
			if r.NextInt(100) == 0 {
				m.tx.PlaySound(m.data.Pos, sound.NativeEvent{SoundType: "celebrate", EntityType: "minecraft:villager_v2", ExtraData: -1})
			}
			if r.NextInt(200) == 0 && villagerNoBlocksAbove(m, m.BlockPos()) {
				colours := item.Colours()
				colour := colours[r.NextInt(int32(len(colours)))]
				flight := int(r.NextInt(3))
				firework := item.Firework{Duration: villagerFireworkDuration(flight), Explosions: []item.FireworkExplosion{{Shape: item.FireworkShapeSmallSphere(), Colour: colour}}}
				m.tx.AddEntity(NewFirework(world.EntitySpawnOpts{Position: mgl64.Vec3{m.data.Pos[0], m.EyeY(), m.data.Pos[2]}}, firework))
			}
		},
	}
}

// villagerFireworkDuration is the flight of a firework of a flight duration
// from 0 to 2, as FireworkRocketEntity reads it.
func villagerFireworkDuration(flight int) time.Duration {
	return time.Duration(flight+1) * time.Second / 2
}

// newLocateHidingPlace ports LocateHidingPlace: a villager hides in a nearby
// bed's house, or its own.
func newLocateHidingPlace(radius int, speed float64, closeEnough int) *oneShot {
	return newOneShot([]memoryCondition{{memWalkTarget, memAbsent}, {memHome, memRegistered}, {memHidingPlace, memRegistered}, {memPath, memRegistered},
		{memLookTarget, memRegistered}, {memBreedTarget, memRegistered}, {memInteractionTarget, memRegistered}}, func(m *Mob, br *Brain, _ int64) bool {
		var pos cube.Pos
		found := false
		if near, _, ok := m.tx.NearestPOI(m.BlockPos(), world.Home, closeEnough+1, nil); ok && near.Vec3Centre().Sub(m.data.Pos).LenSqr() < float64(closeEnough*closeEnough) {
			pos, found = near, true
		} else {
			var homes []cube.Pos
			for p, poi := range m.tx.POIsInRange(m.BlockPos(), radius) {
				if poi.Kind == world.Home {
					homes = append(homes, p)
				}
			}
			if len(homes) > 0 {
				pos, found = homes[m.Random().NextInt(int32(len(homes)))], true
			} else if home, ok := villagerPoi(m, memHome); ok {
				pos, found = home, true
			}
		}
		if !found {
			return true
		}
		for _, mem := range []memoryType{memPath, memLookTarget, memBreedTarget, memInteractionTarget} {
			br.erase(mem)
		}
		br.set(memHidingPlace, globalPos{dim: m.tx.World().Dimension(), pos: pos})
		if pos.Vec3Centre().Sub(m.data.Pos).LenSqr() >= float64(closeEnough*closeEnough) {
			br.set(memWalkTarget, walkTarget{target: newBlockPosTracker(pos), speed: speed, closeEnough: closeEnough})
		}
		return true
	})
}

// newSetHiddenState ports SetHiddenState: a villager stays hidden for some
// seconds after it heard the bell, then returns to its schedule.
func newSetHiddenState(seconds, closeEnough int) *oneShot {
	stay := seconds * 20
	hidden := 0
	return newOneShot([]memoryCondition{{memHidingPlace, memPresent}, {memHeardBellTime, memPresent}}, func(m *Mob, br *Brain, time int64) bool {
		v, _ := br.get(memHeardBellTime)
		timedOut := v.(int64)+300 <= time
		if hidden <= stay && !timedOut {
			if p, _ := br.get(memHidingPlace); distSqrPos(p.(globalPos).pos, m.BlockPos()) < float64(closeEnough*closeEnough) {
				hidden++
			}
			return true
		}
		br.erase(memHeardBellTime)
		br.erase(memHidingPlace)
		villagerOf(m).updateActivityFromSchedule(m)
		hidden = 0
		return true
	})
}

// newInteractWithMem ports InteractWith with filters on both villagers and
// the memory it remembers the target in.
func newInteractWithMem(isType func(world.Entity) bool, r int, self func(*Mob) bool, target func(world.Entity) bool, mem memoryType, speed float64, stop int) *oneShot {
	valid := func(e world.Entity) bool { return isType(e) && target(e) }
	return newOneShot([]memoryCondition{{mem, memRegistered}, {memLookTarget, memRegistered}, {memWalkTarget, memAbsent}, {memNearestVisibleLivingEntities, memPresent}}, func(m *Mob, br *Brain, _ int64) bool {
		v := visibleLiving(m)
		if !self(m) || v.findClosest(m, valid) == nil {
			return false
		}
		if e := v.findClosest(m, func(e world.Entity) bool { return m.DistanceToSqr(e) <= float64(r*r) && valid(e) }); e != nil {
			br.setEntity(mem, e)
			br.set(memLookTarget, newEntityTracker(e, true))
			br.set(memWalkTarget, walkTarget{target: newEntityTracker(e, false), speed: speed, closeEnough: stop})
		}
		return true
	})
}

// villagerHostilesSensor ports VillagerHostilesSensor.
type villagerHostilesSensor struct{}

func (villagerHostilesSensor) scanRate() int          { return 0 }
func (villagerHostilesSensor) requires() []memoryType { return []memoryType{memNearestHostile} }
func (villagerHostilesSensor) doTick(m *Mob, b *Brain) {
	v := visibleLiving(m)
	if v == nil {
		b.erase(memNearestHostile)
		return
	}
	b.setEntity(memNearestHostile, v.findClosest(m, func(e world.Entity) bool {
		d, ok := villagerHostileDistances[e.H().Type().EncodeEntity()]
		return ok && m.DistanceToSqr(e) <= d*d
	}))
}

// villagerBabiesSensor ports VillagerBabiesSensor.
type villagerBabiesSensor struct{}

func (villagerBabiesSensor) scanRate() int          { return 0 }
func (villagerBabiesSensor) requires() []memoryType { return []memoryType{memVisibleVillagerBabies} }
func (villagerBabiesSensor) doTick(m *Mob, b *Brain) {
	v := visibleLiving(m)
	if v == nil {
		b.erase(memVisibleVillagerBabies)
		return
	}
	var babies []*world.EntityHandle
	for _, e := range v.findAll(m, func(e world.Entity) bool { return isVillagerMob(e) && mobBaby(e) }) {
		babies = append(babies, e.H())
	}
	b.set(memVisibleVillagerBabies, babies)
}

// villagerSecondaryPoiSensor ports SecondaryPoiSensor: the farmland around a
// farmer.
type villagerSecondaryPoiSensor struct{ b *VillagerBehaviour }

func (villagerSecondaryPoiSensor) scanRate() int          { return 40 }
func (villagerSecondaryPoiSensor) requires() []memoryType { return []memoryType{memSecondaryJobSite} }
func (s villagerSecondaryPoiSensor) doTick(m *Mob, b *Brain) {
	var sites []globalPos
	if s.b.profession == "minecraft:farmer" {
		dim, centre := m.tx.World().Dimension(), m.BlockPos()
		for x := -4; x <= 4; x++ {
			for y := -2; y <= 2; y++ {
				for z := -4; z <= 4; z++ {
					p := centre.Add(cube.Pos{x, y, z})
					if _, ok := m.tx.Block(p).(block.Farmland); ok {
						sites = append(sites, globalPos{dim: dim, pos: p})
					}
				}
			}
		}
	}
	if len(sites) > 0 {
		b.set(memSecondaryJobSite, sites)
	} else {
		b.erase(memSecondaryJobSite)
	}
}

// villagerGolemSensor ports GolemSensor: a villager that sees an iron golem
// wants no other for thirty seconds.
type villagerGolemSensor struct{}

func (villagerGolemSensor) scanRate() int { return 200 }
func (villagerGolemSensor) requires() []memoryType {
	return []memoryType{memNearestLivingEntities, memGolemDetectedRecently}
}
func (villagerGolemSensor) doTick(m *Mob, b *Brain) {
	for _, e := range nearestLiving(m) {
		if e.H().Type() == IronGolemType {
			b.setWithExpiry(memGolemDetectedRecently, true, 599)
			return
		}
	}
}

// villagerNearestBedSensor ports NearestBedSensor: the nearest bed a baby
// villager can reach.
type villagerNearestBedSensor struct {
	cache      map[cube.Pos]int64
	tried      int
	lastUpdate int64
}

func (*villagerNearestBedSensor) scanRate() int          { return 20 }
func (*villagerNearestBedSensor) requires() []memoryType { return []memoryType{memNearestBed} }
func (s *villagerNearestBedSensor) doTick(m *Mob, b *Brain) {
	if !mobBaby(m.Self) {
		return
	}
	if s.cache == nil {
		s.cache = map[cube.Pos]int64{}
	}
	s.tried = 0
	s.lastUpdate = m.tx.CurrentTick() + int64(m.tx.RegionRandom().NextInt(20))
	var targets []cube.Pos
	for pos, poi := range m.tx.POIsInRange(m.BlockPos(), 48) {
		if poi.Kind != world.Home {
			continue
		}
		if _, cached := s.cache[pos]; cached {
			continue
		}
		if s.tried++; s.tried >= 5 {
			break
		}
		s.cache[pos] = s.lastUpdate + 40
		targets = append(targets, pos)
	}
	if path := m.AI.nav.CreatePathToAny(m, targets, 1); path != nil && path.CanReach() {
		if _, ok := m.tx.POI(path.Target()); ok {
			b.set(memNearestBed, path.Target())
		}
	} else if s.tried < 5 {
		for pos, until := range s.cache {
			if until < s.lastUpdate {
				delete(s.cache, pos)
			}
		}
	}
}

// gossip ports Villager.gossip: villagers that talk share gossip at most once
// a minute, and may agree to summon an iron golem.
func (b *VillagerBehaviour) gossip(m *Mob, target *Villager, time int64) {
	tb := target.behaviour()
	if (time >= b.lastGossip && time < b.lastGossip+1200) || (time >= tb.lastGossip && time < tb.lastGossip+1200) {
		return
	}
	b.transferGossip(m, tb, 10)
	b.lastGossip, tb.lastGossip = time, time
	m.Self.(*Villager).spawnGolemIfNeeded(m.tx, time, 5)
}

// gossipDecayPerTransfer is GossipType.decayPerTransfer.
var gossipDecayPerTransfer = map[string]int{"major_negative": 10, "minor_negative": 20, "minor_positive": 5, "major_positive": 20, "trading": 20}

// gossipWeight is GossipType.weight.
var gossipWeight = map[string]int{"major_negative": -5, "minor_negative": -1, "minor_positive": 1, "major_positive": 5, "trading": 1}

// transferGossip ports GossipContainer.transferFrom: up to count gossips of
// the source, picked by weighted value, are learned decayed.
func (b *VillagerBehaviour) transferGossip(m *Mob, source *VillagerBehaviour, count int) {
	type entry struct {
		target, kind string
		value        int
	}
	var entries []entry
	for target, kinds := range source.gossips {
		for kind, value := range kinds {
			entries = append(entries, entry{target, kind, value})
		}
	}
	if len(entries) == 0 {
		return
	}
	ranges := make([]int, len(entries))
	end := 0
	for i, e := range entries {
		w := e.value * gossipWeight[e.kind]
		if w < 0 {
			w = -w
		}
		end += w
		ranges[i] = end - 1
	}
	if end <= 0 {
		return
	}
	picked := map[int]bool{}
	for range count {
		choice := int(m.Random().NextInt(int32(end)))
		i, _ := slices.BinarySearch(ranges, choice)
		picked[i] = true
	}
	for i := range picked {
		e := entries[i]
		value := e.value - gossipDecayPerTransfer[e.kind]
		if value < 2 {
			continue
		}
		if b.gossips[e.target] == nil {
			b.gossips[e.target] = map[string]int{}
		}
		b.gossips[e.target][e.kind] = max(b.gossips[e.target][e.kind], value)
	}
}

// releasePoi ports Villager.releasePoi.
func (b *VillagerBehaviour) releasePoi(m *Mob, mem memoryType) {
	pos, ok := villagerPoi(m, mem)
	if !ok {
		return
	}
	poi, exists := m.tx.POI(pos)
	if !exists {
		return
	}
	var match bool
	switch mem {
	case memHome:
		match = poi.Kind == world.Home
	case memJobSite:
		match = villagerHeldJobSite(b.profession, poi)
	case memPotentialJobSite:
		match = villagerJobSiteProfession(poi) != ""
	case memMeetingPoint:
		match = poi.Kind == world.Meeting
	}
	if match {
		m.tx.ReleasePOI(pos, m.H().UUID())
	}
}

// refreshBrain ports Villager.refreshBrain: the brain is rebuilt for the
// profession and age of the villager, keeping what it remembers that Java
// saves.
func (b *VillagerBehaviour) refreshBrain(m *Mob) {
	old := b.br
	old.stopAll(m)
	b.br = newVillagerBrain(b)
	for _, mem := range []memoryType{memHome, memJobSite, memPotentialJobSite, memMeetingPoint, memLastSlept, memLastWoken, memLastWorkedAtPoi,
		memGolemDetectedRecently, memHurtBy, memHurtByEntity} {
		if old.memories[mem] != nil {
			b.br.memories[mem] = old.memories[mem]
		}
	}
	b.br.start(m)
	b.br.lastScheduleUpdate = -9999
	b.updateActivityFromSchedule(m)
}

// startSleeping ports LivingEntity.startSleeping for a villager: it lies in
// the bed, which becomes occupied.
func (b *VillagerBehaviour) startSleeping(m *Mob, pos cube.Pos) bool {
	bed, ok := m.tx.Block(pos).(block.Bed)
	if !ok {
		return false
	}
	bed.Occupied = true
	m.tx.SetBlock(pos, bed, nil)
	m.data.Pos = mgl64.Vec3{float64(pos[0]) + .5, float64(pos[1]) + .6875, float64(pos[2]) + .5}
	m.data.Vel = mgl64.Vec3{}
	m.AI.nav.Stop()
	b.sleeping = &pos
	m.updateState()
	return true
}

// stopSleeping ports LivingEntity.stopSleeping for a villager: it gets up
// beside the bed, which becomes free, and remembers when it woke.
func (b *VillagerBehaviour) stopSleeping(m *Mob) {
	pos := *b.sleeping
	b.sleeping = nil
	if bed, ok := m.tx.Block(pos).(block.Bed); ok {
		bed.Occupied = false
		m.tx.SetBlock(pos, bed, nil)
		standUp := pos.Vec3Middle().Add(mgl64.Vec3{0, .5625})
		for _, face := range cube.HorizontalFaces() {
			p := pos.Side(face)
			if standable(m.tx, p.Side(cube.FaceDown)) && isPassable(m.tx, p) && isPassable(m.tx, p.Side(cube.FaceUp)) {
				standUp = p.Vec3Middle()
				break
			}
		}
		m.data.Pos = standUp
	}
	b.br.set(memLastWoken, m.tx.CurrentTick())
	m.updateState()
}

// wantsToPickUp ports Villager.wantsToPickUp: food, the items villagers pick
// up and those its profession asks for, if there is room for them.
func (b *VillagerBehaviour) wantsToPickUp(_ *Mob, s item.Stack) bool {
	name, _ := s.Item().EncodeItem()
	if !inItemTag(s, "villager_picks_up") && villagerFoodPoints(item.NewStack(s.Item(), 1)) == 0 && !slices.Contains(villagerRequestedItems(b.profession), name) {
		return false
	}
	for _, slot := range b.inventory.Slots() {
		if slot.Empty() || slot.Comparable(s) && slot.Count() < slot.MaxCount() {
			return true
		}
	}
	return false
}

// pickUpItems ports Mob.aiStep picking up loot for a villager, with
// InventoryCarrier.pickUpItem.
func (b *VillagerBehaviour) pickUpItems(m *Mob) {
	if mobGriefingDisabled(m.tx) || !m.Alive() {
		return
	}
	box := m.BBox().GrowVec3(mgl64.Vec3{1, 0, 1})
	for e := range m.tx.EntitiesWithin(box.Grow(1)) {
		stack, ok := pickableItem(e)
		if !ok || !e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) || !b.wantsToPickUp(m, stack) {
			continue
		}
		left, _ := b.inventory.AddItem(stack)
		taken := stack.Count() - left
		if taken <= 0 {
			continue
		}
		viewers := m.tx.Viewers(e.Position())
		for _, v := range viewers {
			v.ViewEntityAction(e, PickedUpAction{Collector: m.Self.(Collector)})
		}
		m.tx.ReleaseViewers(viewers)
		takeItemEntity(m, e, taken)
	}
}

// setHeld sets the item the villager shows in its hand.
func (b *VillagerBehaviour) setHeld(m *Mob, s item.Stack) {
	b.held = s
	viewers := m.tx.Viewers(m.data.Pos)
	for _, v := range viewers {
		v.ViewEntityItems(m.Self)
	}
	m.tx.ReleaseViewers(viewers)
}

// throwGift ports Villager.dropFromGiftLootTable for GiveGiftToHero.
func (b *VillagerBehaviour) throwGift(m *Mob, target world.Entity) {
	table := "minecraft:gameplay/hero_of_the_village/unemployed_gift"
	if b.age < 0 {
		table = "minecraft:gameplay/hero_of_the_village/baby_gift"
	} else if g, ok := villagerGifts[b.profession]; ok {
		table = "minecraft:gameplay/hero_of_the_village/" + g
	}
	stacks, err := villagerGenerateLoot(table, m)
	if err != nil {
		return
	}
	for _, s := range stacks {
		throwItemTowards(m, s, target.Position(), mgl64.Vec3{float64(float32(.3)), float64(float32(.3)), float64(float32(.3))}, float64(float32(.3)))
	}
}

// villagerGenerateLoot rolls a loot table for a villager.
func villagerGenerateLoot(table string, m *Mob) ([]item.Stack, error) {
	return loot.Generate(table, m.tx.RegionRandom().NextLong(), loot.Context{Entity: "minecraft:villager", Origin: m.data.Pos})
}
