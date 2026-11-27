package world

import (
	"fmt"
	"strings"
	"sync"

	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/internal/gamedata"
	"github.com/dm-vev/adamant/server/world/chunk"
)

type shapeSupport uint8

const (
	supportFull shapeSupport = iota
	supportCenter
	supportRigid
)

type shapeContext struct {
	tx         *Tx
	generation GenerationShapeAccess
	pos        cube.Pos
	// pure shapes a block without side effects, for a native block that
	// keeps its own: the ticks and writes of handlers are dropped, chunks
	// are not loaded, and source, if set, is read in place of tx. A block
	// that has no Java state, or is not loaded, reads as air and sets
	// unknown, so that the shape read from it can be dropped.
	pure    bool
	source  BlockSource
	unknown bool
}

// JavaBlockCanSurvive evaluates the source survival rule for the current block.
// The second result is false for native-only blocks without a Java identity.
func (tx *Tx) BlockCanSurvive(pos cube.Pos) (survives, hasSourceState bool) {
	state := tx.canonicalBlockStateID(pos)
	if state == 0 {
		return false, false
	}
	context := shapeContext{tx: tx, pos: pos}
	return context.canSurvive(state, pos), true
}

// BlockFaceSturdy reports whether the face at pos provides full support. Vanilla
// blocks use their Java support shapes, including partial block states. Blocks
// without a Java identity fall back to their native model's solid-face rule.
func (tx *Tx) BlockFaceSturdy(pos cube.Pos, face cube.Face) bool {
	if state := tx.canonicalBlockStateID(pos); state != 0 {
		context := shapeContext{tx: tx, pos: pos}
		return context.sturdy(state, face, supportFull)
	}
	return tx.Block(pos).Model().FaceSolid(pos, face, tx)
}

// JavaPlacement returns the state a Java block state takes from its
// neighbours when placed at a position, as Block.updateFromNeighbourShapes,
// and reports if it survives there, as BlockState.canSurvive. It changes
// nothing.
func (tx *Tx) BlockPlacement(state uint32, pos cube.Pos) (uint32, bool) {
	context := shapeContext{tx: tx, pos: pos}
	state = context.updateFromNeighbourShapes(state)
	return state, context.canSurvive(state, pos)
}

// SetJavaBlockState sets a Java block state at a position and updates its
// neighbours, as Level.setBlockAndUpdate.
func (tx *Tx) SetBlockState(pos cube.Pos, state uint32) bool {
	return tx.setCanonicalBlockState(pos, state, 3)
}

type shapeHandler func(*shapeContext, uint32, cube.Face, uint32) uint32
type survivalHandler func(*shapeContext, uint32, cube.Pos) bool

var shapeHandlers = make(map[string]shapeHandler, 109)
var survivalHandlers = make(map[string]survivalHandler)

func registerShape(owner string, handler shapeHandler) {
	if _, exists := shapeHandlers[owner]; exists {
		panic("duplicate Java shape owner " + owner)
	}
	shapeHandlers[owner] = handler
}

func registerSurvival(owner string, handler survivalHandler) {
	if _, exists := survivalHandlers[owner]; exists {
		panic("duplicate Java survival owner " + owner)
	}
	survivalHandlers[owner] = handler
}

func stateName(id uint32) string {
	name, ok := gamedata.BlockStateName(id)
	if !ok {
		panic(fmt.Sprintf("invalid Java block state ID %d", id))
	}
	return strings.TrimPrefix(name, "minecraft:")
}

func stateIsAir(id uint32) bool {
	switch stateName(id) {
	case "air", "cave_air", "void_air":
		return true
	default:
		return false
	}
}

func stateBool(id uint32, property string) bool {
	value, ok := gamedata.BlockStateBool(id, property)
	if !ok {
		panic(fmt.Sprintf("Java state %d has no boolean property %s", id, property))
	}
	return value
}

func stateInt(id uint32, property string) int32 {
	value, ok := gamedata.BlockStateInt(id, property)
	if !ok {
		panic(fmt.Sprintf("Java state %d has no integer property %s", id, property))
	}
	return value
}

func stateString(id uint32, property string) string {
	value, ok := gamedata.BlockStateString(id, property)
	if !ok {
		panic(fmt.Sprintf("Java state %d has no enum property %s", id, property))
	}
	return value
}

