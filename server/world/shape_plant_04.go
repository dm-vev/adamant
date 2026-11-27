package world

import (
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/internal/gamedata"
)

func init() {
	registerShape("DoublePlantBlock", plant04DoublePlantShape)
	registerShape("FlowerPotBlock", plant04FlowerPotShape)
	registerShape("FrogspawnBlock", plant04FrogspawnShape)
	registerShape("GrowingPlantBodyBlock", plant04GrowingPlantBodyShape)
	registerShape("GrowingPlantHeadBlock", plant04GrowingPlantHeadShape)

	registerSurvival("DoublePlantBlock", plant04DoublePlantSurvival)
	registerSurvival("FrogspawnBlock", plant04FrogspawnSurvival)
	registerSurvival("GrowingPlantBlock", plant04GrowingPlantSurvival)
}

func plant04DoublePlantShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	half, _ := gamedata.BlockStateString(state, "half")
	neighborHalf, _ := gamedata.BlockStateString(neighbor, "half")
	if face != cube.FaceDown && face != cube.FaceUp ||
		(half == "lower") != (face == cube.FaceUp) ||
		(stateName(neighbor) == stateName(state) && neighborHalf != half) {
		if half == "lower" && face == cube.FaceDown && !c.canSurvive(state, c.pos) {
			return stateDefault("air")
		}
		return c.ownerShape("VegetationBlock", state, face, neighbor)
	}
	return stateDefault("air")
}

func plant04DoublePlantSurvival(c *shapeContext, state uint32, pos cube.Pos) bool {
	half, _ := gamedata.BlockStateString(state, "half")
	if half != "upper" {
		return c.ownerSurvival("VegetationBlock", state, pos)
	}
	below := c.at(pos.Side(cube.FaceDown))
	belowHalf, _ := gamedata.BlockStateString(below, "half")
	return stateName(below) == stateName(state) && belowHalf == "lower"
}

func plant04FlowerPotShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceDown && !c.canSurvive(state, c.pos) {
		return stateDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func plant04FrogspawnShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if !c.canSurvive(state, c.pos) {
		return stateDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func plant04FrogspawnSurvival(c *shapeContext, state uint32, pos cube.Pos) bool {
	below := c.at(pos.Side(cube.FaceDown))
	belowFluid := c.fluid(below)
	aboveFluid := c.fluid(state)
	return (belowFluid.kind == fluidWater || c.tag(below, "supports_frogspawn")) && aboveFluid.kind == fluidEmpty
}

func plant04GrowingPlantBodyShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	growth := plant04GrowingDirection(state)
	if face == growth.Opposite() && !c.canSurvive(state, c.pos) {
		c.blockTick(c.pos, state, 1)
	}

	if face == growth && !plant04GrowingPlantRelated(state, neighbor) {
		return plant04HeadPlacement(state, c)
	}
	if plant04SchedulesWater(state) {
		water := fluidState{kind: fluidWater, amount: 8, source: true}
		c.fluidTick(c.pos, water, c.fluidDelay(water))
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func plant04GrowingPlantHeadShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	growth := plant04GrowingDirection(state)
	if face == growth.Opposite() {
		if !c.canSurvive(state, c.pos) {
			c.blockTick(c.pos, state, 1)
		} else if plant04GrowingPlantRelated(state, c.at(c.pos.Side(growth))) {
			return plant04BodyConversion(state)
		}
	}

	if face != growth || !plant04GrowingPlantRelated(state, neighbor) {
		if plant04SchedulesWater(state) {
			water := fluidState{kind: fluidWater, amount: 8, source: true}
			c.fluidTick(c.pos, water, c.fluidDelay(water))
		}
		return c.ownerShape("BlockBehaviour", state, face, neighbor)
	}
	return plant04BodyConversion(state)
}

func plant04GrowingPlantSurvival(c *shapeContext, state uint32, pos cube.Pos) bool {
	growth := plant04GrowingDirection(state)
	attached := c.at(pos.Side(growth.Opposite()))
	name := stateName(state)
	if name == "kelp" || name == "kelp_plant" {
		if c.tag(attached, "cannot_support_kelp") {
			return false
		}
	}
	if plant04GrowingPlantRelated(state, attached) {
		return true
	}
	return c.sturdy(attached, growth, supportFull)
}

func plant04GrowingDirection(state uint32) cube.Face {
	switch stateName(state) {
	case "kelp", "kelp_plant", "twisting_vines", "twisting_vines_plant":
		return cube.FaceUp
	case "weeping_vines", "weeping_vines_plant", "cave_vines", "cave_vines_plant":
		return cube.FaceDown
	default:
		panic("unhandled Java growing plant block " + stateName(state))
	}
}

func plant04GrowingPlantRelated(state, other uint32) bool {
	name, otherName := stateName(state), stateName(other)
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

func plant04SchedulesWater(state uint32) bool {
	return stateName(state) == "kelp" || stateName(state) == "kelp_plant"
}

func plant04HeadPlacement(body uint32, c *shapeContext) uint32 {
	head := uint32(0)
	switch stateName(body) {
	case "kelp_plant":
		head = stateDefault("kelp")
	case "weeping_vines_plant":
		head = stateDefault("weeping_vines")
	case "twisting_vines_plant":
		head = stateDefault("twisting_vines")
	case "cave_vines_plant":
		head = stateWith(stateDefault("cave_vines"), "berries", stateBool(body, "berries"))
	default:
		panic("unhandled Java growing plant body " + stateName(body))
	}
	return stateWith(head, "age", c.randomInt(25))
}

func plant04BodyConversion(head uint32) uint32 {
	var body uint32
	switch stateName(head) {
	case "kelp":
		body = stateDefault("kelp_plant")
	case "weeping_vines":
		body = stateDefault("weeping_vines_plant")
	case "twisting_vines":
		body = stateDefault("twisting_vines_plant")
	case "cave_vines":
		body = stateDefault("cave_vines_plant")
		berries, _ := gamedata.BlockStateBool(head, "berries")
		body = stateWith(body, "berries", berries)
	default:
		panic("unhandled Java growing plant head " + stateName(head))
	}
	return body
}
