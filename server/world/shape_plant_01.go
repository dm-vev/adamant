package world

import "github.com/dm-vev/adamant/server/block/cube"

func init() {
	registerShape("AttachedStemBlock", plant01AttachedStemShape)
	registerShape("BambooSaplingBlock", plant01BambooSaplingShape)
	registerShape("BambooStalkBlock", plant01BambooStalkShape)
	registerShape("BaseCoralPlantTypeBlock", plant01CoralPlantShape)
	registerShape("BaseCoralWallFanBlock", plant01CoralWallFanShape)
	registerSurvival("BambooSaplingBlock", plant01BambooSurvival)
	registerSurvival("BambooStalkBlock", plant01BambooSurvival)
	registerSurvival("BaseCoralPlantTypeBlock", plant01CoralPlantSurvival)
	registerSurvival("BaseCoralWallFanBlock", plant01CoralWallFanSurvival)
}

func plant01AttachedStemShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if (stateName(state) == "attached_pumpkin_stem" && stateName(neighbor) != "pumpkin") ||
		(stateName(state) == "attached_melon_stem" && stateName(neighbor) != "melon") {
		if face == stateStringDirection(stateString(state, "facing")) {
			stem := "pumpkin_stem"
			if stateName(state) == "attached_melon_stem" {
				stem = "melon_stem"
			}
			return stateWith(stateDefault(stem), "age", int32(7))
		}
	}
	return c.ownerShape("VegetationBlock", state, face, neighbor)
}

func plant01BambooSaplingShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if !plant01BambooSurvival(c, state, c.pos) {
		return stateDefault("air")
	}
	if face == cube.FaceUp && stateName(neighbor) == "bamboo" {
		return stateDefault("bamboo")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func plant01BambooStalkShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if !plant01BambooSurvival(c, state, c.pos) {
		c.blockTick(c.pos, state, 1)
	}
	if face == cube.FaceUp && stateName(neighbor) == "bamboo" && stateInt(neighbor, "age") > stateInt(state, "age") {
		return stateWith(state, "age", stateInt(state, "age")+1)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func plant01BambooSurvival(c *shapeContext, state uint32, pos cube.Pos) bool {
	return c.tag(c.at(pos.Side(cube.FaceDown)), "supports_bamboo")
}

func plant01CoralPlantShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if stateBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	if face == cube.FaceDown && !plant01CoralPlantSurvival(c, state, c.pos) {
		return stateDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func plant01CoralPlantSurvival(c *shapeContext, state uint32, pos cube.Pos) bool {
	return c.sturdy(c.at(pos.Side(cube.FaceDown)), cube.FaceUp, supportFull)
}

func plant01CoralWallFanShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if stateBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	facing := stateStringDirection(stateString(state, "facing"))
	if face == facing.Opposite() && !plant01CoralWallFanSurvival(c, state, c.pos) {
		return stateDefault("air")
	}
	return state
}

func plant01CoralWallFanSurvival(c *shapeContext, state uint32, pos cube.Pos) bool {
	facing := stateStringDirection(stateString(state, "facing"))
	supportPos := pos.Side(facing.Opposite())
	return c.sturdy(c.at(supportPos), facing, supportFull)
}

func stateStringDirection(value string) cube.Face {
	switch value {
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
		panic("invalid Java direction " + value)
	}
}
