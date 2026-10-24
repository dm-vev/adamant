package entity

import (
	"math"
	"slices"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports the goals of Java's Bee.

func beeOf(m *Mob) *beeBehaviour { return m.Behaviour().(*beeBehaviour) }

// registerGoals ports Bee.registerGoals and the navigation, move control and
// look control of its constructor.
func (b *beeBehaviour) registerGoals(m *Mob) {
	ai := m.AI
	ai.nav.SetFlying()
	ai.nav.SetCanOpenDoors(false)
	ai.nav.SetCanFloat(false)
	ai.nav.SetRequiredPathLength(m, 48)
	// The navigation of Bee lands on any block but air, and stands still
	// while the bee pollinates.
	ai.nav.StableDestination = func(tx *world.Tx, pos cube.Pos) bool { return !isAir(pathBlockAt(tx, pos.Side(cube.FaceDown))) }
	ai.nav.Paused = func() bool { return b.pollinate.pollinating }
	ai.moveOverride = flyingMoveControl{maxTurn: 20, hoversInPlace: true}
	ai.lookOverride = beeLookControl{b}
	ai.SetPathfindingMalus(PathFire, -1)
	ai.SetPathfindingMalus(PathWater, -1)
	ai.SetPathfindingMalus(PathWaterBorder, 16)
	ai.SetPathfindingMalus(PathCocoa, -1)
	ai.SetPathfindingMalus(PathFence, -1)
	r := m.Random()
	b.flowerCooldown = 20 + int(r.NextInt(41))
	b.pollinate = &beePollinateGoal{goalFlags: goalFlags{GoalMove}, bee: b, unreachable: map[cube.Pos]int64{}}
	b.goToHive = &beeGoToHiveGoal{goalFlags: goalFlags{GoalMove}, bee: b}

	ai.Goals.Add(0, beeAttackGoal{NewMeleeAttackGoal(1.4, true), b})
	ai.Goals.Add(1, &beeEnterHiveGoal{bee: b})
	ai.Goals.Add(2, NewBreedGoal(1))
	ai.Goals.Add(3, NewTemptGoal(1.25, func(s item.Stack) bool { return blockItemInTag(s, "bee_attractive") }, false))
	ai.Goals.Add(3, &beeValidateGoal{bee: b, cooldown: 20 + int(r.NextInt(21)), lastTick: -1, validate: b.validateHive})
	ai.Goals.Add(3, &beeValidateGoal{bee: b, cooldown: 20 + int(r.NextInt(21)), lastTick: -1, validate: b.validateFlower})
	ai.Goals.Add(4, b.pollinate)
	ai.Goals.Add(5, NewFollowParentGoal(1.25))
	ai.Goals.Add(5, &beeLocateHiveGoal{bee: b})
	ai.Goals.Add(5, b.goToHive)
	ai.Goals.Add(6, &beeGoToKnownFlowerGoal{goalFlags: goalFlags{GoalMove}, bee: b})
	ai.Goals.Add(7, &beeGrowCropGoal{bee: b})
	ai.Goals.Add(8, &beeWanderGoal{goalFlags{GoalMove}, b})
	ai.Goals.Add(9, NewFloatGoal(m))

	hurt := NewHurtByTargetGoal()
	hurt.AlertOthers = true
	// BeeHurtByOtherGoal.alertOther: a hurt bee alerts the bees around that
	// it sees the attacker from.
	hurt.AlertOther = func(m, other *Mob, attacker world.Entity) {
		if _, ok := other.Self.(*Bee); ok && m.HasLineOfSight(attacker) {
			other.SetTarget(attacker)
		}
	}
	ai.Targets.Add(1, beeHurtByOtherGoal{hurt, b})
	angry := &NearestAttackableTargetGoal{targetGoal: newTargetGoal(true, false), RandomInterval: reducedTickDelay(10), IsType: isPlayerEntity}
	angry.Selector = func(m *Mob, target world.Entity) bool { return b.angryAt(m, target) }
	ai.Targets.Add(2, beeBecomeAngryTargetGoal{angry, b})
	ai.Targets.Add(3, &resetUniversalAngerTargetGoal{lastHurtByPlayer: -1, alertSameType: true})
}

// walkTargetValue ports Bee.getWalkTargetValue: bees fly to air.
func (b *beeBehaviour) walkTargetValue(m *Mob, pos cube.Pos) float64 {
	if isAir(pathBlockAt(m.tx, pos)) {
		return 10
	}
	return 0
}

// omnidirectionalAirMover ports Bee.omnidirectionalAirMover.
func (b *beeBehaviour) omnidirectionalAirMover() bool { return true }

// jumpFromGround keeps the jump of LivingEntity.
func (b *beeBehaviour) jumpFromGround(*Mob) bool { return false }

// jumpInLiquid ports Bee.jumpInLiquid: a bee in water rises slowly.
func (b *beeBehaviour) jumpInLiquid(m *Mob, _ bool) bool {
	m.data.Vel[1] += .01
	return true
}

// customServerAiStep ports Bee.customServerAiStep: a bee drowns after a
// second in water, a bee that stung dies within a minute, a bee without
// nectar grows tired of looking for it and its anger runs out.
func (b *beeBehaviour) customServerAiStep(m *Mob) {
	bee := m.Self.(*Bee)
	hasStung := b.hasStung
	if m.InWater() {
		b.underWaterTicks++
	} else {
		b.underWaterTicks = 0
	}
	if b.underWaterTicks > 20 {
		bee.Hurt(1, DrowningDamageSource{})
	}
	if hasStung {
		b.timeSinceSting++
		if b.timeSinceSting%5 == 0 && m.Random().NextInt(int32(max(1, min(1200-b.timeSinceSting, 1200)))) == 0 {
			bee.Hurt(bee.Health(), GenericDamageSource{})
		}
	}
	if !b.hasNectar {
		b.ticksWithoutNectar++
	}
	b.updatePersistentAnger(m, false)
}

// afterAiStep ports what Bee.aiStep does after the AI: the cooldowns run
// out, an angry bee close to its target rolls, and the bee forgets a hive
// that is gone.
func (b *beeBehaviour) afterAiStep(m *Mob) {
	if b.stayOut > 0 {
		b.stayOut--
	}
	if b.hiveCooldown > 0 {
		b.hiveCooldown--
	}
	if b.flowerCooldown > 0 {
		b.flowerCooldown--
	}
	target := m.Target()
	b.rolling = b.angry() && !b.hasStung && target != nil && m.DistanceToSqr(target) < 4
	if m.AI.tickCount%20 == 0 && !b.hiveValid(m) {
		b.hive = nil
	}
}

// doHurtTarget ports Bee.doHurtTarget: a bee stings for 2 damage and poisons
// its target on Normal and Hard, losing its stinger and its anger.
func (b *beeBehaviour) doHurtTarget(m *Mob, target world.Entity) bool {
	l, ok := target.(Living)
	if !ok {
		return false
	}
	if _, hurt := l.Hurt(2, AttackDamageSource{Attacker: m.Self}); !hurt {
		return false
	}
	l.KnockBack(m.data.Pos, .4, .4)
	poison := time.Duration(0)
	switch m.tx.World().Difficulty() {
	case world.DifficultyNormal:
		poison = 10 * time.Second
	case world.DifficultyHard:
		poison = 18 * time.Second
	}
	if poison > 0 {
		l.AddEffect(effect.New(effect.Poison, 1, poison))
	}
	b.hasStung = true
	b.stopBeingAngry(m)
	m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.bee.sting", Volume: 1, Pitch: 1})
	return true
}

// beeCloserThan ports Bee.closerThan: the block of the bee is within
// distance of pos.
func beeCloserThan(m *Mob, pos cube.Pos, distance float64) bool {
	d := m.BlockPos().Sub(pos)
	return float64(d[0]*d[0]+d[1]*d[1]+d[2]*d[2]) < distance*distance
}

// tooFarAway ports Bee.isTooFarAway.
func tooFarAway(m *Mob, pos cube.Pos) bool { return !beeCloserThan(m, pos, 48) }

// beeHome is a beehive or bee nest bees enter.
type beeHome interface {
	world.Block
	AddBeeOccupant(data map[string]any) (world.Block, bool)
	IsFull() bool
}

// beehive ports Bee.getBeehiveBlockEntity: the hive of the bee if it is
// not too far away.
func (b *beeBehaviour) beehive(m *Mob) (beeHome, bool) {
	if b.hive == nil || tooFarAway(m, *b.hive) {
		return nil, false
	}
	home, ok := m.tx.Block(*b.hive).(beeHome)
	return home, ok
}

// hiveValid ports Bee.isHiveValid.
func (b *beeBehaviour) hiveValid(m *Mob) bool {
	_, ok := b.beehive(m)
	return ok
}

// dropHive ports Bee.dropHive.
func (b *beeBehaviour) dropHive() { b.hive, b.hiveCooldown = nil, 200 }

// dropFlower ports Bee.dropFlower.
func (b *beeBehaviour) dropFlower(m *Mob) {
	b.flower, b.flowerCooldown = nil, 20+int(m.Random().NextInt(41))
}

// wantsToEnterHive ports Bee.wantsToEnterHive: a bee that is not busy goes
// home with nectar, when it is tired of looking for it or when bees stay in
// their hives, unless fire burns by its hive.
func (b *beeBehaviour) wantsToEnterHive(m *Mob) bool {
	if b.stayOut > 0 || b.pollinate.pollinating || b.hasStung || m.Target() != nil {
		return false
	}
	wants := b.hasNectar || b.ticksWithoutNectar > 3600 || block.BeesStayInHive(m.tx)
	return wants && !b.hiveNearFire(m)
}

// hiveNearFire ports Bee.isHiveNearFire: fire burns within a block of the
// hive.
func (b *beeBehaviour) hiveNearFire(m *Mob) bool {
	if !b.hiveValid(m) {
		return false
	}
	for x := -1; x <= 1; x++ {
		for y := -1; y <= 1; y++ {
			for z := -1; z <= 1; z++ {
				if f, ok := m.tx.Block(b.hive.Add(cube.Pos{x, y, z})).(block.Fire); ok && f.Type == block.NormalFire() {
					return true
				}
			}
		}
	}
	return false
}

// enterHive ports BeehiveBlockEntity.addOccupant: the bee leaves the world
// for the hive, which keeps what it saved but its hive and counters.
func (b *beeBehaviour) enterHive(m *Mob, home beeHome) {
	pos := *b.hive
	data := BeeType.EncodeNBT(m.data)
	if j := ReadNativeMobJavaState(data); j != nil {
		// BeehiveBlockEntity.IGNORED_BEE_TAGS.
		for _, key := range [...]string{"NoGravity", "CannotEnterHiveTicks", "TicksSincePollination", "CropsGrownSincePollination", "hive_pos"} {
			delete(j, key)
		}
		data = MergeNativeMobJavaState(data, j)
	}
	next, ok := home.AddBeeOccupant(data)
	if !ok {
		return
	}
	m.tx.SetBlock(pos, next, nil)
	m.tx.ScheduleBlockUpdate(pos, next, time.Second/20)
	m.tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: "block.beehive.enter", Volume: 1, Pitch: 1})
	m.tx.EmitGameEvent(world.GameEventBlockChange, pos.Vec3Centre(), world.GameEventContext{Source: m.Self, AffectedState: next})
	_ = m.Ent.CloseIn(m.tx)
}

