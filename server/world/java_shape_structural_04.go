package world

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
)

func init() {
	registerJavaShape("DecoratedPotBlock", javaStructural04WaterloggedShape)
	registerJavaShape("DoorBlock", javaStructural04DoorShape)
	registerJavaShape("EnderChestBlock", javaStructural04WaterloggedShape)
	registerJavaShape("FaceAttachedHorizontalDirectionalBlock", javaStructural04FaceAttachedShape)
	registerJavaShape("FenceBlock", javaStructural04FenceShape)

	registerJavaSurvival("DoorBlock", javaStructural04DoorSurvives)
	registerJavaSurvival("FaceAttachedHorizontalDirectionalBlock", javaStructural04FaceAttachedSurvives)
}

func javaStructural04WaterloggedShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if waterlogged, _ := gamedata.BlockStateBool(state, "waterlogged"); waterlogged {
		c.waterTick(state, c.pos)
	}
	return state
}

func javaStructural04DoorShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	half, _ := gamedata.BlockStateString(state, "half")
	if face.Axis() != cube.Y || (half == "lower") != (face == cube.FaceUp) {
		if half == "lower" && face == cube.FaceDown && !javaStructural04DoorSurvives(c, state, c.pos) {
			return javaDefault("air")
		}
		return state
	}
	neighborHalf, halfOK := gamedata.BlockStateString(neighbor, "half")
	if c.tag(neighbor, "minecraft:doors") && halfOK && neighborHalf != half {
		return javaWith(neighbor, "half", half)
	}
	return javaDefault("air")
}

func javaStructural04DoorSurvives(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	below := c.at(pos.Side(cube.FaceDown))
	half, _ := gamedata.BlockStateString(state, "half")
	if half == "lower" {
		return c.sturdy(below, cube.FaceUp, javaSupportFull)
	}
	return javaName(below) == javaName(state)
}

func javaStructural04FaceAttachedShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	connected := javaStructural04ConnectedDirection(state)
	if connected.Opposite() == face && !c.canSurvive(state, c.pos) {
		return javaDefault("air")
	}
	return state
}

func javaStructural04FaceAttachedSurvives(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	direction := javaStructural04ConnectedDirection(state).Opposite()
	support := c.at(pos.Side(direction))
	return c.sturdy(support, direction.Opposite(), javaSupportFull)
}

func javaStructural04ConnectedDirection(state uint32) cube.Face {
	face, _ := gamedata.BlockStateString(state, "face")
	switch face {
	case "ceiling":
		return cube.FaceDown
	case "floor":
		return cube.FaceUp
	default:
		return javaStructural04Facing(state)
	}
}

func javaStructural04Facing(state uint32) cube.Face {
	facing, _ := gamedata.BlockStateString(state, "facing")
	switch facing {
	case "north":
		return cube.FaceNorth
	case "south":
		return cube.FaceSouth
	case "west":
		return cube.FaceWest
	case "east":
		return cube.FaceEast
	default:
		panic("invalid horizontal facing in FaceAttachedHorizontalDirectionalBlock")
	}
}

func javaStructural04FenceShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if waterlogged, _ := gamedata.BlockStateBool(state, "waterlogged"); waterlogged {
		c.waterTick(state, c.pos)
	}
	if face.Axis() == cube.Y {
		return state
	}
	property := "north"
	switch face {
	case cube.FaceNorth:
		property = "north"
	case cube.FaceEast:
		property = "east"
	case cube.FaceSouth:
		property = "south"
	case cube.FaceWest:
		property = "west"
	default:
		panic("invalid horizontal face in FenceBlock.updateShape")
	}
	return javaWith(state, property, javaStructural04FenceConnects(c, state, neighbor, face.Opposite()))
}

func javaStructural04FenceConnects(c *javaShapeContext, fenceState, neighbor uint32, towardsFence cube.Face) bool {
	fence := c.tag(neighbor, "minecraft:fences") && c.tag(neighbor, "minecraft:wooden_fences") == c.tag(fenceState, "minecraft:wooden_fences")
	gate := c.tag(neighbor, "minecraft:fence_gates")
	if gate {
		facing := javaStructural04Facing(neighbor)
		gate = facing.Axis() != towardsFence.Axis()
	}
	exception, err := gamedata.BlockExceptionForConnectionID(neighbor)
	if err != nil {
		panic(err)
	}
	return (!exception && c.sturdy(neighbor, towardsFence, javaSupportFull)) || fence || gate
}
