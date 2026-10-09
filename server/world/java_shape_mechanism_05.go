package world

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/javadata"
)

func init() {
	registerJavaShape("ScaffoldingBlock", javaMechanism05ScaffoldingShape)
	registerJavaShape("SculkSensorBlock", javaMechanism05WaterloggedShape)
	registerJavaShape("SculkShriekerBlock", javaMechanism05WaterloggedShape)
	registerJavaShape("SnowyBlock", javaMechanism05SnowyShape)
	registerJavaShape("SoulFireBlock", javaMechanism05SoulFireShape)

	registerJavaSurvival("ScaffoldingBlock", javaMechanism05ScaffoldingSurvives)
	registerJavaSurvival("SoulFireBlock", javaMechanism05SoulFireSurvives)
}

func javaMechanism05ScaffoldingShape(c *javaShapeContext, state uint32, _ cube.Face, _ uint32) uint32 {
	if wet, _ := javadata.BlockStateBool(state, "waterlogged"); wet {
		c.waterTick(state, c.pos)
	}
	c.blockTick(c.pos, state, 1)
	return state
}

func javaMechanism05ScaffoldingSurvives(c *javaShapeContext, _ uint32, pos cube.Pos) bool {
	return javaMechanism05ScaffoldingDistance(c, pos) < 7
}

func javaMechanism05ScaffoldingDistance(c *javaShapeContext, pos cube.Pos) int32 {
	belowPos := pos.Side(cube.FaceDown)
	below := c.at(belowPos)
	distance := int32(7)
	if javaName(below) == "scaffolding" {
		distance, _ = javadata.BlockStateInt(below, "distance")
	} else if c.sturdy(below, cube.FaceUp, javaSupportFull) {
		return 0
	}
	for _, face := range [...]cube.Face{cube.FaceNorth, cube.FaceSouth, cube.FaceWest, cube.FaceEast} {
		relative := c.at(pos.Side(face))
		if javaName(relative) == "scaffolding" {
			neighborDistance, _ := javadata.BlockStateInt(relative, "distance")
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

func javaMechanism05WaterloggedShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if wet, _ := javadata.BlockStateBool(state, "waterlogged"); wet {
		c.waterTick(state, c.pos)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaMechanism05SnowyShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceUp {
		if c.tag(neighbor, "minecraft:snow") {
			return javaWith(state, "snowy", true)
		}
		return javaWith(state, "snowy", false)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaMechanism05SoulFireShape(c *javaShapeContext, _ uint32, _ cube.Face, _ uint32) uint32 {
	if javaMechanism05SoulFireSurvives(c, 0, c.pos) {
		return javaDefault("soul_fire")
	}
	return javaDefault("air")
}

func javaMechanism05SoulFireSurvives(c *javaShapeContext, _ uint32, pos cube.Pos) bool {
	return c.tag(c.at(pos.Side(cube.FaceDown)), "minecraft:soul_fire_base_blocks")
}
