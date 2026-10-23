package player

import (
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func bedStandUpPosition(tx *world.Tx, pos cube.Pos, forward cube.Direction, yaw float64) (mgl64.Vec3, bool) {
	right := forward.RotateRight()
	rightStep := cube.Pos{}.Side(right.Face())
	radians := float64(float32(yaw) * float32(math.Pi/180))
	side := right
	if float32(rightStep[0])*(-vanilla.Sin(radians))+float32(rightStep[2])*vanilla.Cos(radians) > 0 {
		side = right.Opposite()
	}
	f, s := cube.Pos{}.Side(forward.Face()), cube.Pos{}.Side(side.Face())
	back, oppositeSide := cube.Pos{-f[0], 0, -f[2]}, cube.Pos{-s[0], 0, -s[2]}
	backTwo := back.Add(back)
	offsets := [10]cube.Pos{s, s.Add(back), s.Add(backTwo), backTwo, oppositeSide.Add(backTwo), oppositeSide.Add(back), oppositeSide, oppositeSide.Add(f), f, s.Add(f)}
	above := [2]cube.Pos{{}, back}
	search := func(base cube.Pos, offsets []cube.Pos, dangerous bool) (mgl64.Vec3, bool) {
		for _, offset := range offsets {
			if p, ok := bedSafeStandUp(tx, base.Add(offset), dangerous); ok {
				return p, true
			}
		}
		return mgl64.Vec3{}, false
	}
	_, ordinaryBunk := tx.Block(pos.Side(cube.FaceDown)).(block.Bed)
	_, strawBunk := tx.Block(pos.Side(cube.FaceDown)).(block.StrawBed)
	for _, dangerous := range [...]bool{true, false} {
		if p, ok := search(pos, offsets[:], dangerous); ok {
			return p, true
		}
		if ordinaryBunk || strawBunk {
			if p, ok := search(pos.Side(cube.FaceDown), offsets[:], dangerous); ok {
				return p, true
			}
		}
		if p, ok := search(pos, above[:], dangerous); ok {
			return p, true
		}
	}
	return mgl64.Vec3{}, false
}

func bedStateName(tx *world.Tx, pos cube.Pos) (uint32, string) {
	id, err := tx.BlockStateID(pos)
	if err != nil {
		panic(err)
	}
	name, ok := gamedata.BlockStateName(id)
	if !ok {
		panic("bed standup has no Source state")
	}
	return id, strings.TrimPrefix(name, "minecraft:")
}
func bedStateTag(name, tag string) bool {
	ok, err := gamedata.BlockTagContains(tag, name)
	if err != nil {
		panic(err)
	}
	return ok
}
func bedFloorHeight(tx *world.Tx, pos cube.Pos) float64 {
	id, name := bedStateName(tx, pos)
	open, _ := gamedata.BlockStateBool(id, "open")
	if bedStateTag(name, "minecraft:climbable") || strings.HasSuffix(name, "_trapdoor") && open {
		return math.Inf(-1)
	}
	height := math.Inf(-1)
	for _, box := range tx.Block(pos).Model().BBox(pos, tx) {
		height = max(height, box.Max()[1])
	}
	return height
}
func bedDangerous(tx *world.Tx, pos cube.Pos) bool {
	id, name := bedStateName(tx, pos)
	switch name {
	case "wither_rose", "sweet_berry_bush", "cactus", "powder_snow", "lava", "magma_block", "lava_cauldron":
		return true
	case "campfire", "soul_campfire":
		lit, _ := gamedata.BlockStateBool(id, "lit")
		return lit
	}
	return bedStateTag(name, "minecraft:fire")
}
func bedSafeStandUp(tx *world.Tx, pos cube.Pos, checkDangerous bool) (mgl64.Vec3, bool) {
	if checkDangerous && bedDangerous(tx, pos) {
		return mgl64.Vec3{}, false
	}
	floor := bedFloorHeight(tx, pos)
	if math.IsInf(floor, -1) {
		below := bedFloorHeight(tx, pos.Side(cube.FaceDown))
		if below >= 1 {
			floor = below - 1
		}
	}
	if math.IsInf(floor, 0) || floor >= 1 {
		return mgl64.Vec3{}, false
	}
	if checkDangerous && floor <= 0 && bedDangerous(tx, pos.Side(cube.FaceDown)) {
		return mgl64.Vec3{}, false
	}
	for _, p := range [...]cube.Pos{pos, pos.Side(cube.FaceUp)} {
		_, name := bedStateName(tx, p)
		if bedStateTag(name, "minecraft:invalid_spawn_inside") {
			return mgl64.Vec3{}, false
		}
	}
	location := pos.Vec3Middle().Add(mgl64.Vec3{0, floor, 0})
	halfWidth, height := float64(float32(.6)/2), float64(float32(1.8))
	bbox := cube.Box(-halfWidth, 0, -halfWidth, halfWidth, height, halfWidth).Translate(location)
	min, max := bbox.Min(), bbox.Max()
	for x := int(math.Floor(min[0])); x <= int(math.Floor(max[0])); x++ {
		for y := int(math.Floor(min[1])); y <= int(math.Floor(max[1])); y++ {
			for z := int(math.Floor(min[2])); z <= int(math.Floor(max[2])); z++ {
				p := cube.Pos{x, y, z}
				for _, box := range tx.Block(p).Model().BBox(p, tx) {
					if bbox.IntersectsWith(box.Translate(p.Vec3())) {
						return mgl64.Vec3{}, false
					}
				}
			}
		}
	}
	if !tx.World().WithinBorderBounds(bbox) {
		return mgl64.Vec3{}, false
	}
	return location, true
}

func (p *Player) standUpFromBed(tx *world.Tx, pos cube.Pos, facing cube.Direction) {
	stand, ok := bedStandUpPosition(tx, pos, facing, p.Rotation().Yaw())
	if !ok {
		stand = pos.Vec3Middle().Add(mgl64.Vec3{0, 1.1, 0})
	}
	look := pos.Vec3Middle().Sub(stand)
	if length := look.Len(); length >= float64(float32(1e-5)) {
		look = mgl64.Vec3{look[0] / length, look[1] / length, look[2] / length}
	} else {
		look = mgl64.Vec3{}
	}
	yaw := vanilla.Atan2(look[2], look[0])*180/float64(float32(math.Pi)) - 90
	yaw = math.Mod(yaw, 360)
	if yaw >= 180 {
		yaw -= 360
	}
	if yaw < -180 {
		yaw += 360
	}
	p.data.Pos, p.data.Rot = stand, cube.Rotation{float64(float32(yaw)), 0}
}
