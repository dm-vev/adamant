// Package block provides declarative custom block definitions, block registry
// registration, and fast runtime-ID world editing helpers.
package block

import (
	"maps"
	"math/rand/v2"
	"slices"
	"sync"

	coreblock "github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/customblock"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

var hashMu sync.Mutex

func nextHash() uint64 {
	hashMu.Lock()
	defer hashMu.Unlock()
	return coreblock.NextHash()
}

// Option configures a custom block created by Define.
type Option func(*config)

type config struct {
	stateProps       map[string]any
	props            customblock.Properties
	collisionKind    world.CollisionKind
	customBoxes      []cube.BBox
	solidFaces       bool
	transparentFaces bool
	lightEmission    uint8
	lightFilter      uint8
	friction         float64
	replaceable      bool
	randomTicks      bool
	hardness         float64
	unbreakable      bool
	blastResistance  float64
	drops            []item.Stack
	dropsFunc        func(t item.Tool, enchantments []item.Enchantment) []item.Stack
	harvestable      func(t item.Tool) bool
	effective        func(t item.Tool) bool
	xpDrops          coreblock.XPDropRange
	onPlace          func(pos cube.Pos, tx *world.Tx, previous world.Block)
	onBreak          func(pos cube.Pos, tx *world.Tx, u item.User)
	onInteract       func(pos cube.Pos, face cube.Face, tx *world.Tx, u item.User, ctx *item.UseContext) bool
	onRandomTick     func(pos cube.Pos, tx *world.Tx, r *rand.Rand)
	onEntityInside   func(pos cube.Pos, tx *world.Tx, e world.Entity)
	onEntityStep     func(pos cube.Pos, tx *world.Tx, e world.Entity)
}

// StateProperties sets the block state properties returned by EncodeBlock.
func StateProperties(p map[string]any) Option {
	return func(c *config) { c.stateProps = maps.Clone(p) }
}

// Properties sets Bedrock custom block visual/component properties.
func Properties(p customblock.Properties) Option { return func(c *config) { c.props = p } }

// EmptyCollision makes the block non-collidable.
func EmptyCollision() Option {
	return func(c *config) { c.collisionKind = world.CollisionEmpty; c.customBoxes = []cube.BBox{} }
}

// FullCube declares a full 1x1x1 solid unit cube collision shape.
func FullCube() Option { return func(c *config) { c.collisionKind = world.CollisionFullCube } }

// Boxes sets explicit custom collision bounding boxes.
func Boxes(boxes ...cube.BBox) Option {
	return func(c *config) { c.collisionKind = world.CollisionDynamic; c.customBoxes = slices.Clone(boxes) }
}

// SolidFaces marks block faces as solid for neighbor attachment.
func SolidFaces() Option { return func(c *config) { c.solidFaces = true } }

// TransparentFaces forces FaceSolid to return false even for full cubes.
func TransparentFaces() Option { return func(c *config) { c.transparentFaces = true } }

// Light sets the emitted light level (0..15).
func Light(level uint8) Option { return func(c *config) { c.lightEmission = min(level, 15) } }

// LightFilter sets how much light is subtracted passing through the block (0..15).
func LightFilter(level uint8) Option { return func(c *config) { c.lightFilter = min(level, 15) } }

// Friction overrides surface friction (defaults to 0.6).
func Friction(f float64) Option { return func(c *config) { c.friction = f } }

// Replaceable allows other blocks to replace this block on placement.
func Replaceable() Option { return func(c *config) { c.replaceable = true } }

// Hardness sets survival mining hardness (0 breaks instantly).
func Hardness(h float64) Option { return func(c *config) { c.hardness = h } }

// BlastResistance overrides explosion blast resistance.
func BlastResistance(r float64) Option { return func(c *config) { c.blastResistance = r } }

// Unbreakable prevents the block from implementing block.Breakable.
func Unbreakable() Option { return func(c *config) { c.unbreakable = true } }

// Drops sets static item stacks dropped when broken.
func Drops(stacks ...item.Stack) Option { return func(c *config) { c.drops = slices.Clone(stacks) } }

// DropsFunc dynamically computes item drops when broken.
func DropsFunc(fn func(t item.Tool, enchantments []item.Enchantment) []item.Stack) Option {
	return func(c *config) { c.dropsFunc = fn }
}

// Harvestable sets which tools may harvest drops from this block.
func Harvestable(fn func(t item.Tool) bool) Option { return func(c *config) { c.harvestable = fn } }

// Effective sets which tools speed up mining this block.
func Effective(fn func(t item.Tool) bool) Option { return func(c *config) { c.effective = fn } }

// XP configures the experience orb range dropped when mined without Silk Touch.
func XP(minXP, maxXP int) Option {
	return func(c *config) { c.xpDrops = coreblock.XPDropRange{minXP, maxXP} }
}

// OnPlace registers a callback run when the block is placed in a world.
func OnPlace(fn func(pos cube.Pos, tx *world.Tx, previous world.Block)) Option {
	return func(c *config) { c.onPlace = fn }
}

// OnBreak registers a callback run after the block is broken.
func OnBreak(fn func(pos cube.Pos, tx *world.Tx, u item.User)) Option {
	return func(c *config) { c.onBreak = fn }
}

// OnInteract registers a callback run when a user right-clicks the block.
func OnInteract(fn func(pos cube.Pos, face cube.Face, tx *world.Tx, u item.User, ctx *item.UseContext) bool) Option {
	return func(c *config) { c.onInteract = fn }
}

// OnRandomTick enables random block ticks and registers fn.
func OnRandomTick(fn func(pos cube.Pos, tx *world.Tx, r *rand.Rand)) Option {
	return func(c *config) { c.randomTicks = true; c.onRandomTick = fn }
}

// OnEntityInside registers a callback run while an entity intersects the block.
func OnEntityInside(fn func(pos cube.Pos, tx *world.Tx, e world.Entity)) Option {
	return func(c *config) { c.onEntityInside = fn }
}

// OnEntityStep registers a callback run while an entity stands on top of the block.
func OnEntityStep(fn func(pos cube.Pos, tx *world.Tx, e world.Entity)) Option {
	return func(c *config) { c.onEntityStep = fn }
}

type model struct {
	kind       world.CollisionKind
	boxes      []cube.BBox
	solidFaces bool
}

func (m model) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	if len(m.boxes) == 0 {
		return nil
	}
	return m.boxes
}

