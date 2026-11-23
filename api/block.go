package api

import (
	"maps"
	"math/rand/v2"
	"slices"
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/customblock"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

var customBlockHashMu sync.Mutex

func nextCustomBlockHash() uint64 {
	customBlockHashMu.Lock()
	defer customBlockHashMu.Unlock()
	return block.NextHash()
}

// CustomBlockSpec declaratively configures a custom block created by
// NewCustomBlock without boilerplate interface implementations.
type CustomBlockSpec struct {
	// Name is the namespaced block identifier (e.g. "myplugin:glow_pad").
	Name string
	// StateProperties holds optional block state properties returned by EncodeBlock.
	StateProperties map[string]any
	// Properties holds optional Bedrock custom block visual/component properties.
	Properties customblock.Properties
	// CollisionKind declares the static collision shape when CustomBoxes is empty.
	// CollisionEmpty marks the block non-solid; CollisionFullCube marks it a
	// 1x1x1 unit cube; CollisionDynamic with nil CustomBoxes defaults to a
	// collidable 1x1x1 cube unless CustomBoxes is explicitly set to []cube.BBox{}.
	CollisionKind world.CollisionKind
	// CustomBoxes overrides the block's collision bounding boxes. When non-empty,
	// StaticCollisionKind reports CollisionDynamic.
	CustomBoxes []cube.BBox
	// SolidFaces overrides whether block faces are considered solid for attachment.
	// When false, CollisionFullCube (or default unit-cube CollisionDynamic) still
	// reports solid faces unless TransparentFaces is set.
	SolidFaces bool
	// TransparentFaces forces FaceSolid to return false even for full cubes.
	TransparentFaces bool
	// LightEmission is the block's emitted light level (0-15).
	LightEmission uint8
	// LightFilter is the amount of light subtracted when passing through (0-15).
	LightFilter uint8
	// Friction overrides surface friction (default 0.6 when <= 0).
	Friction float64
	// Replaceable reports whether other blocks may replace this block on placement.
	Replaceable bool
	// RandomTicks enables random block ticks calling OnRandomTick.
	RandomTicks bool
	// Hardness controls survival mining duration (0 breaks instantly).
	Hardness float64
	// Unbreakable prevents the block from implementing block.Breakable.
	Unbreakable bool
	// BlastResistance overrides blast resistance (defaults to Hardness when <= 0).
	BlastResistance float64
	// Drops defines static drops when harvested (used when DropsFunc is nil).
	Drops []item.Stack
	// DropsFunc dynamically computes drops when broken.
	DropsFunc func(t item.Tool, enchantments []item.Enchantment) []item.Stack
	// Harvestable reports whether a tool can harvest drops (defaults to always true).
	Harvestable func(t item.Tool) bool
	// Effective reports whether a tool speeds up mining (defaults to false).
	Effective func(t item.Tool) bool
	// XPDrops configures XP orbs dropped when mined without Silk Touch.
	XPDrops block.XPDropRange

	// OnPlace runs when the block is placed into a world transaction.
	OnPlace func(pos cube.Pos, tx *world.Tx, previous world.Block)
	// OnBreak runs after the block is broken by a player or entity.
	OnBreak func(pos cube.Pos, tx *world.Tx, u item.User)
	// OnInteract runs when a user interacts with (right-clicks) the block.
	OnInteract func(pos cube.Pos, face cube.Face, tx *world.Tx, u item.User, ctx *item.UseContext) bool
	// OnRandomTick runs on random world ticks when RandomTicks is true.
	OnRandomTick func(pos cube.Pos, tx *world.Tx, r *rand.Rand)
	// OnEntityInside runs when an entity's bounding box intersects this block.
	OnEntityInside func(pos cube.Pos, tx *world.Tx, e world.Entity)
	// OnEntityStep runs each tick while an entity stands on top of this block.
	OnEntityStep func(pos cube.Pos, tx *world.Tx, e world.Entity)
}

type customBlockModel struct {
	kind       world.CollisionKind
	boxes      []cube.BBox
	solidFaces bool
}

func (m customBlockModel) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	if len(m.boxes) == 0 {
		return nil
	}
	return m.boxes
}

func (m customBlockModel) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool {
	return m.solidFaces
}

func (m customBlockModel) StaticCollisionKind() world.CollisionKind {
	return m.kind
}

type customBlockDef struct {
	name           string
	stateProps     map[string]any
	props          customblock.Properties
	hash           uint64
	model          customBlockModel
	lightEmission  uint8
	lightFilter    uint8
	friction       float64
	replaceable    bool
	randomTicks    bool
	breakInfo      block.BreakInfo
	onPlace        func(pos cube.Pos, tx *world.Tx, previous world.Block)
	onInteract     func(pos cube.Pos, face cube.Face, tx *world.Tx, u item.User, ctx *item.UseContext) bool
	onRandomTick   func(pos cube.Pos, tx *world.Tx, r *rand.Rand)
	onEntityInside func(pos cube.Pos, tx *world.Tx, e world.Entity)
	onEntityStep   func(pos cube.Pos, tx *world.Tx, e world.Entity)
}

