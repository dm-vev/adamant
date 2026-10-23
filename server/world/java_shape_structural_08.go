package world

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
)

func init() {
	registerJavaShape("WallTorchBlock", javaStructural08WallTorchShape)
	registerJavaShape("WaterloggedTransparentBlock", javaStructural08WaterloggedTransparentShape)
	registerJavaShape("ConduitBlock", javaStructural08ConduitShape)
	registerJavaShape("ShelfBlock", javaStructural08ShelfShape)

	registerJavaSurvival("WallTorchBlock", javaStructural08WallTorchSurvives)
}

func javaStructural08WallTorchShape(c *javaShapeContext, state uint32, face cube.Face, _ uint32) uint32 {
	facing := javaStructural08Facing(state)
	if face == facing.Opposite() && !javaStructural08WallTorchSurvives(c, state, c.pos) {
		return javaDefault("air")
	}
	return state
}

func javaStructural08WallTorchSurvives(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	facing := javaStructural08Facing(state)
	supportPos := pos.Side(facing.Opposite())
	support := c.at(supportPos)
	return c.sturdy(support, facing, javaSupportFull)
}

func javaStructural08WaterloggedTransparentShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if waterlogged, _ := gamedata.BlockStateBool(state, "waterlogged"); waterlogged {
		c.waterTick(state, c.pos)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaStructural08ConduitShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if waterlogged, _ := gamedata.BlockStateBool(state, "waterlogged"); waterlogged {
		c.waterTick(state, c.pos)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaStructural08ShelfShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if waterlogged, _ := gamedata.BlockStateBool(state, "waterlogged"); waterlogged {
		c.waterTick(state, c.pos)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaStructural08Facing(state uint32) cube.Face {
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
