package entity

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// This file ports PiglinAi, PiglinBruteAi and what AbstractPiglin, Piglin
// and PiglinBrute do around them: piglins admire and barter for gold, hunt
// hoglins, guard gold from players, celebrate and run from zombified kin.

func (s *piglinState) mobAI() *MobAI { return &s.ai }
func (s *piglinState) brain() *Brain { return s.br }

// Tick runs the MobAI of the piglin with its brain.
func (s *piglinState) Tick(e *Ent, tx *world.Tx) *Movement {
	ai := &s.ai
	ai.MovementSpeed, ai.EyeHeight = s.movementSpeed(), piglinEyeHeight
	if s.baby {
		ai.EyeHeight = .78
	}
	if s.brute {
		// PiglinBrute.createAttributes.
		ai.FollowRange = 12
	}
	return ai.Tick(e, s.self(e), func(m *Mob) {
		// AbstractPiglin: piglins open doors and keep out of fire.
		m.AI.nav.SetCanOpenDoors(true)
		m.AI.SetPathfindingMalus(PathFireInNeighbour, 16)
		m.AI.SetPathfindingMalus(PathFire, -1)
	})
}

// customServerAiStep ports Piglin.customServerAiStep, PiglinBrute's and
// AbstractPiglin's: the brain ticks, and a piglin out of the nether turns
// into a zombified piglin after fifteen seconds.
func (s *piglinState) customServerAiStep(m *Mob) {
	s.br.tick(m)
	if s.brute {
		piglinBruteUpdateActivity(m, s)
		if m.Random().NextFloat() < .0125 {
			piglinBrutePlayActivitySound(m, s)
		}
	} else {
		piglinUpdateActivity(m, s)
	}
	if !s.immune && !m.AI.NoAI && m.tx.World().Dimension() == world.Overworld {
		s.conversion++
	} else {
		s.conversion = 0
	}
	if s.conversion > 300 {
		if m.tx.World().Difficulty() != world.DifficultyPeaceful {
			if s.brute {
				s.makeSound(m, "mob.piglin_brute.converted_to_zombified")
			} else {
				s.makeSound(m, "mob.piglin.converted_to_zombified")
			}
		}
		s.finishConversion(m)
	}
}

// afterAiStep ports the item pickup of Mob.aiStep: a piglin picks up the
// items it wants lying within a block of it.
func (s *piglinState) afterAiStep(m *Mob) {
	if s.dead || mobGriefingDisabled(m.tx) {
		return
	}
	box := m.BBox().GrowVec3(mgl64.Vec3{1, 0, 1})
	for e := range m.tx.EntitiesOfTypesWithin(box.Grow(1), itemEntityTypes) {
		stack, ok := pickableItem(e)
		if !ok || !e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) || !s.wantsToPickUp(m, stack) {
			continue
		}
		viewers := m.tx.Viewers(e.Position())
		for _, v := range viewers {
			if c, ok := m.Self.(Collector); ok {
				v.ViewEntityAction(e, PickedUpAction{Collector: c})
			}
		}
		m.tx.ReleaseViewers(viewers)
		if s.brute {
			// PiglinBrute only wants golden axes, which it equips.
			s.equipItemIfPossible(m, stack)
			takeItemEntity(m, e, 1)
			continue
		}
		piglinPickUpItem(m, s, e, stack)
	}
}

// Collect is never used by item entities: a piglin picks up items itself.
func (p *Piglin) Collect(item.Stack) (int, bool) { return 0, false }

// wantsToPickUp ports Piglin.wantsToPickUp and PiglinBrute's.
func (s *piglinState) wantsToPickUp(m *Mob, stack item.Stack) bool {
	if s.brute {
		if a, ok := stack.Item().(item.Axe); !ok || a.Tier != item.ToolTierGold {
			return false
		}
		return s.canReplaceCurrentItem(stack, s.main)
	}
	return piglinWantsToPickup(m, s, stack)
}

// walkTargetValue ports Monster.getWalkTargetValue.
func (s *piglinState) walkTargetValue(m *Mob, pos cube.Pos) float64 { return monsterWalkTarget(m, pos) }

// doHurtTarget ports Mob.doHurtTarget for piglins: their attack damage, 5
// for piglins and 7 for brutes, and that of their weapon.
func (s *piglinState) doHurtTarget(m *Mob, target world.Entity) bool {
	l, ok := target.(Living)
	if !ok {
		return false
	}
	damage := 5.0
	if s.brute {
		damage = 7
	}
	if w, ok := s.main.Item().(item.Weapon); ok {
		damage += w.AttackDamage() - 1
	}
	_, hurt := l.Hurt(damage, AttackDamageSource{Attacker: m.Self})
	return hurt
}

// nonMeleeWeapon ports Piglin.canUseNonMeleeWeapon: a piglin with a crossbow
// or a spear does not strike with it.
func (s *piglinState) nonMeleeWeapon() bool {
	switch s.main.Item().(type) {
	case item.Crossbow, item.Spear:
		return !s.brute
	}
	return false
}

