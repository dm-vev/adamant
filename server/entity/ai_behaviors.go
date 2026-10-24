package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports the behaviours of net.minecraft.world.entity.ai.behavior
// that brains of many species share.

// uniformInt ports UniformInt: an integer between min and max inclusive.
type uniformInt struct{ min, max int }

func (u uniformInt) sample(m *Mob) int { return u.min + int(m.Random().NextInt(int32(u.max-u.min+1))) }

// newMoveToTargetSink ports MoveToTargetSink: a mob walks to its walk
// target, finding a path again when the target moves more than two blocks,
// and gives up for a while when it gets stuck.
func newMoveToTargetSink(minTimeout, maxTimeout int, extra func(m *Mob) bool) *behavior {
	var (
		remainingCooldown int
		path              *mobPath
		lastTargetPos     *cube.Pos
		speed             float64
	)
	reached := func(m *Mob, wt walkTarget) bool {
		return manhattan(wt.target.currentBlockPos(m.tx), m.BlockPos()) <= wt.closeEnough
	}
	tryComputePath := func(m *Mob, wt walkTarget, time int64) bool {
		b := m.Brain()
		target := wt.target.currentBlockPos(m.tx)
		path = m.AI.nav.CreatePath(m, target, 0)
		speed = wt.speed
		if reached(m, wt) {
			b.erase(memCantReachWalkTargetSince)
			return false
		}
		if path != nil && path.CanReach() {
			b.erase(memCantReachWalkTargetSince)
		} else if !b.has(memCantReachWalkTargetSince) {
			b.set(memCantReachWalkTargetSince, time)
		}
		if path != nil {
			return true
		}
		step, ok := DefaultRandomPosTowards(m, 10, 7, target.Vec3Middle(), math.Pi/2)
		if !ok {
			return false
		}
		path = m.AI.nav.CreatePath(m, cube.PosFromVec3(step), 0)
		return path != nil
	}
	start := func(m *Mob, _ int64) {
		m.Brain().set(memPath, path)
		m.AI.nav.MoveTo(m, path, speed)
	}
	return &behavior{
		entry:       []memoryCondition{{memCantReachWalkTargetSince, memRegistered}, {memPath, memAbsent}, {memWalkTarget, memPresent}},
		minDuration: minTimeout, maxDuration: maxTimeout,
		checkExtra: func(m *Mob) bool {
			if remainingCooldown > 0 {
				remainingCooldown--
				return false
			}
			if extra != nil && !extra(m) {
				return false
			}
			b := m.Brain()
			v, _ := b.get(memWalkTarget)
			wt := v.(walkTarget)
			r := reached(m, wt)
			if !r && tryComputePath(m, wt, m.tx.CurrentTick()) {
				pos := wt.target.currentBlockPos(m.tx)
				lastTargetPos = &pos
				return true
			}
			b.erase(memWalkTarget)
			if r {
				b.erase(memCantReachWalkTargetSince)
			}
			return false
		},
		canStillUse: func(m *Mob, _ int64) bool {
			if path == nil || lastTargetPos == nil {
				return false
			}
			v, ok := m.Brain().get(memWalkTarget)
			if !ok {
				return false
			}
			wt := v.(walkTarget)
			if t, ok := wt.target.(*entityTracker); ok {
				if e := t.entity(m.tx); e != nil && spectator(e) {
					return false
				}
			}
			return !m.AI.nav.IsDone() && !reached(m, wt)
		},
		stop: func(m *Mob, _ int64) {
			b := m.Brain()
			if v, ok := b.get(memWalkTarget); ok && !reached(m, v.(walkTarget)) && m.AI.nav.IsStuck() {
				remainingCooldown = int(m.Random().NextInt(40))
			}
			m.AI.nav.Stop()
			b.erase(memWalkTarget)
			b.erase(memPath)
			path = nil
		},
		start: start,
		tick: func(m *Mob, time int64) {
			b := m.Brain()
			if newPath := m.AI.nav.Path(); newPath != path {
				path = newPath
				b.set(memPath, newPath)
			}
			if path == nil || lastTargetPos == nil {
				return
			}
			v, ok := b.get(memWalkTarget)
			if !ok {
				return
			}
			wt := v.(walkTarget)
			if pos := wt.target.currentBlockPos(m.tx); distSqrPos(pos, *lastTargetPos) > 4 && tryComputePath(m, wt, time) {
				lastTargetPos = &pos
				start(m, time)
			}
		},
	}
}

