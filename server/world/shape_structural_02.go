package world

import "github.com/dm-vev/adamant/server/block/cube"

func init() {
	registerShape("BellBlock", structural02BellShape)
	registerShape("CampfireBlock", structural02CampfireShape)
	registerShape("CandleBlock", structural02CandleShape)
	registerShape("CandleCakeBlock", structural02CandleCakeShape)
	registerShape("CarpetBlock", structural02CarpetShape)

	registerSurvival("BellBlock", structural02BellSurvival)
	registerSurvival("CandleBlock", structural02CandleSurvival)
	registerSurvival("CandleCakeBlock", structural02CandleCakeSurvival)
	registerSurvival("CarpetBlock", structural02CarpetSurvival)
}

func structural02BellShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	attachment := stateString(state, "attachment")
	connectedDirection := structural02BellConnectedDirection(state).Opposite()
	if connectedDirection == face && !c.canSurvive(state, c.pos) && attachment != "double_wall" {
		return stateDefault("air")
	}

	facing := structural02Facing(stateString(state, "facing"))
	if structural02SameAxis(face, facing) {
		if attachment == "double_wall" && !c.sturdy(neighbor, face, supportFull) {
			state = stateWith(state, "attachment", "single_wall")
			return stateWith(state, "facing", structural02FaceName(face.Opposite()))
		}
		if attachment == "single_wall" && connectedDirection.Opposite() == face && c.sturdy(neighbor, facing, supportFull) {
			return stateWith(state, "attachment", "double_wall")
		}
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func structural02CampfireShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	c.waterTick(state, c.pos)
	if face == cube.FaceDown {
		return stateWith(state, "signal_fire", stateName(neighbor) == "hay_block")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func structural02CandleShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	c.waterTick(state, c.pos)
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func structural02CandleCakeShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceDown && !c.canSurvive(state, c.pos) {
		return stateDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func structural02CarpetShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if !c.canSurvive(state, c.pos) {
		return stateDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func structural02BellSurvival(c *shapeContext, state uint32, pos cube.Pos) bool {
	connectedDirection := structural02BellConnectedDirection(state).Opposite()
	if connectedDirection == cube.FaceUp {
		return c.canSupportCenter(pos.Side(cube.FaceUp), cube.FaceDown)
	}
	supportPos := pos.Side(connectedDirection)
	return c.sturdy(c.at(supportPos), connectedDirection.Opposite(), supportFull)
}

func structural02CandleSurvival(c *shapeContext, _ uint32, pos cube.Pos) bool {
	return c.canSupportCenter(pos.Side(cube.FaceDown), cube.FaceUp)
}

func structural02CandleCakeSurvival(c *shapeContext, _ uint32, pos cube.Pos) bool {
	return c.solid(c.at(pos.Side(cube.FaceDown)))
}

func structural02CarpetSurvival(c *shapeContext, _ uint32, pos cube.Pos) bool {
	name := stateName(c.at(pos.Side(cube.FaceDown)))
	return name != "air" && name != "cave_air" && name != "void_air"
}

func structural02BellConnectedDirection(state uint32) cube.Face {
	switch stateString(state, "attachment") {
	case "floor":
		return cube.FaceUp
	case "ceiling":
		return cube.FaceDown
	default:
		return structural02Facing(stateString(state, "facing")).Opposite()
	}
}

func structural02SameAxis(a, b cube.Face) bool {
	aX := a == cube.FaceEast || a == cube.FaceWest
	bX := b == cube.FaceEast || b == cube.FaceWest
	aZ := a == cube.FaceNorth || a == cube.FaceSouth
	bZ := b == cube.FaceNorth || b == cube.FaceSouth
	return aX && bX || aZ && bZ
}

func structural02Facing(name string) cube.Face {
	switch name {
	case "north":
		return cube.FaceNorth
	case "south":
		return cube.FaceSouth
	case "west":
		return cube.FaceWest
	case "east":
		return cube.FaceEast
	default:
		panic("invalid Java bell facing " + name)
	}
}

func structural02FaceName(face cube.Face) string {
	switch face {
	case cube.FaceNorth:
		return "north"
	case cube.FaceSouth:
		return "south"
	case cube.FaceWest:
		return "west"
	case cube.FaceEast:
		return "east"
	default:
		panic("invalid Java horizontal face")
	}
}