// newPiglinBrain ports PiglinAi and the brain provider of Piglin.
func newPiglinBrain(s *piglinState) *Brain {
	br := newBrain(nearestLivingEntitySensor{}, playerSensor{}, nearestItemSensor{wants: func(m *Mob, st item.Stack) bool {
		return !mobGriefingDisabled(m.tx) && piglinWantsToPickup(m, s, st)
	}}, hurtBySensor{}, piglinSpecificSensor{})
	br.register(memUniversalAnger)
	br.register(memAteRecently)
	br.register(memSpearFleeingTime)
	br.register(memSpearFleeingPosition)
	br.register(memSpearChargePosition)
	br.register(memSpearEngageTime)
	adult := func(*Mob) bool { return !s.baby }
	startAttacking := func() behaviorControl {
		return newStartAttacking(adult, func(m *Mob) world.Entity { return piglinNearestValidAttackTarget(m) })
	}
	lookAtLovedItem := func(d float64) behaviorControl { return newSetEntityLookTarget(piglinPlayerHoldingLovedItem, d) }
	avoidRepellent := func() behaviorControl {
		return newSetWalkTargetAwayFrom(memNearestRepellent, 1, 8, false, func(m *Mob, v any) mgl64.Vec3 { return v.(cube.Pos).Vec3Middle() })
	}
	lookBehaviors := func() []weightedBehavior {
		return []weightedBehavior{
			{newSetEntityLookTarget(isPlayerType, 8), 1},
			{newSetEntityLookTarget(isPiglinType, 8), 1},
			{newSetEntityLookTarget(nil, 8), 1},
		}
	}
	idleLook := func() behaviorControl {
		return newRunOne(nil, append(lookBehaviors(), weightedBehavior{newDoNothing(30, 60), 1})...)
	}
	idleMovement := func() behaviorControl {
		return newRunOne(nil,
			weightedBehavior{newRandomStroll(.6, true), 2},
			weightedBehavior{newInteractWith(isPiglinType, 8, .6, 2), 2},
			weightedBehavior{newTriggerIf(func(m *Mob) bool { return !m.Brain().has(memNearestPlayerHoldingWantedItem) }, newSetWalkTargetFromLookTarget(nil, constSpeed(.6), 3)), 2},
			weightedBehavior{newDoNothing(30, 60), 1},
		)
	}
	br.addActivity(activityCore, 0,
		newLookAtTargetSink(45, 90),
		newMoveToTargetSink(150, 250, nil),
		newInteractWithDoor(),
		newCopyMemoryWithExpiry(adultNot(s), memNearestVisibleNemesis, memAvoidTarget, uniformInt{100, 140}),
		newCopyMemoryWithExpiry(piglinNearZombified, memNearestVisibleZombified, memAvoidTarget, uniformInt{100, 140}),
		newStopHoldingItemIfNoLongerAdmiring(s),
		newStartAdmiringItemIfSeen(119),
		newStartCelebratingIfTargetDead(300, piglinWantsToDance),
		newStopBeingAngryIfTargetDead(),
	)
	br.addActivity(activityIdle, 10,
		lookAtLovedItem(14),
		startAttacking(),
		newTriggerIf(func(*Mob) bool { return !s.cannotHunt }, newStartHuntingHoglin(s)),
		avoidRepellent(),
		piglinBabySometimesRideBabyHoglin(s),
		idleLook(),
		idleMovement(),
		newSetLookAndInteract(isPlayerType, 4),
	)
	br.addActivityWith(activityAdmireItem, prioritiesFrom(10,
		newGoToWantedItem(func(*Mob) bool { return !piglinLovedItem(s.off) }, 1, true, 9, func() bool { return true }),
		newStopAdmiringIfItemTooFarAway(s, 9),
		newStopAdmiringIfTiredOfTryingToReachItem(s, 200, 200),
	), []memoryCondition{{memAdmiringItem, memPresent}}, []memoryType{memAdmiringItem})
	br.addActivityWith(activityFight, prioritiesFrom(10,
		newStopAttackingIfTargetInvalid(func(m *Mob, target world.Entity) bool {
			valid := piglinNearestValidAttackTarget(m)
			return valid == nil || valid.H() != target.H()
		}, nil, true),
		newTriggerIf(func(*Mob) bool { _, ok := s.main.Item().(item.Crossbow); return ok }, newBackUpIfTooClose(5, .75)),
		newSetWalkTargetFromAttackTargetIfTargetOutOfReach(constSpeed(1)),
		newSpearApproach(1, 10),
		newSpearAttack(1, 1, 2),
		newSpearRetreat(1),
		newMeleeAttack(20),
		newCrossbowAttack(s),
		newRememberIfHoglinWasKilled(),
		newEraseMemoryIf(piglinNearZombified, memAttackTarget),
	), []memoryCondition{{memAttackTarget, memPresent}}, []memoryType{memAttackTarget})
	br.addActivityWith(activityCelebrate, prioritiesFrom(10,
		avoidRepellent(),
		lookAtLovedItem(14),
		startAttacking(),
		newTriggerIf(func(*Mob) bool { return !s.dancing }, newGoToTargetLocation(memCelebrateLocation, 2, 1)),
		newTriggerIf(func(*Mob) bool { return s.dancing }, newGoToTargetLocation(memCelebrateLocation, 4, .6)),
		newRunOne(nil,
			weightedBehavior{newSetEntityLookTarget(isPiglinType, 8), 1},
			weightedBehavior{newRandomStrollWithinRange(.6, 2, 1), 1},
			weightedBehavior{newDoNothing(10, 20), 1},
		),
	), []memoryCondition{{memCelebrateLocation, memPresent}}, []memoryType{memCelebrateLocation})
	br.addActivityWith(activityRide, prioritiesFrom(10,
		newMount(.8),
		lookAtLovedItem(8),
		newTriggerIf(func(m *Mob) bool { return m.data.Riding != nil },
			newTriggerOneShuffled(append(lookBehaviors(), weightedBehavior{newOneShot(nil, func(*Mob, *Brain, int64) bool { return true }), 1})...)),
		newDismountOrSkipMounting(8, piglinWantsToStopRiding),
	), []memoryCondition{{memRideTarget, memPresent}}, nil)
	br.addActivityWith(activityAvoid, prioritiesFrom(10,
		newSetWalkTargetAwayFrom(memAvoidTarget, 1, 12, true, entityMemoryPos),
		idleLook(),
		idleMovement(),
		newEraseMemoryIf(piglinWantsToStopFleeing, memAvoidTarget),
	), []memoryCondition{{memAvoidTarget, memPresent}}, []memoryType{memAvoidTarget})
	return br
}

// adultNot returns a predicate that holds for baby piglins, as Piglin.isBaby.
func adultNot(s *piglinState) func(*Mob) bool { return func(*Mob) bool { return s.baby } }

// isPiglinType reports if an entity is a piglin, not a brute.
func isPiglinType(e world.Entity) bool { return e.H().Type() == PiglinType{} }

// piglinUpdateActivity ports PiglinAi.updateActivity.
func piglinUpdateActivity(m *Mob, s *piglinState) {
	br := s.br
	old, _ := br.activeNonCore()
	br.setActiveToFirstValid(activityAdmireItem, activityFight, activityAvoid, activityCelebrate, activityRide, activityIdle)
	if now, _ := br.activeNonCore(); now != old {
		s.makeSound(m, piglinActivitySound(m, s))
	}
	m.SetAggressive(br.has(memAttackTarget))
	if !br.has(memRideTarget) && piglinBabyRidingBaby(m, s) {
		mobStopRiding(m.tx, m.Self)
	}
	if !br.has(memCelebrateLocation) {
		br.erase(memDancing)
	}
	if dancing := br.has(memDancing); dancing != s.dancing {
		s.dancing = dancing
		m.updateState()
	}
}

// piglinBabySometimesRideBabyHoglin ports
// PiglinAi.babySometimesRideBabyHoglin: every 10 to 40 seconds a baby
// piglin sets out to ride the nearest baby hoglin it sees, for 10 to 30
// seconds.
func piglinBabySometimesRideBabyHoglin(s *piglinState) *oneShot {
	ticker := &lookTicker{interval: uniformInt{200, 800}}
	return newCopyMemoryWithExpiry(func(m *Mob) bool { return s.baby && ticker.tickDownAndCheck(m) },
		memNearestVisibleBabyHoglin, memRideTarget, uniformInt{200, 600})
}

// piglinBabyRidingBaby ports PiglinAi.isBabyRidingBaby.
func piglinBabyRidingBaby(m *Mob, s *piglinState) bool {
	if !s.baby || m.data.Riding == nil {
		return false
	}
	vehicle, ok := m.data.Riding.Entity(m.tx)
	if !ok || !mobBaby(vehicle) {
		return false
	}
	t := vehicle.H().Type()
	return t == PiglinType{} || t == HoglinType
}

// piglinWantsToStopRiding ports PiglinAi.wantsToStopRiding: a piglin gets
// off a mob that grew up or died, when either of them was hurt, or off a
// piglin that rides nothing itself.
func piglinWantsToStopRiding(m *Mob, vehicle world.Entity) bool {
	if _, ok := mobOf(vehicle); !ok {
		return false
	}
	hurt := func(e world.Entity) bool {
		bm, ok := brainMobOf(e)
		return ok && bm.Brain().has(memHurtBy)
	}
	if !mobBaby(vehicle) || !entityAlive(vehicle) || m.Brain().has(memHurtBy) || hurt(vehicle) {
		return true
	}
	r, ok := vehicle.(Rider)
	return vehicle.H().Type() == (PiglinType{}) && (!ok || r.Riding() == nil)
}