func (m model) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool { return m.solidFaces }
func (m model) StaticCollisionKind() world.CollisionKind              { return m.kind }

type def struct {
	name           string
	stateProps     map[string]any
	props          customblock.Properties
	hash           uint64
	model          model
	lightEmission  uint8
	lightFilter    uint8
	friction       float64
	replaceable    bool
	randomTicks    bool
	breakInfo      coreblock.BreakInfo
	onPlace        func(cube.Pos, *world.Tx, world.Block)
	onInteract     func(cube.Pos, cube.Face, *world.Tx, item.User, *item.UseContext) bool
	onRandomTick   func(cube.Pos, *world.Tx, *rand.Rand)
	onEntityInside func(cube.Pos, *world.Tx, world.Entity)
	onEntityStep   func(cube.Pos, *world.Tx, world.Entity)
}

func buildModel(c config) model {
	unitBox := []cube.BBox{cube.Box(0, 0, 0, 1, 1, 1)}
	if len(c.customBoxes) > 0 {
		boxes := slices.Clone(c.customBoxes)
		boxes = boxes[:len(boxes):len(boxes)]
		return model{kind: world.CollisionDynamic, boxes: boxes, solidFaces: c.solidFaces && !c.transparentFaces}
	}
	switch c.collisionKind {
	case world.CollisionEmpty:
		return model{kind: world.CollisionEmpty, solidFaces: c.solidFaces && !c.transparentFaces}
	case world.CollisionFullCube:
		return model{kind: world.CollisionFullCube, boxes: unitBox, solidFaces: !c.transparentFaces}
	default:
		var boxes []cube.BBox
		solid := c.solidFaces
		if c.customBoxes == nil {
			boxes = unitBox
			solid = !c.transparentFaces
		}
		return model{kind: world.CollisionDynamic, boxes: boxes, solidFaces: solid && !c.transparentFaces}
	}
}

