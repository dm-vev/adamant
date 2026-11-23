package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
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

// solidAtWorld reports whether pos is legacy-solid without allocating a closure,
// fast-pathing blocks with empty or full-cube collision via their runtime ID.
func solidAtWorld(tx *world.Tx, pos cube.Pos) bool {
	if rid, ok := tx.BlockRID(pos); ok {
		flags := tx.World().BlockRegistry().BlockFlags(rid)
		if flags.EmptyCollision() {
			return false
		}
		if flags.FullCubeCollision() {
			return true
		}
	}
	b := pathBlockAt(tx, pos)
	if isAir(b) {
		return false
	}
	return legacySolid(tx, b, pos)
}

// moveUpOutOfSolidWorld ports RandomPos.moveUpOutOfSolid without closure overhead.
func moveUpOutOfSolidWorld(tx *world.Tx, pos cube.Pos, maxY int) cube.Pos {
	if !solidAtWorld(tx, pos) {
		return pos
	}
	pos[1]++
	for pos[1] <= maxY && solidAtWorld(tx, pos) {
		pos[1]++
	}
	return pos
}

// moveUpToAboveSolidWorld ports RandomPos.moveUpToAboveSolid without closure
// overhead: a position inside solid blocks rises out of them and up to
// aboveSolid blocks further, short of a ceiling.
func moveUpToAboveSolidWorld(tx *world.Tx, pos cube.Pos, aboveSolid, maxY int) cube.Pos {
	if !solidAtWorld(tx, pos) {
		return pos
	}
	pos[1]++
	for pos[1] <= maxY && solidAtWorld(tx, pos) {
		pos[1]++
	}
	firstNonSolid := pos[1]
	for pos[1] <= maxY && pos[1]-firstNonSolid < aboveSolid {
		pos[1]++
		if solidAtWorld(tx, pos) {
			pos[1]--
			break
		}
	}
	return pos
}