// piglinActivitySound ports PiglinAi.getSoundForActivity.
func piglinActivitySound(m *Mob, s *piglinState) string {
	act, _ := s.br.activeNonCore()
	br := s.br
	switch {
	case act == activityFight:
		return "mob.piglin.angry"
	case !s.immune && m.tx.World().Dimension() == world.Overworld:
		return "mob.piglin.retreat"
	case act == activityAvoid && piglinNearAvoidTarget(m):
		return "mob.piglin.retreat"
	case act == activityAdmireItem:
		return "mob.piglin.admiring_item"
	case act == activityCelebrate:
		return "mob.piglin.celebrate"
	case br.has(memNearestPlayerHoldingWantedItem):
		return "mob.piglin.jealous"
	case br.has(memNearestRepellent):
		return "mob.piglin.retreat"
	}
	return "mob.piglin.ambient"
}

// piglinNearAvoidTarget ports PiglinAi.isNearAvoidTarget.
func piglinNearAvoidTarget(m *Mob) bool {
	e := m.Brain().entity(m, memAvoidTarget)
	return e != nil && m.DistanceToSqr(e) < 144
}

// piglinNearZombified ports PiglinAi.isNearZombified: a zombified piglin or
// zoglin it sees within six blocks.
func piglinNearZombified(m *Mob) bool {
	e := m.Brain().entity(m, memNearestVisibleZombified)
	return e != nil && m.DistanceToSqr(e) < 36
}

// piglinNearestValidAttackTarget ports PiglinAi.findNearestValidAttackTarget:
// what the piglin is angry at, any player in universal anger, its nemesis,
// or a player not wearing gold.
func piglinNearestValidAttackTarget(m *Mob) world.Entity {
	br := m.Brain()
	if piglinNearZombified(m) {
		return nil
	}
	if v, ok := br.get(memAngryAt); ok {
		if e, ok := m.tx.EntityByUUID(v.(uuid.UUID)); ok && sensorAttackableIgnoringSight(m, e) {
			return e
		}
	}
	if br.has(memUniversalAnger) {
		if p := br.entity(m, memNearestVisibleAttackablePlayer); p != nil {
			return p
		}
	}
	if n := br.entity(m, memNearestVisibleNemesis); n != nil {
		return n
	}
	if p := br.entity(m, memNearestTargetablePlayerNotWearingGold); p != nil && sensorAttackable(m, p) {
		return p
	}
	return nil
}

// sensorAttackableIgnoringSight ports Sensor.isEntityAttackableIgnoringLineOfSight.
func sensorAttackableIgnoringSight(m *Mob, e world.Entity) bool {
	c := forCombat()
	c.rangeLimit, c.ignoreSight = m.AI.FollowRange, true
	if v, ok := m.Brain().get(memAttackTarget); ok && v == e.H() {
		c.ignoreInvis = true
	}
	return c.test(m, e)
}

// piglinWantsToDance ports PiglinAi.wantsToDance: a piglin that killed a
// hoglin dances one time in ten.
func piglinWantsToDance(m *Mob, killed world.EntityType) bool {
	if killed != HoglinType {
		return false
	}
	// RandomSource.createThreadLocalInstance seeded with the game time.
	r := vanilla.NewLegacyRandom(m.tx.CurrentTick())
	return r.NextFloat() < .1
}

// piglinLovedItem ports PiglinAi.isLovedItem: an item in #piglin_loved.
func piglinLovedItem(s item.Stack) bool {
	return !s.Empty() && (inItemTag(s, "piglin_loved") || blockItemInTag(s, "piglin_loved"))
}

// piglinBarterCurrency ports PiglinAi.isBarterCurrency: a gold ingot.
func piglinBarterCurrency(s item.Stack) bool {
	_, ok := s.Item().(item.GoldIngot)
	return ok && !s.Empty()
}

// piglinFood ports PiglinAi.isFood: an item in #piglin_food.
func piglinFood(s item.Stack) bool { return inItemTag(s, "piglin_food") }

// piglinPlayerHoldingLovedItem ports PiglinAi.isPlayerHoldingLovedItem.
func piglinPlayerHoldingLovedItem(e world.Entity) bool {
	if !isPlayerEntity(e) {
		return false
	}
	u, ok := e.(item.User)
	if !ok {
		return false
	}
	main, off := u.HeldItems()
	return piglinLovedItem(main) || piglinLovedItem(off)
}

// piglinWearingSafeArmour ports PiglinAi.isWearingSafeArmor: an entity in a
// piece of #piglin_safe_armor.
func piglinWearingSafeArmour(e world.Entity) bool {
	a, ok := e.(interface {
		EquipmentArmour() (item.Stack, item.Stack, item.Stack, item.Stack)
	})
	if ok {
		h, c, l, b := a.EquipmentArmour()
		for _, s := range []item.Stack{h, c, l, b} {
			if inItemTag(s, "piglin_safe_armor") {
				return true
			}
		}
		return false
	}
	if p, ok := e.(interface{ Armour() *inventory.Armour }); ok {
		for _, s := range p.Armour().Slots() {
			if inItemTag(s, "piglin_safe_armor") {
				return true
			}
		}
	}
	return false
}

// piglinWantsToPickup ports PiglinAi.wantsToPickup.
func piglinWantsToPickup(m *Mob, s *piglinState, stack item.Stack) bool {
	br := m.Brain()
	switch {
	case s.baby && inItemTag(stack, "ignored_by_piglin_babies"):
		return false
	case inItemTag(stack, "piglin_repellents") || blockItemInTag(stack, "piglin_repellents"):
		return false
	case br.has(memAdmiringDisabled) && br.has(memAttackTarget):
		return false
	case piglinBarterCurrency(stack):
		return !piglinLovedItem(s.off)
	}
	space := s.canAddToInventory(stack)
	if _, nugget := stack.Item().(item.GoldNugget); nugget {
		return space
	}
	if piglinFood(stack) {
		return !br.has(memAteRecently) && space
	}
	if !piglinLovedItem(stack) {
		return s.canReplaceCurrentItem(stack, s.slotFor(stack))
	}
	return !piglinLovedItem(s.off) && space
}

// piglinPickUpItem ports PiglinAi.pickUpItem: a piglin admires the gold it
// picks up, eats food, and equips or stows the rest.
func piglinPickUpItem(m *Mob, s *piglinState, e world.Entity, stack item.Stack) {
	piglinStopWalking(m)
	var taken item.Stack
	if _, nugget := stack.Item().(item.GoldNugget); nugget {
		taken = stack
		takeItemEntity(m, e, stack.Count())
	} else {
		taken = stack.Grow(1 - stack.Count())
		takeItemEntity(m, e, 1)
	}
	switch {
	case piglinLovedItem(taken):
		m.Brain().erase(memTimeTryingToReachAdmireItem)
		piglinHoldInOffhand(m, s, taken)
		piglinAdmireGoldItem(m)
	case piglinFood(taken) && !m.Brain().has(memAteRecently):
		m.Brain().setWithExpiry(memAteRecently, true, 200)
	default:
		if s.equipItemIfPossible(m, taken).Empty() {
			piglinPutInInventory(m, s, taken)
		}
	}
}

