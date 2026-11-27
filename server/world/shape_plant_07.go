package world

import (
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/internal/gamedata"
)

func init() {
	registerShape("SeagrassBlock", plant07SeagrassShape)
	registerShape("ShelfMushroomBlock", plant07ShelfMushroomShape)
	registerShape("SmallDripleafBlock", plant07SmallDripleafShape)
	registerShape("SnowLayerBlock", plant07SnowLayerShape)
	registerShape("SporeBlossomBlock", plant07SporeBlossomShape)

	registerSurvival("ShelfMushroomBlock", plant07ShelfMushroomSurvival)
	registerSurvival("SmallDripleafBlock", plant07SmallDripleafSurvival)
	registerSurvival("SnowLayerBlock", plant07SnowLayerSurvival)
	registerSurvival("SporeBlossomBlock", plant07SporeBlossomSurvival)
}

func plant07SeagrassShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	result := plant07VegetationShape(c, state, face, neighbor)
	if stateName(result) != "air" {
		water := fluidState{kind: fluidWater, amount: 8, source: true}
		c.fluidTick(c.pos, water, c.fluidDelay(water))
	}
	return result
}

func plant07VegetationShape(c *shapeContext, state uint32, _ cube.Face, _ uint32) uint32 {
	if !c.ownerSurvival("VegetationBlock", state, c.pos) {
		return stateDefault("air")
	}
	return state
}

func plant07ShelfMushroomShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == plant07Opposite(plant07Facing(state)) && !plant07ShelfMushroomSurvival(c, state, c.pos) {
		return stateDefault("air")
	}
	return state
}

func plant07ShelfMushroomSurvival(c *shapeContext, state uint32, pos cube.Pos) bool {
	facing := plant07Facing(state)
	supportPos := pos.Side(plant07Opposite(facing))
	return c.sturdy(c.at(supportPos), facing, supportFull)
}

func plant07SmallDripleafShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if stateBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	return plant07DoublePlantShape(c, state, face, neighbor)
}

func plant07DoublePlantShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	half := stateString(state, "half")
	if face.Axis() != cube.Y || (half == "lower") != (face == cube.FaceUp) || (stateName(neighbor) == stateName(state) && stateString(neighbor, "half") != half) {
		if half == "lower" && face == cube.FaceDown && !c.canSurvive(state, c.pos) {
			return stateDefault("air")
		}
		return plant07VegetationShape(c, state, face, neighbor)
	}
	return stateDefault("air")
}

func plant07SmallDripleafSurvival(c *shapeContext, state uint32, pos cube.Pos) bool {
	if stateString(state, "half") == "upper" {
		below := c.at(pos.Side(cube.FaceDown))
		return stateName(below) == stateName(state) && stateString(below, "half") == "lower"
	}
	belowPos := pos.Side(cube.FaceDown)
	below := c.at(belowPos)
	aboveFluid := c.fluid(c.at(belowPos.Side(cube.FaceUp)))
	return c.tag(below, "supports_small_dripleaf") || aboveFluid.source && aboveFluid.kind == fluidWater && c.tag(below, "supports_vegetation")
}

func plant07SnowLayerShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if !plant07SnowLayerSurvival(c, state, c.pos) {
		return stateDefault("air")
	}
	return state
}

func plant07SnowLayerSurvival(c *shapeContext, _ uint32, pos cube.Pos) bool {
	belowPos := pos.Side(cube.FaceDown)
	below := c.at(belowPos)
	if c.tag(below, "cannot_support_snow_layer") {
		return false
	}
	if c.tag(below, "support_override_snow_layer") {
		return true
	}
	covered, err := gamedata.GenerationCollisionFaceCoversRectangleID(below, int(cube.FaceUp), 0, 0, 1, 1)
	if err != nil {
		panic(err)
	}
	return covered || stateName(below) == "snow" && stateInt(below, "layers") == 8
}

func plant07SporeBlossomShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceUp && !plant07SporeBlossomSurvival(c, state, c.pos) {
		return stateDefault("air")
	}
	return state
}

func plant07SporeBlossomSurvival(c *shapeContext, _ uint32, pos cube.Pos) bool {
	return c.canSupportCenter(pos.Side(cube.FaceUp), cube.FaceDown) && c.fluid(c.at(pos)).kind != fluidWater
}

func plant07Facing(state uint32) cube.Face {
	switch stateString(state, "facing") {
	case "north":
		return cube.FaceNorth
	case "south":
		return cube.FaceSouth
	case "west":
		return cube.FaceWest
	case "east":
		return cube.FaceEast
	default:
		panic("invalid horizontal facing")
	}
}

func plant07Opposite(face cube.Face) cube.Face {
	switch face {
	case cube.FaceDown:
		return cube.FaceUp
	case cube.FaceUp:
		return cube.FaceDown
	case cube.FaceNorth:
		return cube.FaceSouth
	case cube.FaceSouth:
		return cube.FaceNorth
	case cube.FaceWest:
		return cube.FaceEast
	case cube.FaceEast:
		return cube.FaceWest
	default:
		panic("invalid cube face")
	}
}
