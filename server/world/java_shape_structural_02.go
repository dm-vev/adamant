package world

import "github.com/df-mc/dragonfly/server/block/cube"

func init() {
	registerJavaShape("BellBlock", javaStructural02BellShape)
	registerJavaShape("CampfireBlock", javaStructural02CampfireShape)
	registerJavaShape("CandleBlock", javaStructural02CandleShape)
	registerJavaShape("CandleCakeBlock", javaStructural02CandleCakeShape)
	registerJavaShape("CarpetBlock", javaStructural02CarpetShape)

	registerJavaSurvival("BellBlock", javaStructural02BellSurvival)
	registerJavaSurvival("CandleBlock", javaStructural02CandleSurvival)
	registerJavaSurvival("CandleCakeBlock", javaStructural02CandleCakeSurvival)
	registerJavaSurvival("CarpetBlock", javaStructural02CarpetSurvival)
}

func javaStructural02BellShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	attachment := javaString(state, "attachment")
	connectedDirection := javaStructural02BellConnectedDirection(state).Opposite()
	if connectedDirection == face && !c.canSurvive(state, c.pos) && attachment != "double_wall" {
		return javaDefault("air")
	}

	facing := javaStructural02Facing(javaString(state, "facing"))
	if javaStructural02SameAxis(face, facing) {
		if attachment == "double_wall" && !c.sturdy(neighbor, face, javaSupportFull) {
			state = javaWith(state, "attachment", "single_wall")
			return javaWith(state, "facing", javaStructural02FaceName(face.Opposite()))
		}
		if attachment == "single_wall" && connectedDirection.Opposite() == face && c.sturdy(neighbor, facing, javaSupportFull) {
			return javaWith(state, "attachment", "double_wall")
		}
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaStructural02CampfireShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	c.waterTick(state, c.pos)
	if face == cube.FaceDown {
		return javaWith(state, "signal_fire", javaName(neighbor) == "hay_block")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaStructural02CandleShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	c.waterTick(state, c.pos)
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaStructural02CandleCakeShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceDown && !c.canSurvive(state, c.pos) {
		return javaDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaStructural02CarpetShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if !c.canSurvive(state, c.pos) {
		return javaDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaStructural02BellSurvival(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	connectedDirection := javaStructural02BellConnectedDirection(state).Opposite()
	if connectedDirection == cube.FaceUp {
		return c.canSupportCenter(pos.Side(cube.FaceUp), cube.FaceDown)
	}
	supportPos := pos.Side(connectedDirection)
	return c.sturdy(c.at(supportPos), connectedDirection.Opposite(), javaSupportFull)
}

func javaStructural02CandleSurvival(c *javaShapeContext, _ uint32, pos cube.Pos) bool {
	return c.canSupportCenter(pos.Side(cube.FaceDown), cube.FaceUp)
}

func javaStructural02CandleCakeSurvival(c *javaShapeContext, _ uint32, pos cube.Pos) bool {
	return c.solid(c.at(pos.Side(cube.FaceDown)))
}

func javaStructural02CarpetSurvival(c *javaShapeContext, _ uint32, pos cube.Pos) bool {
	name := javaName(c.at(pos.Side(cube.FaceDown)))
	return name != "air" && name != "cave_air" && name != "void_air"
}

func javaStructural02BellConnectedDirection(state uint32) cube.Face {
	switch javaString(state, "attachment") {
	case "floor":
		return cube.FaceUp
	case "ceiling":
		return cube.FaceDown
	default:
		return javaStructural02Facing(javaString(state, "facing")).Opposite()
	}
}

func javaStructural02SameAxis(a, b cube.Face) bool {
	aX := a == cube.FaceEast || a == cube.FaceWest
	bX := b == cube.FaceEast || b == cube.FaceWest
	aZ := a == cube.FaceNorth || a == cube.FaceSouth
	bZ := b == cube.FaceNorth || b == cube.FaceSouth
	return aX && bX || aZ && bZ
}

func javaStructural02Facing(name string) cube.Face {
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

func javaStructural02FaceName(face cube.Face) string {
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