// piglinHoldInOffhand ports PiglinAi.holdInOffhand.
func piglinHoldInOffhand(m *Mob, s *piglinState, stack item.Stack) {
	if !s.off.Empty() {
		m.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: m.data.Pos}, s.off))
	}
	s.off, s.guaranteedOff = stack, true
	m.updateState()
}

// piglinAdmireGoldItem ports PiglinAi.admireGoldItem.
func piglinAdmireGoldItem(m *Mob) { m.Brain().setWithExpiry(memAdmiringItem, true, 119) }

// piglinStopWalking ports PiglinAi.stopWalking.
func piglinStopWalking(m *Mob) {
	m.Brain().erase(memWalkTarget)
	m.AI.nav.Stop()
}

// piglinCanAdmire ports PiglinAi.canAdmire.
func piglinCanAdmire(m *Mob, s *piglinState, held item.Stack) bool {
	br := m.Brain()
	return !br.has(memAdmiringDisabled) && !br.has(memAdmiringItem) && !s.baby && piglinBarterCurrency(held)
}

// piglinStopHoldingOffHandItem ports PiglinAi.stopHoldingOffHandItem: an
// adult barters away a gold ingot it admired and keeps other loved items, a
// baby takes up what it admired.
func piglinStopHoldingOffHandItem(m *Mob, s *piglinState, barter bool) {
	stack := s.off
	s.off, s.guaranteedOff = item.Stack{}, false
	m.updateState()
	if !s.baby {
		currency := piglinBarterCurrency(stack)
		switch {
		case barter && currency:
			piglinThrowItems(m, s, piglinBarterResponse(m))
		case !currency:
			if s.equipItemIfPossible(m, stack).Empty() {
				piglinPutInInventory(m, s, stack)
			}
		}
		return
	}
	if !s.equipItemIfPossible(m, stack).Empty() {
		return
	}
	if piglinLovedItem(s.main) {
		piglinPutInInventory(m, s, s.main)
	} else {
		piglinThrowItems(m, s, []item.Stack{s.main})
	}
	s.main, s.guaranteedMain = stack, true
	m.updateState()
}

// piglinCancelAdmiring ports PiglinAi.cancelAdmiring.
func piglinCancelAdmiring(m *Mob, s *piglinState) {
	if m.Brain().has(memAdmiringItem) && !s.off.Empty() {
		m.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: m.data.Pos}, s.off))
		s.off = item.Stack{}
		m.updateState()
	}
}

// piglinPutInInventory ports PiglinAi.putInInventory: what does not fit is
// thrown to a random spot nearby.
func piglinPutInInventory(m *Mob, s *piglinState, stack item.Stack) {
	if left := s.addToInventory(stack); !left.Empty() {
		piglinThrowItemsTowards(m, []item.Stack{left}, piglinRandomNearbyPos(m))
	}
}

// piglinThrowItems ports PiglinAi.throwItems: towards the player it sees, or
// a random spot nearby.
func piglinThrowItems(m *Mob, s *piglinState, stacks []item.Stack) {
	if p := m.Brain().entity(m, memNearestVisiblePlayer); p != nil {
		piglinThrowItemsTowards(m, stacks, p.Position())
		return
	}
	piglinThrowItemsTowards(m, stacks, piglinRandomNearbyPos(m))
}

// piglinThrowItemsTowards ports PiglinAi.throwItemsTowardPos.
func piglinThrowItemsTowards(m *Mob, stacks []item.Stack, target mgl64.Vec3) {
	empty := true
	for _, st := range stacks {
		empty = empty && st.Empty()
	}
	if empty {
		return
	}
	m.swing()
	for _, st := range stacks {
		if !st.Empty() {
			throwItemTowards(m, st, target.Add(mgl64.Vec3{0, 1, 0}), mgl64.Vec3{float64(float32(.3)), float64(float32(.3)), float64(float32(.3))}, float64(float32(.3)))
		}
	}
}

// piglinRandomNearbyPos ports PiglinAi.getRandomNearbyPos.
func piglinRandomNearbyPos(m *Mob) mgl64.Vec3 {
	if p, ok := LandRandomPos(m, 4, 2); ok {
		return p
	}
	return m.data.Pos
}

// piglinBarterResponse ports PiglinAi.getBarterResponseItems: the loot of
// the piglin bartering table.
func piglinBarterResponse(m *Mob) []item.Stack {
	stacks, err := loot.Generate("minecraft:gameplay/piglin_bartering", rand.Int64(), loot.Context{Entity: "minecraft:piglin", Origin: m.data.Pos})
	if err != nil {
		return nil
	}
	return stacks
}

// canAddToInventory ports SimpleContainer.canAddItem for the inventory of a
// piglin.
func (s *piglinState) canAddToInventory(stack item.Stack) bool {
	for _, slot := range s.inventory {
		if slot.Empty() || slot.Comparable(stack) && slot.Count() < slot.MaxCount() {
			return true
		}
	}
	return false
}

// addToInventory ports SimpleContainer.addItem, returning what did not fit.
func (s *piglinState) addToInventory(stack item.Stack) item.Stack {
	for i, slot := range s.inventory {
		if stack.Empty() {
			break
		}
		if !slot.Empty() && slot.Comparable(stack) && slot.Count() < slot.MaxCount() {
			merged, left := slot.AddStack(stack)
			s.inventory[i], stack = merged, left
		}
	}
	for i, slot := range s.inventory {
		if stack.Empty() {
			break
		}
		if slot.Empty() {
			s.inventory[i], stack = stack, item.Stack{}
		}
	}
	return stack
}

// slotFor returns what the piglin holds or wears where it would put a stack,
// as LivingEntity.getEquipmentSlotForItem.
func (s *piglinState) slotFor(stack item.Stack) item.Stack {
	if i, ok := armourSlot(stack); ok {
		return s.armor[i]
	}
	return s.main
}

// armourSlot returns the armour slot, head first, a stack is worn in.
func armourSlot(stack item.Stack) (int, bool) {
	switch stack.Item().(type) {
	case item.HelmetType:
		return 0, true
	case item.ChestplateType:
		return 1, true
	case item.LeggingsType:
		return 2, true
	case item.BootsType:
		return 3, true
	}
	return 0, false
}

// canReplaceCurrentItem ports Piglin.canReplaceCurrentItem and Mob's: loved
// items and preferred weapons go first, then better armour and weapons.
func (s *piglinState) canReplaceCurrentItem(stack, current item.Stack) bool {
	preferred := func(st item.Stack) bool {
		return !s.baby && !s.brute && inItemTag(st, "piglin_preferred_weapons")
	}
	wanted := func(st item.Stack) bool { return piglinLovedItem(st) || preferred(st) }
	if current.Empty() {
		return true
	}
	newWanted, currentWanted := wanted(stack), wanted(current)
	if newWanted && !currentWanted {
		return true
	}
	if !newWanted && currentWanted {
		return false
	}
	return mobCanReplaceItem(stack, current, preferred)
}

