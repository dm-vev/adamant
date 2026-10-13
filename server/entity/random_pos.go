package entity

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// mobHome is Java's Mob home restriction: a centre and a radius, where a
// negative radius means the mob is not restricted.
type mobHome struct {
	pos    cube.Pos
	radius int
}

// noHome is the restriction of a mob without a home.
var noHome = mobHome{radius: -1}

func (h mobHome) set() bool { return h.radius != -1 }

// contains ports Mob.isWithinHome(BlockPos).
func (h mobHome) contains(pos cube.Pos) bool {
	if !h.set() {
		return true
	}
	d := pos.Sub(h.pos)
	return float64(d[0]*d[0]+d[1]*d[1]+d[2]*d[2]) < float64(h.radius*h.radius)
}

// readMobHome decodes Mob's home_pos and home_radius.
func readMobHome(m map[string]any) mobHome {
	pos, ok := villagerReadPos(m["home_pos"])
	radius, hasRadius := m["home_radius"].(int32)
	if !ok || !hasRadius {
		return noHome
	}
	return mobHome{pos: pos, radius: int(radius)}
}

// writeMobHome encodes Mob's home_pos and home_radius into m.
func (h mobHome) write(m map[string]any) {
	if h.set() {
		m["home_pos"], m["home_radius"] = villagerPosNBT(h.pos), int32(h.radius)
	}
}

// randomPosMob is the state RandomPos reads from a PathfinderMob.
type randomPosMob struct {
	tx      *world.Tx
	pos     mgl64.Vec3
	home    mobHome
	profile GroundProfile
	random  *java26.Random
	// weight is the mob's getWalkTargetValue. Without one every position
	// weighs the same, as for PathfinderMob.
	weight func(cube.Pos) float64
}

// newRandomPosMob returns the RandomPos state of e with a fresh random
// source and, for animals, Animal's preference for grass and light.
func newRandomPosMob(e *Ent, tx *world.Tx) randomPosMob {
	box := e.H().Type().BBox(e)
	random := java26.NewLegacyRandom(rand.Int64())
	m := randomPosMob{tx: tx, pos: e.data.Pos, home: noHome, profile: GroundProfile{Width: box.Width(), Height: box.Height()}, random: &random}
	if full, ok := e.H().Entity(tx); ok {
		if _, animal := full.(breeder); animal {
			m.weight = animalWalkTarget(tx)
		}
	}
	return m
}

// restricted ports GoalUtils.mobRestricted: the home only restricts
// positions when the mob is close enough to it to reach it.
func (m randomPosMob) restricted(horizontal int) bool {
	if !m.home.set() {
		return false
	}
	limit := float64(m.home.radius + horizontal + 1)
	return m.home.pos.Vec3Centre().Sub(m.pos).LenSqr() < limit*limit
}

// randomDirection ports RandomPos.generateRandomDirection.
func (m randomPosMob) randomDirection(horizontal, vertical int) cube.Pos {
	x := int(m.random.NextInt(int32(2*horizontal+1))) - horizontal
	y := int(m.random.NextInt(int32(2*vertical+1))) - vertical
	z := int(m.random.NextInt(int32(2*horizontal+1))) - horizontal
	return cube.Pos{x, y, z}
}

// randomDirectionWithin ports RandomPos.generateRandomDirectionWithinRadians
// without a vertical offset: a direction within maxRadians of (dx, dz).
func (m randomPosMob) randomDirectionWithin(horizontal, vertical int, dx, dz, maxRadians float64) (cube.Pos, bool) {
	yRot := math.Atan2(dz, dx) - math.Pi/2 + float64(2*m.random.NextFloat()-1)*maxRadians
	dist := math.Sqrt(m.random.NextDouble()) * math.Sqrt2 * float64(horizontal)
	x, z := -dist*math.Sin(yRot), dist*math.Cos(yRot)
	if math.Abs(x) > float64(horizontal) || math.Abs(z) > float64(horizontal) {
		return cube.Pos{}, false
	}
	y := int(m.random.NextInt(int32(2*vertical+1))) - vertical
	return cube.Pos{int(x), y, int(z)}, true
}

// towardDirection ports RandomPos.generateRandomPosTowardDirection: a mob
// with a home leans the offset towards it.
func (m randomPosMob) towardDirection(horizontal int, direction cube.Pos) cube.Pos {
	x, z := direction[0], direction[2]
	if m.home.set() && horizontal > 1 {
		if m.pos[0] > float64(m.home.pos[0]) {
			x -= int(m.random.NextInt(int32(horizontal / 2)))
		} else {
			x += int(m.random.NextInt(int32(horizontal / 2)))
		}
		if m.pos[2] > float64(m.home.pos[2]) {
			z -= int(m.random.NextInt(int32(horizontal / 2)))
		} else {
			z += int(m.random.NextInt(int32(horizontal / 2)))
		}
	}
	return cube.PosFromVec3(m.pos.Add(mgl64.Vec3{float64(x), float64(direction[1]), float64(z)}))
}

