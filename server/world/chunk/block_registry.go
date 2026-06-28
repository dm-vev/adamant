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
