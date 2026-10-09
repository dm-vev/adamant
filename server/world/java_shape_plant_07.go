package world

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/javadata"
)

func init() {
	registerJavaShape("SeagrassBlock", javaPlant07SeagrassShape)
	registerJavaShape("ShelfMushroomBlock", javaPlant07ShelfMushroomShape)
	registerJavaShape("SmallDripleafBlock", javaPlant07SmallDripleafShape)
	registerJavaShape("SnowLayerBlock", javaPlant07SnowLayerShape)
	registerJavaShape("SporeBlossomBlock", javaPlant07SporeBlossomShape)

	registerJavaSurvival("ShelfMushroomBlock", javaPlant07ShelfMushroomSurvival)
	registerJavaSurvival("SmallDripleafBlock", javaPlant07SmallDripleafSurvival)
	registerJavaSurvival("SnowLayerBlock", javaPlant07SnowLayerSurvival)
	registerJavaSurvival("SporeBlossomBlock", javaPlant07SporeBlossomSurvival)
}

func javaPlant07SeagrassShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	result := javaPlant07VegetationShape(c, state, face, neighbor)
	if javaName(result) != "air" {
		water := javaFluidState{kind: javaFluidWater, amount: 8, source: true}
		c.fluidTick(c.pos, water, javaFluidTickDelay(c.tx, water))
	}
	return result
}

func javaPlant07VegetationShape(c *javaShapeContext, state uint32, _ cube.Face, _ uint32) uint32 {
	if !c.ownerSurvival("VegetationBlock", state, c.pos) {
		return javaDefault("air")
	}
	return state
}

func javaPlant07ShelfMushroomShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == javaPlant07Opposite(javaPlant07Facing(state)) && !javaPlant07ShelfMushroomSurvival(c, state, c.pos) {
		return javaDefault("air")
	}
	return state
}

func javaPlant07ShelfMushroomSurvival(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	facing := javaPlant07Facing(state)
	supportPos := pos.Side(javaPlant07Opposite(facing))
	return c.sturdy(c.at(supportPos), facing, javaSupportFull)
}

func javaPlant07SmallDripleafShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if javaBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	return javaPlant07DoublePlantShape(c, state, face, neighbor)
}

func javaPlant07DoublePlantShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	half := javaString(state, "half")
	if face.Axis() != cube.Y || (half == "lower") != (face == cube.FaceUp) || (javaName(neighbor) == javaName(state) && javaString(neighbor, "half") != half) {
		if half == "lower" && face == cube.FaceDown && !c.canSurvive(state, c.pos) {
			return javaDefault("air")
		}
		return javaPlant07VegetationShape(c, state, face, neighbor)
	}
	return javaDefault("air")
}

func javaPlant07SmallDripleafSurvival(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	if javaString(state, "half") == "upper" {
		below := c.at(pos.Side(cube.FaceDown))
		return javaName(below) == javaName(state) && javaString(below, "half") == "lower"
	}
	belowPos := pos.Side(cube.FaceDown)
	below := c.at(belowPos)
	aboveFluid := c.fluid(c.at(belowPos.Side(cube.FaceUp)))
	return c.tag(below, "supports_small_dripleaf") || aboveFluid.source && aboveFluid.kind == javaFluidWater && c.tag(below, "supports_vegetation")
}

func javaPlant07SnowLayerShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if !javaPlant07SnowLayerSurvival(c, state, c.pos) {
		return javaDefault("air")
	}
	return state
}

func javaPlant07SnowLayerSurvival(c *javaShapeContext, _ uint32, pos cube.Pos) bool {
	belowPos := pos.Side(cube.FaceDown)
	below := c.at(belowPos)
	if c.tag(below, "cannot_support_snow_layer") {
		return false
	}
	if c.tag(below, "support_override_snow_layer") {
		return true
	}
	covered, err := javadata.GenerationCollisionFaceCoversRectangleID(below, int(cube.FaceUp), 0, 0, 1, 1)
	if err != nil {
		panic(err)
	}
	return covered || javaName(below) == "snow" && javaInt(below, "layers") == 8
}

func javaPlant07SporeBlossomShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceUp && !javaPlant07SporeBlossomSurvival(c, state, c.pos) {
		return javaDefault("air")
	}
	return state
}

func javaPlant07SporeBlossomSurvival(c *javaShapeContext, _ uint32, pos cube.Pos) bool {
	return c.canSupportCenter(pos.Side(cube.FaceUp), cube.FaceDown) && c.fluid(c.at(pos)).kind != javaFluidWater
}

func javaPlant07Facing(state uint32) cube.Face {
	switch javaString(state, "facing") {
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

func javaPlant07Opposite(face cube.Face) cube.Face {
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
