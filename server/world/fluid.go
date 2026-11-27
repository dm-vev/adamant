package world

import (
	"fmt"
	"strings"

	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/internal/gamedata"
)

type fluidKind uint8

const (
	fluidEmpty fluidKind = iota
	fluidWater
	fluidLava
)

type fluidState struct {
	kind            fluidKind
	amount          int32
	falling, source bool
}

// FluidLegacyBlock decodes a pinned FluidState into its canonical legacy
// block. StateDefinition.any supplies falling=true and flowing level=1 when
// the corresponding Source CODEC properties are absent.
func FluidLegacyBlock(name string, properties map[string]any) (uint32, error) {
	name = NormaliseBlockName(name)
	fluid := fluidState{amount: 8, falling: true, source: true}
	switch name {
	case "minecraft:empty":
		if len(properties) != 0 {
			return 0, fmt.Errorf("empty Java fluid has properties")
		}
		return legacyFluidBlock(fluidState{}), nil
	case "minecraft:water", "minecraft:flowing_water":
		fluid.kind = fluidWater
	case "minecraft:lava", "minecraft:flowing_lava":
		fluid.kind = fluidLava
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
	return legacyFluidBlock(fluid), nil
}

func fluidForState(id uint32) fluidState {
	name, ok := gamedata.BlockStateName(id)
	if !ok {
		panic("Java fluid state has no canonical block name")
	}
	if water, _ := gamedata.BlockStateBool(id, "waterlogged"); water {
		return fluidState{kind: fluidWater, amount: 8, falling: strings.Contains(name, "copper_grate"), source: true}
	}
	var kind fluidKind
	switch name {
	case "minecraft:water", "minecraft:bubble_column",
		"minecraft:seagrass", "minecraft:kelp", "minecraft:kelp_plant", "minecraft:tall_seagrass":
		kind = fluidWater
	case "minecraft:lava":
		kind = fluidLava
	default:
		return fluidState{}
	}
	depth, _ := gamedata.BlockStateInt(id, "level")
	if depth == 0 {
		return fluidState{kind: kind, amount: 8, source: true}
	}
	if depth >= 8 {
		return fluidState{kind: kind, amount: 8, falling: true}
	}
	return fluidState{kind: kind, amount: 8 - depth}
}

// BlockStateHasWaterFluid reports the source state's WATER-tag fluid,
// including waterlogged solids, bubble columns, and aquatic plants.
func BlockStateHasWaterFluid(id uint32) bool {
	return fluidForState(id).kind == fluidWater
}

func fluidTickDelay(tx *Tx, fluid fluidState) int32 {
	if fluid.kind == fluidLava {
		if tx.World().Dimension() == Nether {
			return 10
		}
		return 30
	}
	if fluid.kind == fluidWater {
		return 5
	}
	return 0
}

func legacyFluidBlock(fluid fluidState) uint32 {
	if fluid.kind == fluidEmpty || fluid.amount == 0 {
		return stateDefault("air")
	}
	name := "water"
	if fluid.kind == fluidLava {
		name = "lava"
	}
	depth := int32(8 - fluid.amount)
	if fluid.source {
		depth = 0
	} else if fluid.falling {
		depth += 8
	}
	return stateWith(stateDefault(name), "level", depth)
}

func fluidIsSourceBlock(fluid, candidate fluidState) bool {
	return candidate.source && candidate.kind == fluid.kind
}

func fluidReplaceable(c *shapeContext, pos cube.Pos, existing, incoming fluidState, down bool) bool {
	if existing.kind == fluidEmpty {
		return true
	}
	if existing.kind == fluidWater {
		return down && incoming.kind == fluidLava
	}
	if existing.kind == fluidLava {
		_, above := fluidStateAt(c, pos.Side(cube.FaceUp))
		return (existing.amount >= 4 || above.kind == fluidLava) && incoming.kind == fluidWater
	}
	return false
}

func fluidCanHoldAny(c *shapeContext, block fluidBlock) bool {
	if block.native != nil {
		_, displacer := block.native.(LiquidDisplacer)
		_, replaceable := block.native.(replaceableBlock)
		return displacer || replaceable
	}
	id := block.state
	name := stateName(id)
	if _, ok := gamedata.BlockStateBool(id, "waterlogged"); ok {
		return true
	}
	if name == "kelp" || name == "kelp_plant" || name == "tall_seagrass" {
		return true
	}
	return c.tag(id, "minecraft:washed_away_by_fluids")
}

func fluidCanHold(c *shapeContext, block fluidBlock, fluid fluidState) bool {
	if block.native != nil {
		liquid := c.nativeFluid(fluid)
		if displacer, ok := block.native.(LiquidDisplacer); ok && displacer.CanDisplace(liquid) {
			return true
		}
		replaceable, ok := block.native.(replaceableBlock)
		return ok && replaceable.ReplaceableBy(liquid)
	}
	id := block.state
	if !fluidCanHoldAny(c, block) {
		return false
	}
	name := stateName(id)
	if _, ok := gamedata.BlockStateBool(id, "waterlogged"); ok {
		return !stateBool(id, "waterlogged") && fluid.kind == fluidWater
	}
	if name == "kelp" || name == "kelp_plant" || name == "tall_seagrass" {
		return false
	}
	return true
}

func fluidWallPass(c *shapeContext, from, to fluidBlock, pos cube.Pos, direction int) bool {
	if from.native != nil || to.native != nil {
		return fluidMixedWallPass(c, from, to, pos, direction)
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
var fluidHorizontal = [...]struct {
	face      cube.Face
	direction int
}{
	{cube.FaceNorth, 2}, {cube.FaceEast, 5}, {cube.FaceSouth, 3}, {cube.FaceWest, 4},
}

func fluidNear(c *shapeContext, pos cube.Pos, id fluidBlock, kind fluidKind) (int, int32) {
	highest, sources := 0, int32(0)
	for _, d := range fluidHorizontal {
		nextID, fluid := fluidStateAt(c, pos.Side(d.face))
		if fluid.kind == kind && fluidWallPass(c, id, nextID, pos, d.direction) {
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

func fluidNewLiquid(c *shapeContext, id fluidBlock, pos cube.Pos, fluid fluidState) fluidState {
	highest, sources := fluidNear(c, pos, id, fluid.kind)
	conversionRule := "lava_source_conversion"
	if fluid.kind == fluidWater {
		conversionRule = "water_source_conversion"
	}
	if sources >= 2 && c.rule(conversionRule) {
		below := pos.Side(cube.FaceDown)
		belowID, belowFluid := fluidStateAt(c, below)
		if belowID.solid(c, below) || fluidIsSourceBlock(fluid, belowFluid) {
			return fluidState{kind: fluid.kind, amount: 8, source: true}
		}
	}
	aboveID, above := fluidStateAt(c, pos.Side(cube.FaceUp))
	if above.kind == fluid.kind && fluidWallPass(c, id, aboveID, pos, 1) {
		return fluidState{kind: fluid.kind, amount: 8, falling: true}
	}
	drop := int32(1)
	if fluid.kind == fluidLava {
		drop = 2
		if c.tx.World().Dimension() == Nether {
			drop = 1
		}
	}
	amount := int32(highest) - drop
	if amount <= 0 {
		return fluidState{}
	}
	return fluidState{kind: fluid.kind, amount: amount}
}

func fluidSourceCount(c *shapeContext, pos cube.Pos, kind fluidKind) int {
	count := 0
	for _, d := range fluidHorizontal {
		_, f := fluidStateAt(c, pos.Side(d.face))
		if f.kind == kind && f.source {
			count++
		}
	}
	return count
}

func fluidDownwardAllowed(c *shapeContext, sourceID, targetID fluidBlock, source, target fluidState) bool {
	if fluidIsSourceBlock(source, target) || !fluidCanHoldAny(c, targetID) {
		return false
	}
	return fluidWallPass(c, sourceID, targetID, c.pos, 0)
}

func fluidSlope(c *shapeContext, origin cube.Pos, pos cube.Pos, state fluidBlock, kind fluidKind, from cube.Face, pass, limit int) int {
	lowest := 1000
	for _, d := range fluidHorizontal {
		if pos.Side(d.face) == pos.Side(from) {
			continue
		}
		next := pos.Side(d.face)
		nextID, nextFluid := fluidStateAt(c, next)
		flowing := fluidState{kind: kind, amount: 1}
		if fluidIsSourceBlock(fluidState{kind: kind}, nextFluid) || !fluidCanHoldAny(c, nextID) ||
			!fluidCanHold(c, nextID, flowing) || !fluidWallPass(c, state, nextID, pos, d.direction) {
			continue
		}
		if fluidHole(c, next, nextID, kind) {
			return pass
		}
		if pass < limit {
			distance := fluidSlope(c, origin, next, nextID, kind, d.face.Opposite(), pass+1, limit)
			if distance < lowest {
				lowest = distance
			}
		}
	}
	return lowest
}

func fluidHole(c *shapeContext, pos cube.Pos, id fluidBlock, kind fluidKind) bool {
	below := pos.Side(cube.FaceDown)
	belowID, belowFluid := fluidStateAt(c, below)
	if !fluidWallPass(c, id, belowID, pos, 0) {
		return false
	}
	if belowFluid.kind == kind {
		return true
	}
	return fluidCanHold(c, belowID, fluidState{kind: kind, amount: 7})
}

type fluidSpreadTarget struct {
	pos       cube.Pos
	state     fluidState
	direction int
}

func fluidSpreadTargets(c *shapeContext, pos cube.Pos, id fluidBlock, fluid fluidState) ([4]fluidSpreadTarget, int) {
	limit := 4
	if fluid.kind == fluidLava && c.tx.World().Dimension() != Nether {
		limit = 2
	}
	var targets [4]fluidSpreadTarget
	count, lowest := 0, 1000
	for _, d := range fluidHorizontal {
		next := pos.Side(d.face)
		nextID, nextFluid := fluidStateAt(c, next)
		if fluidIsSourceBlock(fluid, nextFluid) || !fluidCanHoldAny(c, nextID) ||
			!fluidWallPass(c, id, nextID, pos, d.direction) {
			continue
		}
		newFluid := fluidNewLiquid(c, nextID, next, fluid)
		if newFluid.kind == fluidEmpty || !fluidCanHold(c, nextID, newFluid) {
			continue
		}
		distance := 0
		if !fluidHole(c, next, nextID, fluid.kind) {
			distance = fluidSlope(c, pos, next, nextID, fluid.kind, d.face.Opposite(), 1, limit)
		}
		if distance < lowest {
			count = 0
		}
		if distance <= lowest {
			if fluidReplaceable(c, next, nextFluid, newFluid, false) {
				targets[count] = fluidSpreadTarget{next, newFluid, d.direction}
				count++
			}
			lowest = distance
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
func fluidSpreadTo(c *shapeContext, source, pos cube.Pos, target fluidBlock, fluid fluidState) {
	if !c.flowEvent(source, pos, fluid) {
		return
	}
	if target.native != nil {
		liquid := c.nativeFluid(fluid)
		if displacer, ok := target.native.(LiquidDisplacer); ok && displacer.CanDisplace(liquid) {
			c.tx.SetLiquid(pos, liquid)
			c.fluidTick(pos, fluid, fluidTickDelay(c.tx, fluid))
			return
		}
	}
	targetID := target.state
	if _, waterlogged := gamedata.BlockStateBool(targetID, "waterlogged"); target.native == nil && waterlogged {
		if !stateBool(targetID, "waterlogged") && fluid.kind == fluidWater {
			c.set(pos, stateWith(targetID, "waterlogged", true), 3)
			c.fluidTick(pos, fluid, fluidTickDelay(c.tx, fluid))
		}
		return
	}
	c.beforeFluidDestroy(pos, target, fluid)
	c.set(pos, legacyFluidBlock(fluid), 3)
}

func tickFluid(c *shapeContext, capturedBlock fluidBlock, fluid fluidState) {
	if fluid.kind == fluidEmpty {
		return
	}
	pos := c.pos
	if !fluid.source {
		current, _ := fluidStateAt(c, pos)
		newFluid := fluidNewLiquid(c, current, pos, fluid)
		delay := fluidTickDelay(c.tx, fluid)
		_, above := fluidStateAt(c, pos.Side(cube.FaceUp))
		if fluid.kind == fluidLava && newFluid.kind != fluidEmpty &&
			!fluid.falling && !newFluid.falling && above.kind != fluid.kind &&
			newFluid.amount > fluid.amount && c.randomInt(4) != 0 {
			delay *= 4
		}
		if newFluid.kind == fluidEmpty {
			if c.decayEvent(pos, fluid, newFluid) {
				capturedBlock = fluidSet(c, pos, capturedBlock, newFluid)
				fluid = newFluid
			}
		} else if newFluid != fluid && c.decayEvent(pos, fluid, newFluid) {
			fluid = newFluid
			capturedBlock = fluidSet(c, pos, capturedBlock, fluid)
			c.fluidTick(pos, fluid, delay)
		}
	}
	if fluid.kind == fluidEmpty {
		return
	}
	id := capturedBlock
	below := pos.Side(cube.FaceDown)
	belowID, belowFluid := fluidStateAt(c, below)
	if fluidDownwardAllowed(c, id, belowID, fluid, belowFluid) {
		newBelow := fluidNewLiquid(c, belowID, below, fluid)
		if fluidReplaceable(c, below, belowFluid, newBelow, true) && fluidCanHold(c, belowID, newBelow) {
			if fluid.kind == fluidLava && belowFluid.kind == fluidWater {
				if belowID.is("water") {
					c.hardenFlow(pos, below, fluid, stateDefault("stone"))
				} else {
					c.beforeFluidDestroy(below, belowID, fluid)
				}
			} else {
				fluidSpreadTo(c, pos, below, belowID, newBelow)
			}
			if fluidSourceCount(c, pos, fluid.kind) >= 3 {
				fluidSpreadSides(c, pos, id, fluid)
			}
			return
		}
	}
	if fluid.source || !fluidHole(c, pos, id, fluid.kind) {
		fluidSpreadSides(c, pos, id, fluid)
	}
}

func fluidSpreadSides(c *shapeContext, pos cube.Pos, id fluidBlock, fluid fluidState) {
	drop := int32(1)
	if fluid.kind == fluidLava {
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
	targets, count := fluidSpreadTargets(c, pos, id, fluid)
	for _, target := range targets[:count] {
		targetID, _ := fluidStateAt(c, target.pos)
		fluidSpreadTo(c, pos, target.pos, targetID, target.state)
	}
}

func tickLiquidBlock(c *shapeContext, capturedBlock uint32) {
	fluid := fluidForState(capturedBlock)
	if fluid.kind != fluidWater || !fluid.source || fluid.amount < 8 {
		return
	}
	pos := c.pos
	belowID, _ := fluidStateAt(c, pos.Side(cube.FaceDown))
	dragDown, can := bubbleColumnState(c, belowID)
	if !can {
		return
	}
	column := stateWith(stateDefault("bubble_column"), "drag", dragDown)
	c.set(pos, column, 2)
	for above := pos.Side(cube.FaceUp); bubbleColumnCanOccupyAt(c, above); above = above.Side(cube.FaceUp) {
		c.set(above, column, 2)
	}
}

func bubbleColumnCanOccupyAt(c *shapeContext, pos cube.Pos) bool {
	block, _ := fluidStateAt(c, pos)
	return block.native == nil && bubbleColumnCanOccupy(c, block.state)
}

func bubbleColumnCanOccupy(c *shapeContext, id uint32) bool {
	name := stateName(id)
	if name == "bubble_column" {
		return true
	}
	fluid := fluidForState(id)
	return name == "water" && fluid.source && fluid.amount >= 8
}

func bubbleColumnState(c *shapeContext, below fluidBlock) (dragDown, ok bool) {
	switch {
	case below.is("bubble_column"):
		return stateBool(below.state, "drag"), true
	case below.tag(c, "minecraft:enables_bubble_column_push_up"):
		return false, true
	case below.tag(c, "minecraft:enables_bubble_column_drag_down"):
		return true, true
	default:
		return false, false
	}
}

// javaLiquidBlockOnPlace applies LiquidBlock.onPlace after a source-state write.
func liquidBlockOnPlace(c *shapeContext, state uint32) {
	pos := c.pos
	fluid := fluidForState(state)
	if fluid.kind == fluidLava {
		below, _ := fluidStateAt(c, pos.Side(cube.FaceDown))
		overSoulSoil := below.is("soul_soil")
		// LiquidBlock.POSSIBLE_FLOW_DIRECTIONS is DOWN, SOUTH, NORTH, EAST,
		// WEST; shouldSpreadLiquid checks each opposite position in that order.
		neighbors := [...]cube.Face{cube.FaceUp, cube.FaceNorth, cube.FaceSouth, cube.FaceWest, cube.FaceEast}
		for _, face := range neighbors {
			neighborPos := pos.Side(face)
			neighbor, neighborFluid := fluidStateAt(c, neighborPos)
			if neighborFluid.kind == fluidWater {
				replacement := stateDefault("cobblestone")
				if fluid.source {
					replacement = stateDefault("obsidian")
				}
				c.hardenFlow(neighborPos, pos, neighborFluid, replacement)
				return
			}
			if overSoulSoil && neighbor.is("blue_ice") {
				c.hardenFlow(neighborPos, pos, fluidState{}, stateDefault("basalt"))
				return
			}
		}
	}
	c.fluidTick(pos, fluid, fluidTickDelay(c.tx, fluid))
	if fluid.kind == fluidWater && fluid.source && fluid.amount >= 8 {
		below, _ := fluidStateAt(c, pos.Side(cube.FaceDown))
		if below.tag(c, "minecraft:enables_bubble_column_drag_down") ||
			below.tag(c, "minecraft:enables_bubble_column_push_up") {
			c.blockTick(pos, state, 20)
		}
	}
}
