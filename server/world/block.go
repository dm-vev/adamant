package world

import (
	"image"
	"maps"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/customblock"
)

// Block is a block that may be placed or found in a world. In addition, the block may also be added to an
// inventory: It is also an item.
// Every Block implementation must be able to be hashed as key in a map.
type Block interface {
	// EncodeBlock encodes the block to a string ID such as 'minecraft:grass' and properties associated
	// with the block.
	EncodeBlock() (string, map[string]any)
	// Hash returns two different identifiers for the block. The first is the base hash which is unique for
	// each type of block at runtime. For vanilla blocks, this is an auto-incrementing constant and for custom
	// blocks, you can call block.NextHash() to get a unique identifier. The second is the hash of the block's
	// own state and does not need to worry about colliding with other types of blocks. This is later combined
	// with the base hash to create a unique identifier for the full block.
	Hash() (uint64, uint64)
	// Model returns the BlockModel of the Block.
	Model() BlockModel
}

// MissingStateReconstructor reconstructs derived block properties that were absent from a decoded disk state.
// It must preserve all properties not listed in missing and must not cause chunk loads.
type MissingStateReconstructor interface {
	ReconstructMissingState(pos cube.Pos, missing []string, blocks BlockSource) Block
	// ReconstructionOrder orders derived-state dependencies. A block may rely
	// on reconstructed properties of neighbours with a strictly smaller order.
	// Same-order neighbours may only be used for their non-derived properties.
	ReconstructionOrder() int
}

// CustomBlock represents a block that is non-vanilla and requires a resource pack and extra steps to show it to the
// client.
type CustomBlock interface {
	Block
	Properties() customblock.Properties
}

type CustomBlockBuildable interface {
	CustomBlock
	// Name is the name displayed to clients using the block.
	Name() string
	// Geometry is the geometries for the block that define the shape of the block. If false is returned, no custom
	// geometry will be applied. Permutation-specific geometry can be defined by returning a map of permutations to
	// geometry.
	Geometry() []byte
	// Textures is a map of images indexed by their target, used to map textures on to the block. Permutation-specific
	// textures can be defined by returning a map of permutations to textures.
	Textures() map[string]image.Image
}

// Liquid represents a block that can be moved through and which can flow in the world after placement. There
// are two liquids in vanilla, which are lava and water.
type Liquid interface {
	Block
	// LiquidDepth returns the current depth of the liquid.
	LiquidDepth() int
	// SpreadDecay returns the amount of depth that is subtracted from the liquid's depth when it spreads to
	// a next block.
	SpreadDecay() int
	// WithDepth returns the liquid with the depth passed.
	WithDepth(depth int, falling bool) Liquid
	// LiquidFalling checks if the liquid is currently considered falling down.
	LiquidFalling() bool
	// BlastResistance is the blast resistance of the liquid, which influences the liquid's ability to withstand an
	// explosive blast.
	BlastResistance() float64
	// LiquidType returns an int unique for the liquid, used to check if two liquids are considered to be
	// of the same type.
	LiquidType() string
	// Harden checks if the block should harden when looking at the surrounding blocks and sets the position
	// to the hardened block when adequate. If the block was hardened, the method returns true.
	Harden(pos cube.Pos, tx *Tx, flownIntoBy *cube.Pos) bool
	// LiquidRemoveBlock is called when the liquid flows into and removes the block passed.
	LiquidRemoveBlock(pos cube.Pos, tx *Tx, removed Block)
}

// RegisterBlock registers the Block passed in the DefaultBlockRegistry.
//
// This function exists for backwards compatibility and works well for the common "single server per process" setup,
// where all worlds share the global default registry.
//
// If you run multiple servers/registries in a single process, prefer creating a registry using NewBlockRegistry() and
// registering blocks on that instance (e.g. conf.Blocks.RegisterBlock(...)) before calling Finalize().
func RegisterBlock(b Block) {
	DefaultBlockRegistry.RegisterBlock(b)
}