// manhattan returns the Manhattan distance between two block positions.
func manhattan(a, b cube.Pos) int {
	return absInt(a[0]-b[0]) + absInt(a[1]-b[1]) + absInt(a[2]-b[2])
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// distSqrPos returns the squared distance between two block positions.
func distSqrPos(a, b cube.Pos) float64 {
	dx, dy, dz := float64(a[0]-b[0]), float64(a[1]-b[1]), float64(a[2]-b[2])
	return dx*dx + dy*dy + dz*dz
}

// newLookAtTargetSink ports LookAtTargetSink: a mob looks at its look target
// while it can see it.
func newLookAtTargetSink(minDuration, maxDuration int) *behavior {
	return &behavior{
		entry:       []memoryCondition{{memLookTarget, memPresent}},
		minDuration: minDuration, maxDuration: maxDuration,
		canStillUse: func(m *Mob, _ int64) bool {
			v, ok := m.Brain().get(memLookTarget)
			return ok && v.(positionTracker).visibleBy(m)
		},
		stop: func(m *Mob, _ int64) { m.Brain().erase(memLookTarget) },
		tick: func(m *Mob, _ int64) {
			if v, ok := m.Brain().get(memLookTarget); ok {
				p := v.(positionTracker).currentPosition(m.tx)
				m.AI.look.SetLookAt(m, p[0], p[1], p[2])
			}
		},
	}
}

// newSwim ports Swim: a mob in water deeper than its jump threshold, or in
// lava, jumps with a chance to stay afloat.
func newSwim(chance float64) *behavior {
	floating := func(m *Mob) bool {
		return m.AI.waterHeight > m.FluidJumpThreshold() || m.InLava()
	}
	return &behavior{
		checkExtra:  floating,
		canStillUse: func(m *Mob, _ int64) bool { return floating(m) },
		tick: func(m *Mob, _ int64) {
			if float64(m.Random().NextFloat()) < chance {
				m.AI.jump.Jump()
			}
		},
	}
}

// newAnimalPanic ports AnimalPanic: a mob hurt by what causes panics flees
// to random spots nearby for five to six seconds, or to water while it
// burns. position picks the spots, LandRandomPos if nil.
func newAnimalPanic(speed float64, causes func(world.DamageSource) bool, position func(m *Mob) (mgl64.Vec3, bool), onStart func(m *Mob)) *behavior {
	if causes == nil {
		causes = panicCause
	}
	if position == nil {
		position = func(m *Mob) (mgl64.Vec3, bool) { return LandRandomPos(m, 5, 4) }
	}
	return &behavior{
		entry:       []memoryCondition{{memIsPanicking, memRegistered}, {memHurtBy, memRegistered}},
		minDuration: 100, maxDuration: 120,
		checkExtra: func(m *Mob) bool {
			b := m.Brain()
			if v, ok := b.get(memHurtBy); ok && causes(v.(world.DamageSource)) {
				return true
			}
			return b.has(memIsPanicking)
		},
		canStillUse: func(*Mob, int64) bool { return true },
		start: func(m *Mob, _ int64) {
			if onStart != nil {
				onStart(m)
			}
			b := m.Brain()
			b.set(memIsPanicking, true)
			b.erase(memWalkTarget)
			m.AI.nav.Stop()
		},
		stop: func(m *Mob, _ int64) { m.Brain().erase(memIsPanicking) },
		tick: func(m *Mob, _ int64) {
			if !m.AI.nav.IsDone() {
				return
			}
			var to mgl64.Vec3
			ok := false
			if m.OnFireDuration() > 0 {
				var water cube.Pos
				if water, ok = panicWater(m.tx, m.BlockPos(), 5); ok {
					to = water.Vec3Middle()
				}
			}
			if !ok {
				to, ok = position(m)
			}
			if ok {
				m.Brain().set(memWalkTarget, walkTarget{target: newBlockPosTracker(cube.PosFromVec3(to)), speed: speed})
			}
		},
	}
}

// newCountDownCooldownTicks ports CountDownCooldownTicks: a cooldown memory
// counts down to zero and is forgotten.
func newCountDownCooldownTicks(mem memoryType) *behavior {
	return &behavior{
		entry:     []memoryCondition{{mem, memPresent}},
		noTimeout: true,
		canStillUse: func(m *Mob, _ int64) bool {
			v, ok := m.Brain().int(mem)
			return ok && v > 0
		},
		tick: func(m *Mob, _ int64) {
			v, _ := m.Brain().int(mem)
			m.Brain().set(mem, v-1)
		},
		stop: func(m *Mob, _ int64) { m.Brain().erase(mem) },
	}
}

// lookTicker ports SetEntityLookTargetSometimes.Ticker: it lets a behaviour
// start only every interval ticks.
type lookTicker struct {
	interval  uniformInt
	untilNext int
}

func (t *lookTicker) tickDownAndCheck(m *Mob) bool {
	if t.untilNext == 0 {
		t.untilNext = t.interval.sample(m) - 1
		return false
	}
	t.untilNext--
	return t.untilNext == 0
}

// newSetEntityLookTargetSometimes ports SetEntityLookTargetSometimes: now
// and then a mob looks at the nearest entity it sees within maxDist for
// which filter returns true.
func newSetEntityLookTargetSometimes(filter func(world.Entity) bool, maxDist float64, interval uniformInt) *oneShot {
	ticker := &lookTicker{interval: interval}
	return newOneShot([]memoryCondition{{memLookTarget, memAbsent}, {memNearestVisibleLivingEntities, memPresent}}, func(m *Mob, b *Brain, _ int64) bool {
		target := visibleLiving(m).findClosest(m, func(e world.Entity) bool {
			return (filter == nil || filter(e)) && m.DistanceToSqr(e) <= maxDist*maxDist
		})
		if target == nil || !ticker.tickDownAndCheck(m) {
			return false
		}
		b.set(memLookTarget, newEntityTracker(target, true))
		return true
	})
}

// newSetEntityLookTarget ports SetEntityLookTarget: a mob looks at the
// nearest entity it sees within maxDist for which filter returns true.
func newSetEntityLookTarget(filter func(world.Entity) bool, maxDist float64) *oneShot {
	return newOneShot([]memoryCondition{{memLookTarget, memAbsent}, {memNearestVisibleLivingEntities, memPresent}}, func(m *Mob, b *Brain, _ int64) bool {
		target := visibleLiving(m).findClosest(m, func(e world.Entity) bool {
			return (filter == nil || filter(e)) && m.DistanceToSqr(e) <= maxDist*maxDist
		})
		if target == nil {
			return false
		}
		b.set(memLookTarget, newEntityTracker(target, true))
		return true
	})
}

// isType returns a filter for entities of the type passed.
func isType(t world.EntityType) func(world.Entity) bool {
	return func(e world.Entity) bool { return e.H().Type() == t }
}

// isPlayerType is a filter for players.
func isPlayerType(e world.Entity) bool { return isPlayerEntity(e) }

// newAnimalMakeLove ports AnimalMakeLove: an animal in love walks to a
// partner of its kind it sees, the two look at each other and, after three
// to five seconds close together, breed.
func newAnimalMakeLove(partnerType world.EntityType, speed float64, closeEnough int) *behavior {
	var spawnChildAt int64
	find := func(m *Mob) breeder {
		self := m.Self.(breeder)
		e := visibleLiving(m).findClosest(m, func(e world.Entity) bool {
			p, ok := e.(breeder)
			return ok && e.H().Type() == partnerType && canMate(self, p) && !panicking(p)
		})
		if e == nil {
			return nil
		}
		return e.(breeder)
	}
	partnerOf := func(m *Mob) breeder {
		e := m.Brain().entity(m, memBreedTarget)
		if e == nil || e.H().Type() != partnerType {
			return nil
		}
		p, _ := e.(breeder)
		return p
	}
	return &behavior{
		entry: []memoryCondition{{memNearestVisibleLivingEntities, memPresent}, {memBreedTarget, memAbsent}, {memWalkTarget, memRegistered},
			{memLookTarget, memRegistered}, {memIsPanicking, memAbsent}},
		minDuration: 110, maxDuration: 110,
		checkExtra: func(m *Mob) bool { return m.Self.(breeder).InLove() && find(m) != nil },
		start: func(m *Mob, time int64) {
			p := find(m)
			m.Brain().setEntity(memBreedTarget, p)
			if pm, ok := brainMobOf(p); ok {
				pm.Brain().setEntity(memBreedTarget, m.Self)
			}
			lockGazeAndWalkToEachOther(m, p, speed, closeEnough)
			spawnChildAt = time + 60 + int64(m.Random().NextInt(50))
		},
		canStillUse: func(m *Mob, time int64) bool {
			p := partnerOf(m)
			self := m.Self.(breeder)
			return p != nil && !p.Dead() && canMate(self, p) && brainCanSee(m, p) && time <= spawnChildAt && !panicking(self) && !panicking(p)
		},
		tick: func(m *Mob, time int64) {
			p := partnerOf(m)
			lockGazeAndWalkToEachOther(m, p, speed, closeEnough)
			if m.DistanceToSqr(p) < 9 && time >= spawnChildAt {
				breed(m.Self.(breeder), p, m.tx)
				m.Brain().erase(memBreedTarget)
				if pm, ok := brainMobOf(p); ok {
					pm.Brain().erase(memBreedTarget)
				}
			}
		},
		stop: func(m *Mob, _ int64) {
			b := m.Brain()
			b.erase(memBreedTarget)
			b.erase(memWalkTarget)
			b.erase(memLookTarget)
			spawnChildAt = 0
		},
	}
}

// lockGazeAndWalkToEachOther ports BehaviorUtils.lockGazeAndWalkToEachOther.
func lockGazeAndWalkToEachOther(m *Mob, other world.Entity, speed float64, closeEnough int) {
	m.Brain().set(memLookTarget, newEntityTracker(other, true))
	setWalkAndLookTarget(m, newEntityTracker(other, true), speed, closeEnough)
	if om, ok := brainMobOf(other); ok {
		om.Brain().set(memLookTarget, newEntityTracker(m.Self, true))
		setWalkAndLookTarget(om, newEntityTracker(m.Self, true), speed, closeEnough)
	}
}

// newFollowTemptation ports FollowTemptation: a mob follows the player
// tempting it until it is closeEnough, and then waits five seconds before
// it may be tempted again.
func newFollowTemptation(speed func(m *Mob) float64, closeEnough func(m *Mob) float64, lookInTheEyes bool) *behavior {
	if closeEnough == nil {
		closeEnough = func(*Mob) float64 { return 2.5 }
	}
	return &behavior{
		entry: []memoryCondition{{memLookTarget, memRegistered}, {memWalkTarget, memRegistered}, {memTemptationCooldownTicks, memAbsent},
			{memTemptingPlayer, memPresent}, {memBreedTarget, memAbsent}, {memIsPanicking, memAbsent}},
		noTimeout: true,
		canStillUse: func(m *Mob, _ int64) bool {
			b := m.Brain()
			return b.has(memTemptingPlayer) && !b.has(memBreedTarget) && !b.has(memIsPanicking)
		},
		stop: func(m *Mob, _ int64) {
			b := m.Brain()
			b.set(memTemptationCooldownTicks, 100)
			b.erase(memWalkTarget)
			b.erase(memLookTarget)
		},
		tick: func(m *Mob, _ int64) {
			b := m.Brain()
			p := b.entity(m, memTemptingPlayer)
			if p == nil {
				return
			}
			b.set(memLookTarget, newEntityTracker(p, true))
			if d := closeEnough(m); m.DistanceToSqr(p) < d*d {
				b.erase(memWalkTarget)
				return
			}
			t := newEntityTracker(p, lookInTheEyes)
			t.targetEye = lookInTheEyes
			b.set(memWalkTarget, walkTarget{target: t, speed: speed(m), closeEnough: 2})
		},
	}
}

// newBabyFollowAdult ports BabyFollowAdult: a baby walks to the nearest
// adult it remembers in the memory passed while it is within followRange
// but not closer than its minimum.
func newBabyFollowAdult(followRange uniformInt, speed func(m *Mob) float64, nearest memoryType, targetEye bool) *oneShot {
	return newOneShot([]memoryCondition{{nearest, memPresent}, {memLookTarget, memRegistered}, {memWalkTarget, memAbsent}}, func(m *Mob, b *Brain, _ int64) bool {
		if !mobBaby(m.Self) {
			return false
		}
		adult := b.entity(m, nearest)
		if adult == nil {
			return false
		}
		maxD, minD := float64(followRange.max+1), float64(followRange.min)
		if d := m.DistanceToSqr(adult); d >= maxD*maxD || d < minD*minD {
			return false
		}
		walk := newEntityTracker(adult, targetEye)
		walk.targetEye = targetEye
		look := newEntityTracker(adult, true)
		look.targetEye = targetEye
		b.set(memLookTarget, look)
		b.set(memWalkTarget, walkTarget{target: walk, speed: speed(m), closeEnough: followRange.min - 1})
		return true
	})
}

// mobBaby reports if the entity is a baby.
func mobBaby(e world.Entity) bool {
	b, ok := e.(interface{ Baby() bool })
	return ok && b.Baby()
}

// newRandomLookAroundBehavior ports the RandomLookAround behaviour: a mob
// with nothing to look at looks somewhere random now and then.
func newRandomLookAroundBehavior(interval uniformInt, maxYaw, minPitch, maxPitch float64) *behavior {
	return &behavior{
		entry: []memoryCondition{{memLookTarget, memAbsent}, {memGazeCooldownTicks, memAbsent}},
		start: func(m *Mob, _ int64) {
			r := m.Random()
			pitch := math.Max(-90, math.Min(90, float64(r.NextFloat())*(maxPitch-minPitch)+minPitch))
			yaw := wrapDegrees(m.Yaw() + 2*float64(r.NextFloat())*maxYaw - maxYaw)
			dir := cube.Rotation{yaw, pitch}.Vec3()
			eye := mgl64.Vec3{m.data.Pos[0], m.EyeY(), m.data.Pos[2]}
			b := m.Brain()
			b.set(memLookTarget, newVecTracker(eye.Add(dir)))
			b.set(memGazeCooldownTicks, interval.sample(m))
		},
	}
}

// newRandomStroll ports RandomStroll.stroll: a mob without a walk target
// walks to a random spot on land within ten blocks, unless it is in water
// and may not stroll from it.
func newRandomStroll(speed float64, mayStrollFromWater bool) *oneShot {
	return newStrollFlyOrSwim(speed, func(m *Mob) (mgl64.Vec3, bool) { return LandRandomPos(m, 10, 7) }, func(m *Mob) bool {
		return mayStrollFromWater || !m.InWater()
	})
}

// newRandomStrollWithin ports RandomStroll.stroll with distances.
func newRandomStrollWithin(speed float64, horizontal, vertical int) *oneShot {
	return newStrollFlyOrSwim(speed, func(m *Mob) (mgl64.Vec3, bool) { return LandRandomPos(m, horizontal, vertical) }, nil)
}

// newRandomFly ports RandomStroll.fly.
func newRandomFly(speed float64) *oneShot {
	return newStrollFlyOrSwim(speed, func(m *Mob) (mgl64.Vec3, bool) {
		dir := m.data.Rot.Vec3()
		return AirAndWaterRandomPos(m, 10, 7, -2, dir[0], dir[2], math.Pi/2)
	}, nil)
}

// newRandomSwim ports RandomStroll.swim: a mob in water swims to a spot in
// water as far as it reaches, tier by tier.
func newRandomSwim(speed float64) *oneShot {
	return newStrollFlyOrSwim(speed, randomStrollSwimPos, func(m *Mob) bool { return m.InWater() })
}

var swimDistanceTiers = [][2]int{{1, 1}, {3, 3}, {5, 5}, {6, 5}, {7, 7}, {10, 7}}

// randomStrollSwimPos ports RandomStroll.getTargetSwimPos.
func randomStrollSwimPos(m *Mob) (mgl64.Vec3, bool) {
	var fallback, target mgl64.Vec3
	hasFallback := false
	for _, d := range swimDistanceTiers {
		ok := true
		if !hasFallback {
			target, ok = randomSwimmablePosWithin(m, d[0], d[1])
		} else {
			dir := fallback.Sub(m.data.Pos)
			if dir.LenSqr() > 0 {
				dir = dir.Normalize()
			}
			target = m.data.Pos.Add(mgl64.Vec3{dir[0] * float64(d[0]), dir[1] * float64(d[1]), dir[2] * float64(d[0])})
		}
		if _, liquid := m.tx.Liquid(cube.PosFromVec3(target)); !ok || !liquid || !m.WithinHome(cube.PosFromVec3(target)) {
			return fallback, hasFallback
		}
		fallback, hasFallback = target, true
	}
	return target, true
}

func newStrollFlyOrSwim(speed float64, pos func(m *Mob) (mgl64.Vec3, bool), canRun func(m *Mob) bool) *oneShot {
	return newOneShot([]memoryCondition{{memWalkTarget, memAbsent}}, func(m *Mob, b *Brain, _ int64) bool {
		if canRun != nil && !canRun(m) {
			return false
		}
		if p, ok := pos(m); ok {
			b.set(memWalkTarget, walkTarget{target: newBlockPosTracker(cube.PosFromVec3(p)), speed: speed})
		} else {
			b.erase(memWalkTarget)
		}
		return true
	})
}

// newSetWalkTargetFromLookTarget ports SetWalkTargetFromLookTarget: a mob
// walks to what it looks at.
func newSetWalkTargetFromLookTarget(can func(m *Mob) bool, speed func(m *Mob) float64, closeEnough int) *oneShot {
	return newOneShot([]memoryCondition{{memWalkTarget, memAbsent}, {memLookTarget, memPresent}}, func(m *Mob, b *Brain, _ int64) bool {
		if can != nil && !can(m) {
			return false
		}
		v, _ := b.get(memLookTarget)
		b.set(memWalkTarget, walkTarget{target: v.(positionTracker), speed: speed(m), closeEnough: closeEnough})
		return true
	})
}

// constSpeed returns a speed function that always returns v.
func constSpeed(v float64) func(*Mob) float64 { return func(*Mob) float64 { return v } }

// newOneShotIf ports BehaviorBuilder.triggerIf: a OneShot that runs when the
// predicate holds.
func newOneShotIf(pred func(m *Mob) bool) *oneShot {
	return newOneShot(nil, func(m *Mob, _ *Brain, _ int64) bool { return pred(m) })
}

// newStartAttacking ports StartAttacking: a mob that may attack starts
// attacking the target the finder returns.
func newStartAttacking(can func(m *Mob) bool, finder func(m *Mob) world.Entity) *oneShot {
	return newOneShot([]memoryCondition{{memAttackTarget, memAbsent}, {memCantReachWalkTargetSince, memRegistered}}, func(m *Mob, b *Brain, _ int64) bool {
		if can != nil && !can(m) {
			return false
		}
		target := finder(m)
		if target == nil || !m.CanAttack(target) {
			return false
		}
		b.setEntity(memAttackTarget, target)
		b.erase(memCantReachWalkTargetSince)
		return true
	})
}

// newStopAttackingIfTargetInvalid ports StopAttackingIfTargetInvalid: a mob
// forgets a target it may no longer attack, that died or left, that stop
// picks out or, if it may grow tired, that it could not reach for ten
// seconds. onErased runs when it does.
func newStopAttackingIfTargetInvalid(stop func(m *Mob, target world.Entity) bool, onErased func(m *Mob, target world.Entity), canGrowTired bool) *oneShot {
	return newOneShot([]memoryCondition{{memAttackTarget, memPresent}, {memCantReachWalkTargetSince, memRegistered}}, func(m *Mob, b *Brain, time int64) bool {
		target := b.entity(m, memAttackTarget)
		tired := false
		if canGrowTired {
			if v, ok := b.get(memCantReachWalkTargetSince); ok {
				tired = time-v.(int64) > 200
			}
		}
		if target != nil && m.CanAttack(target) && !tired && entityAlive(target) && (stop == nil || !stop(m, target)) {
			return true
		}
		if onErased != nil && target != nil {
			onErased(m, target)
		}
		b.erase(memAttackTarget)
		return true
	})
}

// findManhattan ports Level.findBlocksInBoxByManhattanDistance with a
// filter: the first position within r of origin, nearest first, for which
// match returns true.
func findManhattan(origin cube.Pos, r int, match func(cube.Pos) bool) (cube.Pos, bool) {
	var found cube.Pos
	ok := false
	manhattanOrdered(origin, r, r, r, 3*r, func(p cube.Pos) bool {
		if match(p) {
			found, ok = p, true
			return false
		}
		return true
	})
	return found, ok
}

// collisionEmpty reports if the block at pos has no collision shape.
func collisionEmpty(tx *world.Tx, pos cube.Pos) bool {
	return len(tx.Block(pos).Model().BBox(pos, tx)) == 0
}

// newTryFindLand ports TryFindLand: a mob in water walks, every three
// seconds, to the nearest spot within range out of water above a sturdy
// block.
func newTryFindLand(r int, speed float64) *oneShot {
	var nextOK int64
	return newOneShot([]memoryCondition{{memAttackTarget, memAbsent}, {memWalkTarget, memAbsent}, {memLookTarget, memRegistered}}, func(m *Mob, b *Brain, time int64) bool {
		at := m.BlockPos()
		if !pathWater(m.tx, at) {
			return false
		}
		if time < nextOK {
			nextOK = time + 60
			return true
		}
		if p, ok := findManhattan(at, r, func(p cube.Pos) bool {
			if p[0] == at[0] && p[2] == at[2] {
				return false
			}
			if _, liquid := m.tx.Liquid(p); liquid {
				return false
			}
			below := p.Side(cube.FaceDown)
			return collisionEmpty(m.tx, p) && m.tx.Block(below).Model().FaceSolid(below, cube.FaceUp, m.tx)
		}); ok {
			b.set(memLookTarget, newBlockPosTracker(p))
			b.set(memWalkTarget, walkTarget{target: newBlockPosTracker(p), speed: speed, closeEnough: 1})
		}
		nextOK = time + 60
		return true
	})
}

// newTryFindLandNearLiquid ports TryFindLandNearLiquid: a mob out of water
// walks, every two seconds, to the nearest spot within range on a block
// beside air over water.
func newTryFindLandNearLiquid(r int, speed float64) *oneShot {
	var nextOK int64
	return newOneShot([]memoryCondition{{memAttackTarget, memAbsent}, {memWalkTarget, memAbsent}, {memLookTarget, memRegistered}}, func(m *Mob, b *Brain, time int64) bool {
		at := m.BlockPos()
		if pathWater(m.tx, at) {
			return false
		}
		if time < nextOK {
			nextOK = time + 40
			return true
		}
		if p, ok := findManhattan(at, r, func(p cube.Pos) bool {
			if p[0] == at[0] && p[2] == at[2] || !collisionEmpty(m.tx, p) || collisionEmpty(m.tx, p.Side(cube.FaceDown)) {
				return false
			}
			for _, f := range cube.HorizontalFaces() {
				side := p.Side(f)
				if isAir(m.tx.Block(side)) && pathWater(m.tx, side.Side(cube.FaceDown)) {
					return true
				}
			}
			return false
		}); ok {
			b.set(memLookTarget, newBlockPosTracker(p))
			b.set(memWalkTarget, walkTarget{target: newBlockPosTracker(p), speed: speed})
		}
		nextOK = time + 40
		return true
	})
}

// newEraseMemoryIf ports EraseMemoryIf: a mob forgets a memory once pred
// holds.
func newEraseMemoryIf(pred func(m *Mob) bool, mem memoryType) *oneShot {
	return newOneShot([]memoryCondition{{mem, memPresent}}, func(m *Mob, b *Brain, _ int64) bool {
		if !pred(m) {
			return false
		}
		b.erase(mem)
		return true
	})
}

// isBreeding ports BehaviorUtils.isBreeding: the mob has a partner to breed
// with.
func isBreeding(m *Mob) bool { return m.Brain().has(memBreedTarget) }

// newSetWalkTargetFromAttackTargetIfTargetOutOfReach ports the behaviour of
// that name: a mob walks to its attack target until it is within reach and
// in sight.
func newSetWalkTargetFromAttackTargetIfTargetOutOfReach(speed func(m *Mob) float64) *oneShot {
	return newOneShot([]memoryCondition{{memWalkTarget, memRegistered}, {memLookTarget, memRegistered}, {memAttackTarget, memPresent},
		{memNearestVisibleLivingEntities, memRegistered}}, func(m *Mob, b *Brain, _ int64) bool {
		target := b.entity(m, memAttackTarget)
		if target == nil {
			return false
		}
		if v := visibleLiving(m); v != nil && v.contains(m, target) && m.WithinMeleeAttackRange(target) {
			b.erase(memWalkTarget)
			return true
		}
		b.set(memLookTarget, newEntityTracker(target, true))
		b.set(memWalkTarget, walkTarget{target: newEntityTracker(target, false), speed: speed(m)})
		return true
	})
}

// newMeleeAttack ports MeleeAttack: a mob hits its attack target in reach
// and in sight, and then waits cooldown ticks.
func newMeleeAttack(cooldown int) *oneShot {
	return newOneShot([]memoryCondition{{memLookTarget, memRegistered}, {memAttackTarget, memPresent}, {memAttackCoolingDown, memAbsent},
		{memNearestVisibleLivingEntities, memPresent}}, func(m *Mob, b *Brain, _ int64) bool {
		target := b.entity(m, memAttackTarget)
		if target == nil || !m.WithinMeleeAttackRange(target) || !visibleLiving(m).contains(m, target) {
			return false
		}
		b.set(memLookTarget, newEntityTracker(target, true))
		m.swing()
		m.doHurtTarget(target)
		b.setWithExpiry(memAttackCoolingDown, true, int64(cooldown))
		return true
	})
}

// newTryFindLiquid ports TryFindLiquid for water: a mob out of water walks,
// every two seconds, to the nearest water within range, open to the sky if
// it finds any.
func newTryFindLiquid(r int, speed float64) *oneShot {
	var nextOK int64
	return newOneShot([]memoryCondition{{memAttackTarget, memAbsent}, {memWalkTarget, memAbsent}, {memLookTarget, memRegistered}}, func(m *Mob, b *Brain, time int64) bool {
		at := m.BlockPos()
		if pathWater(m.tx, at) {
			return false
		}
		if time < nextOK {
			nextOK = time + 22
			return true
		}
		var found *cube.Pos
		manhattanOrdered(at, r, r, r, 3*r, func(p cube.Pos) bool {
			if p[0] == at[0] && p[2] == at[2] {
				return true
			}
			if _, ok := m.tx.Block(p).(block.Water); !ok {
				return true
			}
			if isAir(m.tx.Block(p.Side(cube.FaceUp))) {
				found = &p
				return false
			}
			if found == nil && p.Vec3Centre().Sub(m.data.Pos).LenSqr() >= 1.5*1.5 {
				found = &p
			}
			return true
		})
		if found != nil {
			b.set(memLookTarget, newBlockPosTracker(*found))
			b.set(memWalkTarget, walkTarget{target: newBlockPosTracker(*found), speed: speed})
		}
		nextOK = time + 40
		return true
	})
}

// newTriggerIf ports BehaviorBuilder.triggerIf with a behaviour: inner runs
// only while pred holds.
func newTriggerIf(pred func(m *Mob) bool, inner *oneShot) *oneShot {
	trigger := inner.trigger
	inner.trigger = func(m *Mob, time int64) bool { return pred(m) && trigger(m, time) }
	return inner
}

// newGoToWantedItem ports GoToWantedItem: a mob that may pick up loot goes
// for the nearest item it wants within maxDist, breaking off what it walks
// to if interrupt.
func newGoToWantedItem(pred func(m *Mob) bool, speed float64, interrupt bool, maxDist float64, canPickUpLoot func() bool) *oneShot {
	walk := memoryCondition{memWalkTarget, memAbsent}
	if interrupt {
		walk.status = memRegistered
	}
	return newOneShot([]memoryCondition{{memLookTarget, memRegistered}, walk, {memNearestVisibleWantedItem, memPresent}, {memItemPickupCooldownTicks, memRegistered}}, func(m *Mob, b *Brain, _ int64) bool {
		it := b.entity(m, memNearestVisibleWantedItem)
		if it == nil || b.has(memItemPickupCooldownTicks) || pred != nil && !pred(m) {
			return false
		}
		if it.Position().Sub(m.data.Pos).LenSqr() >= maxDist*maxDist || !m.tx.World().WithinBorder(cube.PosFromVec3(it.Position())) || !canPickUpLoot() {
			return false
		}
		b.set(memLookTarget, newEntityTracker(it, true))
		b.set(memWalkTarget, walkTarget{target: newEntityTracker(it, false), speed: speed})
		return true
	})
}

// newGoAndGiveItemsToTarget ports GoAndGiveItemsToTarget: a mob with items
// to give walks to where it gives them and throws them once within three
// blocks of its eyes, waiting a while before it picks up more.
func newGoAndGiveItemsToTarget(target func(m *Mob) (positionTracker, bool), speed float64, timeout int, throw func(m *Mob, at mgl64.Vec3), cooldown memoryType, cooldownTicks int, hasItem func(m *Mob) bool) *behavior {
	canThrow := func(m *Mob) bool {
		if !hasItem(m) {
			return false
		}
		_, ok := target(m)
		return ok
	}
	return &behavior{
		entry:       []memoryCondition{{memLookTarget, memRegistered}, {memWalkTarget, memRegistered}, {cooldown, memRegistered}},
		minDuration: timeout, maxDuration: timeout,
		checkExtra:  canThrow,
		canStillUse: func(m *Mob, _ int64) bool { return canThrow(m) },
		start: func(m *Mob, _ int64) {
			if t, ok := target(m); ok {
				setWalkAndLookTarget(m, t, speed, 3)
			}
		},
		tick: func(m *Mob, _ int64) {
			t, ok := target(m)
			if !ok {
				return
			}
			at := t.currentPosition(m.tx)
			if at.Sub(mgl64.Vec3{m.data.Pos[0], m.EyeY(), m.data.Pos[2]}).Len() < 3 {
				throw(m, at)
				m.Brain().set(cooldown, cooldownTicks)
			}
		},
	}
}

// newStayCloseToTarget ports StayCloseToTarget: a mob further than tooFar
// from its target walks back to within closeEnough of it.
func newStayCloseToTarget(target func(m *Mob) (positionTracker, bool), shouldRun func(m *Mob) bool, closeEnough, tooFar int, speed float64) *oneShot {
	return newOneShot([]memoryCondition{{memLookTarget, memRegistered}, {memWalkTarget, memRegistered}}, func(m *Mob, b *Brain, _ int64) bool {
		t, ok := target(m)
		if !ok || !shouldRun(m) {
			return false
		}
		if t.currentPosition(m.tx).Sub(m.data.Pos).LenSqr() < float64(tooFar*tooFar) {
			return false
		}
		b.set(memLookTarget, t)
		b.set(memWalkTarget, walkTarget{target: t, speed: speed, closeEnough: closeEnough})
		return true
	})
}
