package player

import (
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/go-gl/mathgl/mgl64"
)

// clickEpsilon is how far a point clicked may lie outside the block
// clicked, Java's 1.0000001 from its centre.
const clickEpsilon = 1e-7

// refuseUseOnBlock judges a use of the held item on the block at pos the
// client of the player asks for, clicked on face at clickPos, relative to
// the block. A point outside the block is refused whenever Place runs, as
// Java does, and is no violation. A click on a face the eyes are behind by
// more than FaceEpsilon, or, with LineOfSight, on a point the eyes cannot
// see past full blocks, is a violation of Place. It reports whether the use
// is refused.
func (p *Player) refuseUseOnBlock(pos cube.Pos, face cube.Face, clickPos mgl64.Vec3) bool {
	ac, end := p.beginAction()
	defer end()
	if !p.checks(ac, anticheat.Place) {
		return false
	}
	for _, v := range clickPos {
		if v < -clickEpsilon || v > 1+clickEpsilon || math.IsNaN(v) {
			return true
		}
	}
	eyes := entity.EyePosition(p)
	conf := ac.conf.Place
	if d := faceDistance(eyes, pos, face); d < -conf.FaceEpsilon {
		return p.violate(ac, anticheat.Place, "face", -d, fmt.Sprintf("eyes %.2f behind the %v face of %v", -d, face, pos)) != anticheat.Log
	}
	if conf.LineOfSight {
		target := pos.Vec3().Add(clickPos)
		if blocked, at := p.sightBlocked(eyes, target, pos); blocked {
			return p.violate(ac, anticheat.Place, "sight", 1, fmt.Sprintf("%v hides %v", at, pos)) != anticheat.Log
		}
	}
	return false
}

// refusePlaceRate counts a block the client of the player places against
// the bucket of Place: PerSecond a second, a burst of Burst. It reports
// whether the place is refused.
func (p *Player) refusePlaceRate(pos cube.Pos) bool {
	ac, end := p.beginAction()
	defer end()
	if !p.checks(ac, anticheat.Place) {
		return false
	}
	conf := ac.conf.Place
	if ac.places.Take(ac.wallNow(), conf.PerSecond, conf.Burst) {
		return false
	}
	return p.violate(ac, anticheat.Place, "rate", 1, fmt.Sprintf("at %v beyond %v a second", pos, conf.PerSecond)) != anticheat.Log
}

// faceDistance returns how far the eyes are out of the plane of the face of
// the block at pos: negative behind it.
func faceDistance(eyes mgl64.Vec3, pos cube.Pos, face cube.Face) float64 {
	centre := pos.Vec3Centre()
	axis := face.Axis()
	i := 1
	switch axis {
	case cube.X:
		i = 0
	case cube.Z:
		i = 2
	}
	sign := 1.0
	if face == cube.FaceDown || face == cube.FaceNorth || face == cube.FaceWest {
		sign = -1
	}
	return (eyes[i] - (centre[i] + sign*0.5)) * sign
}

// sightBlocked reports whether a full block other than the block at target
// lies on the ray from the eyes to the point to, and which.
func (p *Player) sightBlocked(eyes, to mgl64.Vec3, target cube.Pos) (bool, cube.Pos) {
	d := to.Sub(eyes)
	dist := d.Len()
	if dist < 1e-6 {
		return false, cube.Pos{}
	}
	step := d.Mul(0.05 / dist)
	last := cube.Pos{math.MinInt32, 0, 0}
	for at, travelled := eyes, 0.0; travelled < dist; at, travelled = at.Add(step), travelled+0.05 {
		pos := cube.PosFromVec3(at)
		if pos == last || pos == target {
			continue
		}
		last = pos
		if p.fullBlock(pos) {
			return true, pos
		}
	}
	return false, cube.Pos{}
}

// fullBlock reports whether the block at pos fills its space: one box, the
// whole block.
func (p *Player) fullBlock(pos cube.Pos) bool {
	b, loaded := p.tx.BlockLoaded(pos)
	if !loaded {
		return false
	}
	boxes := b.Model().BBox(pos, p.tx)
	return len(boxes) == 1 && boxes[0] == cube.Box(0, 0, 0, 1, 1, 1)
}
