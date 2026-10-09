package world

import "github.com/df-mc/dragonfly/server/block/cube"

func init() {
	registerJavaShape("BigDripleafBlock", javaPlant02BigDripleafShape)
	registerJavaShape("BigDripleafStemBlock", javaPlant02DripleafStemShape)
	registerJavaShape("CactusBlock", javaPlant02CactusShape)
	registerJavaShape("ChorusFlowerBlock", javaPlant02ChorusFlowerShape)
	registerJavaShape("ChorusPlantBlock", javaPlant02ChorusPlantShape)
	registerJavaSurvival("BigDripleafBlock", javaPlant02BigDripleafSurvives)
	registerJavaSurvival("BigDripleafStemBlock", javaPlant02DripleafStemSurvives)
	registerJavaSurvival("CactusBlock", javaPlant02CactusSurvives)
	registerJavaSurvival("ChorusFlowerBlock", javaPlant02ChorusFlowerSurvives)
	registerJavaSurvival("ChorusPlantBlock", javaPlant02ChorusPlantSurvives)
}

func javaPlant02BigDripleafSurvives(c *javaShapeContext, _ uint32, pos cube.Pos) bool {
	below := c.at(pos.Side(cube.FaceDown))
	n := javaName(below)
	return n == "big_dripleaf" || n == "big_dripleaf_stem" || c.tag(below, "minecraft:supports_big_dripleaf")
}
func javaPlant02DripleafStemSurvives(c *javaShapeContext, _ uint32, pos cube.Pos) bool {
	below, above := c.at(pos.Side(cube.FaceDown)), c.at(pos.Side(cube.FaceUp))
	b, a := javaName(below), javaName(above)
	return (b == "big_dripleaf_stem" || c.tag(below, "minecraft:supports_big_dripleaf")) && (a == "big_dripleaf_stem" || a == "big_dripleaf")
}
func javaPlant02CactusSurvives(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	for _, f := range [...]cube.Face{cube.FaceNorth, cube.FaceSouth, cube.FaceWest, cube.FaceEast} {
		n := c.at(pos.Side(f))
		if c.solid(n) || c.fluid(n).kind == javaFluidLava {
			return false
		}
	}
	below, above := c.at(pos.Side(cube.FaceDown)), c.at(pos.Side(cube.FaceUp))
	aboveName := javaName(above)
	return (javaName(below) == javaName(state) || c.tag(below, "minecraft:supports_cactus")) && aboveName != "water" && aboveName != "lava" && aboveName != "bubble_column"
}
func javaPlant02ChorusFlowerSurvives(c *javaShapeContext, _ uint32, pos cube.Pos) bool {
	below := c.at(pos.Side(cube.FaceDown))
	if javaName(below) == "chorus_plant" || c.tag(below, "minecraft:supports_chorus_flower") {
		return true
	}
	if !javaIsAir(below) {
		return false
	}
	found := false
	for _, f := range [...]cube.Face{cube.FaceNorth, cube.FaceSouth, cube.FaceWest, cube.FaceEast} {
		n := c.at(pos.Side(f))
		if javaName(n) == "chorus_plant" {
			if found {
				return false
			}
			found = true
		} else if !javaIsAir(n) {
			return false
		}
	}
	return found
}
func javaPlant02ChorusPlantSurvives(c *javaShapeContext, _ uint32, pos cube.Pos) bool {
	below, above := c.at(pos.Side(cube.FaceDown)), c.at(pos.Side(cube.FaceUp))
	vertical := !javaIsAir(above) && !javaIsAir(below)
	for _, f := range [...]cube.Face{cube.FaceNorth, cube.FaceSouth, cube.FaceWest, cube.FaceEast} {
		np := pos.Side(f)
		n := c.at(np)
		if javaName(n) != "chorus_plant" {
			continue
		}
		if vertical {
			return false
		}
		s := c.at(np.Side(cube.FaceDown))
		if javaName(s) == "chorus_plant" || c.tag(s, "minecraft:supports_chorus_plant") {
			return true
		}
	}
	return javaName(below) == "chorus_plant" || c.tag(below, "minecraft:supports_chorus_plant")
}
func javaPlant02BigDripleafShape(c *javaShapeContext, s uint32, f cube.Face, n uint32) uint32 {
	if f == cube.FaceDown && !javaPlant02BigDripleafSurvives(c, s, c.pos) {
		return javaDefault("air")
	}
	if javaBool(s, "waterlogged") {
		c.waterTick(s, c.pos)
	}
	if f == cube.FaceUp && javaName(n) == "big_dripleaf" {
		stem := javaWith(javaDefault("big_dripleaf_stem"), "facing", javaString(s, "facing"))
		return javaWith(stem, "waterlogged", javaBool(s, "waterlogged"))
	}
	return c.ownerShape("BlockBehaviour", s, f, n)
}
func javaPlant02DripleafStemShape(c *javaShapeContext, s uint32, f cube.Face, n uint32) uint32 {
	if (f == cube.FaceDown || f == cube.FaceUp) && !javaPlant02DripleafStemSurvives(c, s, c.pos) {
		c.blockTick(c.pos, s, 1)
	}
	if javaBool(s, "waterlogged") {
		c.waterTick(s, c.pos)
	}
	return c.ownerShape("BlockBehaviour", s, f, n)
}
func javaPlant02CactusShape(c *javaShapeContext, s uint32, f cube.Face, n uint32) uint32 {
	if !javaPlant02CactusSurvives(c, s, c.pos) {
		c.blockTick(c.pos, s, 1)
	}
	return c.ownerShape("BlockBehaviour", s, f, n)
}
func javaPlant02ChorusFlowerShape(c *javaShapeContext, s uint32, f cube.Face, n uint32) uint32 {
	if f != cube.FaceUp && !javaPlant02ChorusFlowerSurvives(c, s, c.pos) {
		c.blockTick(c.pos, s, 1)
	}
	return c.ownerShape("BlockBehaviour", s, f, n)
}
func javaPlant02ChorusPlantShape(c *javaShapeContext, s uint32, f cube.Face, n uint32) uint32 {
	if !javaPlant02ChorusPlantSurvives(c, s, c.pos) {
		c.blockTick(c.pos, s, 1)
		return c.ownerShape("BlockBehaviour", s, f, n)
	}
	connect := javaName(n) == "chorus_plant" || javaName(n) == "chorus_flower" || f == cube.FaceDown && c.tag(n, "minecraft:supports_chorus_plant")
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
	return javaWith(s, key, connect)
}