// pathfindRandomlyTowards ports Bee.pathfindRandomlyTowards: a bee far from
// where it wants to go flies to a spot in the air on the way there.
func (b *beeBehaviour) pathfindRandomlyTowards(m *Mob, target cube.Pos) {
	targetVec := mgl64.Vec3{float64(target.X()) + .5, float64(target.Y()), float64(target.Z()) + .5}
	beePos := m.BlockPos()
	yAdjust := 0
	if yDelta := target.Y() - beePos.Y(); yDelta > 2 {
		yAdjust = 4
	} else if yDelta < -2 {
		yAdjust = -4
	}
	xzDist, yDist := 6, 8
	if dist := absInt(beePos.X()-target.X()) + absInt(beePos.Y()-target.Y()) + absInt(beePos.Z()-target.Z()); dist < 15 {
		xzDist, yDist = dist/2, dist/2
	}
	if next, ok := AirRandomPosTowards(m, xzDist, yDist, yAdjust, targetVec, math.Pi/10); ok {
		m.AI.nav.SetMaxVisitedNodesMultiplier(.5)
		m.AI.nav.MoveToPos(m, next[0], next[1], next[2], 1)
	}
}

// attractsBees ports Bee.attractsBees: a dry block in #bee_attractive, of a
// sunflower its top half.
func attractsBees(tx *world.Tx, pos cube.Pos) bool {
	b := tx.Block(pos)
	if !blockInTag(b, "bee_attractive") {
		return false
	}
	if _, wet := tx.Liquid(pos); wet {
		return false
	}
	if f, ok := b.(block.DoubleFlower); ok && f.Type == block.Sunflower() {
		return f.UpperPart
	}
	return true
}

