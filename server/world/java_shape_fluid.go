package world

import "github.com/df-mc/dragonfly/server/block/cube"

func javaFluidShape(c *javaShapeContext, owner string, state uint32, face cube.Face, neighbor uint32) (uint32, bool) {
	pos := c.pos
	switch owner {
	case "LiquidBlock":
		fluid := javaFluidForState(state)
		neighborFluid := javaFluidForState(neighbor)
		if fluid.source || neighborFluid.source {
			c.fluidTick(pos, fluid, c.fluidDelay(fluid))
		}
		if face == cube.FaceDown && fluid.kind == javaFluidWater && fluid.source && fluid.amount >= 8 {
			below := c.at(pos.Side(cube.FaceDown))
			if c.tag(below, "minecraft:enables_bubble_column_drag_down") || c.tag(below, "minecraft:enables_bubble_column_push_up") {
				c.blockTick(pos, state, 20)
			}
		}
		return state, true
	case "BubbleColumnBlock":
		c.fluidTick(pos, javaFluidState{kind: javaFluidWater, amount: 8, source: true}, 5)
		if !javaBubbleColumnSurvives(c, pos) || face == cube.FaceDown ||
			(face == cube.FaceUp && javaName(neighbor) != "bubble_column" && javaBubbleColumnCanOccupy(c, neighbor)) {
			c.blockTick(pos, state, 5)
		}
		return state, true
	default:
		return 0, false
	}
}

func javaFluidSurvival(c *javaShapeContext, owner string, state uint32, pos cube.Pos) (bool, bool) {
	switch owner {
	case "LiquidBlock":
		return true, true
	case "BubbleColumnBlock":
		return javaBubbleColumnSurvives(c, pos), true
	default:
		return false, false
	}
}

func javaBubbleColumnSurvives(c *javaShapeContext, pos cube.Pos) bool {
	below, _ := javaFluidStateAt(c, pos.Side(cube.FaceDown))
	return below.is("bubble_column") ||
		below.tag(c, "minecraft:enables_bubble_column_push_up") ||
		below.tag(c, "minecraft:enables_bubble_column_drag_down")
}

// javaTickBubbleColumn mirrors BubbleColumnBlock.updateColumn: it chooses the
// resulting column state from below, then propagates it upward through every
// source-water block or existing bubble column in the same vertical run.
func javaTickBubbleColumn(c *javaShapeContext, capturedBlock uint32) {
	pos := c.pos
	occupy := c.at(pos)
	if javaName(occupy) != "bubble_column" {
		return
	}
	below, _ := javaFluidStateAt(c, pos.Side(cube.FaceDown))
	column := javaDefault("water")
	if drag, ok := javaBubbleColumnState(c, below); ok {
		column = javaWith(javaDefault("bubble_column"), "drag", drag)
	}
	c.set(pos, column, 2)
	for above := pos.Side(cube.FaceUp); javaBubbleColumnCanOccupyAt(c, above); above = above.Side(cube.FaceUp) {
		c.set(above, column, 2)
	}
}