// BlockHash returns a unique identifier of the block including the block states using the DefaultBlockRegistry.
// This function is used internally to convert a block to a single integer which can be used in map lookups. The hash
// produced therefore does not need to match anything in the game, but it must be unique among all registered blocks.
// The tool in `/cmd/blockhash` may be used to automatically generate block hashes of blocks in a package.
//
// If you use a non-default registry (NewBlockRegistry), use your registry instance's BlockHash(...) instead so the
// hash is consistent with that registry.
func BlockHash(b Block) uint64 {
	return DefaultBlockRegistry.BlockHash(b)
}

// BlockRuntimeID attempts to return a runtime ID of a block previously registered using RegisterBlock() on the
// DefaultBlockRegistry.
// If the runtime ID cannot be found because the Block wasn't registered, BlockRuntimeID will panic.
// If you use a non-default registry (NewBlockRegistry), use your registry instance's BlockRuntimeID(...) instead.
func BlockRuntimeID(b Block) uint32 {
	return DefaultBlockRegistry.BlockRuntimeID(b)
}

// BlockByRuntimeID attempts to return a Block by its runtime ID using the DefaultBlockRegistry. If not found, the bool
// returned is false. If found, the block is non-nil and the bool true.
// If you use a non-default registry (NewBlockRegistry), use your registry instance's BlockByRuntimeID(...) instead.
func BlockByRuntimeID(rid uint32) (Block, bool) {
	return DefaultBlockRegistry.BlockByRuntimeID(rid)
}

// BlockByName attempts to return a Block by its name and properties using the DefaultBlockRegistry. If not found, the
// bool returned is false.
// If you use a non-default registry (NewBlockRegistry), use your registry instance's BlockByName(...) instead.
func BlockByName(name string, properties map[string]any) (Block, bool) {
	return DefaultBlockRegistry.BlockByName(name, properties)
}

// Blocks returns a slice of all blocks registered in the DefaultBlockRegistry.
// If you use a non-default registry (NewBlockRegistry), use your registry instance's Blocks() instead.
func Blocks() []Block {
	return DefaultBlockRegistry.Blocks()
}

// CustomBlocks returns a map of all custom blocks registered with their names as keys in the DefaultBlockRegistry.
// If you use a non-default registry (NewBlockRegistry), use your registry instance's CustomBlocks() instead.
func CustomBlocks() map[string]CustomBlock {
	return DefaultBlockRegistry.CustomBlocks()
}

// RandomTicker represents a block that executes an action when it is ticked randomly. Every 20th of a second,
// one random block in each sub chunk are picked to receive a random tick.
type RandomTicker interface {
	// RandomTick handles a random tick of the block at the position passed. Additionally, a rand.RandSource
	// instance is passed which may be used to generate values randomly without locking.
	RandomTick(pos cube.Pos, tx *Tx, r *rand.Rand)
}

// ScheduledTicker represents a block that executes an action when it has a block update scheduled, such as
// when a block adjacent to it is broken.
type ScheduledTicker interface {
	// ScheduledTick handles a scheduled tick initiated by an event in one of the neighbouring blocks, such as
	// when a block is placed or broken. Additionally, a rand.RandSource instance is passed which may be used to
	// generate values randomly without locking.
	ScheduledTick(pos cube.Pos, tx *Tx, r *rand.Rand)
}

// TickerBlock is an implementation of NBTer with an additional Tick method that is called on every world
// tick for loaded blocks that implement this interface.
type TickerBlock interface {
	NBTer
	Tick(currentTick int64, pos cube.Pos, tx *Tx)
}

// NeighbourUpdateTicker represents a block that is updated when a block adjacent to it is updated, either
// through placement or being broken.
type NeighbourUpdateTicker interface {
	// NeighbourUpdateTick handles a neighbouring block being updated. The position of that block and the
	// position of this block is passed.
	NeighbourUpdateTick(pos, changedNeighbour cube.Pos, tx *Tx)
}

// NeighbourChangeListener receives a loaded neighbor's notification during the
// notifying transaction. Components that must observe the state at that instant
// (such as Bedrock observers) implement this instead of waiting for the deferred
// neighbor queue. NeighbourUpdateTick is not queued for the same notification.
type NeighbourChangeListener interface {
	NeighbourChanged(pos, changedNeighbour cube.Pos, tx *Tx)
}