// beeLookControl ports BeeLookControl: an angry bee does not look around,
// and a pollinating one keeps looking down at its flower.
type beeLookControl struct{ bee *beeBehaviour }

func (c beeLookControl) tick(m *Mob) {
	if c.bee.angry() {
		return
	}
	m.AI.look.keepPitch = c.bee.pollinate.pollinating
	m.AI.look.tick(m)
}

// beeAttackGoal ports BeeAttackGoal: an angry bee that still has its stinger
// attacks.
type beeAttackGoal struct {
	*MeleeAttackGoal
	bee *beeBehaviour
}

func (g beeAttackGoal) CanUse(m *Mob) bool {
	return g.MeleeAttackGoal.CanUse(m) && g.bee.angry() && !g.bee.hasStung
}

func (g beeAttackGoal) CanContinueToUse(m *Mob) bool {
	return g.MeleeAttackGoal.CanContinueToUse(m) && g.bee.angry() && !g.bee.hasStung
}

// beeEnterHiveGoal ports BeeEnterHiveGoal: a bee within two blocks of its
// hive that wants to go home enters it, and forgets a full one.
type beeEnterHiveGoal struct {
	goalFlags
	bee *beeBehaviour
}

func (g *beeEnterHiveGoal) CanUse(m *Mob) bool {
	b := g.bee
	if b.angry() || b.hive == nil || !b.wantsToEnterHive(m) || b.hive.Vec3Centre().Sub(m.data.Pos).LenSqr() >= 4 {
		return false
	}
	home, ok := b.beehive(m)
	if !ok {
		return false
	}
	if !home.IsFull() {
		return true
	}
	b.hive = nil
	return false
}

