package world

import (
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
)

func init() {
	registerJavaShape("LightBlock", javaStructural06Waterlogged)
	registerJavaShape("LightningRodBlock", javaStructural06Waterlogged)
	registerJavaShape("SlabBlock", javaStructural06Waterlogged)
	registerJavaShape("StairBlock", javaStructural06Stair)
	registerJavaShape("StandingSignBlock", javaStructural06StandingSign)
	registerJavaSurvival("StandingSignBlock", javaStructural06StandingSignSurvives)
}

func javaStructural06Waterlogged(c *javaShapeContext, state uint32, _ cube.Face, _ uint32) uint32 {
	c.waterTick(state, c.pos)
	return state
}

func javaStructural06Stair(c *javaShapeContext, state uint32, face cube.Face, _ uint32) uint32 {
	c.waterTick(state, c.pos)
	if face.Axis() != cube.Y {
		return javaWith(state, "shape", javaStructural06StairsShape(c, state))
	}
	return state
}

func javaStructural06StairsShape(c *javaShapeContext, state uint32) string {
	facing := javaStructural06Facing(state)
	half := javaString(state, "half")
	behind := c.at(c.pos.Side(facing))
	if javaStructural06IsStair(behind) && javaString(behind, "half") == half {
		otherFacing := javaStructural06Facing(behind)
		if otherFacing.Axis() != facing.Axis() && !javaStructural06StairMatches(c, state, otherFacing.Opposite()) {
			if otherFacing == javaStructural06CounterClockwise(facing) {
				return "outer_left"
			}
			return "outer_right"
		}
	}
	front := c.at(c.pos.Side(facing.Opposite()))
	if javaStructural06IsStair(front) && javaString(front, "half") == half {
		otherFacing := javaStructural06Facing(front)
		if otherFacing.Axis() != facing.Axis() && !javaStructural06StairMatches(c, state, otherFacing) {
			if otherFacing == javaStructural06CounterClockwise(facing) {
				return "inner_left"
			}
			return "inner_right"
		}
	}
	return "straight"
}

func javaStructural06StairMatches(c *javaShapeContext, state uint32, face cube.Face) bool {
	other := c.at(c.pos.Side(face))
	return javaStructural06IsStair(other) && javaString(other, "facing") == javaString(state, "facing") && javaString(other, "half") == javaString(state, "half")
}

func javaStructural06IsStair(state uint32) bool {
	return strings.HasSuffix(javaName(state), "_stairs")
}

func javaStructural06Facing(state uint32) cube.Face {
	switch javaString(state, "facing") {
	case "north":
		return cube.FaceNorth
	case "south":
		return cube.FaceSouth
	case "west":
		return cube.FaceWest
	default:
		return cube.FaceEast
	}
}
func javaStructural06CounterClockwise(face cube.Face) cube.Face {
	switch face {
	case cube.FaceNorth:
		return cube.FaceWest
	case cube.FaceWest:
		return cube.FaceSouth
	case cube.FaceSouth:
		return cube.FaceEast
	case cube.FaceEast:
		return cube.FaceNorth
	}
	return face
}

func javaStructural06StandingSign(c *javaShapeContext, state uint32, face cube.Face, _ uint32) uint32 {
	if face == cube.FaceDown && !javaStructural06StandingSignSurvives(c, state, c.pos) {
		return javaDefault("air")
	}
	return state
}
func javaStructural06StandingSignSurvives(c *javaShapeContext, _ uint32, pos cube.Pos) bool {
	return c.solid(c.at(pos.Side(cube.FaceDown)))
}