// mobCanReplaceItem ports Mob.canReplaceCurrentItem for an item in a slot that
// holds current: better armour and weapons, the preferred weapons first.
func mobCanReplaceItem(stack, current item.Stack, preferred func(item.Stack) bool) bool {
	if current.Empty() {
		return true
	}
	if _, armour := armourSlot(stack); armour {
		na, _ := stack.Item().(item.Armour)
		ca, _ := current.Item().(item.Armour)
		if na == nil || ca == nil {
			return false
		}
		if na.DefencePoints() != ca.DefencePoints() {
			return na.DefencePoints() > ca.DefencePoints()
		}
		if na.Toughness() != ca.Toughness() {
			return na.Toughness() > ca.Toughness()
		}
		return mobCanReplaceEqualItem(stack, current)
	}
	if preferred != nil {
		if preferred(current) && !preferred(stack) {
			return false
		}
		if !preferred(current) && preferred(stack) {
			return true
		}
	}
	nd, cd := stack.AttackDamage(), current.AttackDamage()
	if nd != cd {
		return nd > cd
	}
	return mobCanReplaceEqualItem(stack, current)
}

// mobCanReplaceEqualItem ports Mob.canReplaceEqualItem.
func mobCanReplaceEqualItem(stack, current item.Stack) bool {
	if n, c := len(stack.Enchantments()), len(current.Enchantments()); n != c {
		return n > c
	}
	damage := func(st item.Stack) int { return st.MaxDurability() - st.Durability() }
	if damage(stack) != damage(current) {
		return damage(stack) < damage(current)
	}
	return stack.CustomName() != "" && current.CustomName() == ""
}

// equipItemIfPossible ports Mob.equipItemIfPossible: the piglin equips the
// stack if it is better than what it holds or wears there, maybe dropping
// that, and returns it, or nothing.
func (s *piglinState) equipItemIfPossible(m *Mob, stack item.Stack) item.Stack {
	slot, armour := armourSlot(stack)
	var current item.Stack
	if armour {
		current = s.armor[slot]
	} else {
		current = s.main
	}
	replace := s.canReplaceCurrentItem(stack, current)
	if armour && !replace {
		armour, current = false, s.main
		replace = current.Empty()
	}
	if !replace {
		return item.Stack{}
	}
	guaranteed := s.guaranteedMain
	if armour {
		guaranteed = false
	}
	if !current.Empty() && max(float64(m.Random().NextFloat())-.1, 0) < map[bool]float64{true: 2, false: .085}[guaranteed] {
		m.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: m.data.Pos}, current))
	}
	toEquip := stack.Grow(1 - stack.Count())
	if armour {
		s.armor[slot] = toEquip
	} else {
		s.main, s.guaranteedMain = toEquip, true
	}
	m.updateState()
	return toEquip
}

// piglinWantsToStopFleeing ports PiglinAi.wantsToStopFleeing.
func piglinWantsToStopFleeing(m *Mob) bool {
	br := m.Brain()
	avoided := br.entity(m, memAvoidTarget)
	if avoided == nil {
		return true
	}
	if avoided.H().Type() == HoglinType {
		return !piglinHoglinsOutnumber(m)
	}
	if piglinZombified(avoided) {
		v, _ := br.get(memNearestVisibleZombified)
		return v != avoided.H()
	}
	return false
}

// piglinZombified ports PiglinAi.isZombified.
func piglinZombified(e world.Entity) bool {
	return e.H().Type() == (ZombifiedPiglinType{}) || e.H().Type() == ZoglinType
}

// piglinHoglinsOutnumber ports PiglinAi.hoglinsOutnumberPiglins.
func piglinHoglinsOutnumber(m *Mob) bool {
	piglins, _ := m.Brain().int(memVisibleAdultPiglinCount)
	hoglins, _ := m.Brain().int(memVisibleAdultHoglinCount)
	return hoglins > piglins+1
}

// piglinDontHuntForAWhile ports PiglinAi.dontKillAnyMoreHoglinsForAWhile.
func piglinDontHuntForAWhile(m *Mob) {
	m.Brain().setWithExpiry(memHuntedRecently, true, int64(600+m.Random().NextInt(1801)))
}

// piglinSetAvoidTarget ports PiglinAi.setAvoidTargetAndDontHuntForAWhile.
func piglinSetAvoidTarget(m *Mob, target world.Entity) {
	br := m.Brain()
	br.erase(memAngryAt)
	br.erase(memAttackTarget)
	br.erase(memWalkTarget)
	br.setWithExpiry(memAvoidTarget, target.H(), int64(100+m.Random().NextInt(301)))
	piglinDontHuntForAWhile(m)
}

// piglinsOf returns the piglins a memory holds.
func piglinsOf(m *Mob, mem memoryType) []*Mob {
	v, _ := m.Brain().get(mem)
	handles, _ := v.([]*world.EntityHandle)
	var out []*Mob
	for _, h := range handles {
		if e, ok := h.Entity(m.tx); ok {
			if pm, ok := brainMobOf(e); ok {
				out = append(out, pm)
			}
		}
	}
	return out
}

// piglinWasHurtBy ports PiglinAi.wasHurtBy.
func piglinWasHurtBy(m *Mob, s *piglinState, attacker world.Entity) {
	if attacker.H().Type() == (PiglinType{}) {
		return
	}
	if !s.off.Empty() {
		piglinStopHoldingOffHandItem(m, s, false)
	}
	br := m.Brain()
	br.erase(memCelebrateLocation)
	br.erase(memDancing)
	br.erase(memAdmiringItem)
	if isPlayerEntity(attacker) {
		br.setWithExpiry(memAdmiringDisabled, true, 400)
	}
	if avoid := br.entity(m, memAvoidTarget); avoid != nil && avoid.H().Type() != attacker.H().Type() {
		br.erase(memAvoidTarget)
	}
	switch {
	case s.baby:
		br.setWithExpiry(memAvoidTarget, attacker.H(), 100)
		if sensorAttackableIgnoringSight(m, attacker) {
			piglinBroadcastAngerTarget(m, attacker)
		}
	case attacker.H().Type() == HoglinType && piglinHoglinsOutnumber(m):
		piglinSetAvoidTarget(m, attacker)
		for _, p := range piglinsOf(m, memNearestVisibleAdultPiglins) {
			if p.H().Type() == (PiglinType{}) {
				nearest := nearestOf(p, p.Brain().entity(p, memAvoidTarget), attacker)
				nearest = nearestOf(p, p.Brain().entity(p, memAttackTarget), nearest)
				piglinSetAvoidTarget(p, nearest)
			}
		}
	default:
		piglinMaybeRetaliate(m, attacker)
	}
}

