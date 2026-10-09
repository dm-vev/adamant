package world

import "github.com/df-mc/dragonfly/server/block/cube"

func init() {
	registerJavaShape("AbstractBedBlock", javaStructural01BedShape)
	registerJavaShape("AmethystClusterBlock", javaStructural01AmethystShape)
	registerJavaShape("BannerBlock", javaStructural01BannerShape)
	registerJavaShape("BarrierBlock", javaStructural01BarrierShape)
	registerJavaShape("BaseTorchBlock", javaStructural01TorchShape)
	registerJavaSurvival("AmethystClusterBlock", javaStructural01AmethystSurvival)
	registerJavaSurvival("BannerBlock", javaStructural01BannerSurvival)
	registerJavaSurvival("BaseTorchBlock", javaStructural01TorchSurvival)
}

func javaStructural01BedShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	part := javaString(state, "part")
	facing := javaString(state, "facing")
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
		if javaName(neighbor) == javaName(state) && javaString(neighbor, "part") != part {
			return javaWith(state, "occupied", javaBool(neighbor, "occupied"))
		}
		return javaDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaStructural01AmethystShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if javaBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	if face == javaStructural01Opposite(javaStructural01Facing(state)) && !c.canSurvive(state, c.pos) {
		return javaDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaStructural01BannerShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceDown && !c.canSurvive(state, c.pos) {
		return javaDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaStructural01BarrierShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if javaBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaStructural01TorchShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceDown && !c.canSurvive(state, c.pos) {
		return javaDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaStructural01AmethystSurvival(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	face := javaStructural01Facing(state)
	adjacent := pos.Side(javaStructural01Opposite(face))
	return c.sturdy(c.at(adjacent), face, javaSupportFull)
}

func javaStructural01BannerSurvival(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	return c.solid(c.at(pos.Side(cube.FaceDown)))
}

func javaStructural01TorchSurvival(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	return c.canSupportCenter(pos.Side(cube.FaceDown), cube.FaceUp)
}

func javaStructural01Facing(state uint32) cube.Face {
	switch javaString(state, "facing") {
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

func javaStructural01Opposite(face cube.Face) cube.Face {
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
