package world

import (
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
)

type javaFluidKind uint8

const (
	javaFluidEmpty javaFluidKind = iota
	javaFluidWater
	javaFluidLava
)

type javaFluidState struct {
	kind            javaFluidKind
	amount          int32
	falling, source bool
}

// FluidLegacyBlock decodes a pinned FluidState into its canonical legacy
// block. StateDefinition.any supplies falling=true and flowing level=1 when
// the corresponding Source CODEC properties are absent.
func FluidLegacyBlock(name string, properties map[string]any) (uint32, error) {
	name = NormaliseBlockName(name)
	fluid := javaFluidState{amount: 8, falling: true, source: true}
	switch name {
	case "minecraft:empty":
		if len(properties) != 0 {
			return 0, fmt.Errorf("empty Java fluid has properties")
		}
		return javaLegacyFluidBlock(javaFluidState{}), nil
	case "minecraft:water", "minecraft:flowing_water":
		fluid.kind = javaFluidWater
	case "minecraft:lava", "minecraft:flowing_lava":
		fluid.kind = javaFluidLava
	default:
		return 0, fmt.Errorf("unknown Java fluid %q", name)
	}
	flowing := name == "minecraft:flowing_water" || name == "minecraft:flowing_lava"
	if flowing {
		fluid.amount, fluid.source = 1, false
	}
	for property, value := range properties {
		switch property {
		case "falling":
			falling, valid := boolProperty(value)
			if !valid {
				return 0, fmt.Errorf("invalid Java fluid falling property %v", value)
			}
			fluid.falling = falling
		case "level":
			level, valid := intProperty(value)
			if !flowing || !valid || level < 1 || level > 8 {
				return 0, fmt.Errorf("invalid Java fluid level %v for %s", value, name)
			}
			fluid.amount = int32(level)
		default:
			return 0, fmt.Errorf("unknown Java fluid property %q", property)
		}
	}
	return javaLegacyFluidBlock(fluid), nil
}

func javaFluidForState(id uint32) javaFluidState {
	name, ok := gamedata.BlockStateName(id)
	if !ok {
		panic("Java fluid state has no canonical block name")
	}
	if water, _ := gamedata.BlockStateBool(id, "waterlogged"); water {
		return javaFluidState{kind: javaFluidWater, amount: 8, falling: strings.Contains(name, "copper_grate"), source: true}
	}
	kind := javaFluidEmpty
	switch name {
	case "minecraft:water", "minecraft:bubble_column",
		"minecraft:seagrass", "minecraft:kelp", "minecraft:kelp_plant", "minecraft:tall_seagrass":
		kind = javaFluidWater
	case "minecraft:lava":
		kind = javaFluidLava
	default:
		return javaFluidState{}
	}
	depth, _ := gamedata.BlockStateInt(id, "level")
	if depth == 0 {
		return javaFluidState{kind: kind, amount: 8, source: true}
	}
	if depth >= 8 {
		return javaFluidState{kind: kind, amount: 8, falling: true}
	}
	return javaFluidState{kind: kind, amount: 8 - depth}
}

// BlockStateHasWaterFluid reports the source state's WATER-tag fluid,
// including waterlogged solids, bubble columns, and aquatic plants.
func BlockStateHasWaterFluid(id uint32) bool {
	return javaFluidForState(id).kind == javaFluidWater
}

func javaFluidTickDelay(tx *Tx, fluid javaFluidState) int32 {
	if fluid.kind == javaFluidLava {
		if tx.World().Dimension() == Nether {
			return 10
		}
		return 30
	}
	if fluid.kind == javaFluidWater {
		return 5
	}
	return 0
}

func javaLegacyFluidBlock(fluid javaFluidState) uint32 {
	if fluid.kind == javaFluidEmpty || fluid.amount == 0 {
		return javaDefault("air")
	}
	name := "water"
	if fluid.kind == javaFluidLava {
		name = "lava"
	}
	depth := int32(8 - fluid.amount)
	if fluid.source {
		depth = 0
	} else if fluid.falling {
		depth += 8
	}
	return javaWith(javaDefault(name), "level", depth)
}

func javaFluidIsSourceBlock(fluid, candidate javaFluidState) bool {
	return candidate.source && candidate.kind == fluid.kind
}