// piglinMaybeRetaliate ports PiglinAi.maybeRetaliate.
func piglinMaybeRetaliate(m *Mob, attacker world.Entity) {
	if m.Brain().isActive(activityAvoid) || !sensorAttackableIgnoringSight(m, attacker) || otherTargetMuchFurther(m, attacker, 4) {
		return
	}
	if isPlayerEntity(attacker) && m.tx.World().GameRuleBool("universal_anger") {
		piglinSetAngerTargetToNearestPlayer(m, attacker)
		for _, p := range piglinsOf(m, memNearbyAdultPiglins) {
			if pl := p.Brain().entity(p, memNearestVisibleAttackablePlayer); pl != nil {
				piglinSetAngerTarget(p, pl)
			}
		}
		return
	}
	piglinSetAngerTarget(m, attacker)
	piglinBroadcastAngerTarget(m, attacker)
}

// piglinSetAngerTarget ports PiglinAi.setAngerTarget.
func piglinSetAngerTarget(m *Mob, target world.Entity) {
	if !sensorAttackableIgnoringSight(m, target) {
		return
	}
	br := m.Brain()
	br.erase(memCantReachWalkTargetSince)
	br.setWithExpiry(memAngryAt, target.H().UUID(), 600)
	if target.H().Type() == HoglinType && piglinCanHunt(m) {
		piglinDontHuntForAWhile(m)
	}
	if isPlayerEntity(target) && m.tx.World().GameRuleBool("universal_anger") {
		br.setWithExpiry(memUniversalAnger, true, 600)
	}
}

// piglinCanHunt reports if the mob is a piglin that hunts hoglins.
func piglinCanHunt(m *Mob) bool {
	p, ok := m.Self.(interface{ CanHunt() bool })
	return ok && p.CanHunt()
}

// piglinSetAngerTargetToNearestPlayer ports
// PiglinAi.setAngerTargetToNearestTargetablePlayerIfFound.
func piglinSetAngerTargetToNearestPlayer(m *Mob, fallback world.Entity) {
	if p := m.Brain().entity(m, memNearestVisibleAttackablePlayer); p != nil {
		piglinSetAngerTarget(m, p)
		return
	}
	piglinSetAngerTarget(m, fallback)
}

// piglinBroadcastAngerTarget ports PiglinAi.broadcastAngerTarget: the adult
// piglins around it get angry at the target too, those that hunt even at a
// hoglin they may hunt.
func piglinBroadcastAngerTarget(m *Mob, target world.Entity) {
	for _, p := range piglinsOf(m, memNearbyAdultPiglins) {
		if h, ok := target.(*Hoglin); ok && (!piglinCanHunt(p) || !h.CanBeHunted()) {
			continue
		}
		var current world.Entity
		if v, ok := p.Brain().get(memAngryAt); ok {
			current, _ = p.tx.EntityByUUID(v.(uuid.UUID))
		}
		nearest := nearestOf(p, current, target)
		if current == nil || current.H() != nearest.H() {
			piglinSetAngerTarget(p, nearest)
		}
	}
}

// AngerNearbyPiglins ports PiglinAi.angerNearbyPiglins: the idle piglins
// within 16 blocks of a player who opens a container or breaks a block they
// guard turn on them, if they see them when onlyIfSeen.
func AngerNearbyPiglins(tx *world.Tx, player world.Entity, onlyIfSeen bool) {
	box := player.H().Type().BBox(player).Translate(player.Position()).Grow(16)
	for e := range tx.EntitiesWithin(box.Grow(2)) {
		p, ok := e.(*Piglin)
		if !ok || !e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) {
			continue
		}
		s := p.p()
		m := s.ai.Mob(p.Ent, p)
		if !s.br.isActive(activityIdle) || onlyIfSeen && !brainCanSee(m, player) {
			continue
		}
		if tx.World().GameRuleBool("universal_anger") {
			piglinSetAngerTargetToNearestPlayer(m, player)
		} else {
			piglinSetAngerTarget(m, player)
		}
	}
}

// newStopHoldingItemIfNoLongerAdmiring ports StopHoldingItemIfNoLongerAdmiring.
func newStopHoldingItemIfNoLongerAdmiring(s *piglinState) *oneShot {
	return newOneShot([]memoryCondition{{memAdmiringItem, memAbsent}}, func(m *Mob, _ *Brain, _ int64) bool {
		if s.off.Empty() {
			return false
		}
		if _, shield := s.off.Item().(item.Shield); shield {
			return false
		}
		piglinStopHoldingOffHandItem(m, s, true)
		return true
	})
}

// newStartAdmiringItemIfSeen ports StartAdmiringItemIfSeen.
func newStartAdmiringItemIfSeen(duration int) *oneShot {
	return newOneShot([]memoryCondition{{memNearestVisibleWantedItem, memPresent}, {memAdmiringItem, memAbsent}, {memAdmiringDisabled, memAbsent}, {memDisableWalkToAdmireItem, memAbsent}}, func(m *Mob, b *Brain, _ int64) bool {
		it := b.entity(m, memNearestVisibleWantedItem)
		stack, ok := pickableItem(it)
		if it == nil || !ok && !itemEntityStackOf(it, &stack) || !piglinLovedItem(stack) {
			return false
		}
		b.setWithExpiry(memAdmiringItem, true, int64(duration))
		return true
	})
}

// itemEntityStackOf sets the stack of an item entity, even one with a
// pickup delay.
func itemEntityStackOf(e world.Entity, stack *item.Stack) bool {
	ent, ok := e.(*Ent)
	if !ok {
		return false
	}
	i, ok := ent.Behaviour().(*ItemBehaviour)
	if !ok {
		return false
	}
	*stack = i.i
	return true
}

// newStopAdmiringIfItemTooFarAway ports StopAdmiringIfItemTooFarAway.
func newStopAdmiringIfItemTooFarAway(s *piglinState, maxDist float64) *oneShot {
	return newOneShot([]memoryCondition{{memAdmiringItem, memPresent}, {memNearestVisibleWantedItem, memRegistered}}, func(m *Mob, b *Brain, _ int64) bool {
		if !s.off.Empty() {
			return false
		}
		if it := b.entity(m, memNearestVisibleWantedItem); it != nil && it.Position().Sub(m.data.Pos).LenSqr() < maxDist*maxDist {
			return false
		}
		b.erase(memAdmiringItem)
		return true
	})
}

// newStopAdmiringIfTiredOfTryingToReachItem ports
// StopAdmiringIfTiredOfTryingToReachItem.
func newStopAdmiringIfTiredOfTryingToReachItem(s *piglinState, maxTime, disableTime int) *oneShot {
	return newOneShot([]memoryCondition{{memAdmiringItem, memPresent}, {memNearestVisibleWantedItem, memPresent}, {memTimeTryingToReachAdmireItem, memRegistered}, {memDisableWalkToAdmireItem, memRegistered}}, func(m *Mob, b *Brain, _ int64) bool {
		if !s.off.Empty() {
			return false
		}
		t, ok := b.int(memTimeTryingToReachAdmireItem)
		switch {
		case !ok:
			b.set(memTimeTryingToReachAdmireItem, 0)
		case t > maxTime:
			b.erase(memAdmiringItem)
			b.erase(memTimeTryingToReachAdmireItem)
			b.setWithExpiry(memDisableWalkToAdmireItem, true, int64(disableTime))
		default:
			b.set(memTimeTryingToReachAdmireItem, t+1)
		}
		return true
	})
}

