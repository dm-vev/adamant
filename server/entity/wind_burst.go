package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// windBurstAffectedBlocks follows Explosion::explode's original boundary-ray
// pass (0x1134b44). Wind bursts ignore block resistance but still pay the native
// non-air step cost. Collection traversal determines block activation order.
func windBurstAffectedBlocks(origin mgl64.Vec3, power float32, nonAir func(cube.Pos) bool, random func() float32) []cube.Pos {
	positions := make([]cube.Pos, 0, 64)
	seen := make(map[cube.Pos]bool, 64)
	for x := range 16 {
		dx := float32(x) / 15
		dx = float32(dx+dx) - 1
		for y := range 16 {
			dy := float32(y) / 15
			dy = float32(dy+dy) - 1
			xy := float32(float32(dy*dy) + float32(dx*dx))
			for z := range 16 {
				if x != 0 && x != 15 && y != 0 && y != 15 && z != 0 && z != 15 {
					continue
				}
				strength := float32(float32(random()*.6)+.7) * power
				dz := float32(z) / 15
				dz = float32(dz+dz) - 1
				length := float32(math.Sqrt(float64(float32(float32(dz*dz) + xy))))
				step := [3]float32{float32(dx/length) * .3, float32(dy/length) * .3, float32(dz/length) * .3}
				p := [3]float32{float32(origin[0]), float32(origin[1]), float32(origin[2])}
				for strength > 0 {
					pos := cube.Pos{int(math.Floor(float64(p[0]))), int(math.Floor(float64(p[1]))), int(math.Floor(float64(p[2])))}
					if nonAir(pos) {
						strength -= float32(float32(.3) * float32(.3))
						if strength > 0 && !seen[pos] {
							seen[pos] = true
							positions = append(positions, pos)
						}
					}
					for axis := range p {
						p[axis] += step[axis]
					}
					strength += float32(-.225)
				}
			}
		}
	}
	return world.NativeBlockPositionOrder(positions)
}
