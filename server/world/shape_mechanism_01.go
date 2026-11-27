package world

import "github.com/dm-vev/adamant/server/block/cube"

func init() {
	registerShape("BasePressurePlateBlock", mechanism01PressurePlateShape)
	registerShape("BaseRailBlock", mechanism01RailShape)
	registerShape("BeehiveBlock", mechanism01BeehiveShape)
	registerShape("BrushableBlock", mechanism01BrushableShape)
	registerShape("CakeBlock", mechanism01CakeShape)

	registerSurvival("BasePressurePlateBlock", mechanism01PressurePlateSurvives)
	registerSurvival("BaseRailBlock", mechanism01RailSurvives)
}

func mechanism01PressurePlateShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceDown && !mechanism01PressurePlateSurvives(c, state, c.pos) {
		return stateDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func mechanism01PressurePlateSurvives(c *shapeContext, _ uint32, pos cube.Pos) bool {
	belowPos := pos.Side(cube.FaceDown)
	below := c.at(belowPos)
	return c.sturdy(below, cube.FaceUp, supportRigid) || c.canSupportCenter(belowPos, cube.FaceUp)
}

func mechanism01RailShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if stateBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func mechanism01RailSurvives(c *shapeContext, _ uint32, pos cube.Pos) bool {
	return c.sturdy(c.at(pos.Side(cube.FaceDown)), cube.FaceUp, supportRigid)
}

func mechanism01BeehiveShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if mechanism01IsFire(neighbor) {
		if c.generation != nil {
			// ProtoChunk block entities have no Level. Source releaseOccupant
			// dereferences it only when the hive actually contains a bee.
			if c.generation.HiveHasOccupants(c.pos) {
				panic("Java generation cannot release occupants from a hive without a Level")
			}
			return state
		}
		if hive, ok := c.tx.Block(c.pos).(interface {
			EmergencyRelease(cube.Pos, *Tx)
		}); ok {
			hive.EmergencyRelease(c.pos, c.tx)
		}
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func mechanism01IsFire(state uint32) bool {
	return stateName(state) == "fire"
}

func mechanism01BrushableShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	c.blockTick(c.pos, state, 2)
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func mechanism01CakeShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceDown && !c.canSurvive(state, c.pos) {
		return stateDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}
