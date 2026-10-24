package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports the long jumps of goats and frogs: LongJumpToRandomPos,
// LongJumpMidJump and LongJumpUtil.

// longJumper is implemented by the behaviours of mobs that long jump, which
// show their pose to viewers and shrink while they fly.
type longJumper interface {
	setLongJumping(m *Mob, v bool)
}

// newLongJumpMidJump ports LongJumpMidJump: a mob in a long jump flies
// without friction until it lands, slows down and waits a while before it
// jumps again.
func newLongJumpMidJump(between uniformInt, landingSound string) *behavior {
	return &behavior{
		entry:       []memoryCondition{{memLookTarget, memRegistered}, {memLongJumpMidJump, memPresent}},
		minDuration: 100, maxDuration: 100,
		canStillUse: func(m *Mob, _ int64) bool { return !m.OnGround() },
		start: func(m *Mob, _ int64) {
			m.AI.DiscardFriction = true
			if j, ok := m.Behaviour().(longJumper); ok {
				j.setLongJumping(m, true)
			}
		},
		stop: func(m *Mob, _ int64) {
			if m.OnGround() {
				m.data.Vel[0] *= float64(float32(.1))
				m.data.Vel[2] *= float64(float32(.1))
				m.tx.PlaySound(m.data.Pos, sound.Custom{Name: landingSound, Volume: 2, Pitch: 1})
			}
			m.AI.DiscardFriction = false
			if j, ok := m.Behaviour().(longJumper); ok {
				j.setLongJumping(m, false)
			}
			b := m.Brain()
			b.erase(memLongJumpMidJump)
			b.set(memLongJumpCooldownTicks, between.sample(m))
		},
	}
}

// possibleJump ports LongJumpToRandomPos.PossibleJump.
type possibleJump struct {
	pos    cube.Pos
	weight int
}

// newLongJumpToRandomPos ports LongJumpToRandomPos: a mob on the ground looks
// for a spot within maxWidth blocks and maxHeight up or down it cannot walk
// to within eight blocks but can jump to without hitting anything, looks at
// it for two seconds and leaps. acceptable decides where it may land,
// defaultLongJumpLanding if nil.
func newLongJumpToRandomPos(between uniformInt, maxHeight, maxWidth int, maxVelocityMultiplier float64, jumpSound func(m *Mob) string, acceptable func(m *Mob, pos cube.Pos) bool) *behavior {
	return newLongJumpToPreferredBlock(between, maxHeight, maxWidth, maxVelocityMultiplier, jumpSound, nil, 0, acceptable)
}

