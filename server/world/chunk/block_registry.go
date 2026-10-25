package chunk

// BlockRegistry provides the minimal block registry API required by the chunk package.
//
// Implementations must be safe for concurrent read access after construction/finalization, as chunks and chunk
// encoding/decoding may happen from multiple goroutines.
type BlockRegistry interface {
	// BlockCount returns the number of runtime IDs known by the registry.
	BlockCount() int
	// AirRuntimeID returns the runtime ID representing air.
	AirRuntimeID() uint32
	// RuntimeIDToState resolves a runtime ID to its (name, properties) state representation.
	RuntimeIDToState(runtimeID uint32) (name string, properties map[string]any, found bool)
	// StateToRuntimeID resolves a (name, properties) state representation to a runtime ID.
	StateToRuntimeID(name string, properties map[string]any) (runtimeID uint32, found bool)
	// FilteringBlock returns the light filtering value for the runtime ID.
	FilteringBlock(rid uint32) uint8
	// LightBlock returns the light emission value for the runtime ID.
	LightBlock(rid uint32) uint8
	// RandomTickBlock reports whether the runtime ID receives random ticks.
	RandomTickBlock(rid uint32) bool
	// NBTBlock reports whether the runtime ID uses NBT data and requires NBT-aware encoding/decoding.
	NBTBlock(rid uint32) bool
	// LiquidDisplacingBlock reports whether the runtime ID displaces liquids.
	LiquidDisplacingBlock(rid uint32) bool
	// LiquidBlock reports whether the runtime ID represents a liquid block.
	LiquidBlock(rid uint32) bool
	// HashToRuntimeID resolves a "network block hash" to a runtime ID.
	HashToRuntimeID(hash uint32) (rid uint32, ok bool)
	// RuntimeIDToHash resolves a runtime ID to its "network block hash".
	RuntimeIDToHash(runtimeID uint32) (hash uint32, ok bool)
}

// DefaultBlockRegistry is the registry used by the compatibility state lookup helpers below. The world package sets
// it to world.DefaultBlockRegistry during initialisation. New code that works with a specific chunk or world should
// use that instance's registry instead.
var DefaultBlockRegistry BlockRegistry

// RuntimeIDToState resolves a runtime ID using DefaultBlockRegistry.
func RuntimeIDToState(runtimeID uint32) (name string, properties map[string]any, found bool) {
	if DefaultBlockRegistry == nil {
		return "", nil, false
	}
	return DefaultBlockRegistry.RuntimeIDToState(runtimeID)
}

// StateToRuntimeID resolves a block state using DefaultBlockRegistry.
func StateToRuntimeID(name string, properties map[string]any) (runtimeID uint32, found bool) {
	if DefaultBlockRegistry == nil {
		return 0, false
	}
	return DefaultBlockRegistry.StateToRuntimeID(name, properties)
}

// LightProperties are what Java's light engine reads of the blocks in a cell.
type LightProperties struct {
	// Emission is the light the blocks emit, BlockState.getLightEmission.
	Emission uint8
	// Dampening is BlockState.getLightDampening: light entering the cell
	// loses max(1, Dampening) levels, and skylight only falls through cells
	// that do not dampen it.
	Dampening uint8
	// Occlusion holds a bit, by cube.Face, for every face whose occlusion
	// shape is full. It is zero for shapes LightEngine.isEmptyShape leaves
	// out of occlusion.
	Occlusion uint8
	// Partial holds a bit, by cube.Face, for every face its occlusion shape
	// only partly covers. Two partial faces occlude light where together
	// they cover the face, which State, the Java state, tells.
	Partial uint8
	State   uint32
}

// LightRegistry is implemented by block registries that know the light
// properties of a cell from its Java block state. state is the Java state
// stored for the cell, or zero to derive it from the runtime IDs of its
// foreground and background blocks.
type LightRegistry interface {
	LightProperties(state, fg, bg uint32) LightProperties
	// DryLightProperties returns the light properties of every runtime ID
	// with no background block, indexed by runtime ID.
	DryLightProperties() []LightProperties
}
