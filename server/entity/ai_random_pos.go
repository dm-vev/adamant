package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports RandomPos, DefaultRandomPos, LandRandomPos, HoverRandomPos,
// AirAndWaterRandomPos and GoalUtils, which pick random positions for mobs to
// walk or fly to.

// mobWalkTarget is implemented by behaviours of mobs that prefer some
// positions to walk to, as PathfinderMob.getWalkTargetValue.
type mobWalkTarget interface {
	walkTargetValue(m *Mob, pos cube.Pos) float64
}

// walkTargetValue returns how much the mob likes to walk to a position.
func (m *Mob) walkTargetValue(pos cube.Pos) float64 {
	if w, ok := m.Behaviour().(mobWalkTarget); ok {
		return w.walkTargetValue(m, pos)
	}
	return 0
}

// monsterWalkTarget ports Monster.getWalkTargetValue: monsters prefer dark
// positions.
func monsterWalkTarget(m *Mob, pos cube.Pos) float64 {
	return -pathfindingLightCost(m.tx, pos)
}

// animalWalkTargetValue ports Animal.getWalkTargetValue: animals prefer
// grass and otherwise light.
func animalWalkTargetValue(m *Mob, pos cube.Pos) float64 {
	if _, grass := m.tx.Block(pos.Side(cube.FaceDown)).(block.Grass); grass {
		return 10
	}
	return pathfindingLightCost(m.tx, pos)
}

// randomDirection ports RandomPos.generateRandomDirection.
func randomDirection(m *Mob, horizontal, vertical int) cube.Pos {
	r := m.Random()
	x := int(r.NextInt(int32(2*horizontal+1))) - horizontal
	y := int(r.NextInt(int32(2*vertical+1))) - vertical
	z := int(r.NextInt(int32(2*horizontal+1))) - horizontal
	return cube.Pos{x, y, z}
}

// randomDirectionWithinRadians ports
// RandomPos.generateRandomDirectionWithinRadians.
func randomDirectionWithinRadians(m *Mob, minHorizontal, maxHorizontal float64, vertical, flyingHeight int, xDir, zDir, maxRadians float64) (cube.Pos, bool) {
	r := m.Random()
	centre := math.Atan2(zDir, xDir) - math.Pi/2
	yRadians := centre + float64(2*r.NextFloat()-1)*maxRadians
	dist := (minHorizontal + math.Sqrt(r.NextDouble())*(maxHorizontal-minHorizontal)) * math.Sqrt2
	xt, zt := -dist*math.Sin(yRadians), dist*math.Cos(yRadians)
	if math.Abs(xt) > maxHorizontal || math.Abs(zt) > maxHorizontal {
		return cube.Pos{}, false
	}
	yt := int(r.NextInt(int32(2*vertical+1))) - vertical + flyingHeight
	return cube.PosFromVec3(mgl64.Vec3{xt, float64(yt), zt}), true
}

// moveUpOutOfSolid ports RandomPos.moveUpOutOfSolid.
func moveUpOutOfSolid(pos cube.Pos, maxY int, solid func(cube.Pos) bool) cube.Pos {
	if !solid(pos) {
		return pos
	}
	pos = pos.Side(cube.FaceUp)
	for pos.Y() <= maxY && solid(pos) {
		pos = pos.Side(cube.FaceUp)
	}
	return pos
}

// moveUpToAboveSolid ports RandomPos.moveUpToAboveSolid: a position inside
// solid blocks rises out of them and up to aboveSolid blocks further, short
// of a ceiling.
func moveUpToAboveSolid(pos cube.Pos, aboveSolid, maxY int, solid func(cube.Pos) bool) cube.Pos {
	if !solid(pos) {
		return pos
	}
	pos = pos.Side(cube.FaceUp)
	for pos.Y() <= maxY && solid(pos) {
		pos = pos.Side(cube.FaceUp)
	}
	firstNonSolid := pos.Y()
	for pos.Y() <= maxY && pos.Y()-firstNonSolid < aboveSolid {
		pos = pos.Side(cube.FaceUp)
		if solid(pos) {
			pos = pos.Side(cube.FaceDown)
			break
		}
	}
	return pos
}