func javaFluidReplaceable(c *javaShapeContext, pos cube.Pos, existing, incoming javaFluidState, down bool) bool {
	if existing.kind == javaFluidEmpty {
		return true
	}
	if existing.kind == javaFluidWater {
		return down && incoming.kind == javaFluidLava
	}
	if existing.kind == javaFluidLava {
		_, above := javaFluidStateAt(c, pos.Side(cube.FaceUp))
		return (existing.amount >= 4 || above.kind == javaFluidLava) && incoming.kind == javaFluidWater
	}
	return false
}

func javaFluidCanHoldAny(c *javaShapeContext, block javaFluidBlock) bool {
	if block.native != nil {
		_, displacer := block.native.(LiquidDisplacer)
		_, replaceable := block.native.(replaceableBlock)
		return displacer || replaceable
	}
	id := block.state
	name := javaName(id)
	if _, ok := gamedata.BlockStateBool(id, "waterlogged"); ok {
		return true
	}
	if name == "kelp" || name == "kelp_plant" || name == "tall_seagrass" {
		return true
	}
	return c.tag(id, "minecraft:washed_away_by_fluids")
}

func javaFluidCanHold(c *javaShapeContext, block javaFluidBlock, fluid javaFluidState) bool {
	if block.native != nil {
		liquid := c.nativeFluid(fluid)
		if displacer, ok := block.native.(LiquidDisplacer); ok && displacer.CanDisplace(liquid) {
			return true
		}
		replaceable, ok := block.native.(replaceableBlock)
		return ok && replaceable.ReplaceableBy(liquid)
	}
	id := block.state
	if !javaFluidCanHoldAny(c, block) {
		return false
	}
	name := javaName(id)
	if _, ok := gamedata.BlockStateBool(id, "waterlogged"); ok {
		return !javaBool(id, "waterlogged") && fluid.kind == javaFluidWater
	}
	if name == "kelp" || name == "kelp_plant" || name == "tall_seagrass" {
		return false
	}
	return true
}

func javaFluidWallPass(c *javaShapeContext, from, to javaFluidBlock, pos cube.Pos, direction int) bool {
	if from.native != nil || to.native != nil {
		return javaFluidMixedWallPass(c, from, to, pos, direction)
	}
	fromShape, err := gamedata.BlockGenerationShapeID(from.state)
	if err != nil {
		panic(err)
	}
	toShape, err := gamedata.BlockGenerationShapeID(to.state)
	if err != nil {
		panic(err)
	}
	if fromShape.CollisionFull || toShape.CollisionFull {
		return false
	}
	occludes, err := gamedata.GenerationCollisionFacesOccludeIDs(from.state, to.state, direction)
	if err != nil {
		panic(err)
	}
	return !occludes
}

// Directions in Java Direction.Plane.HORIZONTAL order: north, east, south, west.
var javaFluidHorizontal = [...]struct {
	face      cube.Face
	direction int
}{
	{cube.FaceNorth, 2}, {cube.FaceEast, 5}, {cube.FaceSouth, 3}, {cube.FaceWest, 4},
}

func javaFluidNear(c *javaShapeContext, pos cube.Pos, id javaFluidBlock, kind javaFluidKind) (int, int32) {
	highest, sources := 0, int32(0)
	for _, d := range javaFluidHorizontal {
		nextID, fluid := javaFluidStateAt(c, pos.Side(d.face))
		if fluid.kind == kind && javaFluidWallPass(c, id, nextID, pos, d.direction) {
			if fluid.source {
				sources++
			}
			if int(fluid.amount) > highest {
				highest = int(fluid.amount)
			}
		}
	}
	return highest, sources
}

