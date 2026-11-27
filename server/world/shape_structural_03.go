package world

import (
	"github.com/dm-vev/adamant/server/block/cube"
)

func init() {
	registerShape("CeilingHangingSignBlock", structural03CeilingHangingSignShape)
	registerSurvival("CeilingHangingSignBlock", structural03CeilingHangingSignSurvives)
	registerShape("ChainBlock", structural03ChainShape)
	registerShape("ChestBlock", structural03ChestShape)
	registerShape("CopperChestBlock", structural03CopperChestShape)
	registerShape("CopperGolemStatueBlock", structural03CopperGolemStatueShape)
}

func structural03CeilingHangingSignSurvives(c *shapeContext, state uint32, pos cube.Pos) bool {
	return c.sturdy(c.at(pos.Side(cube.FaceUp)), cube.FaceDown, supportCenter)
}

func structural03CeilingHangingSignShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceUp && !c.canSurvive(state, c.pos) {
		return stateDefault("air")
	}
	c.waterTick(state, c.pos)
	return state
}

func structural03ChainShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if stateBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	return state
}

func structural03ChestShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if stateBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	if structural03ChestCanConnect(c, state, neighbor) && face.Axis() != cube.Y {
		neighborType := stateString(neighbor, "type")
		if stateString(state, "type") == "single" && neighborType != "single" &&
			stateString(state, "facing") == stateString(neighbor, "facing") &&
			structural03ConnectedDirection(neighbor) == face.Opposite() {
			return stateWith(state, "type", structural03OppositeChestType(neighborType))
		}
	} else if structural03ConnectedDirection(state) == face {
		return stateWith(state, "type", "single")
	}
	return state
}

func structural03ChestCanConnect(c *shapeContext, state, neighbor uint32) bool {
	if c.tag(state, "minecraft:copper_chests") {
		return c.tag(neighbor, "minecraft:copper_chests")
	}
	return stateName(state) == stateName(neighbor)
}

func structural03CopperChestShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	updated := structural03ChestShape(c, state, face, neighbor)
	if structural03ChestCanConnect(c, state, neighbor) {
		if stateString(updated, "type") != "single" && structural03ConnectedDirection(updated) == face {
			return structural03WithPropertiesOf(neighbor, updated)
		}
	}
	return updated
}

func structural03CopperGolemStatueShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if stateBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	return state
}

func structural03ConnectedDirection(state uint32) cube.Face {
	facing := stateString(state, "facing")
	left := stateString(state, "type") == "left"
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

func structural03OppositeChestType(value string) string {
	if value == "left" {
		return "right"
	}
	return "left"
}

func structural03WithPropertiesOf(target, source uint32) uint32 {
	for _, key := range [...]string{"facing", "type", "waterlogged"} {
		switch key {
		case "facing":
			target = stateWith(target, key, stateString(source, key))
		case "type":
			target = stateWith(target, key, stateString(source, key))
		case "waterlogged":
			target = stateWith(target, key, stateBool(source, key))
		}
	}
	return target
}