func (g *beeEnterHiveGoal) CanContinueToUse(*Mob) bool { return false }

func (g *beeEnterHiveGoal) Start(m *Mob) {
	if home, ok := g.bee.beehive(m); ok {
		g.bee.enterHive(m, home)
	}
}

// beeGoToHiveGoal ports BeeGoToHiveGoal: a bee that wants to go home flies
// to its hive, giving up on one it took two minutes to reach or cannot.
type beeGoToHiveGoal struct {
	goalFlags
	bee                         *beeBehaviour
	travellingTicks, ticksStuck int
	blacklisted                 []cube.Pos
	lastPath                    *mobPath
}

func (g *beeGoToHiveGoal) CanUse(m *Mob) bool {
	b := g.bee
	return !b.angry() && b.hive != nil && !tooFarAway(m, *b.hive) && !m.AI.home.set() && b.wantsToEnterHive(m) &&
		!g.reachedTarget(m, *b.hive) && blockInTag(m.tx.Block(*b.hive), "beehives")
}

func (g *beeGoToHiveGoal) CanContinueToUse(m *Mob) bool { return g.CanUse(m) }

func (g *beeGoToHiveGoal) Start(*Mob) { g.travellingTicks, g.ticksStuck = 0, 0 }

func (g *beeGoToHiveGoal) Stop(m *Mob) {
	g.travellingTicks, g.ticksStuck = 0, 0
	m.AI.nav.Stop()
	m.AI.nav.ResetMaxVisitedNodesMultiplier()
}

func (g *beeGoToHiveGoal) Tick(m *Mob) {
	b := g.bee
	if b.hive == nil {
		return
	}
	g.travellingTicks++
	switch {
	case g.travellingTicks > adjustedTickDelay(g, 2400):
		g.dropAndBlacklistHive()
	case m.AI.nav.InProgress():
	case !beeCloserThan(m, *b.hive, 16):
		if tooFarAway(m, *b.hive) {
			b.dropHive()
		} else {
			b.pathfindRandomlyTowards(m, *b.hive)
		}
	case !g.pathfindDirectlyTowards(m, *b.hive):
		g.dropAndBlacklistHive()
	case g.lastPath != nil && m.AI.nav.Path().sameAs(g.lastPath):
		if g.ticksStuck++; g.ticksStuck > 60 {
			b.dropHive()
			g.ticksStuck = 0
		}
	default:
		g.lastPath = m.AI.nav.Path()
	}
}

