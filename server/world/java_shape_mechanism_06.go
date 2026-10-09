package world

import (
	"github.com/df-mc/dragonfly/server/block/cube"
)

func init() {
	registerJavaShape("SpeleothemBlock", javaMechanism06SpeleothemShape)
	registerJavaShape("TripWireBlock", javaMechanism06TripWireShape)
	registerJavaShape("TripWireHookBlock", javaMechanism06TripWireHookShape)
	registerJavaShape("PistonHeadBlock", javaMechanism06PistonHeadShape)
	registerJavaSurvival("SpeleothemBlock", javaMechanism06SpeleothemSurvival)
	registerJavaSurvival("TripWireHookBlock", javaMechanism06TripWireHookSurvival)
	registerJavaSurvival("PistonHeadBlock", javaMechanism06PistonHeadSurvival)
}

func javaMechanism06SpeleothemSurvival(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	tip := javaMechanism06VerticalFace(javaString(state, "vertical_direction"))
	basePos := pos.Side(tip.Opposite())
	base := c.at(basePos)
	return c.sturdy(base, tip, javaSupportFull) ||
		javaName(base) == javaName(state) && javaString(base, "vertical_direction") == javaMechanism06FaceName(tip)
}

func javaMechanism06SpeleothemShape(c *javaShapeContext, state uint32, face cube.Face, _ uint32) uint32 {
	pos := c.pos
	if javaBool(state, "waterlogged") {
		c.waterTick(state, pos)
	}
	if face != cube.FaceDown && face != cube.FaceUp {
		return state
	}
	tip := javaMechanism06VerticalFace(javaString(state, "vertical_direction"))
	if tip == cube.FaceDown && c.hasBlockTick(pos, state) {
		return state
	}
	if face == tip.Opposite() && !javaMechanism06SpeleothemSurvival(c, state, pos) {
		delay := int32(1)
		if tip == cube.FaceDown {
			delay = 2
		}
		c.blockTick(pos, state, delay)
		return state
	}
	return javaWith(state, "thickness", javaMechanism06Thickness(c, state, tip))
}

func javaMechanism06Thickness(c *javaShapeContext, state uint32, tip cube.Face) string {
	base := tip.Opposite()
	inFront := c.at(c.pos.Side(tip))
	inFrontIsSpeleothem := c.tag(inFront, "speleothems")
	inFrontDirMatchesBase := inFrontIsSpeleothem && javaString(inFront, "vertical_direction") == javaMechanism06FaceName(base)
	inFrontDirMatchesTip := inFrontIsSpeleothem && javaString(inFront, "vertical_direction") == javaMechanism06FaceName(tip)
	if inFrontDirMatchesBase && javaName(inFront) == javaName(state) {
		if javaString(state, "thickness") != "tip_merge" && javaString(inFront, "thickness") != "tip_merge" {
			return "tip"
		}
		return "tip_merge"
	}
	if !inFrontDirMatchesTip {
		return "tip"
	}
	inFrontThickness := javaString(inFront, "thickness")
	if inFrontThickness == "tip" || inFrontThickness == "tip_merge" {
		return "frustum"
	}
	behind := c.at(c.pos.Side(base))
	if !c.tag(behind, "speleothems") || javaString(behind, "vertical_direction") != javaMechanism06FaceName(tip) {
		return "base"
	}
	return "middle"
}

func javaMechanism06TripWireShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceNorth || face == cube.FaceSouth || face == cube.FaceWest || face == cube.FaceEast {
		connect := javaName(neighbor) == "tripwire_hook" && javaString(neighbor, "facing") == javaMechanism06FaceName(face.Opposite()) || javaName(neighbor) == "tripwire"
		return javaWith(state, javaMechanism06FaceName(face), connect)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaMechanism06TripWireHookSurvival(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	facing := javaMechanism06HorizontalFace(javaString(state, "facing"))
	if facing == cube.FaceDown {
		return false
	}
	return c.sturdy(c.at(pos.Side(facing.Opposite())), facing, javaSupportFull)
}

func javaMechanism06TripWireHookShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	facing := javaMechanism06HorizontalFace(javaString(state, "facing"))
	if face == facing.Opposite() && !javaMechanism06TripWireHookSurvival(c, state, c.pos) {
		return javaDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaMechanism06PistonHeadSurvival(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	facing := javaMechanism06AnyFace(javaString(state, "facing"))
	base := c.at(pos.Side(facing.Opposite()))
	baseName := javaName(base)
	baseType := "piston"
	if javaString(state, "type") == "sticky" {
		baseType = "sticky_piston"
	}
	if baseName == baseType {
		return javaBool(base, "extended") && javaString(base, "facing") == javaMechanism06FaceName(facing)
	}
	return baseName == "moving_piston" && javaString(base, "facing") == javaMechanism06FaceName(facing)
}

func javaMechanism06PistonHeadShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	facing := javaMechanism06AnyFace(javaString(state, "facing"))
	if face == facing.Opposite() && !javaMechanism06PistonHeadSurvival(c, state, c.pos) {
		return javaDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaMechanism06AnyFace(name string) cube.Face {
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

func javaMechanism06VerticalFace(name string) cube.Face {
	face := javaMechanism06AnyFace(name)
	if face != cube.FaceDown && face != cube.FaceUp {
		panic("invalid vertical Java direction " + name)
	}
	return face
}

func javaMechanism06HorizontalFace(name string) cube.Face {
	face := javaMechanism06AnyFace(name)
	if face == cube.FaceDown || face == cube.FaceUp {
		return cube.FaceDown
	}
	return face
}

func javaMechanism06FaceName(face cube.Face) string {
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