// generateRandomPos ports RandomPos.generateRandomPos: the position the mob
// likes best of ten candidates, at the bottom centre of its block.
func generateRandomPos(m *Mob, candidate func() (cube.Pos, bool)) (mgl64.Vec3, bool) {
	return generateWeightedRandomPos(candidate, m.walkTargetValue)
}

// generateWeightedRandomPos ports RandomPos.generateRandomPos with the
// position weight passed: the best of ten candidates.
func generateWeightedRandomPos(candidate func() (cube.Pos, bool), positionWeight func(cube.Pos) float64) (mgl64.Vec3, bool) {
	best, bestWeight, found := cube.Pos{}, math.Inf(-1), false
	for range 10 {
		pos, ok := candidate()
		if !ok {
			continue
		}
		if weight := positionWeight(pos); weight > bestWeight {
			best, bestWeight, found = pos, weight, true
		}
	}
	if !found {
		return mgl64.Vec3{}, false
	}
	return mgl64.Vec3{float64(best.X()) + .5, float64(best.Y()), float64(best.Z()) + .5}, true
}

// randomPosTowardDirection ports RandomPos.generateRandomPosTowardDirection:
// a mob with a home leans its positions back towards it.
func randomPosTowardDirection(m *Mob, xzDist float64, direction cube.Pos) cube.Pos {
	xt, zt := float64(direction.X()), float64(direction.Z())
	home := m.AI.home
	if home.set() && xzDist > 1 {
		r := m.Random()
		if m.data.Pos[0] > float64(home.pos.X()) {
			xt -= r.NextDouble() * xzDist / 2
		} else {
			xt += r.NextDouble() * xzDist / 2
		}
		if m.data.Pos[2] > float64(home.pos.Z()) {
			zt -= r.NextDouble() * xzDist / 2
		} else {
			zt += r.NextDouble() * xzDist / 2
		}
	}
	return cube.PosFromVec3(mgl64.Vec3{xt + m.data.Pos[0], float64(direction.Y()) + m.data.Pos[1], zt + m.data.Pos[2]})
}

// mobRestricted ports GoalUtils.mobRestricted.
func mobRestricted(m *Mob, horizontal float64) bool {
	home := m.AI.home
	if !home.set() {
		return false
	}
	r := float64(home.radius) + horizontal + 1
	return home.pos.Vec3Centre().Sub(m.data.Pos).LenSqr() < r*r
}

// acceptableRandomPos ports the checks of DefaultRandomPos and LandRandomPos:
// inside the world, within the home of a restricted mob and above ground the
// mob may stand on.
func acceptableRandomPos(m *Mob, pos cube.Pos, restrict bool) bool {
	if pos.OutOfBounds(m.tx.Range()) {
		return false
	}
	if restrict && !m.WithinHome(pos) {
		return false
	}
	return m.AI.nav.IsStableDestination(m.tx, pos)
}

// hasMalus ports GoalUtils.hasMalus.
func hasMalus(m *Mob, pos cube.Pos) bool {
	return m.PathfindingMalus(pathTypeStatic(m.tx, pos)) != 0
}

// DefaultRandomPos ports DefaultRandomPos.getPos.
func DefaultRandomPos(m *Mob, horizontal, vertical int) (mgl64.Vec3, bool) {
	restrict := mobRestricted(m, float64(horizontal))
	return generateRandomPos(m, func() (cube.Pos, bool) {
		pos := randomPosTowardDirection(m, float64(horizontal), randomDirection(m, horizontal, vertical))
		return pos, acceptableRandomPos(m, pos, restrict) && !hasMalus(m, pos)
	})
}

// DefaultRandomPosAway ports DefaultRandomPos.getPosAway.
func DefaultRandomPosAway(m *Mob, horizontal, vertical int, away mgl64.Vec3) (mgl64.Vec3, bool) {
	dir := m.data.Pos.Sub(away)
	return defaultRandomPosInDirection(m, horizontal, vertical, dir, math.Pi/2)
}