func stateWith(id uint32, property string, value any) uint32 {
	changed, err := gamedata.BlockStateWithProperty(id, property, value)
	if err != nil {
		panic(err)
	}
	return changed
}

func stateDefault(name string) uint32 {
	id, err := gamedata.BlockStateID(name, nil)
	if err != nil {
		panic(err)
	}
	return id
}

// at returns the Java block state the shapes read at pos. Generation holds
// Java states only. The world may hold any native state: at reads it as
// game logic does (Tx.JavaBlockState), and a block of no Java block at all
// by its collision shape (javaStandIn), never failing on world data. A pure
// shape reads it as pureAt does.
func (c *shapeContext) at(pos cube.Pos) uint32 {
	if c.pure {
		return c.pureAt(pos)
	}
	if c.generation != nil {
		id, err := c.generation.BlockStateID(pos)
		if err != nil {
			panic(err)
		}
		return id
	}
	if id, ok := c.tx.CanonicalBlockState(pos); ok {
		return id
	}
	return c.tx.canonicalStandIn(pos)
}

// javaAir and javaBedrock are the Java states javaStandIn stands in with.
var (
	airStateID     = sync.OnceValue(func() uint32 { return stateDefault("minecraft:air") })
	bedrockStateID = sync.OnceValue(func() uint32 { return stateDefault("minecraft:bedrock") })
)

// javaStandIn returns the Java block state that stands in for the block at
// pos, of no Java block at all, in the shapes and survival of its
// neighbours: air if nothing collides with it, else bedrock, a full block in
// no tag that shapes or survival read.
func (tx *Tx) canonicalStandIn(pos cube.Pos) uint32 {
	if len(tx.Block(pos).Model().BBox(pos, tx)) == 0 {
		return airStateID()
	}
	return bedrockStateID()
}

func (c *shapeContext) tag(id uint32, tag string) bool {
	name, ok := gamedata.BlockStateName(id)
	if !ok {
		panic(fmt.Sprintf("invalid Java block state ID %d", id))
	}
	contains, err := gamedata.BlockTagContains(tag, name)
	if err != nil {
		panic(err)
	}
	return contains
}

func (c *shapeContext) solid(id uint32) bool {
	solid, ok := gamedata.BlockStateIsSolid(id)
	if !ok {
		panic(fmt.Sprintf("invalid Java block state ID %d", id))
	}
	return solid
}

func canonicalGenerationShape(id uint32) gamedata.GenerationShape {
	shape, err := gamedata.BlockGenerationShapeID(id)
	if err != nil {
		panic(err)
	}
	return shape
}

func (c *shapeContext) replaceable(id uint32) bool {
	return canonicalGenerationShape(id).Replaceable
}

func (c *shapeContext) sturdy(id uint32, face cube.Face, support shapeSupport) bool {
	shape := canonicalGenerationShape(id)
	var faces uint8
	switch support {
	case supportFull:
		faces = shape.Sturdy
	case supportCenter:
		faces = shape.Center
	case supportRigid:
		faces = shape.Rigid
	default:
		panic("invalid Java support type")
	}
	return faces&(1<<uint(face)) != 0
}

func (c *shapeContext) canSupportCenter(pos cube.Pos, face cube.Face) bool {
	state := c.at(pos)
	return c.sturdy(state, face, supportCenter) && (face != cube.FaceDown || !c.tag(state, "unstable_bottom_center"))
}

func (c *shapeContext) canSurvive(state uint32, pos cube.Pos) bool {
	owner, err := gamedata.BlockCanSurviveOwnerID(state)
	if err != nil {
		panic(err)
	}
	return c.ownerSurvival(owner[strings.LastIndexByte(owner, '.')+1:], state, pos)
}

func (c *shapeContext) ownerSurvival(owner string, state uint32, pos cube.Pos) bool {
	if owner == "BlockBehaviour" {
		return true
	}
	if handler, exists := survivalHandlers[owner]; exists {
		return handler(c, state, pos)
	}
	if survives, handled := fluidSurvival(c, owner, state, pos); handled {
		return survives
	}
	panic(fmt.Sprintf("unhandled Java canSurvive owner %s for %s", owner, stateName(state)))
}

func (c *shapeContext) fluid(state uint32) fluidState {
	return fluidForState(state)
}

func (c *shapeContext) waterTick(state uint32, pos cube.Pos) {
	if wet, _ := gamedata.BlockStateBool(state, "waterlogged"); wet {
		c.fluidTick(pos, fluidState{kind: fluidWater, amount: 8, source: true}, 5)
	}
}

