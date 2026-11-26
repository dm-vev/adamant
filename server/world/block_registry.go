package world

import (
	"cmp"
	"fmt"
	"maps"
	"math"
	"math/bits"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/brentp/intintmap"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/world/chunk"
	"github.com/segmentio/fasthash/fnv1"
)

// DefaultBlockRegistry is the default (vanilla) block registry used by Dragonfly when no custom registry is provided.
//
// The registry is populated during package init (block states + block implementations) and is finalized on first use
// by `server.Config.New()`, `world.Config.New()`, or `mcdb.Config.Open()`. Callers that need custom blocks should
// create a new registry using `NewBlockRegistry()` and pass it through config instead of mutating this value.
var DefaultBlockRegistry = &BasicBlockRegistry{
	blockProperties: make(map[string]map[string]any),
	stateRuntimeIDs: make(map[stateHash]uint32),
	customBlocks:    make(map[string]CustomBlock),
}

// BlockRegistry converts between runtime IDs and block states/implementations.
//
// A BlockRegistry has a build/finalize lifecycle:
//   - During setup, blocks and block states may be registered using RegisterBlockState/RegisterBlock.
//   - After calling Finalize, the registry becomes immutable and is ready for use in chunk encoding/decoding and
//     network serialization. RegisterBlock/RegisterBlockState will panic after finalization.
//
// The interface is split because the chunk package cannot import world.Block.
type BlockRegistry interface {
	chunk.BlockRegistry

	// BlockByRuntimeID looks up a Block by runtime ID. If the runtime ID is unknown/out of range, ok is false.
	BlockByRuntimeID(rid uint32) (Block, bool)
	// BlockByRuntimeIDOrAir looks up a Block by runtime ID. If not found, an air block is returned.
	BlockByRuntimeIDOrAir(rid uint32) Block
	// BlockRuntimeID looks up the runtime ID of a previously registered Block.
	BlockRuntimeID(block Block) (rid uint32)
	// RegisterBlock registers a Block implementation for a previously registered block state.
	RegisterBlock(block Block)
	// RegisterBlockState registers a block state that blocks may encode to.
	RegisterBlockState(blockState BlockState)
	// CustomBlocks returns custom blocks registered in this registry, keyed by identifier.
	CustomBlocks() map[string]CustomBlock
	// BlockByName looks up a Block by full identifier and properties.
	BlockByName(name string, properties map[string]any) (Block, bool)
	// Blocks returns all blocks registered in the registry, indexed by runtime ID.
	Blocks() []Block
	// Air returns the air block registered in the registry.
	Air() Block
	// Finalize finalizes the registry, building derived lookup tables required for runtime usage. Finalize is
	// idempotent.
	Finalize()
	// BitSize returns the number of bits used by BlockHash (depends on the number of registered blocks).
	BitSize() int
	// BlockHash returns a unique identifier of the block including the block states. The hash is internal to Dragonfly
	// and is used for fast map lookups; it does not need to match any in-game identifiers.
	BlockHash(b Block) uint64
	// BlockFlags returns the precomputed per-runtime-ID metadata flags for rid.
	BlockFlags(rid uint32) BlockFlags
	// LiquidInfo returns the liquid kind (0=none, 1=water, 2=lava), depth (0..8), and falling state for rid.
	LiquidInfo(rid uint32) (kind uint8, depth uint8, falling bool)
}

// BlockFlags is a bitmask of per-runtime-ID block properties computed during BlockRegistry.Finalize.
type BlockFlags uint32

const (
	blockFlagNBT BlockFlags = 1 << iota
	blockFlagRandomTick
	blockFlagLiquid
	blockFlagLiquidDisplacing
	blockFlagEmptyCollision
	blockFlagFullCubeCollision
	blockFlagEntityInsider
	blockFlagEntityStepper
	blockFlagRedstoneRelevant
	blockFlagPowderSnow
	blockFlagClimbable
	blockFlagSlowBlock
	blockFlagHoneyBlock
)

// EmptyCollision reports whether the block model unconditionally produces no collision boxes.
func (f BlockFlags) EmptyCollision() bool { return f&blockFlagEmptyCollision != 0 }

// FullCubeCollision reports whether the block model unconditionally spans a full 1x1x1 cube.
func (f BlockFlags) FullCubeCollision() bool { return f&blockFlagFullCubeCollision != 0 }