func buildCustomBlockModel(spec CustomBlockSpec) customBlockModel {
	unitBox := []cube.BBox{cube.Box(0, 0, 0, 1, 1, 1)}
	if len(spec.CustomBoxes) > 0 {
		boxes := slices.Clone(spec.CustomBoxes)
		boxes = boxes[:len(boxes):len(boxes)]
		return customBlockModel{
			kind:       world.CollisionDynamic,
			boxes:      boxes,
			solidFaces: spec.SolidFaces && !spec.TransparentFaces,
		}
	}
	switch spec.CollisionKind {
	case world.CollisionEmpty:
		return customBlockModel{
			kind:       world.CollisionEmpty,
			solidFaces: spec.SolidFaces && !spec.TransparentFaces,
		}
	case world.CollisionFullCube:
		return customBlockModel{
			kind:       world.CollisionFullCube,
			boxes:      unitBox,
			solidFaces: !spec.TransparentFaces,
		}
	default:
		var boxes []cube.BBox
		solid := spec.SolidFaces
		if spec.CustomBoxes == nil {
			boxes = unitBox
			solid = !spec.TransparentFaces
		}
		return customBlockModel{
			kind:       world.CollisionDynamic,
			boxes:      boxes,
			solidFaces: solid && !spec.TransparentFaces,
		}
	}
}

func buildCustomBreakInfo(spec CustomBlockSpec) block.BreakInfo {
	blast := spec.BlastResistance
	if blast <= 0 && spec.Hardness > 0 {
		blast = spec.Hardness
	}
	harvestable := spec.Harvestable
	if harvestable == nil {
		harvestable = func(item.Tool) bool { return true }
	}
	effective := spec.Effective
	if effective == nil {
		effective = func(item.Tool) bool { return false }
	}
	dropsFn := spec.DropsFunc
	if dropsFn == nil {
		staticDrops := slices.Clone(spec.Drops)
		dropsFn = func(item.Tool, []item.Enchantment) []item.Stack {
			return slices.Clone(staticDrops)
		}
	}
	return block.BreakInfo{
		Hardness:        spec.Hardness,
		BlastResistance: blast,
		Harvestable:     harvestable,
		Effective:       effective,
		Drops:           dropsFn,
		BreakHandler:    spec.OnBreak,
		XPDrops:         spec.XPDrops,
	}
}

// NewCustomBlock constructs a hashable, comparable world.Block implementing
// world.CustomBlock, world.StaticCollisionModel, world.RandomTicker,
// block.Breakable, block.EntityInsider, block.EntityStepper, block.Activatable,
// and world.PlacementHandler according to spec.
func NewCustomBlock(spec CustomBlockSpec) world.Block {
	if spec.Name == "" {
		panic("api: CustomBlockSpec.Name must not be empty")
	}
	friction := spec.Friction
	if friction <= 0 {
		friction = 0.6
	}
	def := &customBlockDef{
		name:           spec.Name,
		stateProps:     maps.Clone(spec.StateProperties),
		props:          spec.Properties,
		hash:           nextCustomBlockHash(),
		model:          buildCustomBlockModel(spec),
		lightEmission:  min(spec.LightEmission, 15),
		lightFilter:    min(spec.LightFilter, 15),
		friction:       friction,
		replaceable:    spec.Replaceable,
		randomTicks:    spec.RandomTicks,
		breakInfo:      buildCustomBreakInfo(spec),
		onPlace:        spec.OnPlace,
		onInteract:     spec.OnInteract,
		onRandomTick:   spec.OnRandomTick,
		onEntityInside: spec.OnEntityInside,
		onEntityStep:   spec.OnEntityStep,
	}
	return wrapCustomBlock(
		customBlockBase{def: def},
		!spec.Unbreakable && spec.Hardness >= 0,
		spec.OnEntityInside != nil,
		spec.OnEntityStep != nil,
	)
}

// RegisterBlock registers b on world.DefaultBlockRegistry.
func RegisterBlock(b world.Block) {
	world.DefaultBlockRegistry.RegisterBlock(b)
}

// RegisterBlockIn registers b on reg (defaulting to world.DefaultBlockRegistry).
func RegisterBlockIn(reg world.BlockRegistry, b world.Block) {
	if reg == nil {
		reg = world.DefaultBlockRegistry
	}
	reg.RegisterBlock(b)
}

