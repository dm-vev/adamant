package world

import (
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/internal/gamedata"
)

func init() {
	registerShape("FarmlandBlock", mechanism03FarmlandShape)
	registerShape("FireBlock", mechanism03FireShape)
	registerShape("NetherPortalBlock", mechanism03PortalShape)
	registerShape("NoteBlock", mechanism03NoteShape)
	registerShape("ObserverBlock", mechanism03ObserverShape)

	registerSurvival("FarmlandBlock", mechanism03FarmlandSurvives)
	registerSurvival("FireBlock", mechanism03FireSurvives)
}

func mechanism03FarmlandShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceUp && !mechanism03FarmlandSurvives(c, state, c.pos) {
		c.blockTick(c.pos, state, 1)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func mechanism03FarmlandSurvives(c *shapeContext, _ uint32, pos cube.Pos) bool {
	above := c.at(pos.Side(cube.FaceUp))
	return !c.solid(above) || c.tag(above, "minecraft:maintains_farmland")
}

func mechanism03FireShape(c *shapeContext, state uint32, _ cube.Face, _ uint32) uint32 {
	if !mechanism03FireSurvives(c, state, c.pos) {
		return stateDefault("air")
	}
	below := c.at(c.pos.Side(cube.FaceDown))
	if c.tag(below, "minecraft:soul_fire_base_blocks") {
		return stateDefault("soul_fire")
	}
	result := mechanism03FirePlacementState(c, c.pos)
	if stateName(result) == "fire" {
		age, _ := gamedata.BlockStateInt(state, "age")
		result = stateWith(result, "age", age)
	}
	return result
}

func mechanism03FirePlacementState(c *shapeContext, pos cube.Pos) uint32 {
	below := c.at(pos.Side(cube.FaceDown))
	if mechanism03FireCanBurn(below) || c.sturdy(below, cube.FaceUp, supportFull) {
		return stateDefault("fire")
	}
	state := stateDefault("fire")
	for _, face := range [...]cube.Face{cube.FaceNorth, cube.FaceEast, cube.FaceSouth, cube.FaceWest, cube.FaceUp} {
		state = stateWith(state, mechanism03FaceName(face), mechanism03FireCanBurn(c.at(pos.Side(face))))
	}
	return state
}

func mechanism03FireSurvives(c *shapeContext, _ uint32, pos cube.Pos) bool {
	belowPos := pos.Side(cube.FaceDown)
	if c.sturdy(c.at(belowPos), cube.FaceUp, supportFull) {
		return true
	}
	for face := range 6 {
		if mechanism03FireCanBurn(c.at(pos.Side(cube.Face(face)))) {
			return true
		}
	}
	return false
}

func mechanism03FireCanBurn(state uint32) bool {
	canBurn, err := gamedata.BlockCanBurnID(state)
	if err != nil {
		panic(err)
	}
	return canBurn
}

func mechanism03PortalShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	axis, _ := gamedata.BlockStateString(state, "axis")
	wrongAxis := (axis == "x" && (face == cube.FaceNorth || face == cube.FaceSouth)) || (axis == "z" && (face == cube.FaceWest || face == cube.FaceEast))
	if !wrongAxis && stateName(neighbor) != "nether_portal" && !mechanism03PortalComplete(c, c.pos, axis) {
		return stateDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func mechanism03PortalComplete(c *shapeContext, pos cube.Pos, axis string) bool {
	right := cube.FaceSouth
	if axis == "x" {
		right = cube.FaceWest
	}
	left := right.Opposite()
	bottom := pos
	minY := max(c.blockRange().Min(), pos[1]-21)
	for bottom[1] > minY && mechanism03PortalEmpty(c, c.at(bottom.Side(cube.FaceDown))) {
		bottom = bottom.Side(cube.FaceDown)
	}
	edge := mechanism03PortalEdge(c, bottom, left) - 1
	if edge < 0 {
		return false
	}
	bottom = mechanism03PortalMove(bottom, left, edge)
	width := mechanism03PortalEdge(c, bottom, right)
	if width < 2 || width > 21 {
		return false
	}
	portalCount := 0
	height := 0
	for ; height < 21; height++ {
		row := mechanism03PortalMove(bottom, cube.FaceUp, height)
		if !c.tag(c.at(row.Side(right.Opposite())), "minecraft:nether_portal_frame") || !c.tag(c.at(mechanism03PortalMove(row, right, width)), "minecraft:nether_portal_frame") {
			break
		}
		for i := range width {
			block := c.at(mechanism03PortalMove(row, right, i))
			if !mechanism03PortalEmpty(c, block) {
				goto heightDone
			}
			if stateName(block) == "nether_portal" {
				portalCount++
			}
		}
	}
heightDone:
	if height < 3 || height > 21 {
		return false
	}
	for i := range width {
		if !c.tag(c.at(mechanism03PortalMove(mechanism03PortalMove(bottom, cube.FaceUp, height), right, i)), "minecraft:nether_portal_frame") {
			return false
		}
	}
	return portalCount == width*height
}

func mechanism03PortalEmpty(c *shapeContext, state uint32) bool {
	return stateName(state) == "air" || c.tag(state, "minecraft:fire") || stateName(state) == "nether_portal"
}

func mechanism03PortalEdge(c *shapeContext, pos cube.Pos, face cube.Face) int {
	for width := range 22 {
		at := mechanism03PortalMove(pos, face, width)
		block := c.at(at)
		if !mechanism03PortalEmpty(c, block) {
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

func mechanism03PortalMove(pos cube.Pos, face cube.Face, distance int) cube.Pos {
	for range distance {
		pos = pos.Side(face)
	}
	return pos
}

func mechanism03NoteShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceDown || face == cube.FaceUp {
		instrument := mechanism03BlockInstrument(c.at(c.pos.Side(cube.FaceUp)))
		if mechanism03WorksAboveNoteBlock(instrument) {
			return stateWith(state, "instrument", instrument)
		}
		instrument = mechanism03BlockInstrument(c.at(c.pos.Side(cube.FaceDown)))
		if mechanism03WorksAboveNoteBlock(instrument) {
			instrument = "harp"
		}
		return stateWith(state, "instrument", instrument)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func mechanism03BlockInstrument(state uint32) string {
	instrument, err := gamedata.BlockInstrumentID(state)
	if err != nil {
		panic(err)
	}
	return instrument
}

func mechanism03WorksAboveNoteBlock(instrument string) bool {
	switch instrument {
	case "skeleton", "wither_skeleton", "zombie", "creeper", "dragon", "piglin", "custom_head":
		return true
	default:
		return false
	}
}

func mechanism03ObserverShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	facing, _ := gamedata.BlockStateString(state, "facing")
	powered, _ := gamedata.BlockStateBool(state, "powered")
	if !powered && mechanism03FaceName(face) == facing {
		c.blockTick(c.pos, state, 2)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func mechanism03FaceName(face cube.Face) string {
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