// HasEntityInsider reports whether the block reacts to entities inside its bounds.
func (f BlockFlags) HasEntityInsider() bool { return f&blockFlagEntityInsider != 0 }

// HasEntityStepper reports whether the block reacts to entities standing on top of it.
func (f BlockFlags) HasEntityStepper() bool { return f&blockFlagEntityStepper != 0 }

// IsLiquid reports whether the block implements Liquid.
func (f BlockFlags) IsLiquid() bool { return f&blockFlagLiquid != 0 }

// IsRedstoneRelevant reports whether the block participates in redstone graph compilation.
func (f BlockFlags) IsRedstoneRelevant() bool { return f&blockFlagRedstoneRelevant != 0 }

// IsPowderSnow reports whether the block is powder snow.
func (f BlockFlags) IsPowderSnow() bool { return f&blockFlagPowderSnow != 0 }

// IsClimbable reports whether the block is climbable.
func (f BlockFlags) IsClimbable() bool { return f&blockFlagClimbable != 0 }

// IsSlowBlock reports whether the block slows movement (soul sand or honey block).
func (f BlockFlags) IsSlowBlock() bool { return f&blockFlagSlowBlock != 0 }

// IsHoneyBlock reports whether the block is a honey block.
func (f BlockFlags) IsHoneyBlock() bool { return f&blockFlagHoneyBlock != 0 }

// IsNBT reports whether the block uses NBT data.
func (f BlockFlags) IsNBT() bool { return f&blockFlagNBT != 0 }

// RandomTicks reports whether the block state receives random ticks.
func (f BlockFlags) RandomTicks() bool { return f&blockFlagRandomTick != 0 }

// LiquidDisplacing reports whether the block implements LiquidDisplacer.
func (f BlockFlags) LiquidDisplacing() bool { return f&blockFlagLiquidDisplacing != 0 }

const blockFlagsMask uint32 = (1 << 24) - 1

// blockInfo is a packed set of per-runtime-ID properties used in hot paths (lighting, ticking, fluid checks, etc.).
//
// Layout (uint32):
// - bits 0..23: boolean flags (BlockFlags).
// - bits 24..27: 4-bit light emission level (0-15).
// - bits 28..31: 4-bit light filtering level (0-15).
//
// Values are computed during BlockRegistry.Finalize() and stored in BasicBlockRegistry.blockInfos, indexed by runtime ID.
type blockInfo uint32

func (b *blockInfo) set(flag BlockFlags) {
	*b |= blockInfo(flag)
}

func (b blockInfo) get(flag BlockFlags) bool {
	return uint32(b)&uint32(flag) != 0
}

func (b blockInfo) flags() BlockFlags {
	return BlockFlags(uint32(b) & blockFlagsMask)
}

func (b *blockInfo) setLight(light uint8) {
	// Overwrite the 4-bit light emission field.
	*b &^= blockInfo(0xF) << 24
	*b |= blockInfo(light&0xF) << 24
}

func (b *blockInfo) setLightFilter(light uint8) {
	// Overwrite the 4-bit light filtering field.
	*b &^= blockInfo(0xF) << 28
	*b |= blockInfo(light&0xF) << 28
}

func (b blockInfo) getLight() uint8 {
	return uint8((b >> 24) & 0xF)
}

func (b blockInfo) getLightFilter() uint8 {
	return uint8((b >> 28) & 0xF)
}

// BasicBlockRegistry is the default BlockRegistry implementation used by Dragonfly.
type BasicBlockRegistry struct {
	mu           sync.Mutex
	javaStateIDs sync.Map // Native layer pair -> canonical Java state ID.
	// javaNativeStates holds, by canonical Java state ID, the native state
	// ResolveState gave it, packed as the resolved* bits say; zero is
	// unresolved.
	javaNativeStates atomic.Pointer[[]atomic.Uint64]
	// dryJavaStateIDs holds, by primary runtime ID, the canonical Java state ID
	// plus one of the layer pair with an air liquid layer; zero is unresolved.
	dryJavaStateIDs atomic.Pointer[[]atomic.Uint32]
	// light caches the light properties of cells for the light engine.
	light blockLightCache
	// states holds the encoded state of every runtime ID once finalized, for
	// RuntimeIDToStateView.
	states []encodedState

	finalized bool
	bitSize   int
	hashes    *intintmap.Map

	// stateRuntimeIDs holds a map for looking up the runtime ID of a block by the stateHash it produces.
	stateRuntimeIDs map[stateHash]uint32

	blockProperties map[string]map[string]any
	// blocks holds a list of all registered Blocks indexed by their runtime ID. Blocks that were not explicitly
	// registered are of the type unknownBlock.
	blocks []Block
	// customBlocks maps a custom block's identifier to the custom block.
	customBlocks map[string]CustomBlock

	blockInfos  []blockInfo
	liquidInfos []uint8

	airRID uint32
}