func javaFluidNewLiquid(c *javaShapeContext, id javaFluidBlock, pos cube.Pos, fluid javaFluidState) javaFluidState {
	highest, sources := javaFluidNear(c, pos, id, fluid.kind)
	conversionRule := "lava_source_conversion"
	if fluid.kind == javaFluidWater {
		conversionRule = "water_source_conversion"
	}
	if sources >= 2 && c.rule(conversionRule) {
		below := pos.Side(cube.FaceDown)
		belowID, belowFluid := javaFluidStateAt(c, below)
		if belowID.solid(c, below) || javaFluidIsSourceBlock(fluid, belowFluid) {
			return javaFluidState{kind: fluid.kind, amount: 8, source: true}
		}
	}
	aboveID, above := javaFluidStateAt(c, pos.Side(cube.FaceUp))
	if above.kind == fluid.kind && javaFluidWallPass(c, id, aboveID, pos, 1) {
		return javaFluidState{kind: fluid.kind, amount: 8, falling: true}
	}
	drop := int32(1)
	if fluid.kind == javaFluidLava {
		drop = 2
		if c.tx.World().Dimension() == Nether {
			drop = 1
		}
	}
	amount := int32(highest) - drop
	if amount <= 0 {
		return javaFluidState{}
	}
	return javaFluidState{kind: fluid.kind, amount: amount}
}

func javaFluidSourceCount(c *javaShapeContext, pos cube.Pos, kind javaFluidKind) int {
	count := 0
	for _, d := range javaFluidHorizontal {
		_, f := javaFluidStateAt(c, pos.Side(d.face))
		if f.kind == kind && f.source {
			count++
		}
	}
	return count
}

func javaFluidDownwardAllowed(c *javaShapeContext, sourceID, targetID javaFluidBlock, source, target javaFluidState) bool {
	if javaFluidIsSourceBlock(source, target) || !javaFluidCanHoldAny(c, targetID) {
		return false
	}
	return javaFluidWallPass(c, sourceID, targetID, c.pos, 0)
}

func javaFluidSlope(c *javaShapeContext, origin cube.Pos, pos cube.Pos, state javaFluidBlock, kind javaFluidKind, from cube.Face, pass, limit int) int {
	lowest := 1000
	for _, d := range javaFluidHorizontal {
		if pos.Side(d.face) == pos.Side(from) {
			continue
		}
		next := pos.Side(d.face)
		nextID, nextFluid := javaFluidStateAt(c, next)
		flowing := javaFluidState{kind: kind, amount: 1}
		if javaFluidIsSourceBlock(javaFluidState{kind: kind}, nextFluid) || !javaFluidCanHoldAny(c, nextID) ||
			!javaFluidCanHold(c, nextID, flowing) || !javaFluidWallPass(c, state, nextID, pos, d.direction) {
			continue
		}
		if javaFluidHole(c, next, nextID, kind) {
			return pass
		}
		if pass < limit {
			distance := javaFluidSlope(c, origin, next, nextID, kind, d.face.Opposite(), pass+1, limit)
			if distance < lowest {
				lowest = distance
			}
		}
	}
	return lowest
}

func javaFluidHole(c *javaShapeContext, pos cube.Pos, id javaFluidBlock, kind javaFluidKind) bool {
	below := pos.Side(cube.FaceDown)
	belowID, belowFluid := javaFluidStateAt(c, below)
	if !javaFluidWallPass(c, id, belowID, pos, 0) {
		return false
	}
	if belowFluid.kind == kind {
		return true
	}
	return javaFluidCanHold(c, belowID, javaFluidState{kind: kind, amount: 7})
}

type javaFluidSpreadTarget struct {
	pos       cube.Pos
	state     javaFluidState
	direction int
}

