package world

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/javadata"
)

func init() {
	registerJavaShape("PathBlock", javaMechanism04PathShape)
	registerJavaShape("PotentSulfurBlock", javaMechanism04PotentSulfurShape)
	registerJavaShape("RedstoneWallTorchBlock", javaMechanism04RedstoneWallTorchShape)
	registerJavaShape("RedstoneWireBlock", javaMechanism04RedstoneWireShape)
	registerJavaShape("RepeaterBlock", javaMechanism04RepeaterShape)
	registerJavaSurvival("PathBlock", javaMechanism04PathSurvival)
	registerJavaSurvival("RedstoneWireBlock", javaMechanism04WireSurvival)
	registerJavaSurvival("RedstoneWallTorchBlock", javaMechanism04WallTorchSurvival)
}

func javaMechanism04PathSurvival(c *javaShapeContext, _ uint32, pos cube.Pos) bool {
	above := c.at(pos.Side(cube.FaceUp))
	return !c.solid(above) || c.tag(above, "minecraft:fence_gates")
}

func javaMechanism04PathShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceUp && !c.canSurvive(state, c.pos) {
		c.blockTick(c.pos, state, 1)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaMechanism04PotentSulfurShape(c *javaShapeContext, state uint32, _ cube.Face, _ uint32) uint32 {
	above := c.at(c.pos.Side(cube.FaceUp))
	fluid := c.fluid(above)
	if fluid.kind != javaFluidWater || !fluid.source || fluid.amount < 8 {
		return javaWith(state, "potent_sulfur_state", "dry")
	}
	below := c.at(c.pos.Side(cube.FaceDown))
	if c.tag(below, "minecraft:causes_continuous_geyser_eruptions") && javaMechanism04SourceFluid(below) {
		return javaWith(state, "potent_sulfur_state", "continuous")
	}
	if c.tag(below, "minecraft:causes_periodic_geyser_eruptions") && javaMechanism04SourceFluid(below) {
		if javaString(state, "potent_sulfur_state") == "erupting" {
			return state
		}
		return javaWith(state, "potent_sulfur_state", "dormant")
	}
	return javaWith(state, "potent_sulfur_state", "wet")
}

func javaMechanism04SourceFluid(state uint32) bool {
	fluid := javaFluidForState(state)
	return fluid.kind == javaFluidEmpty || fluid.source
}

func javaMechanism04RedstoneWallTorchShape(c *javaShapeContext, state uint32, face cube.Face, _ uint32) uint32 {
	facing := javaString(state, "facing")
	if face == javaMechanism04OppositeFace(facing) && !c.canSurvive(state, c.pos) {
		return javaDefault("air")
	}
	return state
}

func javaMechanism04WallTorchSurvival(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	facing := javaMechanism04HorizontalFace(javaString(state, "facing"))
	return c.sturdy(c.at(pos.Side(javaMechanism04Opposite(facing))), facing, javaSupportFull)
}

func javaMechanism04WireSurvival(c *javaShapeContext, _ uint32, pos cube.Pos) bool {
	below := c.at(pos.Side(cube.FaceDown))
	return c.sturdy(below, cube.FaceUp, javaSupportFull) || javaName(below) == "hopper"
}

func javaMechanism04RedstoneWireShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceDown {
		if !javaMechanism04WireSurvivesOn(c, neighbor) {
			return javaDefault("air")
		}
		return state
	}
	if face == cube.FaceUp {
		return javaMechanism04WireConnections(c, state)
	}
	prop := javaMechanism04SideProperty(face)
	side := javaMechanism04ConnectingSide(c, state, face)
	if (side != "none") == (javaString(state, prop) != "none") && !javaMechanism04IsCross(state) {
		return javaWith(state, prop, side)
	}
	cross := javaDefault("redstone_wire")
	for _, p := range [...]string{"north", "east", "south", "west"} {
		cross = javaWith(cross, p, "side")
	}
	cross = javaWith(cross, "power", javaInt(state, "power"))
	cross = javaWith(cross, prop, side)
	return javaMechanism04WireConnections(c, cross)
}

func javaMechanism04WireSurvivesOn(c *javaShapeContext, state uint32) bool {
	return c.sturdy(state, cube.FaceUp, javaSupportFull) || javaName(state) == "hopper"
}

