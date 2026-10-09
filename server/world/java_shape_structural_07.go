package world

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/javadata"
)

func init() {
	registerJavaShape("TrapDoorBlock", javaStructural07TrapDoor)
	registerJavaShape("WallBannerBlock", javaStructural07WallBanner)
	registerJavaShape("WallBlock", javaStructural07Wall)
	registerJavaShape("WallHangingSignBlock", javaStructural07WallHangingSign)
	registerJavaShape("WallSignBlock", javaStructural07WallSign)
	registerJavaSurvival("WallBannerBlock", javaStructural07WallBannerSurvives)
	registerJavaSurvival("WallSignBlock", javaStructural07WallSignSurvives)
}

func javaStructural07TrapDoor(c *javaShapeContext, state uint32, _ cube.Face, _ uint32) uint32 {
	if javaBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	return c.ownerShape("BlockBehaviour", state, cube.FaceDown, 0)
}

func javaStructural07WallBannerSurvives(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	facing := javaStructural07HorizontalFace(javaString(state, "facing"))
	return c.solid(c.at(pos.Side(facing.Opposite())))
}

func javaStructural07WallBanner(c *javaShapeContext, state uint32, face cube.Face, _ uint32) uint32 {
	facing := javaStructural07HorizontalFace(javaString(state, "facing"))
	if face == facing.Opposite() && !c.ownerSurvival("WallBannerBlock", state, c.pos) {
		return javaDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, 0)
}

func javaStructural07WallSignSurvives(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	facing := javaStructural07HorizontalFace(javaString(state, "facing"))
	return c.solid(c.at(pos.Side(facing.Opposite())))
}

func javaStructural07WallSign(c *javaShapeContext, state uint32, face cube.Face, _ uint32) uint32 {
	facing := javaStructural07HorizontalFace(javaString(state, "facing"))
	if face == facing.Opposite() && !c.ownerSurvival("WallSignBlock", state, c.pos) {
		return javaDefault("air")
	}
	if javaBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	return c.ownerShape("BlockBehaviour", state, face, 0)
}

// WallHangingSignBlock inherits BlockBehaviour.canSurvive unchanged. Its canPlace
// support test is not part of survival or updateShape.
func javaStructural07WallHangingSign(c *javaShapeContext, state uint32, _ cube.Face, _ uint32) uint32 {
	if javaBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	return c.ownerShape("BlockBehaviour", state, cube.FaceDown, 0)
}

func javaStructural07Wall(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if javaBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	if face == cube.FaceDown {
		return c.ownerShape("BlockBehaviour", state, face, neighbor)
	}
	if face == cube.FaceUp {
		return javaStructural07WallUpdate(c, state, neighbor,
			javaStructural07WallConnected(state, "north"), javaStructural07WallConnected(state, "east"),
			javaStructural07WallConnected(state, "south"), javaStructural07WallConnected(state, "west"))
	}
	opposite := face.Opposite()
	connected := javaStructural07WallConnects(c, neighbor, opposite)
	n, e, s, w := javaStructural07WallConnected(state, "north"), javaStructural07WallConnected(state, "east"),
		javaStructural07WallConnected(state, "south"), javaStructural07WallConnected(state, "west")
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
	return javaStructural07WallUpdate(c, state, c.at(c.pos.Side(cube.FaceUp)), n, e, s, w)
}

func javaStructural07WallConnected(state uint32, property string) bool {
	return javaString(state, property) != "none"
}

func javaStructural07WallConnects(c *javaShapeContext, state uint32, face cube.Face) bool {
	if c.tag(state, "minecraft:walls") {
		return true
	}
	if !javaStructural07WallConnectionException(c, state) && c.sturdy(state, face, javaSupportFull) {
		return true
	}
	name := javaName(state)
	if name == "iron_bars" || name == "copper_bars" || name == "exposed_copper_bars" || name == "weathered_copper_bars" ||
		name == "oxidized_copper_bars" || name == "waxed_copper_bars" || name == "waxed_exposed_copper_bars" ||
		name == "waxed_weathered_copper_bars" || name == "waxed_oxidized_copper_bars" {
		return true
	}
	return javaStructural07FenceGateConnects(state, face)
}

func javaStructural07FenceGateConnects(state uint32, face cube.Face) bool {
	switch javaName(state) {
	case "oak_fence_gate", "spruce_fence_gate", "birch_fence_gate", "jungle_fence_gate", "acacia_fence_gate", "cherry_fence_gate",
		"dark_oak_fence_gate", "pale_oak_fence_gate", "mangrove_fence_gate", "poplar_fence_gate", "bamboo_fence_gate", "crimson_fence_gate", "warped_fence_gate":
		return javaStructural07HorizontalFace(javaString(state, "facing")).Axis() != face.Axis()
	default:
		return false
	}
}

func javaStructural07WallConnectionException(c *javaShapeContext, state uint32) bool {
	name := javaName(state)
	return c.tag(state, "minecraft:leaves") || name == "barrier" || name == "carved_pumpkin" || name == "jack_o_lantern" ||
		name == "melon" || name == "pumpkin" || c.tag(state, "minecraft:shulker_boxes")
}

func javaStructural07WallUpdate(c *javaShapeContext, state, top uint32, north, east, south, west bool) uint32 {
	state = javaWith(state, "north", javaStructural07WallSideValue(north, top, cube.FaceNorth))
	state = javaWith(state, "east", javaStructural07WallSideValue(east, top, cube.FaceEast))
	state = javaWith(state, "south", javaStructural07WallSideValue(south, top, cube.FaceSouth))
	state = javaWith(state, "west", javaStructural07WallSideValue(west, top, cube.FaceWest))
	if c.tag(top, "minecraft:walls") && javaBool(top, "up") {
		return javaWith(state, "up", true)
	}
	n, e, s, w := javaString(state, "north"), javaString(state, "east"), javaString(state, "south"), javaString(state, "west")
	hasCorner := n == "none" && s == "none" && w == "none" && e == "none" || (n == "none") != (s == "none") || (w == "none") != (e == "none")
	raise := true
	if !hasCorner {
		if n == "tall" && s == "tall" || e == "tall" && w == "tall" {
			raise = false
		} else {
			raise = c.tag(top, "minecraft:wall_post_override") || javaStructural07WallAboveCovers(top)
		}
	}
	return javaWith(state, "up", raise)
}

func javaStructural07WallSideValue(connect bool, top uint32, face cube.Face) string {
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
	covered, err := javadata.GenerationCollisionFaceCoversRectangleID(top, int(cube.FaceDown), minU, minV, maxU, maxV)
	if err != nil {
		panic(err)
	}
	if covered {
		return "tall"
	}
	return "low"
}

func javaStructural07WallAboveCovers(top uint32) bool {
	covered, err := javadata.GenerationCollisionFaceCoversRectangleID(top, int(cube.FaceDown), 7.0/16, 7.0/16, 9.0/16, 9.0/16)
	if err != nil {
		panic(err)
	}
	return covered
}

func javaStructural07HorizontalFace(direction string) cube.Face {
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