func javaFluidSpreadTargets(c *javaShapeContext, pos cube.Pos, id javaFluidBlock, fluid javaFluidState) ([4]javaFluidSpreadTarget, int) {
	limit := 4
	if fluid.kind == javaFluidLava && c.tx.World().Dimension() != Nether {
		limit = 2
	}
	var targets [4]javaFluidSpreadTarget
	count, lowest := 0, 1000
	for _, d := range javaFluidHorizontal {
		next := pos.Side(d.face)
		nextID, nextFluid := javaFluidStateAt(c, next)
		if javaFluidIsSourceBlock(fluid, nextFluid) || !javaFluidCanHoldAny(c, nextID) ||
			!javaFluidWallPass(c, id, nextID, pos, d.direction) {
			continue
		}
		newFluid := javaFluidNewLiquid(c, nextID, next, fluid)
		if newFluid.kind == javaFluidEmpty || !javaFluidCanHold(c, nextID, newFluid) ||
			!javaFluidReplaceable(c, next, nextFluid, newFluid, false) {
			continue
		}
		distance := 0
		if !javaFluidHole(c, next, nextID, fluid.kind) {
			distance = javaFluidSlope(c, pos, next, nextID, fluid.kind, d.face.Opposite(), 1, limit)
		}
		if distance < lowest {
			count = 0
			lowest = distance
		}
		if distance <= lowest {
			targets[count] = javaFluidSpreadTarget{next, newFluid, d.direction}
			count++
		}
	}
	// EnumMap iteration follows Direction enum order, not Plane.HORIZONTAL order.
	for i := 1; i < count; i++ {
		for j := i; j > 0 && targets[j].direction < targets[j-1].direction; j-- {
			targets[j], targets[j-1] = targets[j-1], targets[j]
		}
	}
	return targets, count
}
func javaFluidSpreadTo(c *javaShapeContext, source, pos cube.Pos, target javaFluidBlock, fluid javaFluidState) {
	if !c.flowEvent(source, pos, fluid) {
		return
	}
	if target.native != nil {
		liquid := c.nativeFluid(fluid)
		if displacer, ok := target.native.(LiquidDisplacer); ok && displacer.CanDisplace(liquid) {
			c.tx.SetLiquid(pos, liquid)
			c.fluidTick(pos, fluid, javaFluidTickDelay(c.tx, fluid))
			return
		}
	}
	targetID := target.state
	if _, waterlogged := gamedata.BlockStateBool(targetID, "waterlogged"); target.native == nil && waterlogged {
		if !javaBool(targetID, "waterlogged") && fluid.kind == javaFluidWater {
			c.set(pos, javaWith(targetID, "waterlogged", true), 3)
			c.fluidTick(pos, fluid, javaFluidTickDelay(c.tx, fluid))
		}
		return
	}
	c.beforeFluidDestroy(pos, target, fluid)
	c.set(pos, javaLegacyFluidBlock(fluid), 3)
}

func javaTickFluid(c *javaShapeContext, capturedBlock javaFluidBlock, fluid javaFluidState) {
	if fluid.kind == javaFluidEmpty {
		return
	}
	pos := c.pos
	if !fluid.source {
		current, _ := javaFluidStateAt(c, pos)
		newFluid := javaFluidNewLiquid(c, current, pos, fluid)
		delay := javaFluidTickDelay(c.tx, fluid)
		_, above := javaFluidStateAt(c, pos.Side(cube.FaceUp))
		if fluid.kind == javaFluidLava && newFluid.kind != javaFluidEmpty &&
			!fluid.falling && !newFluid.falling && above.kind != fluid.kind &&
			newFluid.amount > fluid.amount && c.randomInt(4) != 0 {
			delay *= 4
		}
		if newFluid.kind == javaFluidEmpty {
			if c.decayEvent(pos, fluid, newFluid) {
				capturedBlock = javaFluidSet(c, pos, capturedBlock, newFluid)
				fluid = newFluid
			}
		} else if newFluid != fluid && c.decayEvent(pos, fluid, newFluid) {
			fluid = newFluid
			capturedBlock = javaFluidSet(c, pos, capturedBlock, fluid)
			c.fluidTick(pos, fluid, delay)
		}
	}
	if fluid.kind == javaFluidEmpty {
		return
	}
	id := capturedBlock
	below := pos.Side(cube.FaceDown)
	belowID, belowFluid := javaFluidStateAt(c, below)
	if javaFluidDownwardAllowed(c, id, belowID, fluid, belowFluid) {
		newBelow := javaFluidNewLiquid(c, belowID, below, fluid)
		if javaFluidReplaceable(c, below, belowFluid, newBelow, true) && javaFluidCanHold(c, belowID, newBelow) {
			if fluid.kind == javaFluidLava && belowFluid.kind == javaFluidWater {
				if belowID.is("water") {
					c.hardenFlow(pos, below, fluid, javaDefault("stone"))
				} else {
					c.beforeFluidDestroy(below, belowID, fluid)
				}
			} else {
				javaFluidSpreadTo(c, pos, below, belowID, newBelow)
			}
			if javaFluidSourceCount(c, pos, fluid.kind) >= 3 {
				javaFluidSpreadSides(c, pos, id, fluid)
			}
			return
		}
	}
	if fluid.source || !javaFluidHole(c, pos, id, fluid.kind) {
		javaFluidSpreadSides(c, pos, id, fluid)
	}
}

