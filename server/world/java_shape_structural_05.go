package world

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/javadata"
)

func init() {
	registerJavaShape("FenceGateBlock", javaStructural05FenceGate)
	registerJavaShape("HeavyCoreBlock", javaStructural05WaterloggedBase)
	registerJavaShape("IronBarsBlock", javaStructural05IronBars)
	registerJavaShape("LadderBlock", javaStructural05Ladder)
	registerJavaShape("LanternBlock", javaStructural05Lantern)
	registerJavaSurvival("LadderBlock", javaStructural05LadderSurvives)
	registerJavaSurvival("LanternBlock", javaStructural05LanternSurvives)
}

func javaStructural05FenceGate(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	facing := javaString(state, "facing")
	facingAxisX := facing == "east" || facing == "west"
	faceAxisX := face == cube.FaceEast || face == cube.FaceWest
	if face.Axis() == cube.Y || facingAxisX == faceAxisX {
		return state
	}
	wall := c.tag(neighbor, "minecraft:walls")
	other := c.at(c.pos.Side(face.Opposite()))
	wall = wall || c.tag(other, "minecraft:walls")
	return javaWith(state, "in_wall", wall)
}

func javaStructural05WaterloggedBase(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	c.waterTick(state, c.pos)
	return state
}

func javaStructural05IronBars(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	c.waterTick(state, c.pos)
	if face == cube.FaceNorth || face == cube.FaceSouth || face == cube.FaceEast || face == cube.FaceWest {
		prop := javaStructural05DirectionProperty(face)
		connect := (!javaStructural05IronBarsException(c, neighbor) && c.sturdy(neighbor, face.Opposite(), javaSupportFull)) ||
			javaStructural05IronBarsInstance(neighbor) || c.tag(neighbor, "minecraft:walls")
		return javaWith(state, prop, connect)
	}
	return state
}

func javaStructural05DirectionProperty(face cube.Face) string {
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
		panic("invalid horizontal Java iron-bars face")
	}
}

func javaStructural05IronBarsInstance(state uint32) bool {
	switch javaName(state) {
	case "iron_bars", "copper_bars", "exposed_copper_bars", "weathered_copper_bars", "oxidized_copper_bars",
		"waxed_copper_bars", "waxed_exposed_copper_bars", "waxed_weathered_copper_bars", "waxed_oxidized_copper_bars",
		"glass_pane", "white_stained_glass_pane", "orange_stained_glass_pane", "magenta_stained_glass_pane",
		"light_blue_stained_glass_pane", "yellow_stained_glass_pane", "lime_stained_glass_pane", "pink_stained_glass_pane",
		"gray_stained_glass_pane", "light_gray_stained_glass_pane", "cyan_stained_glass_pane", "purple_stained_glass_pane",
		"blue_stained_glass_pane", "brown_stained_glass_pane", "green_stained_glass_pane", "red_stained_glass_pane",
		"black_stained_glass_pane":
		return true
	default:
		return false
	}
}

func javaStructural05IronBarsException(c *javaShapeContext, state uint32) bool {
	exception, err := javadata.BlockExceptionForConnectionID(state)
	if err != nil {
		panic(err)
	}
	return exception
}

func javaStructural05LadderSurvives(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	facing := javaString(state, "facing")
	face, ok := javaStructural05Facing(facing)
	if !ok {
		panic("invalid Java ladder facing " + facing)
	}
	return c.sturdy(c.at(pos.Side(face.Opposite())), face, javaSupportFull)
}

func javaStructural05Ladder(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	facing, ok := javaStructural05Facing(javaString(state, "facing"))
	if !ok {
		panic("invalid Java ladder facing")
	}
	if face.Opposite() == facing && !c.canSurvive(state, c.pos) {
		return javaDefault("air")
	}
	c.waterTick(state, c.pos)
	return state
}

func javaStructural05LanternSurvives(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	face := cube.FaceUp
	if javaBool(state, "hanging") {
		face = cube.FaceDown
	}
	return c.canSupportCenter(pos.Side(face.Opposite()), face)
}

func javaStructural05Lantern(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	direction := cube.FaceDown
	if !javaBool(state, "hanging") {
		direction = cube.FaceUp
	}
	c.waterTick(state, c.pos)
	if direction.Opposite() == face && !c.canSurvive(state, c.pos) {
		return javaDefault("air")
	}
	return state
}

func javaStructural05Facing(value string) (cube.Face, bool) {
	switch value {
	case "down":
		return cube.FaceDown, true
	case "up":
		return cube.FaceUp, true
	case "north":
		return cube.FaceNorth, true
	case "south":
		return cube.FaceSouth, true
	case "west":
		return cube.FaceWest, true
	case "east":
		return cube.FaceEast, true
	default:
		return 0, false
	}
}