func buildBreakInfo(c config) coreblock.BreakInfo {
	blast := c.blastResistance
	if blast <= 0 && c.hardness > 0 {
		blast = c.hardness
	}
	harvestable := c.harvestable
	if harvestable == nil {
		harvestable = func(item.Tool) bool { return true }
	}
	effective := c.effective
	if effective == nil {
		effective = func(item.Tool) bool { return false }
	}
	dropsFn := c.dropsFunc
	if dropsFn == nil {
		staticDrops := slices.Clone(c.drops)
		dropsFn = func(item.Tool, []item.Enchantment) []item.Stack { return slices.Clone(staticDrops) }
	}
	return coreblock.BreakInfo{
		Hardness:        c.hardness,
		BlastResistance: blast,
		Harvestable:     harvestable,
		Effective:       effective,
		Drops:           dropsFn,
		BreakHandler:    c.onBreak,
		XPDrops:         c.xpDrops,
	}
}

// Define constructs a hashable, comparable world.Block named id (e.g.
// "myplugin:glow_pad") configured by opts.
func Define(id string, opts ...Option) world.Block {
	if id == "" {
		panic("sdk/block: block id must not be empty")
	}
	var c config
	for _, opt := range opts {
		if opt != nil {
			opt(&c)
		}
	}
	friction := c.friction
	if friction <= 0 {
		friction = 0.6
	}
	d := &def{
		name:           id,
		stateProps:     c.stateProps,
		props:          c.props,
		hash:           nextHash(),
		model:          buildModel(c),
		lightEmission:  c.lightEmission,
		lightFilter:    c.lightFilter,
		friction:       friction,
		replaceable:    c.replaceable,
		randomTicks:    c.randomTicks,
		breakInfo:      buildBreakInfo(c),
		onPlace:        c.onPlace,
		onInteract:     c.onInteract,
		onRandomTick:   c.onRandomTick,
		onEntityInside: c.onEntityInside,
		onEntityStep:   c.onEntityStep,
	}
	return wrap(base{def: d}, !c.unbreakable && c.hardness >= 0, c.onEntityInside != nil, c.onEntityStep != nil)
}

// Register registers b on world.DefaultBlockRegistry.
func Register(b world.Block) { world.DefaultBlockRegistry.RegisterBlock(b) }

// RegisterIn registers b on reg (defaulting to world.DefaultBlockRegistry).
func RegisterIn(reg world.BlockRegistry, b world.Block) {
	if reg == nil {
		reg = world.DefaultBlockRegistry
	}
	reg.RegisterBlock(b)
}

// RuntimeID returns the runtime ID of b in world.DefaultBlockRegistry.
func RuntimeID(b world.Block) uint32 { return world.DefaultBlockRegistry.BlockRuntimeID(b) }

// RuntimeIDIn returns the runtime ID of b in reg.
func RuntimeIDIn(reg world.BlockRegistry, b world.Block) uint32 {
	if reg == nil {
		reg = world.DefaultBlockRegistry
	}
	return reg.BlockRuntimeID(b)
}

// SetRID sets the block at pos directly from a registered runtime ID.
func SetRID(tx *world.Tx, pos cube.Pos, rid uint32, opts *world.SetOpts) {
	if tx != nil && !tx.Closed() {
		tx.SetBlockRID(pos, rid, opts)
	}
}

// FillRID fills the inclusive box [lo, hi] with runtime ID rid and returns how
// many blocks changed.
func FillRID(tx *world.Tx, lo, hi cube.Pos, rid uint32) int {
	if tx == nil || tx.Closed() {
		return 0
	}
	if lo[0] > hi[0] {
		lo[0], hi[0] = hi[0], lo[0]
	}
	if lo[1] > hi[1] {
		lo[1], hi[1] = hi[1], lo[1]
	}
	if lo[2] > hi[2] {
		lo[2], hi[2] = hi[2], lo[2]
	}
	return tx.FillRID(lo, hi, rid)
}

type base struct{ def *def }

