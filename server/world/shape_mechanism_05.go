package world

import (
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/internal/gamedata"
)

func init() {
	registerShape("ScaffoldingBlock", mechanism05ScaffoldingShape)
	registerShape("SculkSensorBlock", mechanism05WaterloggedShape)
	registerShape("SculkShriekerBlock", mechanism05WaterloggedShape)
	registerShape("SnowyBlock", mechanism05SnowyShape)
	registerShape("SoulFireBlock", mechanism05SoulFireShape)

	registerSurvival("ScaffoldingBlock", mechanism05ScaffoldingSurvives)
	registerSurvival("SoulFireBlock", mechanism05SoulFireSurvives)
}

func mechanism05ScaffoldingShape(c *shapeContext, state uint32, _ cube.Face, _ uint32) uint32 {
	if wet, _ := gamedata.BlockStateBool(state, "waterlogged"); wet {
		c.waterTick(state, c.pos)
	}
	c.blockTick(c.pos, state, 1)
	return state
}

func mechanism05ScaffoldingSurvives(c *shapeContext, _ uint32, pos cube.Pos) bool {
	return mechanism05ScaffoldingDistance(c, pos) < 7
}

func mechanism05ScaffoldingDistance(c *shapeContext, pos cube.Pos) int32 {
	belowPos := pos.Side(cube.FaceDown)
	below := c.at(belowPos)
	distance := int32(7)
	if stateName(below) == "scaffolding" {
		distance, _ = gamedata.BlockStateInt(below, "distance")
	} else if c.sturdy(below, cube.FaceUp, supportFull) {
		return 0
	}
	for _, face := range [...]cube.Face{cube.FaceNorth, cube.FaceSouth, cube.FaceWest, cube.FaceEast} {
		relative := c.at(pos.Side(face))
		if stateName(relative) == "scaffolding" {
			neighborDistance, _ := gamedata.BlockStateInt(relative, "distance")
			if neighborDistance+1 < distance {
				distance = neighborDistance + 1
			}
			if distance == 1 {
				break
			}
		}
	}
	return distance
}

func mechanism05WaterloggedShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if wet, _ := gamedata.BlockStateBool(state, "waterlogged"); wet {
		c.waterTick(state, c.pos)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func mechanism05SnowyShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceUp {
		if c.tag(neighbor, "minecraft:snow") {
			return stateWith(state, "snowy", true)
		}
		return stateWith(state, "snowy", false)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func mechanism05SoulFireShape(c *shapeContext, _ uint32, _ cube.Face, _ uint32) uint32 {
	if mechanism05SoulFireSurvives(c, 0, c.pos) {
		return stateDefault("soul_fire")
	}
	return stateDefault("air")
}

func mechanism05SoulFireSurvives(c *shapeContext, _ uint32, pos cube.Pos) bool {
	return c.tag(c.at(pos.Side(cube.FaceDown)), "minecraft:soul_fire_base_blocks")
}
