package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
)

func minecartVec32(v mgl64.Vec3) mgl64.Vec3 {
	return mgl64.Vec3{float64(float32(v[0])), float64(float32(v[1])), float64(float32(v[2]))}
}

func minecartCentre(feet mgl64.Vec3) mgl64.Vec3 {
	feet = minecartVec32(feet)
	feet[1] = float64(float32(feet[1]) + float32(minecartNetworkOffset))
	return feet
}

func minecartFeet(centre mgl64.Vec3) mgl64.Vec3 {
	centre = minecartVec32(centre)
	centre[1] = float64(float32(centre[1]) - float32(minecartNetworkOffset))
	return centre
}

func minecartBBox(e world.Entity) cube.BBox {
	if e == nil {
		return cube.Box(-.49, 0, -.49, .49, .7, .49)
	}
	pos := e.Position()
	if ent, ok := e.(interface{ Behaviour() Behaviour }); ok {
		if b := minecartBaseBehaviour(ent.Behaviour()); b != nil && b.mc.nativeBoxSet && b.mc.nativeBoxPosition == pos {
			lo, hi := b.mc.nativeBox.Min(), b.mc.nativeBox.Max()
			return cube.Box(float64(lo[0]), float64(lo[1]), float64(lo[2]), float64(hi[0]), float64(hi[1]), float64(hi[2])).Translate(pos.Mul(-1))
		}
	}
	c := minecartCentre(pos)
	x, y, z := float32(c[0]), float32(c[1]), float32(c[2])
	return cube.Box(float64(x-.49), float64(y-.35), float64(z-.49), float64(x+.49), float64(y+.35), float64(z+.49)).Translate(pos.Mul(-1))
}

func minecartHorizontalSpeed(v mgl32.Vec3) float32 {
	return float32(math.Sqrt(float64(float32(v[0]*v[0]) + float32(v[2]*v[2]))))
}

// applyNaturalSlowdown follows Minecart::preNormalTick's virtual call, before
// rail projection and calculateMoveVelocity. Container carts use the native
// comparator fullness signal, including its binary32 accumulation.
func (b *MinecartBehaviour) applyNaturalSlowdown(e *Ent, vel *mgl64.Vec3) {
	factor := float32(.96)
	if b.passenger != nil || !b.slowWhenEmpty {
		factor = .997
	}
	var inv *inventory.Inventory
	switch behaviour := e.Behaviour().(type) {
	case *MinecartContainerBehaviour:
		inv = behaviour.inv
	case *HopperMinecartBehaviour:
		inv = behaviour.inv
	case *commandBlockMinecartBehaviour:
		inv = behaviour.inv
	}
	if inv != nil && inv.Size() != 0 {
		var fraction float32
		nonempty := false
		for _, stack := range inv.Slots() {
			if !stack.Empty() {
				nonempty = true
				fraction += float32(stack.Count()) / float32(max(1, stack.MaxCount()))
			}
		}
		signal := 0
		if nonempty {
			signal = int(math.Floor(float64(float32(fraction/float32(inv.Size()))*14))) + 1
		}
		factor = float32(.98) + float32(15-min(15, signal))*float32(.001)
	}
	*vel = mgl64.Vec3{float64(float32((*vel)[0]) * factor), 0, float64(float32((*vel)[2]) * factor)}
}

