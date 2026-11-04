package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
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
