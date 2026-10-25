package entity

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// FrostWalk ports the LOCATION_CHANGED effect of Frost Walker, which runs as
// a living entity on the ground and riding nothing moves into another block:
// still water under it, within a disk of 3 blocks plus one a level beyond
// the first, freezes into frosted ice where nothing stands in it.
func FrostWalk(tx *world.Tx, e world.Entity, pos mgl64.Vec3) {
	level := block.FrostWalkerLevel(e)
	if level == 0 {
		return
	}
	dist := int(min(16, max(0, float32(3)+float32(level-1))))
	centre := cube.PosFromVec3(pos).Side(cube.FaceDown)
	for x := centre[0] - dist; x <= centre[0]+dist; x++ {
		for z := centre[2] - dist; z <= centre[2]+dist; z++ {
			p := cube.Pos{x, centre[1], z}
			dx, dz := float64(x)+.5-pos[0], float64(z)+.5-pos[2]
			if dx*dx+dz*dz >= float64(dist*dist) || !frostWalkerFreezes(tx, p) {
				continue
			}
			tx.SetBlock(p, block.FrostedIce{}, nil)
			tx.EmitGameEvent(world.GameEventBlockPlace, p.Vec3Centre(), world.GameEventContext{Source: e, AffectedState: block.FrostedIce{}})
		}
	}
}

// frostWalkerFreezes ports the block predicate of Frost Walker: air above a
// still water block that no entity stands in.
func frostWalkerFreezes(tx *world.Tx, pos cube.Pos) bool {
	if pos.OutOfBounds(tx.Range()) {
		return false
	}
	w, ok := tx.Block(pos).(block.Water)
	if !ok || w.Depth != 8 || w.Falling {
		return false
	}
	if _, air := tx.Block(pos.Side(cube.FaceUp)).(block.Air); !air {
		return false
	}
	box := cube.Box(float64(pos[0]), float64(pos[1]), float64(pos[2]), float64(pos[0]+1), float64(pos[1]+1), float64(pos[2]+1))
	for other := range tx.EntitiesWithin(box.Grow(1)) {
		if _, living := other.(Living); !living || spectator(other) {
			continue
		}
		if b := other.H().Type().BBox(other).Translate(other.Position()); boxesOverlap(b, box) {
			return false
		}
	}
	return true
}

// boxesOverlap reports if two boxes share some volume, not only a face.
func boxesOverlap(a, b cube.BBox) bool {
	const eps = 1e-7
	return a.Min()[0] < b.Max()[0]-eps && a.Max()[0] > b.Min()[0]+eps &&
		a.Min()[1] < b.Max()[1]-eps && a.Max()[1] > b.Min()[1]+eps &&
		a.Min()[2] < b.Max()[2]-eps && a.Max()[2] > b.Min()[2]+eps
}