// DefaultRandomPosTowards ports DefaultRandomPos.getPosTowards.
func DefaultRandomPosTowards(m *Mob, horizontal, vertical int, towards mgl64.Vec3, maxRadians float64) (mgl64.Vec3, bool) {
	return defaultRandomPosInDirection(m, horizontal, vertical, towards.Sub(m.data.Pos), maxRadians)
}

func defaultRandomPosInDirection(m *Mob, horizontal, vertical int, dir mgl64.Vec3, maxRadians float64) (mgl64.Vec3, bool) {
	restrict := mobRestricted(m, float64(horizontal))
	return generateRandomPos(m, func() (cube.Pos, bool) {
		direction, ok := randomDirectionWithinRadians(m, 0, float64(horizontal), vertical, 0, dir[0], dir[2], maxRadians)
		if !ok {
			return cube.Pos{}, false
		}
		pos := randomPosTowardDirection(m, float64(horizontal), direction)
		return pos, acceptableRandomPos(m, pos, restrict) && !hasMalus(m, pos)
	})
}

// LandRandomPos ports LandRandomPos.getPos.
func LandRandomPos(m *Mob, horizontal, vertical int) (mgl64.Vec3, bool) {
	restrict := mobRestricted(m, float64(horizontal))
	return generateRandomPos(m, func() (cube.Pos, bool) {
		pos := randomPosTowardDirection(m, float64(horizontal), randomDirection(m, horizontal, vertical))
		if !acceptableRandomPos(m, pos, restrict) {
			return cube.Pos{}, false
		}
		return landPosUpOutOfSolid(m, pos)
	})
}

// LandRandomPosWeighted ports LandRandomPos.getPos with the position weight
// passed in place of the walk target value of the mob.
func LandRandomPosWeighted(m *Mob, horizontal, vertical int, positionWeight func(cube.Pos) float64) (mgl64.Vec3, bool) {
	restrict := mobRestricted(m, float64(horizontal))
	return generateWeightedRandomPos(func() (cube.Pos, bool) {
		pos := randomPosTowardDirection(m, float64(horizontal), randomDirection(m, horizontal, vertical))
		if !acceptableRandomPos(m, pos, restrict) {
			return cube.Pos{}, false
		}
		return landPosUpOutOfSolid(m, pos)
	}, positionWeight)
}

// LandRandomPosAway ports LandRandomPos.getPosAway.
func LandRandomPosAway(m *Mob, horizontal, vertical int, away mgl64.Vec3) (mgl64.Vec3, bool) {
	dir := m.data.Pos.Sub(away)
	if dir.Len() == 0 {
		r := m.Random()
		dir = mgl64.Vec3{r.NextDouble() - .5, 0, r.NextDouble() - .5}
	}
	return landRandomPosInDirection(m, 0, float64(horizontal), vertical, dir)
}

// LandRandomPosTowards ports LandRandomPos.getPosTowards.
func LandRandomPosTowards(m *Mob, horizontal, vertical int, towards mgl64.Vec3) (mgl64.Vec3, bool) {
	return landRandomPosInDirection(m, 0, float64(horizontal), vertical, towards.Sub(m.data.Pos))
}

func landRandomPosInDirection(m *Mob, minHorizontal, maxHorizontal float64, vertical int, dir mgl64.Vec3) (mgl64.Vec3, bool) {
	restrict := mobRestricted(m, maxHorizontal)
	return generateRandomPos(m, func() (cube.Pos, bool) {
		direction, ok := randomDirectionWithinRadians(m, minHorizontal, maxHorizontal, vertical, 0, dir[0], dir[2], math.Pi/2)
		if !ok {
			return cube.Pos{}, false
		}
		pos := randomPosTowardDirection(m, maxHorizontal, direction)
		if !acceptableRandomPos(m, pos, restrict) {
			return cube.Pos{}, false
		}
		return landPosUpOutOfSolid(m, pos)
	})
}