func javaMechanism04WireConnections(c *javaShapeContext, state uint32) uint32 {
	wasDot := javaMechanism04IsDot(state)
	state = javaWith(javaDefault("redstone_wire"), "power", javaInt(state, "power"))
	up := !javaMechanism04IsRedstoneConductor(c.at(c.pos.Side(cube.FaceUp)))
	for _, f := range [...]cube.Face{cube.FaceNorth, cube.FaceEast, cube.FaceSouth, cube.FaceWest} {
		p := javaMechanism04SideProperty(f)
		if javaString(state, p) == "none" {
			state = javaWith(state, p, javaMechanism04ConnectingSideUp(c, state, f, up))
		}
	}
	if wasDot && javaMechanism04IsDot(state) {
		return state
	}
	north := javaString(state, "north") != "none"
	south := javaString(state, "south") != "none"
	east := javaString(state, "east") != "none"
	west := javaString(state, "west") != "none"
	if !north && !south {
		if !west {
			state = javaWith(state, "west", "side")
		}
		if !east {
			state = javaWith(state, "east", "side")
		}
	}
	if !east && !west {
		if !north {
			state = javaWith(state, "north", "side")
		}
		if !south {
			state = javaWith(state, "south", "side")
		}
	}
	return state
}

func javaMechanism04ConnectingSide(c *javaShapeContext, state uint32, face cube.Face) string {
	up := !javaMechanism04IsRedstoneConductor(c.at(c.pos.Side(cube.FaceUp)))
	return javaMechanism04ConnectingSideUp(c, state, face, up)
}

func javaMechanism04ConnectingSideUp(c *javaShapeContext, _ uint32, face cube.Face, canUp bool) string {
	dir := javaMechanism04Offset(face)
	relPos := c.pos.Add(dir)
	rel := c.at(relPos)
	if canUp {
		placeable := c.tag(rel, "minecraft:trapdoors") || javaMechanism04WireSurvivesOn(c, rel)
		if placeable && javaMechanism04ShouldConnect(c, c.at(relPos.Side(cube.FaceUp)), face, false) {
			if c.sturdy(rel, javaMechanism04Opposite(face), javaSupportFull) {
				return "up"
			}
			return "side"
		}
	}
	if !javaMechanism04ShouldConnect(c, rel, face, true) && (javaMechanism04IsRedstoneConductor(rel) || !javaMechanism04ShouldConnect(c, c.at(relPos.Side(cube.FaceDown)), face, false)) {
		return "none"
	}
	return "side"
}

func javaMechanism04ShouldConnect(c *javaShapeContext, state uint32, direction cube.Face, hasDirection bool) bool {
	switch javaName(state) {
	case "redstone_wire":
		return true
	case "repeater":
		if !hasDirection {
			return false
		}
		facing := javaMechanism04HorizontalFace(javaString(state, "facing"))
		return facing == direction || javaMechanism04Opposite(facing) == direction
	case "observer":
		return hasDirection && javaString(state, "facing") == javaMechanism04SideProperty(direction)
	default:
		if !hasDirection {
			return false
		}
		signalSource, err := javadata.BlockSignalSourceID(state)
		if err != nil {
			panic(err)
		}
		return signalSource
	}
}
func javaMechanism04IsRedstoneConductor(state uint32) bool {
	conductor, err := javadata.BlockRedstoneConductorID(state)
	if err != nil {
		panic(err)
	}
	return conductor
}

func javaMechanism04IsCross(state uint32) bool {
	return javaString(state, "north") != "none" && javaString(state, "south") != "none" && javaString(state, "east") != "none" && javaString(state, "west") != "none"
}

func javaMechanism04IsDot(state uint32) bool {
	return javaString(state, "north") == "none" && javaString(state, "south") == "none" && javaString(state, "east") == "none" && javaString(state, "west") == "none"
}

func javaMechanism04RepeaterShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceDown && !c.sturdy(neighbor, cube.FaceUp, javaSupportRigid) {
		return javaDefault("air")
	}
	facing := javaMechanism04HorizontalFace(javaString(state, "facing"))
	if javaMechanism04Axis(face) != javaMechanism04Axis(facing) {
		state = javaWith(state, "locked", javaMechanism04RepeaterLocked(c, facing))
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaMechanism04RepeaterLocked(c *javaShapeContext, facing cube.Face) bool {
	clockwise := javaMechanism04Clockwise(facing)
	for _, side := range [...]cube.Face{clockwise, javaMechanism04Opposite(clockwise)} {
		pos := c.pos.Side(side)
		name := javaName(c.at(pos))
		if name != "repeater" && name != "comparator" {
			continue
		}
		source, ok := c.tx.Block(pos).(RedstonePowerSource)
		if ok && source.RedstonePower(pos, c.tx, javaMechanism04Opposite(side)) > 0 {
			return true
		}
	}
	return false
}

func javaMechanism04Axis(face cube.Face) int {
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

func javaMechanism04Clockwise(face cube.Face) cube.Face {
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

func javaMechanism04SideProperty(face cube.Face) string {
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

func javaMechanism04OppositeFace(direction string) cube.Face {
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

func javaMechanism04HorizontalFace(direction string) cube.Face {
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

func javaMechanism04Opposite(face cube.Face) cube.Face {
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

func javaMechanism04Offset(face cube.Face) cube.Pos {
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