func javaFluidSpreadSides(c *javaShapeContext, pos cube.Pos, id javaFluidBlock, fluid javaFluidState) {
	drop := int32(1)
	if fluid.kind == javaFluidLava {
		drop = 2
		if c.tx.World().Dimension() == Nether {
			drop = 1
		}
	}
	neighbor := fluid.amount - drop
	if fluid.falling {
		neighbor = 7
	}
	if neighbor <= 0 {
		return
	}
	targets, count := javaFluidSpreadTargets(c, pos, id, fluid)
	for _, target := range targets[:count] {
		targetID, _ := javaFluidStateAt(c, target.pos)
		javaFluidSpreadTo(c, pos, target.pos, targetID, target.state)
	}
}

func javaTickLiquidBlock(c *javaShapeContext, capturedBlock uint32) {
	fluid := javaFluidForState(capturedBlock)
	if fluid.kind != javaFluidWater || !fluid.source || fluid.amount < 8 {
		return
	}
	pos := c.pos
	belowID, _ := javaFluidStateAt(c, pos.Side(cube.FaceDown))
	dragDown, can := javaBubbleColumnState(c, belowID)
	if !can {
		return
	}
	column := javaWith(javaDefault("bubble_column"), "drag", dragDown)
	c.set(pos, column, 2)
	for above := pos.Side(cube.FaceUp); javaBubbleColumnCanOccupyAt(c, above); above = above.Side(cube.FaceUp) {
		c.set(above, column, 2)
	}
}

func javaBubbleColumnCanOccupyAt(c *javaShapeContext, pos cube.Pos) bool {
	block, _ := javaFluidStateAt(c, pos)
	return block.native == nil && javaBubbleColumnCanOccupy(c, block.state)
}

func javaBubbleColumnCanOccupy(c *javaShapeContext, id uint32) bool {
	name := javaName(id)
	if name == "bubble_column" {
		return true
	}
	fluid := javaFluidForState(id)
	return name == "water" && fluid.source && fluid.amount >= 8
}

func javaBubbleColumnState(c *javaShapeContext, below javaFluidBlock) (dragDown, ok bool) {
	switch {
	case below.is("bubble_column"):
		return javaBool(below.state, "drag"), true
	case below.tag(c, "minecraft:enables_bubble_column_push_up"):
		return false, true
	case below.tag(c, "minecraft:enables_bubble_column_drag_down"):
		return true, true
	default:
		return false, false
	}
}

// javaLiquidBlockOnPlace applies LiquidBlock.onPlace after a source-state write.
func javaLiquidBlockOnPlace(c *javaShapeContext, state uint32) {
	pos := c.pos
	fluid := javaFluidForState(state)
	if fluid.kind == javaFluidLava {
		below, _ := javaFluidStateAt(c, pos.Side(cube.FaceDown))
		overSoulSoil := below.is("soul_soil")
		// LiquidBlock.POSSIBLE_FLOW_DIRECTIONS is DOWN, SOUTH, NORTH, EAST,
		// WEST; shouldSpreadLiquid checks each opposite position in that order.
		neighbors := [...]cube.Face{cube.FaceUp, cube.FaceNorth, cube.FaceSouth, cube.FaceWest, cube.FaceEast}
		for _, face := range neighbors {
			neighborPos := pos.Side(face)
			neighbor, neighborFluid := javaFluidStateAt(c, neighborPos)
			if neighborFluid.kind == javaFluidWater {
				replacement := javaDefault("cobblestone")
				if fluid.source {
					replacement = javaDefault("obsidian")
				}
				c.hardenFlow(neighborPos, pos, neighborFluid, replacement)
				return
			}
			if overSoulSoil && neighbor.is("blue_ice") {
				c.hardenFlow(neighborPos, pos, javaFluidState{}, javaDefault("basalt"))
				return
			}
		}
	}
	c.fluidTick(pos, fluid, javaFluidTickDelay(c.tx, fluid))
	if fluid.kind == javaFluidWater && fluid.source && fluid.amount >= 8 {
		below, _ := javaFluidStateAt(c, pos.Side(cube.FaceDown))
		if below.tag(c, "minecraft:enables_bubble_column_drag_down") ||
			below.tag(c, "minecraft:enables_bubble_column_push_up") {
			c.blockTick(pos, state, 20)
		}
	}
}
