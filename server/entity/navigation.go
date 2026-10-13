package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// GroundProfile controls ground path evaluation.
type GroundProfile struct {
	MaxVisitedNodes            int
	StepHeight, MaxFall        int
	Width, Height, FollowRange float64
	AllowWater, AllowDoors     bool
	Allowed                    func(cube.Pos) bool
}

// Navigator owns a reusable ground route; movement remains the caller's responsibility.
type Navigator struct {
	Path            []cube.Pos
	Target          cube.Pos
	HasTarget       bool
	LastComputeTick int64
	LastStuckCheck  int64
	LastPosition    mgl64.Vec3
	Index           int
}

// MoveTo keeps an existing route for an unchanged target and throttles replanning to 20 ticks.
func (n *Navigator) MoveTo(tx *world.Tx, start, target cube.Pos, profile GroundProfile, tick int64) bool {
	if n.HasTarget && n.Target == target && len(n.Path) > n.Index {
		return true
	}
	if n.HasTarget && tick-n.LastComputeTick < 20 && n.Target.Sub(target).Vec3().Len() <= 4 {
		return len(n.Path) > n.Index
	}
	n.Path = FindGroundPath(tx, start, target, profile)
	n.Target, n.HasTarget, n.Index, n.LastComputeTick = target, true, 0, tick
	return len(n.Path) > 0
}

// Tick advances reached waypoints and returns the next centered waypoint.
func (n *Navigator) Tick(position mgl64.Vec3, tick int64, width float64) (mgl64.Vec3, bool) {
	if tick-n.LastStuckCheck >= 100 {
		if tick > 0 && position.Sub(n.LastPosition).Len() < math.Max(.1, width*.1) {
			n.Path, n.Index, n.HasTarget = nil, 0, false
		}
		n.LastStuckCheck, n.LastPosition = tick, position
	}
	for n.Index < len(n.Path) {
		p := n.Path[n.Index]
		if position.Sub(p.Vec3().Add(mgl64.Vec3{.5, 0, .5})).Len() > math.Max(.5, width) {
			break
		}
		n.Index++
	}
	if n.Index >= len(n.Path) {
		return mgl64.Vec3{}, false
	}
	return n.Path[n.Index].Vec3().Add(mgl64.Vec3{.5, 0, .5}), true
}

func navigatorTick(e *Ent) int64 {
	return int64(e.Age() / (50 * 1e6))
}
