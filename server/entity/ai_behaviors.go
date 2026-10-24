package entity

import (
	"math"

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