// pathfindDirectlyTowards reports if the bee found a path all the way to
// its hive.
func (g *beeGoToHiveGoal) pathfindDirectlyTowards(m *Mob, pos cube.Pos) bool {
	closeEnough := 2
	if beeCloserThan(m, pos, 3) {
		closeEnough = 1
	}
	m.AI.nav.SetMaxVisitedNodesMultiplier(10)
	m.AI.nav.MoveToPosReach(m, float64(pos.X()), float64(pos.Y()), float64(pos.Z()), closeEnough, 1)
	path := m.AI.nav.Path()
	return path != nil && path.CanReach()
}

func (g *beeGoToHiveGoal) blacklistedHive(pos cube.Pos) bool {
	return slices.Contains(g.blacklisted, pos)
}

func (g *beeGoToHiveGoal) dropAndBlacklistHive() {
	if b := g.bee; b.hive != nil {
		g.blacklisted = append(g.blacklisted, *b.hive)
		if len(g.blacklisted) > 3 {
			g.blacklisted = g.blacklisted[len(g.blacklisted)-3:]
		}
	}
	g.bee.dropHive()
}

func (g *beeGoToHiveGoal) reachedTarget(m *Mob, pos cube.Pos) bool {
	if beeCloserThan(m, pos, 2) {
		return true
	}
	path := m.AI.nav.Path()
	return path != nil && path.Target() == pos && path.CanReach() && path.Done()
}

// beeGoToKnownFlowerGoal ports BeeGoToKnownFlowerGoal: a bee that went half a
// minute without nectar flies back to the flower it remembers.
type beeGoToKnownFlowerGoal struct {
	goalFlags
	bee             *beeBehaviour
	travellingTicks int
}

func (g *beeGoToKnownFlowerGoal) CanUse(m *Mob) bool {
	b := g.bee
	return !b.angry() && b.flower != nil && !m.AI.home.set() && b.ticksWithoutNectar > 600 && !beeCloserThan(m, *b.flower, 2)
}

func (g *beeGoToKnownFlowerGoal) CanContinueToUse(m *Mob) bool { return g.CanUse(m) }

func (g *beeGoToKnownFlowerGoal) Start(*Mob) { g.travellingTicks = 0 }

func (g *beeGoToKnownFlowerGoal) Stop(m *Mob) {
	g.travellingTicks = 0
	m.AI.nav.Stop()
	m.AI.nav.ResetMaxVisitedNodesMultiplier()
}

func (g *beeGoToKnownFlowerGoal) Tick(m *Mob) {
	b := g.bee
	if b.flower == nil {
		return
	}
	g.travellingTicks++
	switch {
	case g.travellingTicks > adjustedTickDelay(g, 2400):
		b.dropFlower(m)
	case m.AI.nav.InProgress():
	case tooFarAway(m, *b.flower):
		b.dropFlower(m)
	default:
		b.pathfindRandomlyTowards(m, *b.flower)
	}
}

// beeGrowCropGoal ports BeeGrowCropGoal: a bee carrying nectar now and then
// grows a crop one or two blocks below it, up to ten after pollinating.
type beeGrowCropGoal struct {
	goalFlags
	bee *beeBehaviour
}

func (g *beeGrowCropGoal) CanUse(m *Mob) bool {
	b := g.bee
	if b.angry() || b.crops >= 10 || m.Random().NextFloat() < .3 {
		return false
	}
	return b.hasNectar && b.hiveValid(m)
}

func (g *beeGrowCropGoal) CanContinueToUse(m *Mob) bool { return g.CanUse(m) }

