package world

import (
	"github.com/df-mc/dragonfly/server/block/cube"
)

func init() {
	registerJavaShape("CeilingHangingSignBlock", javaStructural03CeilingHangingSignShape)
	registerJavaSurvival("CeilingHangingSignBlock", javaStructural03CeilingHangingSignSurvives)
	registerJavaShape("ChainBlock", javaStructural03ChainShape)
	registerJavaShape("ChestBlock", javaStructural03ChestShape)
	registerJavaShape("CopperChestBlock", javaStructural03CopperChestShape)
	registerJavaShape("CopperGolemStatueBlock", javaStructural03CopperGolemStatueShape)
}

func javaStructural03CeilingHangingSignSurvives(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	return c.sturdy(c.at(pos.Side(cube.FaceUp)), cube.FaceDown, javaSupportCenter)
}

func javaStructural03CeilingHangingSignShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceUp && !c.canSurvive(state, c.pos) {
		return javaDefault("air")
	}
	c.waterTick(state, c.pos)
	return state
}

func javaStructural03ChainShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if javaBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	return state
}

func javaStructural03ChestShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if javaBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	if javaStructural03ChestCanConnect(c, state, neighbor) && face.Axis() != cube.Y {
		neighborType := javaString(neighbor, "type")
		if javaString(state, "type") == "single" && neighborType != "single" &&
			javaString(state, "facing") == javaString(neighbor, "facing") &&
			javaStructural03ConnectedDirection(neighbor) == face.Opposite() {
			return javaWith(state, "type", javaStructural03OppositeChestType(neighborType))
		}
	} else if javaStructural03ConnectedDirection(state) == face {
		return javaWith(state, "type", "single")
	}
	return state
}

func javaStructural03ChestCanConnect(c *javaShapeContext, state, neighbor uint32) bool {
	if c.tag(state, "minecraft:copper_chests") {
		return c.tag(neighbor, "minecraft:copper_chests")
	}
	return javaName(state) == javaName(neighbor)
}

func javaStructural03CopperChestShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	updated := javaStructural03ChestShape(c, state, face, neighbor)
	if javaStructural03ChestCanConnect(c, state, neighbor) {
		if javaString(updated, "type") != "single" && javaStructural03ConnectedDirection(updated) == face {
			return javaStructural03WithPropertiesOf(neighbor, updated)
		}
	}
	return updated
}

func javaStructural03CopperGolemStatueShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if javaBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	return state
}

func javaStructural03ConnectedDirection(state uint32) cube.Face {
	facing := javaString(state, "facing")
	left := javaString(state, "type") == "left"
	switch facing {
	case "north":
		if left {
			return cube.FaceEast
		}
		return cube.FaceWest
	case "south":
		if left {
			return cube.FaceWest
		}
		return cube.FaceEast
	case "east":
		if left {
			return cube.FaceSouth
		}
		return cube.FaceNorth
	default:
		if left {
			return cube.FaceNorth
		}
		return cube.FaceSouth
	}
}

func javaStructural03OppositeChestType(value string) string {
	if value == "left" {
		return "right"
	}
	return "left"
}

func javaStructural03WithPropertiesOf(target, source uint32) uint32 {
	for _, key := range [...]string{"facing", "type", "waterlogged"} {
		switch key {
		case "facing":
			target = javaWith(target, key, javaString(source, key))
		case "type":
			target = javaWith(target, key, javaString(source, key))
		case "waterlogged":
			target = javaWith(target, key, javaBool(source, key))
		}
	}
	return target
}