// newLongJumpToPreferredBlock ports LongJumpToPreferredBlock: as
// LongJumpToRandomPos, but with a chance of preferredChance a mob first tries
// the spots above the blocks preferred picks out.
func newLongJumpToPreferredBlock(between uniformInt, maxHeight, maxWidth int, maxVelocityMultiplier float64, jumpSound func(m *Mob) string, preferred func(world.Block) bool, preferredChance float64, acceptable func(m *Mob, pos cube.Pos) bool) *behavior {
	if acceptable == nil {
		acceptable = defaultLongJumpLanding
	}
	var (
		candidates    []possibleJump
		initial       *mgl64.Vec3
		chosen        *mgl64.Vec3
		findTries     int
		prepareStart  int64
		allowedAngles = []int{65, 70, 75, 80}
		notPreferred  []possibleJump
		wanting       bool
	)
	pick := func(m *Mob) (possibleJump, bool) {
		if !wanting {
			if len(candidates) == 0 {
				return possibleJump{}, false
			}
			return pickWeightedJump(m, &candidates), true
		}
		for len(candidates) > 0 {
			c := pickWeightedJump(m, &candidates)
			if preferred(m.tx.Block(c.pos.Side(cube.FaceDown))) {
				return c, true
			}
			notPreferred = append(notPreferred, c)
		}
		if len(notPreferred) == 0 {
			return possibleJump{}, false
		}
		c := notPreferred[0]
		notPreferred = notPreferred[1:]
		return c, true
	}
	cooldown := func(m *Mob) { m.Brain().set(memLongJumpCooldownTicks, between.sample(m)/2) }
	return &behavior{
		entry:       []memoryCondition{{memLookTarget, memRegistered}, {memLongJumpCooldownTicks, memAbsent}, {memLongJumpMidJump, memAbsent}},
		minDuration: 200, maxDuration: 200,
		checkExtra: func(m *Mob) bool {
			_, honey := m.tx.Block(m.BlockPos()).(block.HoneyBlock)
			ok := m.OnGround() && !m.InWater() && !m.InLava() && !honey
			if !ok {
				cooldown(m)
			}
			return ok
		},
		canStillUse: func(m *Mob, _ int64) bool {
			valid := initial != nil && *initial == m.data.Pos && findTries > 0 && !m.InWater() && (chosen != nil || len(candidates) > 0)
			if !valid && !m.Brain().has(memLongJumpMidJump) {
				cooldown(m)
				m.Brain().erase(memLookTarget)
			}
			return valid
		},
		start: func(m *Mob, _ int64) {
			chosen, findTries = nil, 20
			notPreferred = notPreferred[:0]
			wanting = preferred != nil && float64(m.Random().NextFloat()) < preferredChance
			pos := m.data.Pos
			initial = &pos
			at := m.BlockPos()
			candidates = candidates[:0]
			for x := at[0] - maxWidth; x <= at[0]+maxWidth; x++ {
				for y := at[1] - maxHeight; y <= at[1]+maxHeight; y++ {
					for z := at[2] - maxWidth; z <= at[2]+maxWidth; z++ {
						p := cube.Pos{x, y, z}
						if p != at {
							candidates = append(candidates, possibleJump{p, int(math.Ceil(distSqrPos(at, p)))})
						}
					}
				}
			}
		},
		tick: func(m *Mob, time int64) {
			if chosen != nil {
				if time-prepareStart >= 40 {
					m.SetYaw(m.AI.bodyYaw)
					m.AI.DiscardFriction = true
					length := chosen.Len()
					m.data.Vel = chosen.Mul((length + jumpBoostPower(m)) / length)
					m.Brain().set(memLongJumpMidJump, true)
					m.tx.PlaySound(m.data.Pos, sound.Custom{Name: jumpSound(m), Volume: 1, Pitch: 1})
				}
				return
			}
			findTries--
			// A mob wanting a preferred spot takes every candidate off the
			// list at once, so it tries at most one it does not prefer.
			for len(candidates) > 0 {
				c, ok := pick(m)
				if !ok {
					break
				}
				if c.pos[0] == m.BlockPos()[0] && c.pos[2] == m.BlockPos()[2] || !acceptable(m, c.pos) {
					continue
				}
				target := c.pos.Vec3Centre()
				jump, ok := optimalJumpVector(m, target, m.AI.JumpStrength*maxVelocityMultiplier, allowedAngles)
				if !ok {
					continue
				}
				m.Brain().set(memLookTarget, newBlockPosTracker(c.pos))
				if path := m.AI.nav.CreatePathWithLength(m, c.pos, 0, 8); path == nil || !path.CanReach() {
					chosen, prepareStart = &jump, time
					return
				}
			}
		},
	}
}

// pickWeightedJump ports WeightedRandom.getRandomItem: a candidate picked by
// weight and taken off the list.
func pickWeightedJump(m *Mob, candidates *[]possibleJump) possibleJump {
	total := 0
	for _, c := range *candidates {
		total += c.weight
	}
	i := 0
	if total > 0 {
		r := int(m.Random().NextInt(int32(total)))
		for j, c := range *candidates {
			if r -= c.weight; r < 0 {
				i = j
				break
			}
		}
	}
	c := (*candidates)[i]
	*candidates = append((*candidates)[:i], (*candidates)[i+1:]...)
	return c
}

// defaultLongJumpLanding ports LongJumpToRandomPos.defaultAcceptableLandingSpot:
// a spot above a solid block the mob would walk through freely.
func defaultLongJumpLanding(m *Mob, pos cube.Pos) bool {
	below := pos.Side(cube.FaceDown)
	return world.RedstoneFullPowerConductor(below, m.tx.Block(below), m.tx) && m.PathfindingMalus(pathTypeStatic(m.tx, pos)) == 0
}

