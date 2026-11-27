package world

import (
	"github.com/dm-vev/adamant/server/block/cube"
)

func init() {
	registerShape("SpeleothemBlock", mechanism06SpeleothemShape)
	registerShape("TripWireBlock", mechanism06TripWireShape)
	registerShape("TripWireHookBlock", mechanism06TripWireHookShape)
	registerShape("PistonHeadBlock", mechanism06PistonHeadShape)
	registerSurvival("SpeleothemBlock", mechanism06SpeleothemSurvival)
	registerSurvival("TripWireHookBlock", mechanism06TripWireHookSurvival)
	registerSurvival("PistonHeadBlock", mechanism06PistonHeadSurvival)
}

func mechanism06SpeleothemSurvival(c *shapeContext, state uint32, pos cube.Pos) bool {
	tip := mechanism06VerticalFace(stateString(state, "vertical_direction"))
	basePos := pos.Side(tip.Opposite())
	base := c.at(basePos)
	return c.sturdy(base, tip, supportFull) ||
		stateName(base) == stateName(state) && stateString(base, "vertical_direction") == mechanism06FaceName(tip)
}

func mechanism06SpeleothemShape(c *shapeContext, state uint32, face cube.Face, _ uint32) uint32 {
	pos := c.pos
	if stateBool(state, "waterlogged") {
		c.waterTick(state, pos)
	}
	if face != cube.FaceDown && face != cube.FaceUp {
		return state
	}
	tip := mechanism06VerticalFace(stateString(state, "vertical_direction"))
	if tip == cube.FaceDown && c.hasBlockTick(pos, state) {
		return state
	}
	if face == tip.Opposite() && !mechanism06SpeleothemSurvival(c, state, pos) {
		delay := int32(1)
		if tip == cube.FaceDown {
			delay = 2
		}
		c.blockTick(pos, state, delay)
		return state
	}
	return stateWith(state, "thickness", mechanism06Thickness(c, state, tip))
}

func mechanism06Thickness(c *shapeContext, state uint32, tip cube.Face) string {
	base := tip.Opposite()
	inFront := c.at(c.pos.Side(tip))
	inFrontIsSpeleothem := c.tag(inFront, "speleothems")
	inFrontDirMatchesBase := inFrontIsSpeleothem && stateString(inFront, "vertical_direction") == mechanism06FaceName(base)
	inFrontDirMatchesTip := inFrontIsSpeleothem && stateString(inFront, "vertical_direction") == mechanism06FaceName(tip)
	if inFrontDirMatchesBase && stateName(inFront) == stateName(state) {
		if stateString(state, "thickness") != "tip_merge" && stateString(inFront, "thickness") != "tip_merge" {
			return "tip"
		}
		return "tip_merge"
	}
	if !inFrontDirMatchesTip {
		return "tip"
	}
	inFrontThickness := stateString(inFront, "thickness")
	if inFrontThickness == "tip" || inFrontThickness == "tip_merge" {
		return "frustum"
	}
	behind := c.at(c.pos.Side(base))
	if !c.tag(behind, "speleothems") || stateString(behind, "vertical_direction") != mechanism06FaceName(tip) {
		return "base"
	}
	return "middle"
}

func mechanism06TripWireShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceNorth || face == cube.FaceSouth || face == cube.FaceWest || face == cube.FaceEast {
		connect := stateName(neighbor) == "tripwire_hook" && stateString(neighbor, "facing") == mechanism06FaceName(face.Opposite()) || stateName(neighbor) == "tripwire"
		return stateWith(state, mechanism06FaceName(face), connect)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func mechanism06TripWireHookSurvival(c *shapeContext, state uint32, pos cube.Pos) bool {
	facing := mechanism06HorizontalFace(stateString(state, "facing"))
	if facing == cube.FaceDown {
		return false
	}
	return c.sturdy(c.at(pos.Side(facing.Opposite())), facing, supportFull)
}

func mechanism06TripWireHookShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	facing := mechanism06HorizontalFace(stateString(state, "facing"))
	if face == facing.Opposite() && !mechanism06TripWireHookSurvival(c, state, c.pos) {
		return stateDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func mechanism06PistonHeadSurvival(c *shapeContext, state uint32, pos cube.Pos) bool {
	facing := mechanism06AnyFace(stateString(state, "facing"))
	base := c.at(pos.Side(facing.Opposite()))
	baseName := stateName(base)
	baseType := "piston"
	if stateString(state, "type") == "sticky" {
		baseType = "sticky_piston"
	}
	if baseName == baseType {
		return stateBool(base, "extended") && stateString(base, "facing") == mechanism06FaceName(facing)
	}
	return baseName == "moving_piston" && stateString(base, "facing") == mechanism06FaceName(facing)
}

func mechanism06PistonHeadShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	facing := mechanism06AnyFace(stateString(state, "facing"))
	if face == facing.Opposite() && !mechanism06PistonHeadSurvival(c, state, c.pos) {
		return stateDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func mechanism06AnyFace(name string) cube.Face {
	switch name {
	case "down":
		return cube.FaceDown
	case "up":
		return cube.FaceUp
	case "north":
		return cube.FaceNorth
	case "south":
		return cube.FaceSouth
	case "west":
		return cube.FaceWest
	case "east":
		return cube.FaceEast
	default:
		panic("invalid Java direction " + name)
	}
}

func mechanism06VerticalFace(name string) cube.Face {
	face := mechanism06AnyFace(name)
	if face != cube.FaceDown && face != cube.FaceUp {
		panic("invalid vertical Java direction " + name)
	}
	return face
}

func mechanism06HorizontalFace(name string) cube.Face {
	face := mechanism06AnyFace(name)
	if face == cube.FaceDown || face == cube.FaceUp {
		return cube.FaceDown
	}
	return face
}

func mechanism06FaceName(face cube.Face) string {
	switch face {
	case cube.FaceDown:
		return "down"
	case cube.FaceUp:
		return "up"
	case cube.FaceNorth:
		return "north"
	case cube.FaceSouth:
		return "south"
	case cube.FaceWest:
		return "west"
	case cube.FaceEast:
		return "east"
	default:
		panic("invalid cube face")
	}
}