// PlacementHandler receives a foreground block replacement after its state
// and viewer updates have been applied. Implementations may replace blocks
// again, for example when completing a constructed mob.
type PlacementHandler interface {
	Placed(pos cube.Pos, tx *Tx, previous Block)
}

// BlockRemovalHandler receives a foreground replacement while the old block
// actor is still available. State-only changes also call Removed; the actor
// decides whether the replacement preserves its lifecycle.
type BlockRemovalHandler interface {
	Removed(pos cube.Pos, tx *Tx, replacement Block)
}

// PlayerBlockDestroyer runs after a player break is accepted but before the
// actor is removed. The damage source preserves responsibility for linked mobs.
type PlayerBlockDestroyer interface {
	PlayerWillDestroy(pos cube.Pos, tx *Tx, source DamageSource)
}

// NBTer represents either an item or a block which may decode NBT data and encode to NBT data. Typically,
// this is done to store additional data.
type NBTer interface {
	// DecodeNBT returns the (new) item, block or Entity, depending on which of those the NBTer was, with the NBT data
	// decoded into it.
	DecodeNBT(data map[string]any) any
	// EncodeNBT encodes the Entity into a map which can then be encoded as NBT to be written.
	EncodeNBT() map[string]any
}

// ServerOnlyNBTer is implemented by blocks that keep server state in NBT
// although Bedrock has no block actor for them, such as a target block's
// output signal. Their NBT is saved but never sent to clients.
type ServerOnlyNBTer interface {
	NBTer
	ServerOnlyNBT()
}

// UpdateNBTer is implemented by block actors that BDS updates on clients with
// a compound other than the one it saves, such as the item a vault displays.
// UpdateNBT returns that compound, which is sent as it is when the block is
// set, or nil to send none. Chunks still carry the compound returned by
// EncodeNBT.
type UpdateNBTer interface {
	NBTer
	UpdateNBT() map[string]any
}

// BlockActorNBT returns the block actor compound of a block at pos with the
// NBT data passed, as Bedrock writes it: the data with the block's position,
// and a BlockEntityVersion of 0 unless the data sets one. data is not
// modified.
func BlockActorNBT(data map[string]any, pos cube.Pos) map[string]any {
	out := make(map[string]any, len(data)+4)
	maps.Copy(out, data)
	if _, ok := out["BlockEntityVersion"]; !ok {
		out["BlockEntityVersion"] = int32(0)
	}
	out["x"], out["y"], out["z"] = int32(pos[0]), int32(pos[1]), int32(pos[2])
	return out
}

// LiquidDisplacer represents a block that is able to displace a liquid to a different world layer, without
// fully removing the liquid.
type LiquidDisplacer interface {
	// CanDisplace specifies if the block is able to displace the liquid passed.
	CanDisplace(b Liquid) bool
	// SideClosed checks if a position on the side of the block placed in the world at a specific position is
	// closed. When this returns true (for example, when the side is below the position and the block is a
	// slab), liquid inside the displacer won't flow from pos into side.
	SideClosed(pos, side cube.Pos, tx *Tx) bool
}

// lightEmitter is identical to a block.LightEmitter.
type lightEmitter interface {
	LightEmissionLevel() uint8
}

// lightDiffuser is identical to a block.LightDiffuser.
type lightDiffuser interface {
	LightDiffusionLevel() uint8
}

// replaceableBlock represents a block that may be replaced by another block automatically. An example is
// grass, which may be replaced by clicking it with another block.
type replaceableBlock interface {
	// ReplaceableBy returns a bool which indicates if the block is replaceable by another block.
	ReplaceableBy(b Block) bool
}

// replaceable checks if the block at the position passed is replaceable with the block passed.
func replaceable(w *World, c *Column, pos cube.Pos, with Block) bool {
	if r, ok := w.blockInChunk(c, pos).(replaceableBlock); ok {
		return r.ReplaceableBy(with)
	}
	return false
}

// BlockAction represents an action that may be performed by a block. Typically, these actions are sent to
// viewers in a world so that they can see these actions.
type BlockAction interface {
	BlockAction()
}