func (br *BasicBlockRegistry) BitSize() int {
	if !br.finalized {
		panic("BlockRegistry.BitSize called on non finalized BlockRegistry")
	}
	return br.bitSize
}

func (br *BasicBlockRegistry) BlockCount() int {
	if !br.finalized {
		panic("BlockRegistry.BlockCount called on non finalized BlockRegistry")
	}
	return len(br.blockInfos)
}

func (br *BasicBlockRegistry) RandomTickBlock(rid uint32) bool {
	if !br.finalized {
		panic("BlockRegistry.RandomTickBlock called on non finalized BlockRegistry")
	}
	return br.blockInfos[rid].get(blockFlagRandomTick)
}

func (br *BasicBlockRegistry) FilteringBlock(rid uint32) uint8 {
	if !br.finalized {
		panic("BlockRegistry.FilteringBlock called on non finalized BlockRegistry")
	}
	return br.blockInfos[rid].getLightFilter()
}

func (br *BasicBlockRegistry) LightBlock(rid uint32) uint8 {
	if !br.finalized {
		panic("BlockRegistry.LightBlock called on non finalized BlockRegistry")
	}
	return br.blockInfos[rid].getLight()
}

func (br *BasicBlockRegistry) NBTBlock(rid uint32) bool {
	if !br.finalized {
		panic("BlockRegistry.NBTBlock called on non finalized BlockRegistry")
	}
	return br.blockInfos[rid].get(blockFlagNBT)
}

func (br *BasicBlockRegistry) LiquidDisplacingBlock(rid uint32) bool {
	if !br.finalized {
		panic("BlockRegistry.LiquidDisplacingBlock called on non finalized BlockRegistry")
	}
	return br.blockInfos[rid].get(blockFlagLiquidDisplacing)
}

func (br *BasicBlockRegistry) LiquidBlock(rid uint32) bool {
	if !br.finalized {
		panic("BlockRegistry.LiquidBlock called on non finalized BlockRegistry")
	}
	return br.blockInfos[rid].get(blockFlagLiquid)
}

// BlockFlags returns the precomputed per-runtime-ID metadata flags for rid.
func (br *BasicBlockRegistry) BlockFlags(rid uint32) BlockFlags {
	if !br.finalized {
		panic("BlockRegistry.BlockFlags called on non finalized BlockRegistry")
	}
	if rid >= uint32(len(br.blockInfos)) {
		return 0
	}
	return br.blockInfos[rid].flags()
}

// LiquidInfo returns the liquid kind (0=none, 1=water, 2=lava), depth (0..8), and falling state for rid.
func (br *BasicBlockRegistry) LiquidInfo(rid uint32) (kind uint8, depth uint8, falling bool) {
	if !br.finalized {
		panic("BlockRegistry.LiquidInfo called on non finalized BlockRegistry")
	}
	if rid >= uint32(len(br.liquidInfos)) {
		return 0, 0, false
	}
	v := br.liquidInfos[rid]
	if v == 0 {
		return 0, 0, false
	}
	return (v >> 4) & 0x07, v & 0x0F, v&0x80 != 0
}

func (br *BasicBlockRegistry) Blocks() []Block {
	if !br.finalized {
		panic("BlockRegistry.Blocks called on non finalized BlockRegistry")
	}
	return slices.Clone(br.blocks)
}

// Clone returns an independent copy of the registry. If the source registry is finalized, the clone is also finalized.
// If the source is not finalized, the clone remains mutable.
func (br *BasicBlockRegistry) Clone() *BasicBlockRegistry {
	return br.clone(false)
}

