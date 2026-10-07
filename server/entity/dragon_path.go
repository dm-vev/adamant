package entity

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

var dragonNodeAdjacency = [24]uint32{
	6146, 8197, 8202, 16404, 32808, 32848, 65696, 131392,
	131712, 263424, 526848, 525313, 1581057, 3166214, 2138120, 6373424,
	4358208, 12910976, 9044480, 9706496, 15216640, 13688832, 11763712, 8257536,
}

func (b *EnderDragonBehaviour) ensureDragonNodes(tx *world.Tx) {
	if b.nodesInitialized {
		return
	}
	for i := range b.nodes {
		yAdjustment, multiplier := 5, i
		var x, z int
		switch {
		case i < 12:
			angle := float32(2) * (float32(-math.Pi) + float32(math.Pi/12)*float32(multiplier))
			x, z = int(math.Floor(float64(float32(60)*java26.Cos(float64(angle))))), int(math.Floor(float64(float32(60)*java26.Sin(float64(angle)))))
		case i < 20:
			multiplier -= 12
			angle := float32(2) * (float32(-math.Pi) + float32(math.Pi/8)*float32(multiplier))
			x, z = int(math.Floor(float64(float32(40)*java26.Cos(float64(angle))))), int(math.Floor(float64(float32(40)*java26.Sin(float64(angle)))))
			yAdjustment += 10
		default:
			multiplier -= 20
			angle := float32(2) * (float32(-math.Pi) + float32(math.Pi/4)*float32(multiplier))
			x, z = int(math.Floor(float64(float32(20)*java26.Cos(float64(angle))))), int(math.Floor(float64(float32(20)*java26.Sin(float64(angle)))))
		}
		y := max(73, tx.HighestBlock(x, z)+1+yAdjustment)
		b.nodes[i] = cube.Pos{x, y, z}
	}
	b.nodesInitialized = true
}

func (b *EnderDragonBehaviour) closestDragonNode(tx *world.Tx, x, y, z float64) int {
	b.ensureDragonNodes(tx)
	start := 0
	if b.fight == nil || b.fight.AliveCrystals() == 0 {
		start = 12
	}
	point := cube.Pos{int(math.Floor(x)), int(math.Floor(y)), int(math.Floor(z))}
	closest, distance := start, float64(10000)
	for i := start; i < len(b.nodes); i++ {
		delta := b.nodes[i].Sub(point)
		d := float64(delta[0]*delta[0] + delta[1]*delta[1] + delta[2]*delta[2])
		if d < distance {
			closest, distance = i, d
		}
	}
	return closest
}

func (b *EnderDragonBehaviour) findDragonPath(tx *world.Tx, start, end int, final *mgl64.Vec3) bool {
	b.ensureDragonNodes(tx)
	if start < 0 || start >= len(b.nodes) || end < 0 || end >= len(b.nodes) {
		return false
	}
	var g, f [24]float64
	var parent [24]int
	var open, closed [24]bool
	for i := range parent {
		parent[i] = -1
	}
	minimumNodeIndex := 0
	if b.fight == nil || b.fight.AliveCrystals() == 0 {
		minimumNodeIndex = 12
	}
	distance := func(a, c cube.Pos) float64 {
		dx, dy, dz := float64(a[0]-c[0]), float64(a[1]-c[1]), float64(a[2]-c[2])
		return math.Sqrt(dx*dx + dy*dy + dz*dz)
	}
	from, to := b.nodes[start], b.nodes[end]
	f[start] = distance(from, to)
	open[start] = true
	closest := start
	found := false
	for {
		current, best := -1, math.Inf(1)
		for i := range open {
			if open[i] && f[i] < best {
				current, best = i, f[i]
			}
		}
		if current < 0 {
			break
		}
		open[current] = false
		if current == end {
			found = true
			break
		}
		if distance(b.nodes[current], to) < distance(b.nodes[closest], to) {
			closest = current
		}
		closed[current] = true
		for i := minimumNodeIndex; i < len(b.nodes); i++ {
			if dragonNodeAdjacency[current]&(uint32(1)<<i) == 0 || closed[i] {
				continue
			}
			tentative := g[current] + distance(b.nodes[current], b.nodes[i])
			if !open[i] || tentative < g[i] {
				parent[i], g[i] = current, tentative
				f[i] = tentative + distance(b.nodes[i], to)
				open[i] = true
			}
		}
	}
	endNode := end
	if !found {
		if closest == start {
			return false
		}
		endNode = closest
	}
	var reverse [24]int
	length := 0
	for index := endNode; index >= 0 && length < len(reverse); index = parent[index] {
		reverse[length] = index
		length++
		if index == start {
			break
		}
	}
	if length == 0 || reverse[length-1] != start {
		return false
	}
	b.pathLen, b.pathIndex = 0, 0
	for i := length - 1; i >= 0; i-- {
		pos := b.nodes[reverse[i]]
		b.path[b.pathLen] = mgl64.Vec3{float64(pos[0]), float64(pos[1]), float64(pos[2])}
		b.pathLen++
	}
	if final != nil && endNode == end && b.pathLen < len(b.path) {
		b.path[b.pathLen] = *final
		b.pathLen++
	}
	return b.pathLen > 1
}

func (b *EnderDragonBehaviour) nextDragonPathTarget() bool {
	if b.pathIndex+1 >= b.pathLen {
		return false
	}
	b.pathIndex++
	pos := b.path[b.pathIndex]
	if b.pathIndex < b.pathLen-1 {
		pos[1] += rand.Float64() * 20
	}
	b.target, b.targetSet = pos, true
	return true
}

func (b *EnderDragonBehaviour) advanceDragonPathTarget(position mgl64.Vec3) bool {
	if !b.targetSet || b.target.Sub(position).LenSqr() < 100 {
		return b.nextDragonPathTarget()
	}
	return false
}

func (b *EnderDragonBehaviour) clearDragonPath() {
	b.pathLen, b.pathIndex = 0, 0
	b.targetSet = false
}