// moveOnRails follows the native ECS pre-move, move and post-move stages
// (0x95e7df0, 0x33db740, 0x33dbac0). Positions here address the actor centre.
func (b *MinecartBehaviour) moveOnRails(e *Ent, tx *world.Tx, railPos cube.Pos, rail world.Block, pos, vel mgl64.Vec3) (mgl64.Vec3, mgl64.Vec3) {
	v := mgl32.Vec3{float32(vel[0]), float32(vel[1]), float32(vel[2])}
	before, beforeOK := b.nextRailPos(tx, pos)
	dir, _, _ := block.RailInfo(rail)
	facing := minecartMatrix[int(dir)]
	y := float32(railPos[1])
	if dir.Ascending() {
		y++
	}
	pos[1] = float64(float32(y+float32(minecartNetworkOffset)) + float32(.00001))

	x0 := float32(float32(railPos[0])+.5) + float32(facing[0][0])*.5
	z0 := float32(float32(railPos[2])+.5) + float32(facing[0][2])*.5
	x1 := float32(float32(railPos[0])+.5) + float32(facing[1][0])*.5
	z1 := float32(float32(railPos[2])+.5) + float32(facing[1][2])*.5
	dx, dz := x1-x0, z1-z0
	var along float32
	switch {
	case dx == 0:
		along = float32(pos[2]) - float32(railPos[2])
	case dz == 0:
		along = float32(pos[0]) - float32(railPos[0])
	default:
		along = (float32(float32(pos[0])-x0)*dx + float32(float32(pos[2])-z0)*dz) * 2
	}
	pos[0], pos[2] = float64(x0+float32(dx*along)), float64(z0+float32(dz*along))

	switch dir {
	case block.RailAscendingNorth:
		v[2] += .0078125
	case block.RailAscendingSouth:
		v[2] -= .0078125
	case block.RailAscendingEast:
		v[0] -= .0078125
	case block.RailAscendingWest:
		v[0] += .0078125
	}
	fx, fz := float32(facing[1][0]-facing[0][0]), float32(facing[1][2]-facing[0][2])
	if float32(v[0]*fx)+float32(v[2]*fz) < 0 {
		fx, fz = -fx, -fz
	}
	speed := min(float32(2), minecartHorizontalSpeed(v))
	length := float32(math.Sqrt(float64(float32(fx*fx) + float32(fz*fz))))
	v[0], v[2] = float32(speed*fx)/length, float32(speed*fz)/length
	pr, poweredRail := rail.(block.PoweredRail)
	slowed := poweredRail && !pr.Powered
	if b.passenger != nil && b.currentSpeed > 0 {
		if rider, ok := b.passenger.Entity(tx); ok && float32(v[0]*v[0])+float32(v[2]*v[2]) < .01 {
			yaw := rider.Rotation().Yaw() * math.Pi / 180
			v[0] += float32(-math.Sin(yaw)) * .1
			v[2] += float32(math.Cos(yaw)) * .1
			slowed = false
		}
	}
	if slowed {
		if float32(v[0]*v[0])+float32(v[2]*v[2]) < float32(.0009) {
			v = mgl32.Vec3{}
		} else {
			v[0], v[1], v[2] = v[0]*.5, 0, v[2]*.5
		}
	}
	motion := v
	if b.passenger != nil {
		motion[0], motion[2] = motion[0]*.75, motion[2]*.75
	}
	maximum := float32(b.maxSpeed)
	delta := mgl64.Vec3{float64(max(-maximum, min(maximum, motion[0]))), 0, float64(max(-maximum, min(maximum, motion[2])))}
	pos, vel = b.moveWithCollision(e, tx, pos, delta, mgl64.Vec3{float64(v[0]), float64(v[1]), float64(v[2])})
	v = mgl32.Vec3{float32(vel[0]), float32(vel[1]), float32(vel[2])}
	for _, end := range facing {
		if end[1] != 0 && int(math.Floor(pos[0]))-railPos[0] == end[0] && int(math.Floor(pos[2]))-railPos[2] == end[2] {
			pos[1] = float64(float32(pos[1]) + float32(end[1]))
			break
		}
	}
	if after, ok := b.nextRailPos(tx, pos); ok && beforeOK {
		speed = minecartHorizontalSpeed(v)
		if speed > 0 {
			change := float32(float32(before[1])-float32(after[1])) * .05
			v[0], v[2] = float32(v[0]/speed)*float32(speed+change), float32(v[2]/speed)*float32(speed+change)
		}
		pos[1] = after[1]
	}
	ix, iz := int(math.Floor(pos[0]))-railPos[0], int(math.Floor(pos[2]))-railPos[2]
	if ix != 0 || iz != 0 {
		speed = minecartHorizontalSpeed(v)
		v[0], v[2] = speed*float32(ix), speed*float32(iz)
	}
	if poweredRail && pr.Powered {
		speed = minecartHorizontalSpeed(v)
		if speed > .01 {
			v[0] += float32(v[0]/speed) * .06
			v[2] += float32(v[2]/speed) * .06
		} else if dir == block.RailNorthSouth || dir == block.RailEastWest {
			negative, positive, axis := cube.FaceNorth, cube.FaceSouth, 2
			if dir == block.RailEastWest {
				negative, positive, axis = cube.FaceWest, cube.FaceEast, 0
			}
			p, n := railPos.Side(positive), railPos.Side(negative)
			if tx.Block(n).Model().FaceSolid(n, positive, tx) {
				v[axis] = .02
			} else if tx.Block(p).Model().FaceSolid(p, negative, tx) {
				v[axis] = -.02
			}
		}
	}
	return pos, mgl64.Vec3{float64(v[0]), float64(v[1]), float64(v[2])}
}