func (g *beeGrowCropGoal) Tick(m *Mob) {
	if m.Random().NextInt(int32(adjustedTickDelay(g, 30))) != 0 {
		return
	}
	for i := 1; i <= 2; i++ {
		pos := m.BlockPos().Sub(cube.Pos{0, i, 0})
		current := m.tx.Block(pos)
		if !blockInTag(current, "bee_growables") {
			continue
		}
		var grown world.Block
		switch c := current.(type) {
		case block.WheatSeeds:
			if c.Growth < 7 {
				c.Growth++
				grown = c
			}
		case block.Carrot:
			if c.Growth < 7 {
				c.Growth++
				grown = c
			}
		case block.Potato:
			if c.Growth < 7 {
				c.Growth++
				grown = c
			}
		case block.BeetrootSeeds:
			if c.Growth < 3 {
				c.Growth++
				grown = c
			}
		case block.TorchflowerCrop:
			if c.Growth < 1 {
				c.Growth++
				grown = c
			}
		case block.PumpkinSeeds:
			if c.Growth < 7 {
				c.Growth++
				grown = c
			}
		case block.MelonSeeds:
			if c.Growth < 7 {
				c.Growth++
				grown = c
			}
		case block.SweetBerries:
			if c.Growth < 3 {
				c.Growth++
				grown = c
			}
		case block.CaveVines:
			if !c.HasBerries() {
				grown = c.WithBerries(pos, m.tx)
			}
		}
		if grown != nil {
			m.tx.AddParticle(pos.Vec3(), particle.BoneMeal{})
			m.tx.SetBlock(pos, grown, nil)
			g.bee.crops++
		}
	}
}

// beeHurtByOtherGoal ports BeeHurtByOtherGoal, which only keeps an angry
// bee after its attacker.
type beeHurtByOtherGoal struct {
	*HurtByTargetGoal
	bee *beeBehaviour
}

func (g beeHurtByOtherGoal) CanContinueToUse(m *Mob) bool {
	return g.bee.angry() && g.HurtByTargetGoal.CanContinueToUse(m)
}

// beeBecomeAngryTargetGoal ports BeeBecomeAngryTargetGoal: an angry bee that
// still has its stinger targets the player it is angry at.
type beeBecomeAngryTargetGoal struct {
	*NearestAttackableTargetGoal
	bee *beeBehaviour
}

func (g beeBecomeAngryTargetGoal) canTarget() bool { return g.bee.angry() && !g.bee.hasStung }

func (g beeBecomeAngryTargetGoal) CanUse(m *Mob) bool {
	return g.canTarget() && g.NearestAttackableTargetGoal.CanUse(m)
}

func (g beeBecomeAngryTargetGoal) CanContinueToUse(m *Mob) bool {
	if g.canTarget() && m.Target() != nil {
		return g.NearestAttackableTargetGoal.CanContinueToUse(m)
	}
	g.target = nil
	return false
}

// beeLocateHiveGoal ports BeeLocateHiveGoal: a bee without a hive that
// wants to go home takes the closest hive with room within 20 blocks it did
// not give up on.
type beeLocateHiveGoal struct {
	goalFlags
	bee *beeBehaviour
}

func (g *beeLocateHiveGoal) CanUse(m *Mob) bool {
	b := g.bee
	return !b.angry() && b.hiveCooldown == 0 && b.hive == nil && b.wantsToEnterHive(m)
}

func (g *beeLocateHiveGoal) CanContinueToUse(*Mob) bool { return false }

func (g *beeLocateHiveGoal) Start(m *Mob) {
	b := g.bee
	b.hiveCooldown = 200
	beePos := m.BlockPos()
	var hives []cube.Pos
	for pos, poi := range m.tx.POIsInRange(beePos, 20) {
		if poi.Type != "minecraft:beehive" && poi.Type != "minecraft:bee_nest" {
			continue
		}
		if home, ok := m.tx.Block(pos).(beeHome); ok && !home.IsFull() {
			hives = append(hives, pos)
		}
	}
	if len(hives) == 0 {
		return
	}
	distSqr := func(pos cube.Pos) int {
		d := pos.Sub(beePos)
		return d[0]*d[0] + d[1]*d[1] + d[2]*d[2]
	}
	slices.SortFunc(hives, func(a, c cube.Pos) int {
		if d := distSqr(a) - distSqr(c); d != 0 {
			return d
		}
		return comparePos(a, c)
	})
	for _, pos := range hives {
		if !b.goToHive.blacklistedHive(pos) {
			b.hive = &pos
			return
		}
	}
	b.goToHive.blacklisted = nil
	b.hive = &hives[0]
}

// comparePos orders block positions by x, then y, then z.
func comparePos(a, b cube.Pos) int {
	for i := range 3 {
		if a[i] != b[i] {
			return a[i] - b[i]
		}
	}
	return 0
}

