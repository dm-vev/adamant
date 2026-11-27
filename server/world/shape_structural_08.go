package world

import (
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/internal/gamedata"
)

func init() {
	registerShape("WallTorchBlock", structural08WallTorchShape)
	registerShape("WaterloggedTransparentBlock", structural08WaterloggedTransparentShape)
	registerShape("ConduitBlock", structural08ConduitShape)
	registerShape("ShelfBlock", structural08ShelfShape)

	registerSurvival("WallTorchBlock", structural08WallTorchSurvives)
}

func structural08WallTorchShape(c *shapeContext, state uint32, face cube.Face, _ uint32) uint32 {
	facing := structural08Facing(state)
	if face == facing.Opposite() && !structural08WallTorchSurvives(c, state, c.pos) {
		return stateDefault("air")
	}
	return state
}

func structural08WallTorchSurvives(c *shapeContext, state uint32, pos cube.Pos) bool {
	facing := structural08Facing(state)
	supportPos := pos.Side(facing.Opposite())
	support := c.at(supportPos)
	return c.sturdy(support, facing, supportFull)
}

func structural08WaterloggedTransparentShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if waterlogged, _ := gamedata.BlockStateBool(state, "waterlogged"); waterlogged {
		c.waterTick(state, c.pos)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func structural08ConduitShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if waterlogged, _ := gamedata.BlockStateBool(state, "waterlogged"); waterlogged {
		c.waterTick(state, c.pos)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func structural08ShelfShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if waterlogged, _ := gamedata.BlockStateBool(state, "waterlogged"); waterlogged {
		c.waterTick(state, c.pos)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func structural08Facing(state uint32) cube.Face {
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
		panic("invalid horizontal facing in WallTorchBlock")
	}
}