func (br *BasicBlockRegistry) clone(mutable bool) *BasicBlockRegistry {
	br.mu.Lock()
	defer br.mu.Unlock()

	br2 := &BasicBlockRegistry{
		blockProperties: make(map[string]map[string]any, len(br.blockProperties)),
		stateRuntimeIDs: make(map[stateHash]uint32, len(br.stateRuntimeIDs)),
		customBlocks:    make(map[string]CustomBlock, len(br.customBlocks)),
		finalized:       br.finalized && !mutable,
		bitSize:         br.bitSize,
		airRID:          br.airRID,
	}

	for k, v := range br.blockProperties {
		br2.blockProperties[k] = maps.Clone(v)
	}
	maps.Copy(br2.stateRuntimeIDs, br.stateRuntimeIDs)
	maps.Copy(br2.customBlocks, br.customBlocks)

	br2.blocks = make([]Block, len(br.blocks))
	copy(br2.blocks, br.blocks)

	if br2.finalized {
		br2.blockInfos = slices.Clone(br.blockInfos)
		br2.liquidInfos = slices.Clone(br.liquidInfos)
		br2.states = br.states
		br2.hashes = intintmap.New(len(br.blocks), 0.999)
		for rid, b := range br2.blocks {
			if _, hash := b.Hash(); hash == math.MaxUint64 {
				continue
			}
			br2.hashes.Put(int64(br2.BlockHash(b)), int64(rid))
		}
	}

	return br2
}

// NewBlockRegistry returns a mutable registry seeded with all vanilla block states and block implementations.
// Callers may RegisterBlockState/RegisterBlock and must call Finalize() before using the registry for world/chunk
// serialization. The returned registry is independent from DefaultBlockRegistry.
func NewBlockRegistry() BlockRegistry {
	// Composite block hashes use the default registry's bit width. Initialise it
	// before building an independent registry, even when no world exists yet.
	DefaultBlockRegistry.Finalize()
	return DefaultBlockRegistry.clone(true)
}

// RegisterBlock registers the Block passed. The EncodeBlock method will be used to encode and decode the
// block passed. RegisterBlock panics if the block properties returned were not valid, existing properties.
func (br *BasicBlockRegistry) RegisterBlock(b Block) {
	br.mu.Lock()
	defer br.mu.Unlock()

	if br.finalized {
		panic("BlockRegistry.RegisterBlock called on finalized BlockRegistry")
	}
	name, properties := b.EncodeBlock()
	if _, ok := b.(CustomBlock); ok {
		br.registerBlockStateLocked(BlockState{Name: name, Properties: properties})
	}
	rid, ok := br.stateRuntimeIDs[stateHash{name: name, properties: hashProperties(properties)}]
	if !ok {
		// We assume all blocks must have all their states registered beforehand. Vanilla blocks will have
		// this done through registering of all states present in the block_states.nbt file.
		panic(fmt.Sprintf("block state returned is not registered (%v {%#v})", name, properties))
	}
	if _, ok := br.blocks[rid].(unknownBlock); !ok {
		panic(fmt.Sprintf("block with name and properties %v {%#v} already registered", name, properties))
	}
	br.blocks[rid] = b
	if c, ok := b.(CustomBlock); ok {
		if _, ok := br.customBlocks[name]; !ok {
			br.customBlocks[name] = c
		}
	}
}

// RegisterBlockState registers a BlockState to the registry. The function panics if the properties the
// BlockState holds are invalid or if the BlockState was already registered.
func (br *BasicBlockRegistry) RegisterBlockState(s BlockState) {
	br.mu.Lock()
	defer br.mu.Unlock()
	br.registerBlockStateLocked(s)
}

// registerBlockStateLocked is the implementation of RegisterBlockState. br.mu must be held by the caller.
func (br *BasicBlockRegistry) registerBlockStateLocked(s BlockState) {
	if br.finalized {
		panic("BlockRegistry.RegisterBlockState called on finalized BlockRegistry")
	}
	h := stateHash{name: s.Name, properties: hashProperties(s.Properties)}
	if _, ok := br.stateRuntimeIDs[h]; ok {
		panic(fmt.Sprintf("cannot register the same state twice (%+v)", s))
	}
	if _, ok := br.blockProperties[s.Name]; !ok {
		br.blockProperties[s.Name] = s.Properties
	}
	rid := uint32(len(br.blocks))
	br.blocks = append(br.blocks, unknownBlock{BlockState: s})
	br.stateRuntimeIDs[h] = rid
}