// beePollinateGoal ports BeePollinateGoal: a bee without nectar flies to a
// flower within five blocks it can reach and hovers over it, gathering
// nectar after 20 seconds.
type beePollinateGoal struct {
	goalFlags
	bee                                              *beeBehaviour
	successfulTicks, lastSoundTick, pollinatingTicks int
	pollinating                                      bool
	hoverPos                                         *mgl64.Vec3
	unreachable                                      map[cube.Pos]int64
}

func (g *beePollinateGoal) CanUse(m *Mob) bool {
	b := g.bee
	if b.angry() || b.flowerCooldown > 0 || b.hasNectar || m.tx.Raining() {
		return false
	}
	pos, ok := g.findNearbyFlower(m)
	if !ok {
		b.flowerCooldown = 20 + int(m.Random().NextInt(41))
		return false
	}
	b.flower = &pos
	m.AI.nav.MoveToPos(m, float64(pos.X())+.5, float64(pos.Y())+.5, float64(pos.Z())+.5, 1.2)
	return true
}

func (g *beePollinateGoal) CanContinueToUse(m *Mob) bool {
	b := g.bee
	if b.angry() || !g.pollinating || b.flower == nil || m.tx.Raining() {
		return false
	}
	if g.successfulTicks > 400 {
		return m.Random().NextFloat() < .2
	}
	return true
}

func (g *beePollinateGoal) RequiresUpdateEveryTick() bool { return true }

func (g *beePollinateGoal) Start(*Mob) {
	g.successfulTicks, g.pollinatingTicks, g.lastSoundTick = 0, 0, 0
	g.pollinating = true
	g.bee.ticksWithoutNectar = 0
}

func (g *beePollinateGoal) Stop(m *Mob) {
	if g.successfulTicks > 400 {
		// Bee.setHasNectar.
		g.bee.hasNectar, g.bee.ticksWithoutNectar = true, 0
	}
	g.pollinating = false
	m.AI.nav.Stop()
	g.bee.flowerCooldown = 200
}

func (g *beePollinateGoal) Tick(m *Mob) {
	b := g.bee
	if b.flower == nil {
		return
	}
	g.pollinatingTicks++
	if g.pollinatingTicks > 600 {
		b.dropFlower(m)
		g.pollinating = false
		b.flowerCooldown = 200
		return
	}
	flowerPos := mgl64.Vec3{float64(b.flower.X()) + .5, float64(b.flower.Y()) + float64(float32(.6)), float64(b.flower.Z()) + .5}
	if flowerPos.Sub(m.data.Pos).Len() > 1 {
		g.hoverPos = &flowerPos
		g.setWantedPos(m)
		return
	}
	if g.hoverPos == nil {
		g.hoverPos = &flowerPos
	}
	arrived := m.data.Pos.Sub(*g.hoverPos).Len() <= .1
	if !arrived && g.pollinatingTicks > 600 {
		b.dropFlower(m)
		return
	}
	setWanted := true
	if arrived {
		r := m.Random()
		if r.NextInt(25) == 0 {
			// BeePollinateGoal.getOffset.
			offset := func() float64 { return float64((r.NextFloat()*2 - 1) * float32(1.0/3)) }
			x := flowerPos[0] + offset()
			hover := mgl64.Vec3{x, flowerPos[1], flowerPos[2] + offset()}
			g.hoverPos = &hover
			m.AI.nav.Stop()
		} else {
			setWanted = false
		}
		m.AI.look.SetLookAt(m, flowerPos[0], flowerPos[1], flowerPos[2])
	}
	if setWanted {
		g.setWantedPos(m)
	}
	g.successfulTicks++
	if m.Random().NextFloat() < .05 && g.successfulTicks > g.lastSoundTick+60 {
		g.lastSoundTick = g.successfulTicks
		m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.bee.pollinate", Volume: 1, Pitch: 1})
	}
}

func (g *beePollinateGoal) setWantedPos(m *Mob) {
	m.AI.move.SetWantedPosition(g.hoverPos[0], g.hoverPos[1], g.hoverPos[2], float64(float32(.35)))
}