// target validates a candidate the way DefaultRandomPos and LandRandomPos
// do: inside the world and the home, lifted out of solid blocks onto a
// walkable floor that carries no pathfinding malus.
func (m randomPosMob) target(pos cube.Pos, restrict bool) (cube.Pos, bool) {
	if pos.OutOfBounds(m.tx.Range()) || restrict && !m.home.contains(pos) {
		return cube.Pos{}, false
	}
	for !isPassable(m.tx, pos) {
		if pos = pos.Side(cube.FaceUp); pos.OutOfBounds(m.tx.Range()) {
			return cube.Pos{}, false
		}
	}
	if !groundWalkable(m.tx, pos, m.profile) || pathMalus(m.tx, pos) {
		return cube.Pos{}, false
	}
	return pos, true
}

// pathMalus reports blocks whose Java path type carries a malus: fire,
// lava, damaging plants and magma.
func pathMalus(tx *world.Tx, pos cube.Pos) bool {
	if _, liquid := tx.Liquid(pos); liquid {
		return true
	}
	for _, p := range [2]cube.Pos{pos, pos.Side(cube.FaceDown)} {
		switch b := tx.Block(p).(type) {
		case block.Fire, block.Cactus, block.SweetBerries, block.Magma:
			return true
		case block.Campfire:
			if !b.Extinguished {
				return true
			}
		}
	}
	return false
}

// generate ports RandomPos.generateRandomPos: of ten attempts the valid
// candidate the mob values most wins, the first one on a tie.
func (m randomPosMob) generate(candidate func() (cube.Pos, bool)) (cube.Pos, bool) {
	var best cube.Pos
	found, bestWeight := false, math.Inf(-1)
	for range 10 {
		pos, ok := candidate()
		if !ok {
			continue
		}
		weight := 0.0
		if m.weight != nil {
			weight = m.weight(pos)
		}
		if weight > bestWeight {
			best, bestWeight, found = pos, weight, true
		}
	}
	return best, found
}

// animalWalkTarget ports Animal.getWalkTargetValue: animals prefer grass
// blocks and otherwise brighter spots.
func animalWalkTarget(tx *world.Tx) func(cube.Pos) float64 {
	return func(pos cube.Pos) float64 {
		if _, grass := tx.Block(pos.Side(cube.FaceDown)).(block.Grass); grass {
			return 10
		}
		return pathfindingLightCost(tx, pos)
	}
}

// pathfindingLightCost ports LevelReader.getPathfindingCostFromLightLevels:
// the dimension's brightness curve of the light at pos, less a half.
func pathfindingLightCost(tx *world.Tx, pos cube.Pos) float64 {
	v := float32(tx.RawBrightness(pos)) / 15
	curved := v / (4 - 3*v)
	var ambient float32
	if tx.World().Dimension() == world.Nether {
		ambient = .1
	}
	return float64(curved+ambient*(1-curved)) - .5
}

// randomPos ports DefaultRandomPos.getPos.
func (m randomPosMob) randomPos(horizontal, vertical int) (cube.Pos, bool) {
	restrict := m.restricted(horizontal)
	return m.generate(func() (cube.Pos, bool) {
		return m.target(m.towardDirection(horizontal, m.randomDirection(horizontal, vertical)), restrict)
	})
}

// randomPosAway ports DefaultRandomPos.getPosAway.
func (m randomPosMob) randomPosAway(horizontal, vertical int, from mgl64.Vec3) (cube.Pos, bool) {
	away := m.pos.Sub(from)
	return m.randomPosInDirection(horizontal, vertical, away)
}

// randomPosTowards ports DefaultRandomPos.getPosTowards with a quarter turn
// of spread.
func (m randomPosMob) randomPosTowards(horizontal, vertical int, towards mgl64.Vec3) (cube.Pos, bool) {
	return m.randomPosInDirection(horizontal, vertical, towards.Sub(m.pos))
}

func (m randomPosMob) randomPosInDirection(horizontal, vertical int, dir mgl64.Vec3) (cube.Pos, bool) {
	restrict := m.restricted(horizontal)
	return m.generate(func() (cube.Pos, bool) {
		direction, ok := m.randomDirectionWithin(horizontal, vertical, dir[0], dir[2], math.Pi/2)
		if !ok {
			return cube.Pos{}, false
		}
		return m.target(m.towardDirection(horizontal, direction), restrict)
	})
}
