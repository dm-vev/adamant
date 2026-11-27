package world

import (
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/internal/gamedata"
)

func init() {
	registerShape("FenceGateBlock", structural05FenceGate)
	registerShape("HeavyCoreBlock", structural05WaterloggedBase)
	registerShape("IronBarsBlock", structural05IronBars)
	registerShape("LadderBlock", structural05Ladder)
	registerShape("LanternBlock", structural05Lantern)
	registerSurvival("LadderBlock", structural05LadderSurvives)
	registerSurvival("LanternBlock", structural05LanternSurvives)
}

func structural05FenceGate(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	facing := stateString(state, "facing")
	facingAxisX := facing == "east" || facing == "west"
	faceAxisX := face == cube.FaceEast || face == cube.FaceWest
	if face.Axis() == cube.Y || facingAxisX == faceAxisX {
		return state
	}
	wall := c.tag(neighbor, "minecraft:walls")
	other := c.at(c.pos.Side(face.Opposite()))
	wall = wall || c.tag(other, "minecraft:walls")
	return stateWith(state, "in_wall", wall)
}

func structural05WaterloggedBase(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	c.waterTick(state, c.pos)
	return state
}

func structural05IronBars(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	c.waterTick(state, c.pos)
	if face == cube.FaceNorth || face == cube.FaceSouth || face == cube.FaceEast || face == cube.FaceWest {
		prop := structural05DirectionProperty(face)
		connect := (!structural05IronBarsException(c, neighbor) && c.sturdy(neighbor, face.Opposite(), supportFull)) ||
			structural05IronBarsInstance(neighbor) || c.tag(neighbor, "minecraft:walls")
		return stateWith(state, prop, connect)
	}
	return state
}

func structural05DirectionProperty(face cube.Face) string {
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

func structural05IronBarsInstance(state uint32) bool {
	switch stateName(state) {
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

func structural05IronBarsException(c *shapeContext, state uint32) bool {
	exception, err := gamedata.BlockExceptionForConnectionID(state)
	if err != nil {
		panic(err)
	}
	return exception
}

func structural05LadderSurvives(c *shapeContext, state uint32, pos cube.Pos) bool {
	facing := stateString(state, "facing")
	face, ok := structural05Facing(facing)
	if !ok {
		panic("invalid Java ladder facing " + facing)
	}
	return c.sturdy(c.at(pos.Side(face.Opposite())), face, supportFull)
}

func structural05Ladder(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	facing, ok := structural05Facing(stateString(state, "facing"))
	if !ok {
		panic("invalid Java ladder facing")
	}
	if face.Opposite() == facing && !c.canSurvive(state, c.pos) {
		return stateDefault("air")
	}
	c.waterTick(state, c.pos)
	return state
}

func structural05LanternSurvives(c *shapeContext, state uint32, pos cube.Pos) bool {
	face := cube.FaceUp
	if stateBool(state, "hanging") {
		face = cube.FaceDown
	}
	return c.canSupportCenter(pos.Side(face.Opposite()), face)
}

func structural05Lantern(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	direction := cube.FaceDown
	if !stateBool(state, "hanging") {
		direction = cube.FaceUp
	}
	c.waterTick(state, c.pos)
	if direction.Opposite() == face && !c.canSurvive(state, c.pos) {
		return stateDefault("air")
	}
	return state
}

func structural05Facing(value string) (cube.Face, bool) {
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
