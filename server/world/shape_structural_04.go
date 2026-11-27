package world

import (
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/internal/gamedata"
)

func init() {
	registerShape("DecoratedPotBlock", structural04WaterloggedShape)
	registerShape("DoorBlock", structural04DoorShape)
	registerShape("EnderChestBlock", structural04WaterloggedShape)
	registerShape("FaceAttachedHorizontalDirectionalBlock", structural04FaceAttachedShape)
	registerShape("FenceBlock", structural04FenceShape)

	registerSurvival("DoorBlock", structural04DoorSurvives)
	registerSurvival("FaceAttachedHorizontalDirectionalBlock", structural04FaceAttachedSurvives)
}

func structural04WaterloggedShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if waterlogged, _ := gamedata.BlockStateBool(state, "waterlogged"); waterlogged {
		c.waterTick(state, c.pos)
	}
	return state
}

func structural04DoorShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	half, _ := gamedata.BlockStateString(state, "half")
	if face.Axis() != cube.Y || (half == "lower") != (face == cube.FaceUp) {
		if half == "lower" && face == cube.FaceDown && !structural04DoorSurvives(c, state, c.pos) {
			return stateDefault("air")
		}
		return state
	}
	neighborHalf, halfOK := gamedata.BlockStateString(neighbor, "half")
	if c.tag(neighbor, "minecraft:doors") && halfOK && neighborHalf != half {
		return stateWith(neighbor, "half", half)
	}
	return stateDefault("air")
}

func structural04DoorSurvives(c *shapeContext, state uint32, pos cube.Pos) bool {
	below := c.at(pos.Side(cube.FaceDown))
	half, _ := gamedata.BlockStateString(state, "half")
	if half == "lower" {
		return c.sturdy(below, cube.FaceUp, supportFull)
	}
	return stateName(below) == stateName(state)
}

func structural04FaceAttachedShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	connected := structural04ConnectedDirection(state)
	if connected.Opposite() == face && !c.canSurvive(state, c.pos) {
		return stateDefault("air")
	}
	return state
}

func structural04FaceAttachedSurvives(c *shapeContext, state uint32, pos cube.Pos) bool {
	direction := structural04ConnectedDirection(state).Opposite()
	support := c.at(pos.Side(direction))
	return c.sturdy(support, direction.Opposite(), supportFull)
}

func structural04ConnectedDirection(state uint32) cube.Face {
	face, _ := gamedata.BlockStateString(state, "face")
	switch face {
	case "ceiling":
		return cube.FaceDown
	case "floor":
		return cube.FaceUp
	default:
		return structural04Facing(state)
	}
}

func structural04Facing(state uint32) cube.Face {
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

func structural04FenceShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
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
	return stateWith(state, property, structural04FenceConnects(c, state, neighbor, face.Opposite()))
}

func structural04FenceConnects(c *shapeContext, fenceState, neighbor uint32, towardsFence cube.Face) bool {
	fence := c.tag(neighbor, "minecraft:fences") && c.tag(neighbor, "minecraft:wooden_fences") == c.tag(fenceState, "minecraft:wooden_fences")
	gate := c.tag(neighbor, "minecraft:fence_gates")
	if gate {
		facing := structural04Facing(neighbor)
		gate = facing.Axis() != towardsFence.Axis()
	}
	exception, err := gamedata.BlockExceptionForConnectionID(neighbor)
	if err != nil {
		panic(err)
	}
	return (!exception && c.sturdy(neighbor, towardsFence, supportFull)) || fence || gate
}