func (b base) EncodeBlock() (string, map[string]any) {
	if len(b.def.stateProps) == 0 {
		return b.def.name, nil
	}
	return b.def.name, maps.Clone(b.def.stateProps)
}
func (b base) EncodeItem() (string, int16)              { return b.def.name, 0 }
func (b base) Hash() (uint64, uint64)                   { return b.def.hash, 0 }
func (b base) Model() world.BlockModel                  { return b.def.model }
func (b base) StaticCollisionKind() world.CollisionKind { return b.def.model.kind }
func (b base) Properties() customblock.Properties       { return b.def.props }
func (b base) LightEmissionLevel() uint8                { return b.def.lightEmission }
func (b base) LightDiffusionLevel() uint8               { return b.def.lightFilter }
func (b base) Friction() float64                        { return b.def.friction }
func (b base) ReplaceableBy(world.Block) bool           { return b.def.replaceable }
func (b base) RandomlyTicks() bool                      { return b.def.randomTicks }
func (b base) RandomTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	if b.def.onRandomTick != nil {
		b.def.onRandomTick(pos, tx, r)
	}
}
func (b base) Placed(pos cube.Pos, tx *world.Tx, previous world.Block) {
	if b.def.onPlace != nil {
		b.def.onPlace(pos, tx, previous)
	}
}
func (b base) Activate(pos cube.Pos, face cube.Face, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	if b.def.onInteract != nil {
		return b.def.onInteract(pos, face, tx, u, ctx)
	}
	return false
}
func (b base) breakInfo() coreblock.BreakInfo { return b.def.breakInfo }
func (b base) entityInside(pos cube.Pos, tx *world.Tx, e world.Entity) {
	if b.def.onEntityInside != nil {
		b.def.onEntityInside(pos, tx, e)
	}
}
func (b base) entityStepOn(pos cube.Pos, tx *world.Tx, e world.Entity) {
	if b.def.onEntityStep != nil {
		b.def.onEntityStep(pos, tx, e)
	}
}

type (
	bBreak       struct{ base }
	bInside      struct{ base }
	bStep        struct{ base }
	bBreakInside struct{ base }
	bBreakStep   struct{ base }
	bInsideStep  struct{ base }
	bAll         struct{ base }
)

func (b bBreak) BreakInfo() coreblock.BreakInfo                           { return b.breakInfo() }
func (b bInside) EntityInside(pos cube.Pos, tx *world.Tx, e world.Entity) { b.entityInside(pos, tx, e) }
func (b bStep) EntityStepOn(pos cube.Pos, tx *world.Tx, e world.Entity)   { b.entityStepOn(pos, tx, e) }
func (b bBreakInside) BreakInfo() coreblock.BreakInfo                     { return b.breakInfo() }
func (b bBreakInside) EntityInside(pos cube.Pos, tx *world.Tx, e world.Entity) {
	b.entityInside(pos, tx, e)
}
func (b bBreakStep) BreakInfo() coreblock.BreakInfo { return b.breakInfo() }
func (b bBreakStep) EntityStepOn(pos cube.Pos, tx *world.Tx, e world.Entity) {
	b.entityStepOn(pos, tx, e)
}
func (b bInsideStep) EntityInside(pos cube.Pos, tx *world.Tx, e world.Entity) {
	b.entityInside(pos, tx, e)
}
func (b bInsideStep) EntityStepOn(pos cube.Pos, tx *world.Tx, e world.Entity) {
	b.entityStepOn(pos, tx, e)
}
func (b bAll) BreakInfo() coreblock.BreakInfo                          { return b.breakInfo() }
func (b bAll) EntityInside(pos cube.Pos, tx *world.Tx, e world.Entity) { b.entityInside(pos, tx, e) }
func (b bAll) EntityStepOn(pos cube.Pos, tx *world.Tx, e world.Entity) { b.entityStepOn(pos, tx, e) }

func wrap(b base, breakable, insider, stepper bool) world.Block {
	mask := 0
	if breakable {
		mask |= 1
	}
	if insider {
		mask |= 2
	}
	if stepper {
		mask |= 4
	}
	switch mask {
	case 0:
		return b
	case 1:
		return bBreak{b}
	case 2:
		return bInside{b}
	case 3:
		return bBreakInside{b}
	case 4:
		return bStep{b}
	case 5:
		return bBreakStep{b}
	case 6:
		return bInsideStep{b}
	default:
		return bAll{b}
	}
}
