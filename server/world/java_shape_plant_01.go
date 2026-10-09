package world

import "github.com/df-mc/dragonfly/server/block/cube"

func init() {
	registerJavaShape("AttachedStemBlock", javaPlant01AttachedStemShape)
	registerJavaShape("BambooSaplingBlock", javaPlant01BambooSaplingShape)
	registerJavaShape("BambooStalkBlock", javaPlant01BambooStalkShape)
	registerJavaShape("BaseCoralPlantTypeBlock", javaPlant01CoralPlantShape)
	registerJavaShape("BaseCoralWallFanBlock", javaPlant01CoralWallFanShape)
	registerJavaSurvival("BambooSaplingBlock", javaPlant01BambooSurvival)
	registerJavaSurvival("BambooStalkBlock", javaPlant01BambooSurvival)
	registerJavaSurvival("BaseCoralPlantTypeBlock", javaPlant01CoralPlantSurvival)
	registerJavaSurvival("BaseCoralWallFanBlock", javaPlant01CoralWallFanSurvival)
}

func javaPlant01AttachedStemShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if (javaName(state) == "attached_pumpkin_stem" && javaName(neighbor) != "pumpkin") ||
		(javaName(state) == "attached_melon_stem" && javaName(neighbor) != "melon") {
		if face == javaStringDirection(javaString(state, "facing")) {
			stem := "pumpkin_stem"
			if javaName(state) == "attached_melon_stem" {
				stem = "melon_stem"
			}
			return javaWith(javaDefault(stem), "age", int32(7))
		}
	}
	return c.ownerShape("VegetationBlock", state, face, neighbor)
}

func javaPlant01BambooSaplingShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if !javaPlant01BambooSurvival(c, state, c.pos) {
		return javaDefault("air")
	}
	if face == cube.FaceUp && javaName(neighbor) == "bamboo" {
		return javaDefault("bamboo")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaPlant01BambooStalkShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if !javaPlant01BambooSurvival(c, state, c.pos) {
		c.blockTick(c.pos, state, 1)
	}
	if face == cube.FaceUp && javaName(neighbor) == "bamboo" && javaInt(neighbor, "age") > javaInt(state, "age") {
		return javaWith(state, "age", javaInt(state, "age")+1)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaPlant01BambooSurvival(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	return c.tag(c.at(pos.Side(cube.FaceDown)), "supports_bamboo")
}

func javaPlant01CoralPlantShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if javaBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	if face == cube.FaceDown && !javaPlant01CoralPlantSurvival(c, state, c.pos) {
		return javaDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaPlant01CoralPlantSurvival(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	return c.sturdy(c.at(pos.Side(cube.FaceDown)), cube.FaceUp, javaSupportFull)
}

func javaPlant01CoralWallFanShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if javaBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	facing := javaStringDirection(javaString(state, "facing"))
	if face == facing.Opposite() && !javaPlant01CoralWallFanSurvival(c, state, c.pos) {
		return javaDefault("air")
	}
	return state
}

func javaPlant01CoralWallFanSurvival(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	facing := javaStringDirection(javaString(state, "facing"))
	supportPos := pos.Side(facing.Opposite())
	return c.sturdy(c.at(supportPos), facing, javaSupportFull)
}

func javaStringDirection(value string) cube.Face {
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
