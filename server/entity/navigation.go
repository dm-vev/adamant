package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// GroundProfile controls ground path evaluation.
type groundProfile struct {
	MaxVisitedNodes            int
	StepHeight, MaxFall        int
	Width, Height, FollowRange float64
	AllowWater, AllowDoors     bool
	Allowed                    func(cube.Pos) bool
}

// Navigator owns a reusable ground route; movement remains the caller's responsibility.
type navigator struct {
	Path            []cube.Pos
	Target          cube.Pos
	HasTarget       bool
	LastComputeTick int64
	LastStuckCheck  int64
	LastPosition    mgl64.Vec3
	Index           int
}

// MoveTo keeps an existing route for an unchanged target and throttles replanning to 20 ticks.
func (n *navigator) MoveTo(tx *world.Tx, start, target cube.Pos, profile groundProfile, tick int64) bool {
	if n.HasTarget && n.Target == target && len(n.Path) > n.Index {
		return true
	}
	if n.HasTarget && tick-n.LastComputeTick < 20 {
		dx, dy, dz := n.Target[0]-target[0], n.Target[1]-target[1], n.Target[2]-target[2]
		if dx*dx+dy*dy+dz*dz <= 16 {
			return len(n.Path) > n.Index
		}
	}
	n.Path = findGroundPath(tx, start, target, profile)
	n.Target, n.HasTarget, n.Index, n.LastComputeTick = target, true, 0, tick
	return len(n.Path) > 0
}

// Tick advances reached waypoints and returns the next centered waypoint.
func (n *navigator) Tick(position mgl64.Vec3, tick int64, width float64) (mgl64.Vec3, bool) {
	if tick-n.LastStuckCheck >= 100 {
		if tick > 0 {
			minMove := math.Max(.1, width*.1)
			if position.Sub(n.LastPosition).LenSqr() < minMove*minMove {
				n.Path, n.Index, n.HasTarget = nil, 0, false
			}
		}
		n.LastStuckCheck, n.LastPosition = tick, position
	}
	reach := math.Max(.5, width)
	reachSqr := reach * reach
	for n.Index < len(n.Path) {
		p := n.Path[n.Index]
		dx := position[0] - (float64(p[0]) + .5)
		dy := position[1] - float64(p[1])
		dz := position[2] - (float64(p[2]) + .5)
		if dx*dx+dy*dy+dz*dz > reachSqr {
			break
		}
		n.Index++
	}
	if n.Index >= len(n.Path) {
		return mgl64.Vec3{}, false
	}
	p := n.Path[n.Index]
	return mgl64.Vec3{float64(p[0]) + .5, float64(p[1]), float64(p[2]) + .5}, true
}
