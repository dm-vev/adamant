package world

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/javadata"
)

func init() {
	registerJavaShape("FarmlandBlock", javaMechanism03FarmlandShape)
	registerJavaShape("FireBlock", javaMechanism03FireShape)
	registerJavaShape("NetherPortalBlock", javaMechanism03PortalShape)
	registerJavaShape("NoteBlock", javaMechanism03NoteShape)
	registerJavaShape("ObserverBlock", javaMechanism03ObserverShape)

	registerJavaSurvival("FarmlandBlock", javaMechanism03FarmlandSurvives)
	registerJavaSurvival("FireBlock", javaMechanism03FireSurvives)
}

func javaMechanism03FarmlandShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceUp && !javaMechanism03FarmlandSurvives(c, state, c.pos) {
		c.blockTick(c.pos, state, 1)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaMechanism03FarmlandSurvives(c *javaShapeContext, _ uint32, pos cube.Pos) bool {
	above := c.at(pos.Side(cube.FaceUp))
	return !c.solid(above) || c.tag(above, "minecraft:maintains_farmland")
}

func javaMechanism03FireShape(c *javaShapeContext, state uint32, _ cube.Face, _ uint32) uint32 {
	if !javaMechanism03FireSurvives(c, state, c.pos) {
		return javaDefault("air")
	}
	below := c.at(c.pos.Side(cube.FaceDown))
	if c.tag(below, "minecraft:soul_fire_base_blocks") {
		return javaDefault("soul_fire")
	}
	result := javaMechanism03FirePlacementState(c, c.pos)
	if javaName(result) == "fire" {
		age, _ := javadata.BlockStateInt(state, "age")
		result = javaWith(result, "age", age)
	}
	return result
}

func javaMechanism03FirePlacementState(c *javaShapeContext, pos cube.Pos) uint32 {
	below := c.at(pos.Side(cube.FaceDown))
	if javaMechanism03FireCanBurn(below) || c.sturdy(below, cube.FaceUp, javaSupportFull) {
		return javaDefault("fire")
	}
	state := javaDefault("fire")
	for _, face := range [...]cube.Face{cube.FaceNorth, cube.FaceEast, cube.FaceSouth, cube.FaceWest, cube.FaceUp} {
		state = javaWith(state, javaMechanism03FaceName(face), javaMechanism03FireCanBurn(c.at(pos.Side(face))))
	}
	return state
}

func javaMechanism03FireSurvives(c *javaShapeContext, _ uint32, pos cube.Pos) bool {
	belowPos := pos.Side(cube.FaceDown)
	if c.sturdy(c.at(belowPos), cube.FaceUp, javaSupportFull) {
		return true
	}
	for face := range 6 {
		if javaMechanism03FireCanBurn(c.at(pos.Side(cube.Face(face)))) {
			return true
		}
	}
	return false
}

func javaMechanism03FireCanBurn(state uint32) bool {
	canBurn, err := javadata.BlockCanBurnID(state)
	if err != nil {
		panic(err)
	}
	return canBurn
}

func javaMechanism03PortalShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	axis, _ := javadata.BlockStateString(state, "axis")
	wrongAxis := (axis == "x" && (face == cube.FaceNorth || face == cube.FaceSouth)) || (axis == "z" && (face == cube.FaceWest || face == cube.FaceEast))
	if !wrongAxis && javaName(neighbor) != "nether_portal" && !javaMechanism03PortalComplete(c, c.pos, axis) {
		return javaDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaMechanism03PortalComplete(c *javaShapeContext, pos cube.Pos, axis string) bool {
	right := cube.FaceSouth
	if axis == "x" {
		right = cube.FaceWest
	}
	left := right.Opposite()
	bottom := pos
	minY := max(c.tx.Range().Min(), pos[1]-21)
	for bottom[1] > minY && javaMechanism03PortalEmpty(c, c.at(bottom.Side(cube.FaceDown))) {
		bottom = bottom.Side(cube.FaceDown)
	}
	edge := javaMechanism03PortalEdge(c, bottom, left) - 1
	if edge < 0 {
		return false
	}
	bottom = javaMechanism03PortalMove(bottom, left, edge)
	width := javaMechanism03PortalEdge(c, bottom, right)
	if width < 2 || width > 21 {
		return false
	}
	portalCount := 0
	height := 0
	for ; height < 21; height++ {
		row := javaMechanism03PortalMove(bottom, cube.FaceUp, height)
		if !c.tag(c.at(row.Side(right.Opposite())), "minecraft:nether_portal_frame") || !c.tag(c.at(javaMechanism03PortalMove(row, right, width)), "minecraft:nether_portal_frame") {
			break
		}
		for i := range width {
			block := c.at(javaMechanism03PortalMove(row, right, i))
			if !javaMechanism03PortalEmpty(c, block) {
				goto heightDone
			}
			if javaName(block) == "nether_portal" {
				portalCount++
			}
		}
	}
heightDone:
	if height < 3 || height > 21 {
		return false
	}
	for i := range width {
		if !c.tag(c.at(javaMechanism03PortalMove(javaMechanism03PortalMove(bottom, cube.FaceUp, height), right, i)), "minecraft:nether_portal_frame") {
			return false
		}
	}
	return portalCount == width*height
}

func javaMechanism03PortalEmpty(c *javaShapeContext, state uint32) bool {
	return javaName(state) == "air" || c.tag(state, "minecraft:fire") || javaName(state) == "nether_portal"
}

func javaMechanism03PortalEdge(c *javaShapeContext, pos cube.Pos, face cube.Face) int {
	for width := range 22 {
		at := javaMechanism03PortalMove(pos, face, width)
		block := c.at(at)
		if !javaMechanism03PortalEmpty(c, block) {
			if c.tag(block, "minecraft:nether_portal_frame") {
				return width
			}
			break
		}
		if !c.tag(c.at(at.Side(cube.FaceDown)), "minecraft:nether_portal_frame") {
			break
		}
	}
	return 0
}

func javaMechanism03PortalMove(pos cube.Pos, face cube.Face, distance int) cube.Pos {
	for range distance {
		pos = pos.Side(face)
	}
	return pos
}

func javaMechanism03NoteShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceDown || face == cube.FaceUp {
		instrument := javaMechanism03BlockInstrument(c.at(c.pos.Side(cube.FaceUp)))
		if javaMechanism03WorksAboveNoteBlock(instrument) {
			return javaWith(state, "instrument", instrument)
		}
		instrument = javaMechanism03BlockInstrument(c.at(c.pos.Side(cube.FaceDown)))
		if javaMechanism03WorksAboveNoteBlock(instrument) {
			instrument = "harp"
		}
		return javaWith(state, "instrument", instrument)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaMechanism03BlockInstrument(state uint32) string {
	instrument, err := javadata.BlockInstrumentID(state)
	if err != nil {
		panic(err)
	}
	return instrument
}

func javaMechanism03WorksAboveNoteBlock(instrument string) bool {
	switch instrument {
	case "skeleton", "wither_skeleton", "zombie", "creeper", "dragon", "piglin", "custom_head":
		return true
	default:
		return false
	}
}

func javaMechanism03ObserverShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	facing, _ := javadata.BlockStateString(state, "facing")
	powered, _ := javadata.BlockStateBool(state, "powered")
	if !powered && javaMechanism03FaceName(face) == facing {
		c.blockTick(c.pos, state, 2)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaMechanism03FaceName(face cube.Face) string {
	switch face {
	case cube.FaceDown:
		return "down"
	case cube.FaceUp:
		return "up"
	case cube.FaceNorth:
		return "north"
	case cube.FaceSouth:
		return "south"
	case cube.FaceWest:
		return "west"
	case cube.FaceEast:
		return "east"
	default:
		return ""
	}
}