// findNearbyFlower ports BeePollinateGoal.findNearbyFlower: the first flower
// by Manhattan distance within five blocks the bee finds a path to, skipping
// flowers it could not reach in the last half minute.
func (g *beePollinateGoal) findNearbyFlower(m *Mob) (cube.Pos, bool) {
	now := m.GameTime()
	cache := make(map[cube.Pos]int64, len(g.unreachable))
	found, ok := cube.Pos{}, false
	manhattanOrdered(m.BlockPos(), 5, 5, 5, 15, func(pos cube.Pos) bool {
		if !attractsBees(m.tx, pos) {
			return true
		}
		if until, ok := g.unreachable[pos]; ok && now < until {
			cache[pos] = until
			return true
		}
		if path := m.AI.nav.CreatePath(m, pos, 1); path != nil && path.CanReach() {
			found, ok = pos, true
			return false
		}
		cache[pos] = now + 600
		return true
	})
	g.unreachable = cache
	return found, ok
}

// manhattanOrdered ports BlockPos.manhattanOrdered: the blocks within reach of
// origin on each axis, by Manhattan distance up to maxDepth, until visit
// returns false.
func manhattanOrdered(origin cube.Pos, reachX, reachY, reachZ, maxDepth int, visit func(cube.Pos) bool) {
	for depth := 0; depth <= maxDepth; depth++ {
		maxX := min(reachX, depth)
		for x := -maxX; x <= maxX; x++ {
			maxY := min(reachY, depth-absInt(x))
			for y := -maxY; y <= maxY; y++ {
				z := depth - absInt(x) - absInt(y)
				if z > reachZ {
					continue
				}
				if !visit(origin.Add(cube.Pos{x, y, z})) {
					return
				}
				if z != 0 && !visit(origin.Add(cube.Pos{x, y, -z})) {
					return
				}
			}
		}
	}
}

// beeWanderGoal ports BeeWanderGoal: an idle bee flies to a spot ahead of
// it, or towards its hive when it strayed far from it.
type beeWanderGoal struct {
	goalFlags
	bee *beeBehaviour
}

func (g *beeWanderGoal) CanUse(m *Mob) bool { return m.AI.nav.IsDone() && m.Random().NextInt(10) == 0 }

func (g *beeWanderGoal) CanContinueToUse(m *Mob) bool { return m.AI.nav.InProgress() }

func (g *beeWanderGoal) Start(m *Mob) {
	b := g.bee
	dir := m.data.Rot.Vec3()
	threshold := 24.0
	if b.hive == nil && b.flower == nil {
		threshold = 32
	}
	if b.hiveValid(m) && !beeCloserThan(m, *b.hive, threshold) {
		dir = b.hive.Vec3Centre().Sub(m.data.Pos).Normalize()
	}
	pos, ok := HoverRandomPos(m, 8, 7, dir[0], dir[2], math.Pi/2, 3, 1)
	if !ok {
		pos, ok = AirAndWaterRandomPos(m, 8, 4, -2, dir[0], dir[2], math.Pi/2)
	}
	if ok {
		m.AI.nav.MoveTo(m, m.AI.nav.CreatePath(m, cube.PosFromVec3(pos), 1), 1)
	}
}

// beeValidateGoal ports ValidateHiveGoal and ValidateFlowerGoal: a calm bee
// checks every second or two that its hive or flower is still there.
type beeValidateGoal struct {
	goalFlags
	bee      *beeBehaviour
	cooldown int
	lastTick int64
	validate func(m *Mob)
}

func (g *beeValidateGoal) CanUse(m *Mob) bool {
	return !g.bee.angry() && m.GameTime() > g.lastTick+int64(g.cooldown)
}

func (g *beeValidateGoal) CanContinueToUse(*Mob) bool { return false }

func (g *beeValidateGoal) Start(m *Mob) {
	g.validate(m)
	g.lastTick = m.GameTime()
}

// validateHive drops a loaded hive that is gone.
func (b *beeBehaviour) validateHive(m *Mob) {
	if b.hive == nil {
		return
	}
	if _, loaded := m.tx.BlockLoaded(*b.hive); loaded && !b.hiveValid(m) {
		b.dropHive()
	}
}

// validateFlower drops a loaded flower that is gone.
func (b *beeBehaviour) validateFlower(m *Mob) {
	if b.flower == nil {
		return
	}
	if _, loaded := m.tx.BlockLoaded(*b.flower); loaded && !attractsBees(m.tx, *b.flower) {
		b.dropFlower(m)
	}
}
