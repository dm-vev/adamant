package world

import "github.com/dm-vev/adamant/server/block/cube"

func init() {
	registerShape("BigDripleafBlock", plant02BigDripleafShape)
	registerShape("BigDripleafStemBlock", plant02DripleafStemShape)
	registerShape("CactusBlock", plant02CactusShape)
	registerShape("ChorusFlowerBlock", plant02ChorusFlowerShape)
	registerShape("ChorusPlantBlock", plant02ChorusPlantShape)
	registerSurvival("BigDripleafBlock", plant02BigDripleafSurvives)
	registerSurvival("BigDripleafStemBlock", plant02DripleafStemSurvives)
	registerSurvival("CactusBlock", plant02CactusSurvives)
	registerSurvival("ChorusFlowerBlock", plant02ChorusFlowerSurvives)
	registerSurvival("ChorusPlantBlock", plant02ChorusPlantSurvives)
}

func plant02BigDripleafSurvives(c *shapeContext, _ uint32, pos cube.Pos) bool {
	below := c.at(pos.Side(cube.FaceDown))
	n := stateName(below)
	return n == "big_dripleaf" || n == "big_dripleaf_stem" || c.tag(below, "minecraft:supports_big_dripleaf")
}
func plant02DripleafStemSurvives(c *shapeContext, _ uint32, pos cube.Pos) bool {
	below, above := c.at(pos.Side(cube.FaceDown)), c.at(pos.Side(cube.FaceUp))
	b, a := stateName(below), stateName(above)
	return (b == "big_dripleaf_stem" || c.tag(below, "minecraft:supports_big_dripleaf")) && (a == "big_dripleaf_stem" || a == "big_dripleaf")
}
func plant02CactusSurvives(c *shapeContext, state uint32, pos cube.Pos) bool {
	for _, f := range [...]cube.Face{cube.FaceNorth, cube.FaceSouth, cube.FaceWest, cube.FaceEast} {
		n := c.at(pos.Side(f))
		if c.solid(n) || c.fluid(n).kind == fluidLava {
			return false
		}
	}
	below, above := c.at(pos.Side(cube.FaceDown)), c.at(pos.Side(cube.FaceUp))
	aboveName := stateName(above)
	return (stateName(below) == stateName(state) || c.tag(below, "minecraft:supports_cactus")) && aboveName != "water" && aboveName != "lava" && aboveName != "bubble_column"
}
func plant02ChorusFlowerSurvives(c *shapeContext, _ uint32, pos cube.Pos) bool {
	below := c.at(pos.Side(cube.FaceDown))
	if stateName(below) == "chorus_plant" || c.tag(below, "minecraft:supports_chorus_flower") {
		return true
	}
	if !stateIsAir(below) {
		return false
	}
	found := false
	for _, f := range [...]cube.Face{cube.FaceNorth, cube.FaceSouth, cube.FaceWest, cube.FaceEast} {
		n := c.at(pos.Side(f))
		if stateName(n) == "chorus_plant" {
			if found {
				return false
			}
			found = true
		} else if !stateIsAir(n) {
			return false
		}
	}
	return found
}
func plant02ChorusPlantSurvives(c *shapeContext, _ uint32, pos cube.Pos) bool {
	below, above := c.at(pos.Side(cube.FaceDown)), c.at(pos.Side(cube.FaceUp))
	vertical := !stateIsAir(above) && !stateIsAir(below)
	for _, f := range [...]cube.Face{cube.FaceNorth, cube.FaceSouth, cube.FaceWest, cube.FaceEast} {
		np := pos.Side(f)
		n := c.at(np)
		if stateName(n) != "chorus_plant" {
			continue
		}
		if vertical {
			return false
		}
		s := c.at(np.Side(cube.FaceDown))
		if stateName(s) == "chorus_plant" || c.tag(s, "minecraft:supports_chorus_plant") {
			return true
		}
	}
	return stateName(below) == "chorus_plant" || c.tag(below, "minecraft:supports_chorus_plant")
}
func plant02BigDripleafShape(c *shapeContext, s uint32, f cube.Face, n uint32) uint32 {
	if f == cube.FaceDown && !plant02BigDripleafSurvives(c, s, c.pos) {
		return stateDefault("air")
	}
	if stateBool(s, "waterlogged") {
		c.waterTick(s, c.pos)
	}
	if f == cube.FaceUp && stateName(n) == "big_dripleaf" {
		stem := stateWith(stateDefault("big_dripleaf_stem"), "facing", stateString(s, "facing"))
		return stateWith(stem, "waterlogged", stateBool(s, "waterlogged"))
	}
	return c.ownerShape("BlockBehaviour", s, f, n)
}
func plant02DripleafStemShape(c *shapeContext, s uint32, f cube.Face, n uint32) uint32 {
	if (f == cube.FaceDown || f == cube.FaceUp) && !plant02DripleafStemSurvives(c, s, c.pos) {
		c.blockTick(c.pos, s, 1)
	}
	if stateBool(s, "waterlogged") {
		c.waterTick(s, c.pos)
	}
	return c.ownerShape("BlockBehaviour", s, f, n)
}
func plant02CactusShape(c *shapeContext, s uint32, f cube.Face, n uint32) uint32 {
	if !plant02CactusSurvives(c, s, c.pos) {
		c.blockTick(c.pos, s, 1)
	}
	return c.ownerShape("BlockBehaviour", s, f, n)
}
func plant02ChorusFlowerShape(c *shapeContext, s uint32, f cube.Face, n uint32) uint32 {
	if f != cube.FaceUp && !plant02ChorusFlowerSurvives(c, s, c.pos) {
		c.blockTick(c.pos, s, 1)
	}
	return c.ownerShape("BlockBehaviour", s, f, n)
}
func plant02ChorusPlantShape(c *shapeContext, s uint32, f cube.Face, n uint32) uint32 {
	if !plant02ChorusPlantSurvives(c, s, c.pos) {
		c.blockTick(c.pos, s, 1)
		return c.ownerShape("BlockBehaviour", s, f, n)
	}
	connect := stateName(n) == "chorus_plant" || stateName(n) == "chorus_flower" || f == cube.FaceDown && c.tag(n, "minecraft:supports_chorus_plant")
	var key string
	switch f {
	case cube.FaceDown:
		key = "down"
	case cube.FaceUp:
		key = "up"
	case cube.FaceNorth:
		key = "north"
	case cube.FaceSouth:
		key = "south"
	case cube.FaceWest:
		key = "west"
	case cube.FaceEast:
		key = "east"
	}
	return stateWith(s, key, connect)
}