type customBlockBase struct {
	def *customBlockDef
}

func (b customBlockBase) EncodeBlock() (string, map[string]any) {
	if len(b.def.stateProps) == 0 {
		return b.def.name, nil
	}
	return b.def.name, maps.Clone(b.def.stateProps)
}

func (b customBlockBase) EncodeItem() (string, int16) { return b.def.name, 0 }
func (b customBlockBase) Hash() (uint64, uint64)      { return b.def.hash, 0 }
func (b customBlockBase) Model() world.BlockModel     { return b.def.model }
func (b customBlockBase) StaticCollisionKind() world.CollisionKind {
	return b.def.model.StaticCollisionKind()
}
func (b customBlockBase) Properties() customblock.Properties { return b.def.props }
func (b customBlockBase) LightEmissionLevel() uint8          { return b.def.lightEmission }
func (b customBlockBase) LightDiffusionLevel() uint8         { return b.def.lightFilter }
func (b customBlockBase) Friction() float64                  { return b.def.friction }
func (b customBlockBase) ReplaceableBy(world.Block) bool     { return b.def.replaceable }
func (b customBlockBase) RandomlyTicks() bool                { return b.def.randomTicks }
func (b customBlockBase) RandomTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	if b.def.onRandomTick != nil {
		b.def.onRandomTick(pos, tx, r)
	}
}
func (b customBlockBase) Placed(pos cube.Pos, tx *world.Tx, previous world.Block) {
	if b.def.onPlace != nil {
		b.def.onPlace(pos, tx, previous)
	}
}
func (b customBlockBase) Activate(pos cube.Pos, face cube.Face, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	if b.def.onInteract != nil {
		return b.def.onInteract(pos, face, tx, u, ctx)
	}
	return false
}

func (b customBlockBase) breakInfo() block.BreakInfo { return b.def.breakInfo }
func (b customBlockBase) entityInside(pos cube.Pos, tx *world.Tx, e world.Entity) {
	if b.def.onEntityInside != nil {
		b.def.onEntityInside(pos, tx, e)
	}
}
func (b customBlockBase) entityStepOn(pos cube.Pos, tx *world.Tx, e world.Entity) {
	if b.def.onEntityStep != nil {
		b.def.onEntityStep(pos, tx, e)
	}
}

type (
	cbBreak       struct{ customBlockBase }
	cbInside      struct{ customBlockBase }
	cbStep        struct{ customBlockBase }
	cbBreakInside struct{ customBlockBase }
	cbBreakStep   struct{ customBlockBase }
	cbInsideStep  struct{ customBlockBase }
	cbAllCaps     struct{ customBlockBase }
)

func (b cbBreak) BreakInfo() block.BreakInfo { return b.breakInfo() }
func (b cbInside) EntityInside(pos cube.Pos, tx *world.Tx, e world.Entity) {
	b.entityInside(pos, tx, e)
}
func (b cbStep) EntityStepOn(pos cube.Pos, tx *world.Tx, e world.Entity) { b.entityStepOn(pos, tx, e) }
func (b cbBreakInside) BreakInfo() block.BreakInfo                       { return b.breakInfo() }
func (b cbBreakInside) EntityInside(pos cube.Pos, tx *world.Tx, e world.Entity) {
	b.entityInside(pos, tx, e)
}
func (b cbBreakStep) BreakInfo() block.BreakInfo { return b.breakInfo() }
func (b cbBreakStep) EntityStepOn(pos cube.Pos, tx *world.Tx, e world.Entity) {
	b.entityStepOn(pos, tx, e)
}
func (b cbInsideStep) EntityInside(pos cube.Pos, tx *world.Tx, e world.Entity) {
	b.entityInside(pos, tx, e)
}
func (b cbInsideStep) EntityStepOn(pos cube.Pos, tx *world.Tx, e world.Entity) {
	b.entityStepOn(pos, tx, e)
}
func (b cbAllCaps) BreakInfo() block.BreakInfo { return b.breakInfo() }
func (b cbAllCaps) EntityInside(pos cube.Pos, tx *world.Tx, e world.Entity) {
	b.entityInside(pos, tx, e)
}
func (b cbAllCaps) EntityStepOn(pos cube.Pos, tx *world.Tx, e world.Entity) {
	b.entityStepOn(pos, tx, e)
}

func wrapCustomBlock(base customBlockBase, breakable, insider, stepper bool) world.Block {
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
		return base
	case 1:
		return cbBreak{base}
	case 2:
		return cbInside{base}
	case 3:
		return cbBreakInside{base}
	case 4:
		return cbStep{base}
	case 5:
		return cbBreakStep{base}
	case 6:
		return cbInsideStep{base}
	default:
		return cbAllCaps{base}
	}
}
