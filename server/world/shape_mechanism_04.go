package world

import (
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/internal/gamedata"
)

func init() {
	registerShape("PathBlock", mechanism04PathShape)
	registerShape("PotentSulfurBlock", mechanism04PotentSulfurShape)
	registerShape("RedstoneWallTorchBlock", mechanism04RedstoneWallTorchShape)
	registerShape("RedstoneWireBlock", mechanism04RedstoneWireShape)
	registerShape("RepeaterBlock", mechanism04RepeaterShape)
	registerSurvival("PathBlock", mechanism04PathSurvival)
	registerSurvival("RedstoneWireBlock", mechanism04WireSurvival)
	registerSurvival("RedstoneWallTorchBlock", mechanism04WallTorchSurvival)
}

func mechanism04PathSurvival(c *shapeContext, _ uint32, pos cube.Pos) bool {
	above := c.at(pos.Side(cube.FaceUp))
	return !c.solid(above) || c.tag(above, "minecraft:fence_gates")
}

func mechanism04PathShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceUp && !c.canSurvive(state, c.pos) {
		c.blockTick(c.pos, state, 1)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func mechanism04PotentSulfurShape(c *shapeContext, state uint32, _ cube.Face, _ uint32) uint32 {
	above := c.at(c.pos.Side(cube.FaceUp))
	fluid := c.fluid(above)
	if fluid.kind != fluidWater || !fluid.source || fluid.amount < 8 {
		return stateWith(state, "potent_sulfur_state", "dry")
	}
	below := c.at(c.pos.Side(cube.FaceDown))
	if c.tag(below, "minecraft:causes_continuous_geyser_eruptions") && mechanism04SourceFluid(below) {
		return stateWith(state, "potent_sulfur_state", "continuous")
	}
	if c.tag(below, "minecraft:causes_periodic_geyser_eruptions") && mechanism04SourceFluid(below) {
		if stateString(state, "potent_sulfur_state") == "erupting" {
			return state
		}
		return stateWith(state, "potent_sulfur_state", "dormant")
	}
	return stateWith(state, "potent_sulfur_state", "wet")
}

func mechanism04SourceFluid(state uint32) bool {
	fluid := fluidForState(state)
	return fluid.kind == fluidEmpty || fluid.source
}

func mechanism04RedstoneWallTorchShape(c *shapeContext, state uint32, face cube.Face, _ uint32) uint32 {
	facing := stateString(state, "facing")
	if face == mechanism04OppositeFace(facing) && !c.canSurvive(state, c.pos) {
		return stateDefault("air")
	}
	return state
}

func mechanism04WallTorchSurvival(c *shapeContext, state uint32, pos cube.Pos) bool {
	facing := mechanism04HorizontalFace(stateString(state, "facing"))
	return c.sturdy(c.at(pos.Side(mechanism04Opposite(facing))), facing, supportFull)
}

func mechanism04WireSurvival(c *shapeContext, _ uint32, pos cube.Pos) bool {
	below := c.at(pos.Side(cube.FaceDown))
	return c.sturdy(below, cube.FaceUp, supportFull) || stateName(below) == "hopper"
}

func mechanism04RedstoneWireShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceDown {
		if !mechanism04WireSurvivesOn(c, neighbor) {
			return stateDefault("air")
		}
		return state
	}
	if face == cube.FaceUp {
		return mechanism04WireConnections(c, state)
	}
	prop := mechanism04SideProperty(face)
	side := mechanism04ConnectingSide(c, state, face)
	if (side != "none") == (stateString(state, prop) != "none") && !mechanism04IsCross(state) {
		return stateWith(state, prop, side)
	}
	cross := stateDefault("redstone_wire")
	for _, p := range [...]string{"north", "east", "south", "west"} {
		cross = stateWith(cross, p, "side")
	}
	cross = stateWith(cross, "power", stateInt(state, "power"))
	cross = stateWith(cross, prop, side)
	return mechanism04WireConnections(c, cross)
}

func mechanism04WireSurvivesOn(c *shapeContext, state uint32) bool {
	return c.sturdy(state, cube.FaceUp, supportFull) || stateName(state) == "hopper"
}

func mechanism04WireConnections(c *shapeContext, state uint32) uint32 {
	wasDot := mechanism04IsDot(state)
	state = stateWith(stateDefault("redstone_wire"), "power", stateInt(state, "power"))
	up := !mechanism04IsRedstoneConductor(c.at(c.pos.Side(cube.FaceUp)))
	for _, f := range [...]cube.Face{cube.FaceNorth, cube.FaceEast, cube.FaceSouth, cube.FaceWest} {
		p := mechanism04SideProperty(f)
		if stateString(state, p) == "none" {
			state = stateWith(state, p, mechanism04ConnectingSideUp(c, state, f, up))
		}
	}
	if wasDot && mechanism04IsDot(state) {
		return state
	}
	north := stateString(state, "north") != "none"
	south := stateString(state, "south") != "none"
	east := stateString(state, "east") != "none"
	west := stateString(state, "west") != "none"
	if !north && !south {
		if !west {
			state = stateWith(state, "west", "side")
		}
		if !east {
			state = stateWith(state, "east", "side")
		}
	}
	if !east && !west {
		if !north {
			state = stateWith(state, "north", "side")
		}
		if !south {
			state = stateWith(state, "south", "side")
		}
	}
	return state
}

func mechanism04ConnectingSide(c *shapeContext, state uint32, face cube.Face) string {
	up := !mechanism04IsRedstoneConductor(c.at(c.pos.Side(cube.FaceUp)))
	return mechanism04ConnectingSideUp(c, state, face, up)
}