func (c *shapeContext) blockTick(pos cube.Pos, state uint32, delay int32) {
	name, ok := gamedata.BlockStateName(state)
	if !ok {
		panic(fmt.Sprintf("invalid Java block state ID %d", state))
	}
	c.schedule(pos, chunk.BlockTick, name, delay)
}

func (c *shapeContext) hasBlockTick(pos cube.Pos, state uint32) bool {
	name, ok := gamedata.BlockStateName(state)
	if !ok {
		panic(fmt.Sprintf("invalid Java block state ID %d", state))
	}
	if c.generation != nil {
		return c.generation.HasBlockTick(pos, name)
	}
	_, scheduled := c.tx.World().scheduledBlockTicks.scheduledTicks[scheduledTickIndex{pos: pos, typeName: name}]
	return scheduled
}

func (c *shapeContext) fluidTick(pos cube.Pos, fluid fluidState, delay int32) {
	var name string
	switch fluid.kind {
	case fluidWater:
		name = "minecraft:water"
		if !fluid.source {
			name = "minecraft:flowing_water"
		}
	case fluidLava:
		name = "minecraft:lava"
		if !fluid.source {
			name = "minecraft:flowing_lava"
		}
	default:
		panic("cannot schedule empty Java fluid")
	}
	c.schedule(pos, chunk.FluidTick, name, delay)
}

func (c *shapeContext) schedule(pos cube.Pos, kind chunk.ScheduledTickKind, name string, delay int32) {
	if c.pure {
		return
	}
	if c.generation != nil {
		c.generation.ScheduleTick(pos, kind, name, delay)
		return
	}
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
	} else if !w.blockTickHasHandler(name) {
		return
	}
	if queue.insertTypeTick(pos, name, queue.currentTick+int64(delay), 0, false) {
		w.markScheduledTickColumnModified(pos)
	}
}

func (c *shapeContext) randomInt(n int32) int32 {
	if c.generation != nil {
		return c.generation.RegionRandom().NextInt(n)
	}
	return c.tx.RegionRandom().NextInt(n)
}

func (c *shapeContext) light(pos cube.Pos) int {
	if c.generation != nil {
		return int(c.generation.Light(pos))
	}
	return int(c.tx.Light(pos))
}

func (c *shapeContext) blockRange() cube.Range {
	if c.generation != nil {
		return c.generation.Range()
	}
	return c.tx.Range()
}

func (c *shapeContext) fluidDelay(fluid fluidState) int32 {
	if c.generation == nil {
		return fluidTickDelay(c.tx, fluid)
	}
	switch fluid.kind {
	case fluidWater:
		return c.generation.FluidTickDelay("minecraft:water")
	case fluidLava:
		return c.generation.FluidTickDelay("minecraft:lava")
	default:
		panic("cannot obtain tick delay for empty Java fluid")
	}
}

func (c *shapeContext) rule(name string) bool {
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

func (c *shapeContext) set(pos cube.Pos, state uint32, flags int) {
	if c.pure {
		return
	}
	c.tx.setCanonicalBlockState(pos, state, flags)
}

func (c *shapeContext) updateShape(state uint32, face cube.Face, neighbor uint32) uint32 {
	owner, err := gamedata.BlockUpdateShapeOwnerID(state)
	if err != nil {
		panic(err)
	}
	return c.ownerShape(owner[strings.LastIndexByte(owner, '.')+1:], state, face, neighbor)
}

func (c *shapeContext) ownerShape(owner string, state uint32, face cube.Face, neighbor uint32) uint32 {
	if owner == "BlockBehaviour" {
		return state
	}
	if handler, exists := shapeHandlers[owner]; exists {
		return handler(c, state, face, neighbor)
	}
	if updated, handled := fluidShape(c, owner, state, face, neighbor); handled {
		return updated
	}
	panic(fmt.Sprintf("unhandled Java updateShape owner %s for %s", owner, stateName(state)))
}

func (c *shapeContext) updateFromNeighbourShapes(state uint32) uint32 {
	for _, face := range [...]cube.Face{cube.FaceWest, cube.FaceEast, cube.FaceNorth, cube.FaceSouth, cube.FaceDown, cube.FaceUp} {
		state = c.updateShape(state, face, c.at(c.pos.Side(face)))
	}
	return state
}