func (br *BasicBlockRegistry) Finalize() {
	br.mu.Lock()
	defer br.mu.Unlock()

	if br.finalized {
		// Finalize is intentionally idempotent so code can defensively call it when a registry is provided through
		// config. RegisterBlock/RegisterBlockState still panic after finalization.
		return
	}

	br.bitSize = bits.Len64(uint64(len(br.blocks)))
	br.sortBlocksLocked()

	br.blockInfos = make([]blockInfo, len(br.blocks))
	br.liquidInfos = make([]uint8, len(br.blocks))
	br.states = make([]encodedState, len(br.blocks))
	br.hashes = intintmap.New(len(br.blocks), 0.999)
	br.stateRuntimeIDs = make(map[stateHash]uint32, len(br.blocks))
	emptySrc := emptyBlockSource{air: unknownBlock{BlockState: BlockState{Name: "minecraft:air"}}}
	foundAir := false

	for idx, b := range br.blocks {
		rid := uint32(idx)
		name, properties := b.EncodeBlock()
		br.states[rid] = encodedState{name: name, properties: properties}
		h := stateHash{name: name, properties: hashProperties(properties)}
		if name == "minecraft:air" {
			br.airRID = rid
			foundAir = true
		}
		if _, ok := br.stateRuntimeIDs[h]; ok {
			panic(fmt.Sprintf("cannot register the same state twice (%s %+v)", name, properties))
		}
		br.stateRuntimeIDs[h] = rid

		info, liquidInfo := computeBlockMeta(b, name, emptySrc)
		br.blockInfos[rid] = info
		br.liquidInfos[rid] = liquidInfo

		if _, hash := b.Hash(); hash != math.MaxUint64 {
			h := int64(br.BlockHash(b))
			if other, ok := br.hashes.Get(h); ok {
				panic(fmt.Sprintf("block %#v with hash %v already registered by %#v", b, h, br.blocks[other]))
			}
			br.hashes.Put(h, int64(rid))
		}
	}
	if !foundAir {
		panic("BlockRegistry.Finalize: no minecraft:air block state registered")
	}
	br.finalized = true
}

func (br *BasicBlockRegistry) sortBlocksLocked() {
	type sortEntry struct {
		b    Block
		hash uint64
	}
	entries := make([]sortEntry, len(br.blocks))
	for i, b := range br.blocks {
		var name string
		if ub, ok := b.(unknownBlock); ok {
			name = ub.Name
		} else {
			name, _ = b.EncodeBlock()
		}
		entries[i] = sortEntry{b: b, hash: fnv1.HashString64(name)}
	}
	slices.SortStableFunc(entries, func(a, b sortEntry) int {
		return cmp.Compare(a.hash, b.hash)
	})
	for i, e := range entries {
		br.blocks[i] = e.b
	}
}

type emptyBlockSource struct{ air Block }

func (s emptyBlockSource) Block(cube.Pos) Block { return s.air }

var fullCubeBox = cube.Box(0, 0, 0, 1, 1, 1)

func computeBlockMeta(b Block, name string, emptySrc BlockSource) (blockInfo, uint8) {
	var info blockInfo
	// Default to fully opaque. Blocks that implement lightDiffuser may override this (e.g., air -> 0, leaves -> 1-14).
	info.setLightFilter(15)
	if diffuser, ok := b.(lightDiffuser); ok {
		info.setLightFilter(diffuser.LightDiffusionLevel())
	}
	if emitter, ok := b.(lightEmitter); ok {
		info.setLight(emitter.LightEmissionLevel())
	}
	switch blockStaticCollisionKind(b, emptySrc) {
	case CollisionEmpty:
		info.set(blockFlagEmptyCollision)
	case CollisionFullCube:
		info.set(blockFlagFullCubeCollision)
	}
	if _, ok := b.(NBTer); ok {
		info.set(blockFlagNBT)
	}
	if _, ok := b.(RandomTicker); ok {
		if r, ok := b.(interface{ RandomlyTicks() bool }); !ok || r.RandomlyTicks() {
			info.set(blockFlagRandomTick)
		}
	}
	if _, ok := b.(LiquidDisplacer); ok {
		info.set(blockFlagLiquidDisplacing)
	}
	if _, ok := b.(interface{ EntityInside(cube.Pos, *Tx, Entity) }); ok {
		info.set(blockFlagEntityInsider)
	}
	if _, ok := b.(interface{ EntityStepOn(cube.Pos, *Tx, Entity) }); ok {
		info.set(blockFlagEntityStepper)
	}
	if isRedstoneRelevant(b) {
		info.set(blockFlagRedstoneRelevant)
	}
	var liquidInfo uint8
	if liq, ok := b.(Liquid); ok {
		info.set(blockFlagLiquid)
		liquidInfo = encodeLiquidInfo(liq)
	}
	setNamedBlockFlags(&info, name)
	return info, liquidInfo
}