func mechanism04ConnectingSideUp(c *shapeContext, _ uint32, face cube.Face, canUp bool) string {
	dir := mechanism04Offset(face)
	relPos := c.pos.Add(dir)
	rel := c.at(relPos)
	if canUp {
		placeable := c.tag(rel, "minecraft:trapdoors") || mechanism04WireSurvivesOn(c, rel)
		if placeable && mechanism04ShouldConnect(c, c.at(relPos.Side(cube.FaceUp)), face, false) {
			if c.sturdy(rel, mechanism04Opposite(face), supportFull) {
				return "up"
			}
			return "side"
		}
	}
	if !mechanism04ShouldConnect(c, rel, face, true) && (mechanism04IsRedstoneConductor(rel) || !mechanism04ShouldConnect(c, c.at(relPos.Side(cube.FaceDown)), face, false)) {
		return "none"
	}
	return "side"
}

func mechanism04ShouldConnect(c *shapeContext, state uint32, direction cube.Face, hasDirection bool) bool {
	switch stateName(state) {
	case "redstone_wire":
		return true
	case "repeater":
		if !hasDirection {
			return false
		}
		facing := mechanism04HorizontalFace(stateString(state, "facing"))
		return facing == direction || mechanism04Opposite(facing) == direction
	case "observer":
		return hasDirection && stateString(state, "facing") == mechanism04SideProperty(direction)
	default:
		if !hasDirection {
			return false
		}
		signalSource, err := gamedata.BlockSignalSourceID(state)
		if err != nil {
			panic(err)
		}
		return signalSource
	}
}
func mechanism04IsRedstoneConductor(state uint32) bool {
	conductor, err := gamedata.BlockRedstoneConductorID(state)
	if err != nil {
		panic(err)
	}
	return conductor
}

func mechanism04IsCross(state uint32) bool {
	return stateString(state, "north") != "none" && stateString(state, "south") != "none" && stateString(state, "east") != "none" && stateString(state, "west") != "none"
}

func mechanism04IsDot(state uint32) bool {
	return stateString(state, "north") == "none" && stateString(state, "south") == "none" && stateString(state, "east") == "none" && stateString(state, "west") == "none"
}

func mechanism04RepeaterShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceDown && !c.sturdy(neighbor, cube.FaceUp, supportRigid) {
		return stateDefault("air")
	}
	facing := mechanism04HorizontalFace(stateString(state, "facing"))
	if mechanism04Axis(face) != mechanism04Axis(facing) {
		state = stateWith(state, "locked", mechanism04RepeaterLocked(c, facing))
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func mechanism04RepeaterLocked(c *shapeContext, facing cube.Face) bool {
	clockwise := mechanism04Clockwise(facing)
	for _, side := range [...]cube.Face{clockwise, mechanism04Opposite(clockwise)} {
		pos := c.pos.Side(side)
		name := stateName(c.at(pos))
		if name != "repeater" && name != "comparator" {
			continue
		}
		if c.generation != nil {
			if c.generation.Signal(pos, mechanism04Opposite(side)) > 0 {
				return true
			}
			continue
		}
		source, ok := c.tx.Block(pos).(RedstonePowerSource)
		if ok && source.RedstonePower(pos, c.tx, mechanism04Opposite(side)) > 0 {
			return true
		}
	}
	return false
}

func mechanism04Axis(face cube.Face) int {
	switch face {
	case cube.FaceNorth, cube.FaceSouth:
		return 2
	case cube.FaceWest, cube.FaceEast:
		return 0
	case cube.FaceDown, cube.FaceUp:
		return 1
	default:
		panic("invalid direction axis")
	}
}

func mechanism04Clockwise(face cube.Face) cube.Face {
	switch face {
	case cube.FaceNorth:
		return cube.FaceEast
	case cube.FaceEast:
		return cube.FaceSouth
	case cube.FaceSouth:
		return cube.FaceWest
	case cube.FaceWest:
		return cube.FaceNorth
	default:
		panic("not horizontal")
	}
}

func mechanism04SideProperty(face cube.Face) string {
	switch face {
	case cube.FaceNorth:
		return "north"
	case cube.FaceEast:
		return "east"
	case cube.FaceSouth:
		return "south"
	case cube.FaceWest:
		return "west"
	default:
		panic("not a horizontal redstone side")
	}
}

func mechanism04OppositeFace(direction string) cube.Face {
	switch direction {
	case "north":
		return cube.FaceSouth
	case "south":
		return cube.FaceNorth
	case "east":
		return cube.FaceWest
	case "west":
		return cube.FaceEast
	default:
		panic("invalid horizontal facing")
	}
}

func mechanism04HorizontalFace(direction string) cube.Face {
	switch direction {
	case "north":
		return cube.FaceNorth
	case "south":
		return cube.FaceSouth
	case "east":
		return cube.FaceEast
	case "west":
		return cube.FaceWest
	default:
		panic("invalid horizontal facing")
	}
}

func mechanism04Opposite(face cube.Face) cube.Face {
	switch face {
	case cube.FaceNorth:
		return cube.FaceSouth
	case cube.FaceSouth:
		return cube.FaceNorth
	case cube.FaceEast:
		return cube.FaceWest
	case cube.FaceWest:
		return cube.FaceEast
	default:
		return face
	}
}

func mechanism04Offset(face cube.Face) cube.Pos {
	switch face {
	case cube.FaceNorth:
		return cube.Pos{0, 0, -1}
	case cube.FaceSouth:
		return cube.Pos{0, 0, 1}
	case cube.FaceWest:
		return cube.Pos{-1, 0, 0}
	case cube.FaceEast:
		return cube.Pos{1, 0, 0}
	default:
		panic("not horizontal")
	}
}
