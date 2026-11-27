package world

import "github.com/dm-vev/adamant/server/block/cube"

func init() {
	registerShape("AbstractBedBlock", structural01BedShape)
	registerShape("AmethystClusterBlock", structural01AmethystShape)
	registerShape("BannerBlock", structural01BannerShape)
	registerShape("BarrierBlock", structural01BarrierShape)
	registerShape("BaseTorchBlock", structural01TorchShape)
	registerSurvival("AmethystClusterBlock", structural01AmethystSurvival)
	registerSurvival("BannerBlock", structural01BannerSurvival)
	registerSurvival("BaseTorchBlock", structural01TorchSurvival)
}

func structural01BedShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	part := stateString(state, "part")
	facing := stateString(state, "facing")
	neighborFace := cube.FaceNorth
	if part == "foot" {
		switch facing {
		case "north":
			neighborFace = cube.FaceNorth
		case "south":
			neighborFace = cube.FaceSouth
		case "west":
			neighborFace = cube.FaceWest
		case "east":
			neighborFace = cube.FaceEast
		}
	} else {
		switch facing {
		case "north":
			neighborFace = cube.FaceSouth
		case "south":
			neighborFace = cube.FaceNorth
		case "west":
			neighborFace = cube.FaceEast
		case "east":
			neighborFace = cube.FaceWest
		}
	}
	if face == neighborFace {
		if stateName(neighbor) == stateName(state) && stateString(neighbor, "part") != part {
			return stateWith(state, "occupied", stateBool(neighbor, "occupied"))
		}
		return stateDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func structural01AmethystShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if stateBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	if face == structural01Opposite(structural01Facing(state)) && !c.canSurvive(state, c.pos) {
		return stateDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func structural01BannerShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceDown && !c.canSurvive(state, c.pos) {
		return stateDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func structural01BarrierShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if stateBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func structural01TorchShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceDown && !c.canSurvive(state, c.pos) {
		return stateDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func structural01AmethystSurvival(c *shapeContext, state uint32, pos cube.Pos) bool {
	face := structural01Facing(state)
	adjacent := pos.Side(structural01Opposite(face))
	return c.sturdy(c.at(adjacent), face, supportFull)
}

func structural01BannerSurvival(c *shapeContext, state uint32, pos cube.Pos) bool {
	return c.solid(c.at(pos.Side(cube.FaceDown)))
}

func structural01TorchSurvival(c *shapeContext, state uint32, pos cube.Pos) bool {
	return c.canSupportCenter(pos.Side(cube.FaceDown), cube.FaceUp)
}

func structural01Facing(state uint32) cube.Face {
	switch stateString(state, "facing") {
	case "down":
		return cube.FaceDown
	case "up":
		return cube.FaceUp
	case "north":
		return cube.FaceNorth
	case "south":
		return cube.FaceSouth
	case "west":
		return cube.FaceWest
	case "east":
		return cube.FaceEast
	default:
		panic("invalid Java facing")
	}
}

func structural01Opposite(face cube.Face) cube.Face {
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
