package world

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/javadata"
)

func init() {
	registerJavaShape("DoublePlantBlock", javaPlant04DoublePlantShape)
	registerJavaShape("FlowerPotBlock", javaPlant04FlowerPotShape)
	registerJavaShape("FrogspawnBlock", javaPlant04FrogspawnShape)
	registerJavaShape("GrowingPlantBodyBlock", javaPlant04GrowingPlantBodyShape)
	registerJavaShape("GrowingPlantHeadBlock", javaPlant04GrowingPlantHeadShape)

	registerJavaSurvival("DoublePlantBlock", javaPlant04DoublePlantSurvival)
	registerJavaSurvival("FrogspawnBlock", javaPlant04FrogspawnSurvival)
	registerJavaSurvival("GrowingPlantBlock", javaPlant04GrowingPlantSurvival)
}

func javaPlant04DoublePlantShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	half, _ := javadata.BlockStateString(state, "half")
	neighborHalf, _ := javadata.BlockStateString(neighbor, "half")
	if face != cube.FaceDown && face != cube.FaceUp ||
		(half == "lower") != (face == cube.FaceUp) ||
		(javaName(neighbor) == javaName(state) && neighborHalf != half) {
		if half == "lower" && face == cube.FaceDown && !c.canSurvive(state, c.pos) {
			return javaDefault("air")
		}
		return c.ownerShape("VegetationBlock", state, face, neighbor)
	}
	return javaDefault("air")
}

func javaPlant04DoublePlantSurvival(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	half, _ := javadata.BlockStateString(state, "half")
	if half != "upper" {
		return c.ownerSurvival("VegetationBlock", state, pos)
	}
	below := c.at(pos.Side(cube.FaceDown))
	belowHalf, _ := javadata.BlockStateString(below, "half")
	return javaName(below) == javaName(state) && belowHalf == "lower"
}

func javaPlant04FlowerPotShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceDown && !c.canSurvive(state, c.pos) {
		return javaDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaPlant04FrogspawnShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if !c.canSurvive(state, c.pos) {
		return javaDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaPlant04FrogspawnSurvival(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	below := c.at(pos.Side(cube.FaceDown))
	belowFluid := c.fluid(below)
	aboveFluid := c.fluid(state)
	return (belowFluid.kind == javaFluidWater || c.tag(below, "supports_frogspawn")) && aboveFluid.kind == javaFluidEmpty
}

func javaPlant04GrowingPlantBodyShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	growth := javaPlant04GrowingDirection(state)
	if face == growth.Opposite() && !c.canSurvive(state, c.pos) {
		c.blockTick(c.pos, state, 1)
	}

	if face == growth && !javaPlant04GrowingPlantRelated(state, neighbor) {
		return javaPlant04HeadPlacement(state, c)
	}
	if javaPlant04SchedulesWater(state) {
		water := javaFluidState{kind: javaFluidWater, amount: 8, source: true}
		c.fluidTick(c.pos, water, javaFluidTickDelay(c.tx, water))
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaPlant04GrowingPlantHeadShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	growth := javaPlant04GrowingDirection(state)
	if face == growth.Opposite() {
		if !c.canSurvive(state, c.pos) {
			c.blockTick(c.pos, state, 1)
		} else if javaPlant04GrowingPlantRelated(state, c.at(c.pos.Side(growth))) {
			return javaPlant04BodyConversion(state)
		}
	}

	if face != growth || !javaPlant04GrowingPlantRelated(state, neighbor) {
		if javaPlant04SchedulesWater(state) {
			water := javaFluidState{kind: javaFluidWater, amount: 8, source: true}
			c.fluidTick(c.pos, water, javaFluidTickDelay(c.tx, water))
		}
		return c.ownerShape("BlockBehaviour", state, face, neighbor)
	}
	return javaPlant04BodyConversion(state)
}

func javaPlant04GrowingPlantSurvival(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	growth := javaPlant04GrowingDirection(state)
	attached := c.at(pos.Side(growth.Opposite()))
	name := javaName(state)
	if name == "kelp" || name == "kelp_plant" {
		if c.tag(attached, "cannot_support_kelp") {
			return false
		}
	}
	if javaPlant04GrowingPlantRelated(state, attached) {
		return true
	}
	return c.sturdy(attached, growth, javaSupportFull)
}

func javaPlant04GrowingDirection(state uint32) cube.Face {
	switch javaName(state) {
	case "kelp", "kelp_plant", "twisting_vines", "twisting_vines_plant":
		return cube.FaceUp
	case "weeping_vines", "weeping_vines_plant", "cave_vines", "cave_vines_plant":
		return cube.FaceDown
	default:
		panic("unhandled Java growing plant block " + javaName(state))
	}
}

func javaPlant04GrowingPlantRelated(state, other uint32) bool {
	name, otherName := javaName(state), javaName(other)
	switch name {
	case "kelp", "kelp_plant":
		return otherName == "kelp" || otherName == "kelp_plant"
	case "weeping_vines", "weeping_vines_plant":
		return otherName == "weeping_vines" || otherName == "weeping_vines_plant"
	case "twisting_vines", "twisting_vines_plant":
		return otherName == "twisting_vines" || otherName == "twisting_vines_plant"
	case "cave_vines", "cave_vines_plant":
		return otherName == "cave_vines" || otherName == "cave_vines_plant"
	default:
		panic("unhandled Java growing plant block " + name)
	}
}

func javaPlant04SchedulesWater(state uint32) bool {
	return javaName(state) == "kelp" || javaName(state) == "kelp_plant"
}

func javaPlant04HeadPlacement(body uint32, c *javaShapeContext) uint32 {
	head := uint32(0)
	switch javaName(body) {
	case "kelp_plant":
		head = javaDefault("kelp")
	case "weeping_vines_plant":
		head = javaDefault("weeping_vines")
	case "twisting_vines_plant":
		head = javaDefault("twisting_vines")
	case "cave_vines_plant":
		head = javaWith(javaDefault("cave_vines"), "berries", javaBool(body, "berries"))
	default:
		panic("unhandled Java growing plant body " + javaName(body))
	}
	return javaWith(head, "age", c.randomInt(25))
}

func javaPlant04BodyConversion(head uint32) uint32 {
	var body uint32
	switch javaName(head) {
	case "kelp":
		body = javaDefault("kelp_plant")
	case "weeping_vines":
		body = javaDefault("weeping_vines_plant")
	case "twisting_vines":
		body = javaDefault("twisting_vines_plant")
	case "cave_vines":
		body = javaDefault("cave_vines_plant")
		berries, _ := javadata.BlockStateBool(head, "berries")
		body = javaWith(body, "berries", berries)
	default:
		panic("unhandled Java growing plant head " + javaName(head))
	}
	return body
}
