package world

import "github.com/dm-vev/adamant/server/block/cube"

func fluidShape(c *shapeContext, owner string, state uint32, face cube.Face, neighbor uint32) (uint32, bool) {
	pos := c.pos
	switch owner {
	case "LiquidBlock":
		fluid := fluidForState(state)
		neighborFluid := fluidForState(neighbor)
		if fluid.source || neighborFluid.source {
			c.fluidTick(pos, fluid, c.fluidDelay(fluid))
		}
		if face == cube.FaceDown && fluid.kind == fluidWater && fluid.source && fluid.amount >= 8 {
			below := c.at(pos.Side(cube.FaceDown))
			if c.tag(below, "minecraft:enables_bubble_column_drag_down") || c.tag(below, "minecraft:enables_bubble_column_push_up") {
				c.blockTick(pos, state, 20)
			}
		}
		return state, true
	case "BubbleColumnBlock":
		c.fluidTick(pos, fluidState{kind: fluidWater, amount: 8, source: true}, 5)
		if !bubbleColumnSurvives(c, pos) || face == cube.FaceDown ||
			(face == cube.FaceUp && stateName(neighbor) != "bubble_column" && bubbleColumnCanOccupy(c, neighbor)) {
			c.blockTick(pos, state, 5)
		}
		return state, true
	default:
		return 0, false
	}
}

func fluidSurvival(c *shapeContext, owner string, state uint32, pos cube.Pos) (bool, bool) {
	switch owner {
	case "LiquidBlock":
		return true, true
	case "BubbleColumnBlock":
		return bubbleColumnSurvives(c, pos), true
	default:
		return false, false
	}
}

func bubbleColumnSurvives(c *shapeContext, pos cube.Pos) bool {
	below, _ := fluidStateAt(c, pos.Side(cube.FaceDown))
	return below.is("bubble_column") ||
		below.tag(c, "minecraft:enables_bubble_column_push_up") ||
		below.tag(c, "minecraft:enables_bubble_column_drag_down")
}

// javaTickBubbleColumn mirrors BubbleColumnBlock.updateColumn: it chooses the
// resulting column state from below, then propagates it upward through every
// source-water block or existing bubble column in the same vertical run.
func tickBubbleColumn(c *shapeContext, capturedBlock uint32) {
	pos := c.pos
	occupy := c.at(pos)
	if stateName(occupy) != "bubble_column" {
		return
	}
	below, _ := fluidStateAt(c, pos.Side(cube.FaceDown))
	column := stateDefault("water")
	if drag, ok := bubbleColumnState(c, below); ok {
		column = stateWith(stateDefault("bubble_column"), "drag", drag)
	}
	c.set(pos, column, 2)
	for above := pos.Side(cube.FaceUp); bubbleColumnCanOccupyAt(c, above); above = above.Side(cube.FaceUp) {
		c.set(above, column, 2)
	}
}