// newStartHuntingHoglin ports StartHuntingHoglin: an adult piglin that sees a
// hoglin it may hunt sets on it with the piglins around, unless one of them
// hunted lately.
func newStartHuntingHoglin(s *piglinState) *oneShot {
	return newOneShot([]memoryCondition{{memNearestVisibleHuntableHoglin, memPresent}, {memAngryAt, memAbsent}, {memHuntedRecently, memAbsent}, {memNearestVisibleAdultPiglins, memRegistered}}, func(m *Mob, b *Brain, _ int64) bool {
		if s.baby {
			return false
		}
		piglins := piglinsOf(m, memNearestVisibleAdultPiglins)
		for _, p := range piglins {
			if p.Brain().has(memHuntedRecently) {
				return false
			}
		}
		target := b.entity(m, memNearestVisibleHuntableHoglin)
		if target == nil {
			return false
		}
		piglinSetAngerTarget(m, target)
		piglinDontHuntForAWhile(m)
		piglinBroadcastAngerTarget(m, target)
		for _, p := range piglins {
			piglinDontHuntForAWhile(p)
		}
		return true
	})
}

// newRememberIfHoglinWasKilled ports RememberIfHoglinWasKilled.
func newRememberIfHoglinWasKilled() *oneShot {
	return newOneShot([]memoryCondition{{memAttackTarget, memPresent}, {memHuntedRecently, memRegistered}}, func(m *Mob, b *Brain, _ int64) bool {
		if d, ok := attackTargetDeath(m); ok && d.typ == HoglinType {
			b.setWithExpiry(memHuntedRecently, true, int64(600+m.Random().NextInt(1801)))
		}
		return true
	})
}

// newCrossbowAttack ports CrossbowAttack for piglins: a piglin with a
// crossbow loads it, waits a second or two and shoots its target.
func newCrossbowAttack(s *piglinState) *behavior {
	state, delay, used := crossbowUncharged, 0, 0
	holding := func() bool { _, ok := s.main.Item().(item.Crossbow); return ok }
	canShoot := func(m *Mob) bool {
		target := m.Brain().entity(m, memAttackTarget)
		return holding() && target != nil && brainCanSee(m, target) && m.DistanceToSqr(target) <= 64
	}
	setCharged := func(m *Mob, charged bool) {
		if c, ok := s.main.Item().(item.Crossbow); ok {
			if charged {
				c.Item = item.NewStack(item.Arrow{}, 1)
			} else {
				c.Item = item.Stack{}
			}
			s.main = item.NewStack(c, 1).WithEnchantments(s.main.Enchantments()...)
			m.updateState()
		}
	}
	return &behavior{
		entry:       []memoryCondition{{memLookTarget, memRegistered}, {memAttackTarget, memPresent}},
		minDuration: 1200, maxDuration: 1200,
		checkExtra:  canShoot,
		canStillUse: func(m *Mob, _ int64) bool { return m.Brain().has(memAttackTarget) && canShoot(m) },
		tick: func(m *Mob, _ int64) {
			target := m.Brain().entity(m, memAttackTarget)
			if target == nil {
				return
			}
			m.Brain().set(memLookTarget, newEntityTracker(target, true))
			switch state {
			case crossbowUncharged:
				state, used = crossbowCharging, 0
				s.chargingCrossbow = true
				m.updateState()
			case crossbowCharging:
				if used++; used >= crossbowChargeDuration(s.main) {
					state, delay = crossbowCharged, 20+int(m.Random().NextInt(20))
					s.chargingCrossbow = false
					setCharged(m, true)
				}
			case crossbowCharged:
				if delay--; delay == 0 {
					state = crossbowReady
				}
			case crossbowReady:
				mobShootCrossbow(m, target, 1.6)
				setCharged(m, false)
				state = crossbowUncharged
			}
		},
		stop: func(m *Mob, _ int64) {
			if holding() {
				s.chargingCrossbow = false
				setCharged(m, false)
			}
		},
	}
}

// mobShootCrossbow ports CrossbowAttackMob.performCrossbowAttack: an arrow
// aimed a little above the target.
func mobShootCrossbow(m *Mob, target world.Entity, power float64) {
	pos := mgl64.Vec3{m.data.Pos[0], m.EyeY() - .1, m.data.Pos[2]}
	tp := target.Position()
	xd, zd := tp[0]-m.data.Pos[0], tp[2]-m.data.Pos[2]
	yd := tp[1] + target.H().Type().BBox(target).Height()/3 - pos[1] + mgl64.Vec2{xd, zd}.Len()*.2
	vel := shootDirection(m, mgl64.Vec3{xd, yd, zd}, power, rangedAttackUncertainty(m))
	m.tx.AddEntity(NewArrowWithDamage(world.EntitySpawnOpts{Position: pos, Velocity: vel}, 2, m.Self))
	m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "item.crossbow.shoot", Volume: 1, Pitch: 1})
	m.AI.noActionTime = 0
}

// piglinSpecificSensor ports PiglinSpecificSensor.
type piglinSpecificSensor struct{}

func (piglinSpecificSensor) scanRate() int { return 0 }
func (piglinSpecificSensor) requires() []memoryType {
	return []memoryType{memNearestVisibleLivingEntities, memNearestLivingEntities, memNearestVisibleNemesis, memNearestTargetablePlayerNotWearingGold,
		memNearestPlayerHoldingWantedItem, memNearestVisibleHuntableHoglin, memNearestVisibleBabyHoglin, memNearestVisibleAdultPiglins,
		memNearbyAdultPiglins, memVisibleAdultPiglinCount, memVisibleAdultHoglinCount, memNearestRepellent, memNearestVisibleZombified}
}

func (piglinSpecificSensor) doTick(m *Mob, b *Brain) {
	if p, ok := findManhattanBox(m.BlockPos(), 8, 4, func(p cube.Pos) bool { return piglinRepellent(m.tx.Block(p)) }); ok {
		b.set(memNearestRepellent, p)
	} else {
		b.erase(memNearestRepellent)
	}
	var nemesis, huntable, babyHoglin, zombified, notWearingGold, holdingWanted world.Entity
	hoglins := 0
	var adultPiglins []*world.EntityHandle
	if v := visibleLiving(m); v != nil {
		for _, e := range v.findAll(m, func(world.Entity) bool { return true }) {
			switch t := e.(type) {
			case *Hoglin:
				if t.beast().age < 0 {
					if babyHoglin == nil {
						babyHoglin = e
					}
				} else {
					hoglins++
					if huntable == nil && t.CanBeHunted() {
						huntable = e
					}
				}
			case *PiglinBrute:
				adultPiglins = append(adultPiglins, e.H())
			case *Piglin:
				if !t.p().baby {
					adultPiglins = append(adultPiglins, e.H())
				}
			default:
				switch {
				case isPlayerEntity(e):
					if notWearingGold == nil && !piglinWearingSafeArmour(e) && m.CanAttack(e) {
						notWearingGold = e
					}
					if holdingWanted == nil && !spectator(e) && piglinPlayerHoldingLovedItem(e) {
						holdingWanted = e
					}
				case nemesis == nil && (e.H().Type().EncodeEntity() == "minecraft:wither_skeleton" || e.H().Type() == WitherType):
					nemesis = e
				case zombified == nil && piglinZombified(e):
					zombified = e
				}
			}
		}
	}
	b.setEntity(memNearestVisibleNemesis, nemesis)
	b.setEntity(memNearestVisibleHuntableHoglin, huntable)
	b.setEntity(memNearestVisibleBabyHoglin, babyHoglin)
	b.setEntity(memNearestVisibleZombified, zombified)
	b.setEntity(memNearestTargetablePlayerNotWearingGold, notWearingGold)
	b.setEntity(memNearestPlayerHoldingWantedItem, holdingWanted)
	b.set(memNearbyAdultPiglins, nearbyAdultPiglins(m))
	b.set(memNearestVisibleAdultPiglins, adultPiglins)
	b.set(memVisibleAdultPiglinCount, len(adultPiglins))
	b.set(memVisibleAdultHoglinCount, hoglins)
}

