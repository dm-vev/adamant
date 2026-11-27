package world

import (
	"fmt"

	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/internal/gamedata"
)

func init() {
	registerShape("TrapDoorBlock", structural07TrapDoor)
	registerShape("WallBannerBlock", structural07WallBanner)
	registerShape("WallBlock", structural07Wall)
	registerShape("WallHangingSignBlock", structural07WallHangingSign)
	registerShape("WallSignBlock", structural07WallSign)
	registerSurvival("WallBannerBlock", structural07WallBannerSurvives)
	registerSurvival("WallSignBlock", structural07WallSignSurvives)
}

func structural07TrapDoor(c *shapeContext, state uint32, _ cube.Face, _ uint32) uint32 {
	if stateBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	return c.ownerShape("BlockBehaviour", state, cube.FaceDown, 0)
}

func structural07WallBannerSurvives(c *shapeContext, state uint32, pos cube.Pos) bool {
	facing := structural07HorizontalFace(stateString(state, "facing"))
	return c.solid(c.at(pos.Side(facing.Opposite())))
}

func structural07WallBanner(c *shapeContext, state uint32, face cube.Face, _ uint32) uint32 {
	facing := structural07HorizontalFace(stateString(state, "facing"))
	if face == facing.Opposite() && !c.ownerSurvival("WallBannerBlock", state, c.pos) {
		return stateDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, 0)
}

func structural07WallSignSurvives(c *shapeContext, state uint32, pos cube.Pos) bool {
	facing := structural07HorizontalFace(stateString(state, "facing"))
	return c.solid(c.at(pos.Side(facing.Opposite())))
}

func structural07WallSign(c *shapeContext, state uint32, face cube.Face, _ uint32) uint32 {
	facing := structural07HorizontalFace(stateString(state, "facing"))
	if face == facing.Opposite() && !c.ownerSurvival("WallSignBlock", state, c.pos) {
		return stateDefault("air")
	}
	if stateBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	return c.ownerShape("BlockBehaviour", state, face, 0)
}

// WallHangingSignBlock inherits BlockBehaviour.canSurvive unchanged. Its canPlace
// support test is not part of survival or updateShape.
func structural07WallHangingSign(c *shapeContext, state uint32, _ cube.Face, _ uint32) uint32 {
	if stateBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	return c.ownerShape("BlockBehaviour", state, cube.FaceDown, 0)
}

func structural07Wall(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if stateBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	if face == cube.FaceDown {
		return c.ownerShape("BlockBehaviour", state, face, neighbor)
	}
	if face == cube.FaceUp {
		return structural07WallUpdate(c, state, neighbor,
			structural07WallConnected(state, "north"), structural07WallConnected(state, "east"),
			structural07WallConnected(state, "south"), structural07WallConnected(state, "west"))
	}
	opposite := face.Opposite()
	connected := structural07WallConnects(c, neighbor, opposite)
	n, e, s, w := structural07WallConnected(state, "north"), structural07WallConnected(state, "east"),
		structural07WallConnected(state, "south"), structural07WallConnected(state, "west")
	switch face {
	case cube.FaceNorth:
		n = connected
	case cube.FaceEast:
		e = connected
	case cube.FaceSouth:
		s = connected
	case cube.FaceWest:
		w = connected
	}
	return structural07WallUpdate(c, state, c.at(c.pos.Side(cube.FaceUp)), n, e, s, w)
}

func structural07WallConnected(state uint32, property string) bool {
	return stateString(state, property) != "none"
}

func structural07WallConnects(c *shapeContext, state uint32, face cube.Face) bool {
	if c.tag(state, "minecraft:walls") {
		return true
	}
	if !structural07WallConnectionException(c, state) && c.sturdy(state, face, supportFull) {
		return true
	}
	name := stateName(state)
	if name == "iron_bars" || name == "copper_bars" || name == "exposed_copper_bars" || name == "weathered_copper_bars" ||
		name == "oxidized_copper_bars" || name == "waxed_copper_bars" || name == "waxed_exposed_copper_bars" ||
		name == "waxed_weathered_copper_bars" || name == "waxed_oxidized_copper_bars" {
		return true
	}
	return structural07FenceGateConnects(state, face)
}