// landPosUpOutOfSolid ports LandRandomPos.movePosUpOutOfSolid.
func landPosUpOutOfSolid(m *Mob, pos cube.Pos) (cube.Pos, bool) {
	pos = moveUpOutOfSolid(pos, m.tx.Range().Max(), func(p cube.Pos) bool {
		return legacySolid(m.tx, pathBlockAt(m.tx, p), p)
	})
	if pathWater(m.tx, pos) || hasMalus(m, pos) {
		return cube.Pos{}, false
	}
	return pos, true
}

// solidAt ports GoalUtils.isSolid.
func solidAt(m *Mob, pos cube.Pos) bool { return legacySolid(m.tx, pathBlockAt(m.tx, pos), pos) }

// HoverRandomPos ports HoverRandomPos.getPos: a position in a direction
// within maxRadians of the one passed, hoverMin to hoverMax blocks above the
// ground there and out of water.
func HoverRandomPos(m *Mob, horizontal, vertical int, xDir, zDir, maxRadians float64, hoverMax, hoverMin int) (mgl64.Vec3, bool) {
	restrict := mobRestricted(m, float64(horizontal))
	return generateRandomPos(m, func() (cube.Pos, bool) {
		direction, ok := randomDirectionWithinRadians(m, 0, float64(horizontal), vertical, 0, xDir, zDir, maxRadians)
		if !ok {
			return cube.Pos{}, false
		}
		pos := randomPosTowardDirection(m, float64(horizontal), direction)
		if !acceptableRandomPos(m, pos, restrict) {
			return cube.Pos{}, false
		}
		above := int(m.Random().NextInt(int32(hoverMax-hoverMin+1))) + hoverMin
		pos = moveUpToAboveSolid(pos, above, m.tx.Range().Max(), func(p cube.Pos) bool { return solidAt(m, p) })
		return pos, !pathWater(m.tx, pos) && !hasMalus(m, pos)
	})
}

// AirAndWaterRandomPos ports AirAndWaterRandomPos.getPos: a position in a
// direction within maxRadians of the one passed, flyingHeight blocks up on
// average, in the air or in water.
func AirAndWaterRandomPos(m *Mob, horizontal, vertical, flyingHeight int, xDir, zDir, maxRadians float64) (mgl64.Vec3, bool) {
	restrict := mobRestricted(m, float64(horizontal))
	return generateRandomPos(m, func() (cube.Pos, bool) {
		return airAndWaterPos(m, horizontal, vertical, flyingHeight, xDir, zDir, maxRadians, restrict)
	})
}

// airAndWaterPos ports AirAndWaterRandomPos.generateRandomPos.
func airAndWaterPos(m *Mob, horizontal, vertical, flyingHeight int, xDir, zDir, maxRadians float64, restrict bool) (cube.Pos, bool) {
	direction, ok := randomDirectionWithinRadians(m, 0, float64(horizontal), vertical, flyingHeight, xDir, zDir, maxRadians)
	if !ok {
		return cube.Pos{}, false
	}
	pos := randomPosTowardDirection(m, float64(horizontal), direction)
	if pos.OutOfBounds(m.tx.Range()) || restrict && !m.WithinHome(pos) {
		return cube.Pos{}, false
	}
	pos = moveUpOutOfSolid(pos, m.tx.Range().Max(), func(p cube.Pos) bool { return solidAt(m, p) })
	return pos, !hasMalus(m, pos)
}

// AirRandomPosTowards ports AirRandomPos.getPosTowards: a position in the air
// in a direction within maxRadians of the one to towards.
func AirRandomPosTowards(m *Mob, horizontal, vertical, flyingHeight int, towards mgl64.Vec3, maxRadians float64) (mgl64.Vec3, bool) {
	dir := towards.Sub(m.data.Pos)
	restrict := mobRestricted(m, float64(horizontal))
	return generateRandomPos(m, func() (cube.Pos, bool) {
		pos, ok := airAndWaterPos(m, horizontal, vertical, flyingHeight, dir[0], dir[2], maxRadians, restrict)
		return pos, ok && !pathWater(m.tx, pos)
	})
}
