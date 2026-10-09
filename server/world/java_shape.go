package world

import (
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

type javaSupport uint8

const (
	javaSupportFull javaSupport = iota
	javaSupportCenter
	javaSupportRigid
)

type javaShapeContext struct {
	tx  *Tx
	pos cube.Pos
}

// JavaBlockCanSurvive evaluates the source survival rule for the current block.
// The second result is false for native-only blocks without a Java identity.
func (tx *Tx) JavaBlockCanSurvive(pos cube.Pos) (survives, hasSourceState bool) {
	state := tx.javaBlockStateID(pos)
	if state == 0 {
		return false, false
	}
	context := javaShapeContext{tx: tx, pos: pos}
	return context.canSurvive(state, pos), true
}

type javaShapeHandler func(*javaShapeContext, uint32, cube.Face, uint32) uint32
type javaSurvivalHandler func(*javaShapeContext, uint32, cube.Pos) bool

var javaShapeHandlers = make(map[string]javaShapeHandler, 109)
var javaSurvivalHandlers = make(map[string]javaSurvivalHandler)

func registerJavaShape(owner string, handler javaShapeHandler) {
	if _, exists := javaShapeHandlers[owner]; exists {
		panic("duplicate Java shape owner " + owner)
	}
	javaShapeHandlers[owner] = handler
}

func registerJavaSurvival(owner string, handler javaSurvivalHandler) {
	if _, exists := javaSurvivalHandlers[owner]; exists {
		panic("duplicate Java survival owner " + owner)
	}
	javaSurvivalHandlers[owner] = handler
}

func javaName(id uint32) string {
	name, ok := javadata.BlockStateName(id)
	if !ok {
		panic(fmt.Sprintf("invalid Java block state ID %d", id))
	}
	return strings.TrimPrefix(name, "minecraft:")
}

func javaIsAir(id uint32) bool {
	switch javaName(id) {
	case "air", "cave_air", "void_air":
		return true
	default:
		return false
	}
}

func javaBool(id uint32, property string) bool {
	value, ok := javadata.BlockStateBool(id, property)
	if !ok {
		panic(fmt.Sprintf("Java state %d has no boolean property %s", id, property))
	}
	return value
}

func javaInt(id uint32, property string) int32 {
	value, ok := javadata.BlockStateInt(id, property)
	if !ok {
		panic(fmt.Sprintf("Java state %d has no integer property %s", id, property))
	}
	return value
}

func javaString(id uint32, property string) string {
	value, ok := javadata.BlockStateString(id, property)
	if !ok {
		panic(fmt.Sprintf("Java state %d has no enum property %s", id, property))
	}
	return value
}

func javaWith(id uint32, property string, value any) uint32 {
	changed, err := javadata.BlockStateWithProperty(id, property, value)
	if err != nil {
		panic(err)
	}
	return changed
}

func javaDefault(name string) uint32 {
	id, err := javadata.BlockStateID(name, nil)
	if err != nil {
		panic(err)
	}
	return id
}

func (c *javaShapeContext) at(pos cube.Pos) uint32 {
	id, err := c.tx.JavaBlockStateID(pos)
	if err != nil {
		panic(err)
	}
	return id
}

func (c *javaShapeContext) tag(id uint32, tag string) bool {
	name, ok := javadata.BlockStateName(id)
	if !ok {
		panic(fmt.Sprintf("invalid Java block state ID %d", id))
	}
	contains, err := javadata.BlockTagContains(tag, name)
	if err != nil {
		panic(err)
	}
	return contains
}

func (c *javaShapeContext) solid(id uint32) bool {
	solid, ok := javadata.BlockStateIsSolid(id)
	if !ok {
		panic(fmt.Sprintf("invalid Java block state ID %d", id))
	}
	return solid
}

func javaGenerationShape(id uint32) javadata.GenerationShape {
	shape, err := javadata.BlockGenerationShapeID(id)
	if err != nil {
		panic(err)
	}
	return shape
}

func (c *javaShapeContext) collisionFull(id uint32) bool {
	return javaGenerationShape(id).CollisionFull
}

func (c *javaShapeContext) collisionEmpty(id uint32) bool {
	return javaGenerationShape(id).CollisionEmpty
}

func (c *javaShapeContext) replaceable(id uint32) bool {
	return javaGenerationShape(id).Replaceable
}

func (c *javaShapeContext) sturdy(id uint32, face cube.Face, support javaSupport) bool {
	shape := javaGenerationShape(id)
	var faces uint8
	switch support {
	case javaSupportFull:
		faces = shape.Sturdy
	case javaSupportCenter:
		faces = shape.Center
	case javaSupportRigid:
		faces = shape.Rigid
	default:
		panic("invalid Java support type")
	}
	return faces&(1<<uint(face)) != 0
}

func (c *javaShapeContext) canSupportCenter(pos cube.Pos, face cube.Face) bool {
	state := c.at(pos)
	return c.sturdy(state, face, javaSupportCenter) && (face != cube.FaceDown || !c.tag(state, "unstable_bottom_center"))
}

func (c *javaShapeContext) canSurvive(state uint32, pos cube.Pos) bool {
	owner, err := javadata.BlockCanSurviveOwnerID(state)
	if err != nil {
		panic(err)
	}
	return c.ownerSurvival(owner[strings.LastIndexByte(owner, '.')+1:], state, pos)
}

func (c *javaShapeContext) ownerSurvival(owner string, state uint32, pos cube.Pos) bool {
	if owner == "BlockBehaviour" {
		return true
	}
	if handler, exists := javaSurvivalHandlers[owner]; exists {
		return handler(c, state, pos)
	}
	if survives, handled := javaFluidSurvival(c, owner, state, pos); handled {
		return survives
	}
	panic(fmt.Sprintf("unhandled Java canSurvive owner %s for %s", owner, javaName(state)))
}

func (c *javaShapeContext) fluid(state uint32) javaFluidState {
	return javaFluidForState(state)
}

func (c *javaShapeContext) waterTick(state uint32, pos cube.Pos) {
	if wet, _ := javadata.BlockStateBool(state, "waterlogged"); wet {
		c.fluidTick(pos, javaFluidState{kind: javaFluidWater, amount: 8, source: true}, 5)
	}
}

func (c *javaShapeContext) blockTick(pos cube.Pos, state uint32, delay int32) {
	name, ok := javadata.BlockStateName(state)
	if !ok {
		panic(fmt.Sprintf("invalid Java block state ID %d", state))
	}
	c.schedule(pos, chunk.BlockTick, name, delay)
}

func (c *javaShapeContext) hasBlockTick(pos cube.Pos, state uint32) bool {
	name, ok := javadata.BlockStateName(state)
	if !ok {
		panic(fmt.Sprintf("invalid Java block state ID %d", state))
	}
	_, scheduled := c.tx.World().scheduledBlockTicks.scheduledTicks[scheduledTickIndex{pos: pos, typeName: name}]
	return scheduled
}

func (c *javaShapeContext) fluidTick(pos cube.Pos, fluid javaFluidState, delay int32) {
	var name string
	switch fluid.kind {
	case javaFluidWater:
		name = "minecraft:water"
		if !fluid.source {
			name = "minecraft:flowing_water"
		}
	case javaFluidLava:
		name = "minecraft:lava"
		if !fluid.source {
			name = "minecraft:flowing_lava"
		}
	default:
		panic("cannot schedule empty Java fluid")
	}
	c.schedule(pos, chunk.FluidTick, name, delay)
}

func (c *javaShapeContext) schedule(pos cube.Pos, kind chunk.ScheduledTickKind, name string, delay int32) {
	w := c.tx.World()
	if pos.OutOfBounds(w.Range()) {
		return
	}
	_, ok := w.resolveScheduledTickBlock(chunk.ScheduledTick{Pos: pos, Kind: kind, Type: name})
	if !ok {
		panic(fmt.Sprintf("cannot schedule Java type %s at %v", name, pos))
	}
	queue := w.scheduledBlockTicks
	if kind == chunk.FluidTick {
		queue = w.scheduledFluidTicks
	}
	if queue.insertTypeTick(pos, name, queue.currentTick+int64(delay), 0, false) {
		w.markScheduledTickColumnModified(pos)
	}
}

func (c *javaShapeContext) randomInt(n int32) int32 {
	return c.tx.JavaRandom().NextInt(n)
}

func (c *javaShapeContext) randomFloat() float32 {
	return c.tx.JavaRandom().NextFloat()
}

func (c *javaShapeContext) light(pos cube.Pos) int {
	return int(c.tx.Light(pos))
}

func (c *javaShapeContext) rule(name string) bool {
	w := c.tx.World()
	w.set.Lock()
	defer w.set.Unlock()
	switch name {
	case "water_source_conversion":
		return w.set.WaterSourceConversion
	case "lava_source_conversion":
		return w.set.LavaSourceConversion
	default:
		panic(fmt.Sprintf("unknown Java fluid rule %s", name))
	}
}

func (c *javaShapeContext) set(pos cube.Pos, state uint32, flags int) {
	c.tx.setJavaBlockState(pos, state, flags)
}

func (c *javaShapeContext) updateShape(state uint32, face cube.Face, neighbor uint32) uint32 {
	owner, err := javadata.BlockUpdateShapeOwnerID(state)
	if err != nil {
		panic(err)
	}
	return c.ownerShape(owner[strings.LastIndexByte(owner, '.')+1:], state, face, neighbor)
}

func (c *javaShapeContext) ownerShape(owner string, state uint32, face cube.Face, neighbor uint32) uint32 {
	if owner == "BlockBehaviour" {
		return state
	}
	if handler, exists := javaShapeHandlers[owner]; exists {
		return handler(c, state, face, neighbor)
	}
	if updated, handled := javaFluidShape(c, owner, state, face, neighbor); handled {
		return updated
	}
	panic(fmt.Sprintf("unhandled Java updateShape owner %s for %s", owner, javaName(state)))
}

func (c *javaShapeContext) updateFromNeighbourShapes(state uint32) uint32 {
	for _, face := range [...]cube.Face{cube.FaceWest, cube.FaceEast, cube.FaceNorth, cube.FaceSouth, cube.FaceDown, cube.FaceUp} {
		state = c.updateShape(state, face, c.at(c.pos.Side(face)))
	}
	return state
}