// optimalJumpVector ports LongJumpToRandomPos.calculateOptimalJumpVector: the
// velocity of a jump to the target at one of the angles passed, tried in a
// random order.
func optimalJumpVector(m *Mob, target mgl64.Vec3, maxVelocity float64, angles []int) (mgl64.Vec3, bool) {
	shuffled := append([]int(nil), angles...)
	r := m.Random()
	for i := len(shuffled) - 1; i > 0; i-- {
		j := int(r.NextInt(int32(i + 1)))
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}
	for _, angle := range shuffled {
		if v, ok := jumpVectorForAngle(m, target, maxVelocity, angle, true); ok {
			return v, true
		}
	}
	return mgl64.Vec3{}, false
}

// jumpVectorForAngle ports LongJumpUtil.calculateJumpVectorForAngle: the
// velocity of a jump at the angle that lands half a block short of the
// target, if it is no faster than maxVelocity and, with checkCollision, its
// arc is clear.
func jumpVectorForAngle(m *Mob, target mgl64.Vec3, maxVelocity float64, angle int, checkCollision bool) (mgl64.Vec3, bool) {
	pos := m.data.Pos
	plane := mgl64.Vec3{target[0] - pos[0], 0, target[2] - pos[2]}
	if plane.LenSqr() > 0 {
		plane = plane.Normalize().Mul(.5)
	}
	dir := target.Sub(plane).Sub(pos)
	ang := float64(float32(float64(angle) * math.Pi / 180))
	xzAng := math.Atan2(dir[2], dir[0])
	r2 := dir[0]*dir[0] + dir[2]*dir[2]
	r := math.Sqrt(r2)
	y := dir[1]
	g := m.AI.Gravity
	cos2 := math.Pow(math.Cos(ang), 2)
	v0sqr := r2 * g / (r*math.Sin(2*ang) - 2*y*cos2)
	if v0sqr < 0 {
		return mgl64.Vec3{}, false
	}
	v0 := math.Sqrt(v0sqr)
	if v0 > maxVelocity {
		return mgl64.Vec3{}, false
	}
	v0r, v0y := v0*math.Cos(ang), v0*math.Sin(ang)
	if checkCollision {
		samples := int(math.Ceil(r/v0r)) * 2
		ri := 0.0
		var prev *mgl64.Vec3
		box := m.H().Type().BBox(m.Self)
		width, height := box.Width()*.7, box.Height()*.7
		for i := 0; i < samples-1; i++ {
			ri += r / float64(samples)
			yi := math.Sin(ang)/math.Cos(ang)*ri - ri*ri*g/(2*v0sqr*cos2)
			sample := mgl64.Vec3{pos[0] + ri*math.Cos(xzAng), pos[1] + yi, pos[2] + ri*math.Sin(xzAng)}
			if prev != nil && !clearJumpTransition(m, width, height, *prev, sample) {
				return mgl64.Vec3{}, false
			}
			prev = &sample
		}
	}
	return mgl64.Vec3{v0r * math.Cos(xzAng), v0y, v0r * math.Sin(xzAng)}.Mul(float64(float32(.95))), true
}

// clearJumpTransition ports LongJumpUtil.isClearTransition: no block stands
// in the way of the mob, in its long jumping pose, between two points.
func clearJumpTransition(m *Mob, width, height float64, from, to mgl64.Vec3) bool {
	dir := to.Sub(from)
	step := math.Min(width, height)
	checks := int(math.Ceil(dir.Len() / step))
	if dir.LenSqr() > 0 {
		dir = dir.Normalize()
	}
	next := from
	for i := range checks {
		if i == checks-1 {
			next = to
		} else {
			next = next.Add(dir.Mul(step * float64(float32(.9))))
		}
		box := cube.Box(next[0]-width/2, next[1], next[2]-width/2, next[0]+width/2, next[1]+height, next[2]+width/2)
		if !boxFree(m.tx, box) {
			return false
		}
	}
	return true
}
