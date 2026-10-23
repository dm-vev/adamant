package world

import "github.com/df-mc/dragonfly/server/block/cube"

func init() {
	registerJavaShape("BasePressurePlateBlock", javaMechanism01PressurePlateShape)
	registerJavaShape("BaseRailBlock", javaMechanism01RailShape)
	registerJavaShape("BeehiveBlock", javaMechanism01BeehiveShape)
	registerJavaShape("BrushableBlock", javaMechanism01BrushableShape)
	registerJavaShape("CakeBlock", javaMechanism01CakeShape)

	registerJavaSurvival("BasePressurePlateBlock", javaMechanism01PressurePlateSurvives)
	registerJavaSurvival("BaseRailBlock", javaMechanism01RailSurvives)
}

func javaMechanism01PressurePlateShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceDown && !javaMechanism01PressurePlateSurvives(c, state, c.pos) {
		return javaDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaMechanism01PressurePlateSurvives(c *javaShapeContext, _ uint32, pos cube.Pos) bool {
	belowPos := pos.Side(cube.FaceDown)
	below := c.at(belowPos)
	return c.sturdy(below, cube.FaceUp, javaSupportRigid) || c.canSupportCenter(belowPos, cube.FaceUp)
}

func javaMechanism01RailShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if javaBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaMechanism01RailSurvives(c *javaShapeContext, _ uint32, pos cube.Pos) bool {
	return c.sturdy(c.at(pos.Side(cube.FaceDown)), cube.FaceUp, javaSupportRigid)
}

func javaMechanism01BeehiveShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if javaMechanism01IsFire(neighbor) {
		if c.generation != nil {
			// ProtoChunk block entities have no Level. Source releaseOccupant
			// dereferences it only when the hive actually contains a bee.
			if c.generation.HiveHasOccupants(c.pos) {
				panic("Java generation cannot release occupants from a hive without a Level")
			}
			return state
		}
		if hive, ok := c.tx.Block(c.pos).(interface {
			JavaEmergencyRelease(cube.Pos, *Tx)
		}); ok {
			hive.JavaEmergencyRelease(c.pos, c.tx)
		}
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaMechanism01IsFire(state uint32) bool {
	return javaName(state) == "fire"
}

func javaMechanism01BrushableShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	c.blockTick(c.pos, state, 2)
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaMechanism01CakeShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceDown && !c.canSurvive(state, c.pos) {
		return javaDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}