// randomPosTowardDirection ports RandomPos.generateRandomPosTowardDirection:
// a mob with a home leans its positions back towards it.
func randomPosTowardDirection(m *Mob, xzDist float64, direction cube.Pos) cube.Pos {
	xt, zt := float64(direction[0]), float64(direction[2])
	home := m.AI.home
	if home.set() && xzDist > 1 {
		r := m.Random()
		if m.data.Pos[0] > float64(home.pos[0]) {
			xt -= r.NextDouble() * xzDist / 2
		} else {
			xt += r.NextDouble() * xzDist / 2
		}
		if m.data.Pos[2] > float64(home.pos[2]) {
			zt -= r.NextDouble() * xzDist / 2
		} else {
			zt += r.NextDouble() * xzDist / 2
		}
	}
	return cube.PosFromVec3(mgl64.Vec3{xt + m.data.Pos[0], float64(direction[1]) + m.data.Pos[1], zt + m.data.Pos[2]})
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

// acceptableRandomPosInRange ports the checks of DefaultRandomPos and
// LandRandomPos using pre-fetched world vertical bounds.
func acceptableRandomPosInRange(m *Mob, pos cube.Pos, restrict bool, minY, maxY int) bool {
	if pos[1] < minY || pos[1] > maxY {
		return false
	}
	if restrict && !m.WithinHome(pos) {
		return false
	}
	return m.AI.nav.IsStableDestination(m.tx, pos)
}

func finishRandomPos(best cube.Pos, found bool) (mgl64.Vec3, bool) {
	if !found {
		return mgl64.Vec3{}, false
	}
	return mgl64.Vec3{float64(best[0]) + .5, float64(best[1]), float64(best[2]) + .5}, true
}

func evalWalkWeight(m *Mob, wt mobWalkTarget, hasWT bool, pos cube.Pos) float64 {
	if hasWT {
		return wt.walkTargetValue(m, pos)
	}
	return 0
}

// DefaultRandomPos ports DefaultRandomPos.getPos.
func defaultRandomPos(m *Mob, horizontal, vertical int) (mgl64.Vec3, bool) {
	restrict := mobRestricted(m, float64(horizontal))
	var reader pathStateReader
	reader.initFromWorld(m.tx)
	wt, hasWT := m.Behaviour().(mobWalkTarget)
	best, bestWeight, found := cube.Pos{}, math.Inf(-1), false
	for range 10 {
		pos := randomPosTowardDirection(m, float64(horizontal), randomDirection(m, horizontal, vertical))
		if !acceptableRandomPosInRange(m, pos, restrict, reader.minY, reader.maxY) || m.PathfindingMalus(reader.pathTypeStatic(pos)) != 0 {
			continue
		}
		if weight := evalWalkWeight(m, wt, hasWT, pos); weight > bestWeight {
			best, bestWeight, found = pos, weight, true
		}
	}
	return finishRandomPos(best, found)
}

// DefaultRandomPosAway ports DefaultRandomPos.getPosAway.
func defaultRandomPosAway(m *Mob, horizontal, vertical int, away mgl64.Vec3) (mgl64.Vec3, bool) {
	dir := m.data.Pos.Sub(away)
	return defaultRandomPosInDirection(m, horizontal, vertical, dir, math.Pi/2)
}

// DefaultRandomPosTowards ports DefaultRandomPos.getPosTowards.
func defaultRandomPosTowards(m *Mob, horizontal, vertical int, towards mgl64.Vec3, maxRadians float64) (mgl64.Vec3, bool) {
	return defaultRandomPosInDirection(m, horizontal, vertical, towards.Sub(m.data.Pos), maxRadians)
}

func defaultRandomPosInDirection(m *Mob, horizontal, vertical int, dir mgl64.Vec3, maxRadians float64) (mgl64.Vec3, bool) {
	restrict := mobRestricted(m, float64(horizontal))
	var reader pathStateReader
	reader.initFromWorld(m.tx)
	wt, hasWT := m.Behaviour().(mobWalkTarget)
	best, bestWeight, found := cube.Pos{}, math.Inf(-1), false
	for range 10 {
		direction, ok := randomDirectionWithinRadians(m, 0, float64(horizontal), vertical, 0, dir[0], dir[2], maxRadians)
		if !ok {
			continue
		}
		pos := randomPosTowardDirection(m, float64(horizontal), direction)
		if !acceptableRandomPosInRange(m, pos, restrict, reader.minY, reader.maxY) || m.PathfindingMalus(reader.pathTypeStatic(pos)) != 0 {
			continue
		}
		if weight := evalWalkWeight(m, wt, hasWT, pos); weight > bestWeight {
			best, bestWeight, found = pos, weight, true
		}
	}
	return finishRandomPos(best, found)
}

// landPosUpOutOfSolid ports LandRandomPos.movePosUpOutOfSolid using a shared reader.
func landPosUpOutOfSolid(m *Mob, reader *pathStateReader, pos cube.Pos) (cube.Pos, bool) {
	pos = moveUpOutOfSolidWorld(m.tx, pos, reader.maxY)
	if pathWater(m.tx, pos) || m.PathfindingMalus(reader.pathTypeStatic(pos)) != 0 {
		return cube.Pos{}, false
	}
	return pos, true
}

// LandRandomPos ports LandRandomPos.getPos.
func landRandomPos(m *Mob, horizontal, vertical int) (mgl64.Vec3, bool) {
	restrict := mobRestricted(m, float64(horizontal))
	var reader pathStateReader
	reader.initFromWorld(m.tx)
	wt, hasWT := m.Behaviour().(mobWalkTarget)
	best, bestWeight, found := cube.Pos{}, math.Inf(-1), false
	for range 10 {
		pos := randomPosTowardDirection(m, float64(horizontal), randomDirection(m, horizontal, vertical))
		if !acceptableRandomPosInRange(m, pos, restrict, reader.minY, reader.maxY) {
			continue
		}
		pos, ok := landPosUpOutOfSolid(m, &reader, pos)
		if !ok {
			continue
		}
		if weight := evalWalkWeight(m, wt, hasWT, pos); weight > bestWeight {
			best, bestWeight, found = pos, weight, true
		}
	}
	return finishRandomPos(best, found)
}

// LandRandomPosWeighted ports LandRandomPos.getPos with the position weight
// passed in place of the walk target value of the mob.
func landRandomPosWeighted(m *Mob, horizontal, vertical int, positionWeight func(cube.Pos) float64) (mgl64.Vec3, bool) {
	restrict := mobRestricted(m, float64(horizontal))
	var reader pathStateReader
	reader.initFromWorld(m.tx)
	best, bestWeight, found := cube.Pos{}, math.Inf(-1), false
	for range 10 {
		pos := randomPosTowardDirection(m, float64(horizontal), randomDirection(m, horizontal, vertical))
		if !acceptableRandomPosInRange(m, pos, restrict, reader.minY, reader.maxY) {
			continue
		}
		pos, ok := landPosUpOutOfSolid(m, &reader, pos)
		if !ok {
			continue
		}
		if weight := positionWeight(pos); weight > bestWeight {
			best, bestWeight, found = pos, weight, true
		}
	}
	return finishRandomPos(best, found)
}

// LandRandomPosAway ports LandRandomPos.getPosAway.
func landRandomPosAway(m *Mob, horizontal, vertical int, away mgl64.Vec3) (mgl64.Vec3, bool) {
	dir := m.data.Pos.Sub(away)
	if dir.LenSqr() == 0 {
		r := m.Random()
		dir = mgl64.Vec3{r.NextDouble() - .5, 0, r.NextDouble() - .5}
	}
	return landRandomPosInDirection(m, 0, float64(horizontal), vertical, dir)
}

// LandRandomPosTowards ports LandRandomPos.getPosTowards.
func landRandomPosTowards(m *Mob, horizontal, vertical int, towards mgl64.Vec3) (mgl64.Vec3, bool) {
	return landRandomPosInDirection(m, 0, float64(horizontal), vertical, towards.Sub(m.data.Pos))
}

func landRandomPosInDirection(m *Mob, minHorizontal, maxHorizontal float64, vertical int, dir mgl64.Vec3) (mgl64.Vec3, bool) {
	restrict := mobRestricted(m, maxHorizontal)
	var reader pathStateReader
	reader.initFromWorld(m.tx)
	wt, hasWT := m.Behaviour().(mobWalkTarget)
	best, bestWeight, found := cube.Pos{}, math.Inf(-1), false
	for range 10 {
		direction, ok := randomDirectionWithinRadians(m, minHorizontal, maxHorizontal, vertical, 0, dir[0], dir[2], math.Pi/2)
		if !ok {
			continue
		}
		pos := randomPosTowardDirection(m, maxHorizontal, direction)
		if !acceptableRandomPosInRange(m, pos, restrict, reader.minY, reader.maxY) {
			continue
		}
		pos, ok = landPosUpOutOfSolid(m, &reader, pos)
		if !ok {
			continue
		}
		if weight := evalWalkWeight(m, wt, hasWT, pos); weight > bestWeight {
			best, bestWeight, found = pos, weight, true
		}
	}
	return finishRandomPos(best, found)
}

// HoverRandomPos ports HoverRandomPos.getPos: a position in a direction
// within maxRadians of the one passed, hoverMin to hoverMax blocks above the
// ground there and out of water.
func hoverRandomPos(m *Mob, horizontal, vertical int, xDir, zDir, maxRadians float64, hoverMax, hoverMin int) (mgl64.Vec3, bool) {
	restrict := mobRestricted(m, float64(horizontal))
	var reader pathStateReader
	reader.initFromWorld(m.tx)
	wt, hasWT := m.Behaviour().(mobWalkTarget)
	best, bestWeight, found := cube.Pos{}, math.Inf(-1), false
	for range 10 {
		direction, ok := randomDirectionWithinRadians(m, 0, float64(horizontal), vertical, 0, xDir, zDir, maxRadians)
		if !ok {
			continue
		}
		pos := randomPosTowardDirection(m, float64(horizontal), direction)
		if !acceptableRandomPosInRange(m, pos, restrict, reader.minY, reader.maxY) {
			continue
		}
		above := int(m.Random().NextInt(int32(hoverMax-hoverMin+1))) + hoverMin
		pos = moveUpToAboveSolidWorld(m.tx, pos, above, reader.maxY)
		if pathWater(m.tx, pos) || m.PathfindingMalus(reader.pathTypeStatic(pos)) != 0 {
			continue
		}
		if weight := evalWalkWeight(m, wt, hasWT, pos); weight > bestWeight {
			best, bestWeight, found = pos, weight, true
		}
	}
	return finishRandomPos(best, found)
}

// AirAndWaterRandomPos ports AirAndWaterRandomPos.getPos: a position in a
// direction within maxRadians of the one passed, flyingHeight blocks up on
// average, in the air or in water.
func airAndWaterRandomPos(m *Mob, horizontal, vertical, flyingHeight int, xDir, zDir, maxRadians float64) (mgl64.Vec3, bool) {
	restrict := mobRestricted(m, float64(horizontal))
	var reader pathStateReader
	reader.initFromWorld(m.tx)
	wt, hasWT := m.Behaviour().(mobWalkTarget)
	best, bestWeight, found := cube.Pos{}, math.Inf(-1), false
	for range 10 {
		pos, ok := airAndWaterPos(m, &reader, horizontal, vertical, flyingHeight, xDir, zDir, maxRadians, restrict)
		if !ok {
			continue
		}
		if weight := evalWalkWeight(m, wt, hasWT, pos); weight > bestWeight {
			best, bestWeight, found = pos, weight, true
		}
	}
	return finishRandomPos(best, found)
}

// airAndWaterPos ports AirAndWaterRandomPos.generateRandomPos.
func airAndWaterPos(m *Mob, reader *pathStateReader, horizontal, vertical, flyingHeight int, xDir, zDir, maxRadians float64, restrict bool) (cube.Pos, bool) {
	direction, ok := randomDirectionWithinRadians(m, 0, float64(horizontal), vertical, flyingHeight, xDir, zDir, maxRadians)
	if !ok {
		return cube.Pos{}, false
	}
	pos := randomPosTowardDirection(m, float64(horizontal), direction)
	if pos[1] < reader.minY || pos[1] > reader.maxY || restrict && !m.WithinHome(pos) {
		return cube.Pos{}, false
	}
	pos = moveUpOutOfSolidWorld(m.tx, pos, reader.maxY)
	return pos, m.PathfindingMalus(reader.pathTypeStatic(pos)) == 0
}

// AirRandomPosTowards ports AirRandomPos.getPosTowards: a position in the air
// in a direction within maxRadians of the one to towards.
func airRandomPosTowards(m *Mob, horizontal, vertical, flyingHeight int, towards mgl64.Vec3, maxRadians float64) (mgl64.Vec3, bool) {
	dir := towards.Sub(m.data.Pos)
	restrict := mobRestricted(m, float64(horizontal))
	var reader pathStateReader
	reader.initFromWorld(m.tx)
	wt, hasWT := m.Behaviour().(mobWalkTarget)
	best, bestWeight, found := cube.Pos{}, math.Inf(-1), false
	for range 10 {
		pos, ok := airAndWaterPos(m, &reader, horizontal, vertical, flyingHeight, dir[0], dir[2], maxRadians, restrict)
		if !ok || pathWater(m.tx, pos) {
			continue
		}
		if weight := evalWalkWeight(m, wt, hasWT, pos); weight > bestWeight {
			best, bestWeight, found = pos, weight, true
		}
	}
	return finishRandomPos(best, found)
}
