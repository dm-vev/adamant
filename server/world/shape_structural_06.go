package world

import (
	"strings"

	"github.com/dm-vev/adamant/server/block/cube"
)

func init() {
	registerShape("LightBlock", structural06Waterlogged)
	registerShape("LightningRodBlock", structural06Waterlogged)
	registerShape("SlabBlock", structural06Waterlogged)
	registerShape("StairBlock", structural06Stair)
	registerShape("StandingSignBlock", structural06StandingSign)
	registerSurvival("StandingSignBlock", structural06StandingSignSurvives)
}

func structural06Waterlogged(c *shapeContext, state uint32, _ cube.Face, _ uint32) uint32 {
	c.waterTick(state, c.pos)
	return state
}

func structural06Stair(c *shapeContext, state uint32, face cube.Face, _ uint32) uint32 {
	c.waterTick(state, c.pos)
	if face.Axis() != cube.Y {
		return stateWith(state, "shape", structural06StairsShape(c, state))
	}
	return state
}

func structural06StairsShape(c *shapeContext, state uint32) string {
	facing := structural06Facing(state)
	half := stateString(state, "half")
	behind := c.at(c.pos.Side(facing))
	if structural06IsStair(behind) && stateString(behind, "half") == half {
		otherFacing := structural06Facing(behind)
		if otherFacing.Axis() != facing.Axis() && !structural06StairMatches(c, state, otherFacing.Opposite()) {
			if otherFacing == structural06CounterClockwise(facing) {
				return "outer_left"
			}
			return "outer_right"
		}
	}
	front := c.at(c.pos.Side(facing.Opposite()))
	if structural06IsStair(front) && stateString(front, "half") == half {
		otherFacing := structural06Facing(front)
		if otherFacing.Axis() != facing.Axis() && !structural06StairMatches(c, state, otherFacing) {
			if otherFacing == structural06CounterClockwise(facing) {
				return "inner_left"
			}
			return "inner_right"
		}
	}
	return "straight"
}

func structural06StairMatches(c *shapeContext, state uint32, face cube.Face) bool {
	other := c.at(c.pos.Side(face))
	return structural06IsStair(other) && stateString(other, "facing") == stateString(state, "facing") && stateString(other, "half") == stateString(state, "half")
}

func structural06IsStair(state uint32) bool {
	return strings.HasSuffix(stateName(state), "_stairs")
}

func structural06Facing(state uint32) cube.Face {
	switch stateString(state, "facing") {
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
func structural06CounterClockwise(face cube.Face) cube.Face {
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

func structural06StandingSign(c *shapeContext, state uint32, face cube.Face, _ uint32) uint32 {
	if face == cube.FaceDown && !structural06StandingSignSurvives(c, state, c.pos) {
		return stateDefault("air")
	}
	return state
}
func structural06StandingSignSurvives(c *shapeContext, _ uint32, pos cube.Pos) bool {
	return c.solid(c.at(pos.Side(cube.FaceDown)))
}