func blockStaticCollisionKind(b Block, emptySrc BlockSource) (kind CollisionKind) {
	// Zero-value registered NBT block prototypes (e.g. ShulkerBox with nil progress) can panic in Model().
	// Such stateful blocks never have static collision anyway.
	defer func() {
		if recover() != nil {
			kind = CollisionDynamic
		}
	}()
	m := b.Model()
	sc, ok := m.(StaticCollisionModel)
	if !ok {
		return CollisionDynamic
	}
	kind = sc.StaticCollisionKind()
	switch kind {
	case CollisionEmpty:
		if len(m.BBox(cube.Pos{}, emptySrc)) != 0 {
			return CollisionDynamic
		}
	case CollisionFullCube:
		if boxes := m.BBox(cube.Pos{}, emptySrc); len(boxes) != 1 || boxes[0] != fullCubeBox {
			return CollisionDynamic
		}
	}
	return kind
}

func encodeLiquidInfo(liq Liquid) uint8 {
	var kind uint8
	switch liq.LiquidType() {
	case "water":
		kind = 1
	case "lava":
		kind = 2
	default:
		return 0
	}
	d := max(0, min(8, liq.LiquidDepth()))
	v := (kind << 4) | uint8(d)
	if liq.LiquidFalling() {
		v |= 0x80
	}
	return v
}

func setNamedBlockFlags(info *blockInfo, name string) {
	switch name {
	case "minecraft:powder_snow":
		info.set(blockFlagPowderSnow)
	case "minecraft:honey_block":
		info.set(blockFlagHoneyBlock)
		info.set(blockFlagSlowBlock)
	case "minecraft:soul_sand":
		info.set(blockFlagSlowBlock)
	case "minecraft:ladder",
		"minecraft:vine",
		"minecraft:scaffolding",
		"minecraft:weeping_vines",
		"minecraft:weeping_vines_plant",
		"minecraft:twisting_vines",
		"minecraft:twisting_vines_plant",
		"minecraft:cave_vines",
		"minecraft:cave_vines_plant",
		"minecraft:cave_vines_head_with_berries",
		"minecraft:cave_vines_body_with_berries":
		info.set(blockFlagClimbable)
	}
}

// AirRuntimeID returns the runtime ID of the air block.
func (br *BasicBlockRegistry) AirRuntimeID() uint32 {
	if !br.finalized {
		panic("BlockRegistry.AirRuntimeID called on non finalized BlockRegistry")
	}
	return br.airRID
}

// RuntimeIDToState returns the name and state properties of a block by its runtime ID.
func (br *BasicBlockRegistry) RuntimeIDToState(runtimeID uint32) (name string, properties map[string]any, found bool) {
	if !br.finalized {
		panic("BlockRegistry.RuntimeIDToState called on non finalized BlockRegistry")
	}
	if runtimeID >= uint32(len(br.blocks)) {
		return "", nil, false
	}
	name, properties = br.blocks[runtimeID].EncodeBlock()
	return name, properties, true
}

// encodedState is the name and properties a block encodes to.
type encodedState struct {
	name       string
	properties map[string]any
}

// RuntimeIDToStateView returns the same state as RuntimeIDToState, but the
// properties are shared by every caller and must not be modified.
func (br *BasicBlockRegistry) RuntimeIDToStateView(runtimeID uint32) (name string, properties map[string]any, found bool) {
	if !br.finalized {
		panic("BlockRegistry.RuntimeIDToStateView called on non finalized BlockRegistry")
	}
	if runtimeID >= uint32(len(br.states)) {
		return "", nil, false
	}
	state := br.states[runtimeID]
	return state.name, state.properties, true
}