// nearbyAdultPiglins ports PiglinAi.findNearbyAdultPiglins: the adult
// piglins and brutes among the living entities near the mob.
func nearbyAdultPiglins(m *Mob) []*world.EntityHandle {
	var out []*world.EntityHandle
	for _, e := range nearestLiving(m) {
		switch p := e.(type) {
		case *PiglinBrute:
			out = append(out, e.H())
		case *Piglin:
			if !p.p().baby {
				out = append(out, e.H())
			}
		}
	}
	return out
}

// piglinRepellent ports PiglinSpecificSensor.isValidRepellent: a block in
// #piglin_repellents, a soul campfire only while lit.
func piglinRepellent(b world.Block) bool {
	if !blockInTag(b, "piglin_repellents") {
		return false
	}
	if c, ok := b.(block.Campfire); ok && c.Type == block.SoulFire() {
		return !c.Extinguished
	}
	return true
}

// newPiglinBruteBrain ports PiglinBruteAi and the brain provider of
// PiglinBrute.
func newPiglinBruteBrain(s *piglinState) *Brain {
	br := newBrain(nearestLivingEntitySensor{}, playerSensor{}, nearestItemSensor{wants: func(m *Mob, st item.Stack) bool {
		return !mobGriefingDisabled(m.tx) && s.wantsToPickUp(m, st)
	}}, hurtBySensor{}, piglinBruteSpecificSensor{})
	br.register(memNearestVisibleAdultPiglins)
	isBruteType := func(e world.Entity) bool { return e.H().Type() == (PiglinBruteType{}) }
	br.addActivity(activityCore, 0,
		newLookAtTargetSink(45, 90),
		newMoveToTargetSink(150, 250, nil),
		newInteractWithDoor(),
		newStopBeingAngryIfTargetDead(),
	)
	br.addActivity(activityIdle, 10,
		newStartAttacking(nil, func(m *Mob) world.Entity { return piglinBruteNearestValidAttackTarget(m) }),
		newRunOne(nil,
			weightedBehavior{newSetEntityLookTarget(isPlayerType, 8), 1},
			weightedBehavior{newSetEntityLookTarget(isPiglinType, 8), 1},
			weightedBehavior{newSetEntityLookTarget(isBruteType, 8), 1},
			weightedBehavior{newSetEntityLookTarget(nil, 8), 1},
			weightedBehavior{newDoNothing(30, 60), 1},
		),
		newRunOne(nil,
			weightedBehavior{newRandomStroll(.6, true), 2},
			weightedBehavior{newInteractWith(isPiglinType, 8, .6, 2), 2},
			weightedBehavior{newInteractWith(isBruteType, 8, .6, 2), 2},
			weightedBehavior{newStrollToPoi(memHome, .6, 2, 100), 2},
			weightedBehavior{newStrollAroundPoi(memHome, .6, 5), 2},
			weightedBehavior{newDoNothing(30, 60), 1},
		),
		newSetLookAndInteract(isPlayerType, 4),
	)
	br.addActivityWith(activityFight, prioritiesFrom(10,
		newStopAttackingIfTargetInvalid(func(m *Mob, target world.Entity) bool {
			valid := piglinBruteNearestValidAttackTarget(m)
			return valid == nil || valid.H() != target.H()
		}, nil, true),
		newSetWalkTargetFromAttackTargetIfTargetOutOfReach(constSpeed(1)),
		newMeleeAttack(20),
	), []memoryCondition{{memAttackTarget, memPresent}}, []memoryType{memAttackTarget})
	return br
}

// piglinBruteNearestValidAttackTarget ports
// PiglinBruteAi.findNearestValidAttackTarget.
func piglinBruteNearestValidAttackTarget(m *Mob) world.Entity {
	br := m.Brain()
	if v, ok := br.get(memAngryAt); ok {
		if e, ok := m.tx.EntityByUUID(v.(uuid.UUID)); ok && sensorAttackableIgnoringSight(m, e) {
			return e
		}
	}
	if p := br.entity(m, memNearestVisibleAttackablePlayer); p != nil {
		return p
	}
	return br.entity(m, memNearestVisibleNemesis)
}

// piglinBruteUpdateActivity ports PiglinBruteAi.updateActivity.
func piglinBruteUpdateActivity(m *Mob, s *piglinState) {
	old, _ := s.br.activeNonCore()
	s.br.setActiveToFirstValid(activityFight, activityIdle)
	if now, _ := s.br.activeNonCore(); now != old {
		piglinBrutePlayActivitySound(m, s)
	}
	m.SetAggressive(s.br.has(memAttackTarget))
}

// piglinBrutePlayActivitySound ports PiglinBruteAi.playActivitySound.
func piglinBrutePlayActivitySound(m *Mob, s *piglinState) {
	if act, _ := s.br.activeNonCore(); act == activityFight {
		s.makeSound(m, "mob.piglin_brute.angry")
	}
}

// piglinBruteWasHurtBy ports PiglinBruteAi.wasHurtBy.
func piglinBruteWasHurtBy(m *Mob, _ *piglinState, attacker world.Entity) {
	switch attacker.(type) {
	case *Piglin, *PiglinBrute:
		return
	}
	piglinMaybeRetaliate(m, attacker)
}

// piglinBruteSpecificSensor ports PiglinBruteSpecificSensor.
type piglinBruteSpecificSensor struct{}

func (piglinBruteSpecificSensor) scanRate() int { return 0 }
func (piglinBruteSpecificSensor) requires() []memoryType {
	return []memoryType{memNearestVisibleLivingEntities, memNearestVisibleNemesis, memNearbyAdultPiglins}
}

func (piglinBruteSpecificSensor) doTick(m *Mob, b *Brain) {
	var nemesis world.Entity
	if v := visibleLiving(m); v != nil {
		nemesis = v.findClosest(m, func(e world.Entity) bool {
			return e.H().Type().EncodeEntity() == "minecraft:wither_skeleton" || e.H().Type() == WitherType
		})
	}
	b.setEntity(memNearestVisibleNemesis, nemesis)
	b.set(memNearbyAdultPiglins, nearbyAdultPiglins(m))
}

// attackDamageBase returns the attack damage attribute of the piglin.
func (s *piglinState) attackDamageBase() float64 {
	if s.brute {
		return 7
	}
	return 5
}