func (b *MinecartBehaviour) nextRailPos(tx *world.Tx, pos mgl64.Vec3) (mgl64.Vec3, bool) {
	p := cube.PosFromVec3(pos)
	if isRailAt(tx, p.Side(cube.FaceDown)) {
		p = p.Side(cube.FaceDown)
	}
	rail := tx.Block(p)
	if !block.IsRail(rail) {
		return mgl64.Vec3{}, false
	}
	dir, _, _ := block.RailInfo(rail)
	ends := minecartMatrix[int(dir)]
	var a, z mgl32.Vec3
	for axis := range 3 {
		a[axis] = float32(float32(p[axis])+.5) + float32(ends[0][axis])*.5
		z[axis] = float32(float32(p[axis])+.5) + float32(ends[1][axis])*.5
	}
	d := z.Sub(a)
	d[1] *= 2
	var along float32
	switch {
	case d[0] == 0:
		along = float32(pos[2]) - float32(p[2])
	case d[2] == 0:
		along = float32(pos[0]) - float32(p[0])
	default:
		along = (float32(float32(pos[0])-a[0])*d[0] + float32(float32(pos[2])-a[2])*d[2]) * 2
	}
	for axis := range 3 {
		a[axis] += float32(d[axis] * along)
	}
	if d[1] < 0 {
		a[1]++
	} else if d[1] > 0 {
		a[1] += .5
	}
	return mgl64.Vec3{float64(a[0]), float64(a[1]), float64(a[2])}, true
}

func (b *MinecartBehaviour) moveDerailed(e *Ent, tx *world.Tx, pos, vel mgl64.Vec3) (mgl64.Vec3, mgl64.Vec3) {
	vel[0] = clamp(vel[0], -b.maxSpeed, b.maxSpeed)
	vel[2] = clamp(vel[2], -b.maxSpeed, b.maxSpeed)
	if b.mc.onGround {
		for axis := range 3 {
			vel[axis] = float64(float32(vel[axis]) * float32(b.derailedVelocityMod[axis]))
		}
	}
	pos, vel = b.moveWithCollision(e, tx, pos, minecartVec32(vel), minecartVec32(vel))
	if !b.mc.onGround {
		for axis := range 3 {
			vel[axis] = float64(float32(vel[axis]) * float32(b.flyingVelocityMod[axis]))
		}
	}
	return pos, vel
}

func (b *MinecartBehaviour) moveWithCollision(e *Ent, tx *world.Tx, centre, delta, vel mgl64.Vec3) (mgl64.Vec3, mgl64.Vec3) {
	feet := minecartFeet(centre)
	dPos, moved := b.mc.checkCollision(tx, e, feet, delta)
	centre = minecartCentre(feet.Add(dPos))
	for axis := range 3 {
		if moved[axis] != delta[axis] {
			vel[axis] = 0
		}
	}
	return centre, vel
}