// StateToRuntimeID returns the runtime ID of a block by its name and state properties.
func (br *BasicBlockRegistry) StateToRuntimeID(name string, properties map[string]any) (runtimeID uint32, found bool) {
	if !br.finalized {
		panic("BlockRegistry.StateToRuntimeID called on non finalized BlockRegistry")
	}
	if rid, ok := br.stateRuntimeIDs[stateHash{name: name, properties: hashProperties(properties)}]; ok {
		return rid, true
	}
	defaults, ok := br.blockProperties[name]
	if !ok {
		return 0, false
	}
	if len(properties) == 0 {
		rid, ok := br.stateRuntimeIDs[stateHash{name: name, properties: hashProperties(defaults)}]
		return rid, ok
	}
	merged := maps.Clone(defaults)
	for key, value := range properties {
		if _, known := defaults[key]; !known {
			return 0, false
		}
		merged[key] = value
	}
	rid, ok := br.stateRuntimeIDs[stateHash{name: name, properties: hashProperties(merged)}]
	return rid, ok
}

// BlockHash returns a unique identifier of the block including the block states. This function is used internally
// to convert a block to a single integer which can be used in map lookups. The hash produced therefore does not
// need to match anything in the game, but it must be unique among all registered blocks.
// The tool in `/cmd/blockhash` may be used to automatically generate block hashes of blocks in a package.
func (br *BasicBlockRegistry) BlockHash(b Block) uint64 {
	base, hash := b.Hash()
	return base | (hash << uint64(br.bitSize))
}

// BlockRuntimeID attempts to return a runtime ID of a block previously registered using RegisterBlock().
// If the runtime ID cannot be found because the Block wasn't registered, BlockRuntimeID will panic.
func (br *BasicBlockRegistry) BlockRuntimeID(b Block) uint32 {
	if !br.finalized {
		panic("BlockRegistry.BlockRuntimeID called on non finalized BlockRegistry")
	}
	if b == nil {
		return br.airRID
	}
	if _, hash := b.Hash(); hash != math.MaxUint64 {
		// b is not an unknownBlock.
		if rid, ok := br.hashes.Get(int64(br.BlockHash(b))); ok {
			return uint32(rid)
		}
		panic(fmt.Sprintf("cannot find block by non-0 hash of block %#v", b))
	}
	return br.slowBlockRuntimeID(b)
}

func (br *BasicBlockRegistry) BlockByRuntimeIDOrAir(rid uint32) Block {
	bl, _ := br.BlockByRuntimeID(rid)
	return bl
}

// slowBlockRuntimeID finds the runtime ID of a Block by hashing the properties produced by calling the
// Block.EncodeBlock method and looking it up in the stateRuntimeIDs map.
func (br *BasicBlockRegistry) slowBlockRuntimeID(b Block) uint32 {
	name, properties := b.EncodeBlock()
	rid, ok := br.stateRuntimeIDs[stateHash{name: name, properties: hashProperties(properties)}]
	if !ok {
		panic(fmt.Sprintf("cannot find block by (name + properties): %#v", b))
	}
	return rid
}

// BlockByRuntimeID attempts to return a Block by its runtime ID. If not found, the bool returned is
// false. If found, the block is non-nil and the bool true.
func (br *BasicBlockRegistry) BlockByRuntimeID(rid uint32) (Block, bool) {
	if !br.finalized {
		panic("BlockRegistry.BlockByRuntimeID called on non finalized BlockRegistry")
	}
	if rid >= uint32(len(br.blocks)) {
		return br.Air(), false
	}
	return br.blocks[rid], true
}

// BlockByName attempts to return a Block by its name and properties. If not found, the bool returned is
// false.
func (br *BasicBlockRegistry) BlockByName(name string, properties map[string]any) (Block, bool) {
	if !br.finalized {
		panic("BlockRegistry.BlockByName called on non finalized BlockRegistry")
	}
	rid, ok := br.stateRuntimeIDs[stateHash{name: name, properties: hashProperties(properties)}]
	if !ok {
		return nil, false
	}
	return br.blocks[rid], true
}

// CustomBlocks returns a map of all custom blocks registered with their names as keys.
func (br *BasicBlockRegistry) CustomBlocks() map[string]CustomBlock {
	return maps.Clone(br.customBlocks)
}

// Air returns an air block.
func (br *BasicBlockRegistry) Air() Block {
	if !br.finalized {
		panic("BlockRegistry.Air called on non finalized BlockRegistry")
	}
	if br.airRID >= uint32(len(br.blocks)) {
		// This should never happen for a valid registry (Finalize enforces air exists).
		panic("BlockRegistry.Air: air runtime ID out of range")
	}
	return br.blocks[br.airRID]
}