func structural07FenceGateConnects(state uint32, face cube.Face) bool {
	switch stateName(state) {
	case "oak_fence_gate", "spruce_fence_gate", "birch_fence_gate", "jungle_fence_gate", "acacia_fence_gate", "cherry_fence_gate",
		"dark_oak_fence_gate", "pale_oak_fence_gate", "mangrove_fence_gate", "poplar_fence_gate", "bamboo_fence_gate", "crimson_fence_gate", "warped_fence_gate":
		return structural07HorizontalFace(stateString(state, "facing")).Axis() != face.Axis()
	default:
		return false
	}
}

func structural07WallConnectionException(c *shapeContext, state uint32) bool {
	name := stateName(state)
	return c.tag(state, "minecraft:leaves") || name == "barrier" || name == "carved_pumpkin" || name == "jack_o_lantern" ||
		name == "melon" || name == "pumpkin" || c.tag(state, "minecraft:shulker_boxes")
}

func structural07WallUpdate(c *shapeContext, state, top uint32, north, east, south, west bool) uint32 {
	state = stateWith(state, "north", structural07WallSideValue(north, top, cube.FaceNorth))
	state = stateWith(state, "east", structural07WallSideValue(east, top, cube.FaceEast))
	state = stateWith(state, "south", structural07WallSideValue(south, top, cube.FaceSouth))
	state = stateWith(state, "west", structural07WallSideValue(west, top, cube.FaceWest))
	if c.tag(top, "minecraft:walls") && stateBool(top, "up") {
		return stateWith(state, "up", true)
	}
	n, e, s, w := stateString(state, "north"), stateString(state, "east"), stateString(state, "south"), stateString(state, "west")
	hasCorner := n == "none" && s == "none" && w == "none" && e == "none" || (n == "none") != (s == "none") || (w == "none") != (e == "none")
	raise := true
	if !hasCorner {
		if n == "tall" && s == "tall" || e == "tall" && w == "tall" {
			raise = false
		} else {
			raise = c.tag(top, "minecraft:wall_post_override") || structural07WallAboveCovers(top)
		}
	}
	return stateWith(state, "up", raise)
}

func structural07WallSideValue(connect bool, top uint32, face cube.Face) string {
	if !connect {
		return "none"
	}
	minU, minV, maxU, maxV := 0.0, 0.0, 1.0, 1.0
	switch face {
	case cube.FaceNorth:
		minU, maxU, minV, maxV = 7.0/16, 9.0/16, 0, 9.0/16
	case cube.FaceSouth:
		minU, maxU, minV, maxV = 7.0/16, 9.0/16, 7.0/16, 1
	case cube.FaceWest:
		minU, maxU, minV, maxV = 0, 9.0/16, 7.0/16, 9.0/16
	case cube.FaceEast:
		minU, maxU, minV, maxV = 7.0/16, 1, 7.0/16, 9.0/16
	}
	covered, err := gamedata.GenerationCollisionFaceCoversRectangleID(top, int(cube.FaceDown), minU, minV, maxU, maxV)
	if err != nil {
		panic(err)
	}
	if covered {
		return "tall"
	}
	return "low"
}

func structural07WallAboveCovers(top uint32) bool {
	covered, err := gamedata.GenerationCollisionFaceCoversRectangleID(top, int(cube.FaceDown), 7.0/16, 7.0/16, 9.0/16, 9.0/16)
	if err != nil {
		panic(err)
	}
	return covered
}

func structural07HorizontalFace(direction string) cube.Face {
	switch direction {
	case "north":
		return cube.FaceNorth
	case "south":
		return cube.FaceSouth
	case "west":
		return cube.FaceWest
	case "east":
		return cube.FaceEast
	default:
		panic(fmt.Sprintf("invalid horizontal facing %q", direction))
	}
}
